package ai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type trialStub func(*http.Request) (*http.Response, error)

func (stub trialStub) RoundTrip(req *http.Request) (*http.Response, error) { return stub(req) }

type trialAlertStub func([]byte) (int, error)

func (stub trialAlertStub) Write(p []byte) (int, error) { return stub(p) }

func TestTrialResultLedgerAndFailClosed(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("目录持久同步按部署目标在 Linux 验证")
	}
	for _, scenario := range []struct {
		name, outcome, event string
		calls                int
		estimate, complete   bool
	}{
		{"known usage", "success", "quota_spent", 1, true, true},
		{"unknown usage", "halted", "reservation_retained", 1, false, false},
		{"result alert unavailable", "success", "alert_unavailable", 1, true, false},
		{"stop while in flight", "halted", "reservation_retained", 1, true, false},
		{"reservation alert unavailable", "", "not_approved", 0, false, false},
		{"expired window", "", "not_approved", 0, false, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := t.TempDir()
			// Docker Desktop 的共享卷时间戳精度可能低于纳秒。
			now := time.Now().In(time.FixedZone("CST", 8*3600)).Truncate(time.Second)
			start := now.Add(-time.Minute)
			window := trialWindow{Start: start, End: start.Add(2 * time.Hour), Users: []uint64{1, 2}}
			put := func(name string, value any) {
				t.Helper()
				if err := writeTrialRecord(root, name, value); err != nil {
					t.Fatal(err)
				}
			}
			put("window.json", window)
			put("isolated-approval.json", trialApproval{BootID: "local", WindowStart: start, MonitorAt: now, Start: now, End: window.End, Users: window.Users})
			put("monitor", struct{}{})
			if err := os.Chtimes(filepath.Join(root, "monitor"), now, now); err != nil {
				t.Fatal(err)
			}
			var events bytes.Buffer
			alert := trialAlertStub(func(p []byte) (int, error) {
				if (scenario.name == "result alert unavailable" && bytes.Contains(p, []byte("result_persisted"))) ||
					(scenario.name == "reservation alert unavailable" && bytes.Contains(p, []byte("reserving"))) {
					return 0, io.ErrClosedPipe
				}
				return events.Write(p)
			})
			ledger := &trialLedger{root: root, stage: isolatedTrial, bootID: "local", window: window, alert: alert, now: func() time.Time { return now }}
			ctx := context.Background()
			req := Request{UserID: 1, Mode: ModeQuick3, Topic: Topic{Title: "合成辩题", Description: "合成描述"}, UserStance: StancePro, RoundNo: 1, UserContent: "合成发言"}
			var generator trialGenerator
			var calls int
			transport := trialTransport{reserve: ledger.reserve, base: trialStub(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls != 1 {
					return nil, errors.New("unexpected extra provider request")
				}
				if scenario.name == "stop while in flight" {
					put("stop", struct{}{})
					if _, err := generator.Generate(ctx, req); err == nil {
						t.Error("designated user admitted again after stop while first request was in flight")
					}
				}
				usage := `,"usage":{"prompt_tokens":31,"completion_tokens":8,"total_tokens":39}`
				if scenario.name == "unknown usage" {
					usage = ""
				}
				body := `{"id":"stub","model":"deepseek-flash","choices":[{"index":0,"message":{"role":"assistant","content":"合成回复"},"finish_reason":"stop"}]` + usage + "}"
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			cfg := testConfig(transport)
			cfg.MaxOutputTokens, cfg.Window.MaxOutputTokens = trialOutputTokens, trialOutputTokens
			adapter, err := NewOpenAIGenerator(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			generator = trialGenerator{adapter: adapter, ledger: ledger}
			if scenario.name == "expired window" {
				now = window.End
			}
			if _, err := generator.Generate(ctx, req); (err == nil) != scenario.complete {
				t.Fatalf("unexpected result error: %v", err)
			}
			if _, err := generator.Generate(ctx, req); err == nil || calls != scenario.calls {
				t.Fatalf("repeat result error=%v, provider calls=%d", err, calls)
			}
			reserved, reserveErr := readTrialRecord[trialReservation](filepath.Join(root, "isolated-reserved.json"))
			record, resultErr := readTrialRecord[trialResult](filepath.Join(root, "isolated-result.json"))
			_, completionErr := readTrialCompletion(root, isolatedTrial)
			if scenario.calls == 0 {
				if !errors.Is(reserveErr, os.ErrNotExist) || !errors.Is(resultErr, os.ErrNotExist) {
					t.Fatal("denied request left a reservation or result")
				}
			} else {
				if reserveErr != nil || reserved.Calls != 1 || reserved.MaxCostMicroCNY != 40960 {
					t.Fatalf("unexpected reservation: %+v, %v", reserved, reserveErr)
				}
				if resultErr != nil || record.Outcome != scenario.outcome || record.Settlement != nil || record.Balance != nil {
					t.Fatalf("unexpected result: %+v, %v", record, resultErr)
				}
				if (record.EstimateMicroCNY != nil) != scenario.estimate ||
					(scenario.estimate && (*record.EstimateMicroCNY != 126 || !saneTrialUsage(record.Usage))) {
					t.Fatalf("unexpected usage or estimate: %+v", record)
				}
			}
			if (completionErr == nil) != scenario.complete || !strings.Contains(events.String(), scenario.event) {
				t.Fatalf("completion error=%v, events=%s", completionErr, events.String())
			}
			t.Logf("fixture alerts:\n%s", events.String())
			if !scenario.complete && scenario.name != "expired window" {
				if _, err := os.Stat(filepath.Join(root, "stop")); err != nil {
					t.Fatalf("stop marker: %v", err)
				}
			}
		})
	}
}

func TestTrialTransportFailsClosed(t *testing.T) {
	const valid = `{"model":"deepseek-flash","max_tokens":1024,"thinking":{"type":"disabled"},"messages":[{"role":"system","content":"s"},{"role":"user","content":"u"}]}`
	for _, tc := range []struct {
		name, payload              string
		ledgerFails, providerFails bool
		want                       int32
	}{
		{"unknown reservation result", valid, true, false, 0},
		{"provider connection failure", valid, false, true, 1},
		{"valid", valid, false, false, 1},
		{"thinking enabled", strings.Replace(valid, "disabled", "enabled", 1), false, false, 0},
		{"oversize", strings.Replace(valid, `"u"`, fmt.Sprintf("%q", strings.Repeat("x", 16384)), 1), false, false, 0},
		{"tool field", strings.Replace(valid, `"messages"`, `"tools":[],"messages"`, 1), false, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var attempts, reservations atomic.Int32
			transport := trialTransport{
				reserve: func(context.Context) error {
					if reservations.Add(1) > 1 || tc.ledgerFails {
						return errors.New("stub ledger unavailable")
					}
					return nil
				},
				base: trialStub(func(r *http.Request) (*http.Response, error) {
					attempts.Add(1)
					if r.GetBody != nil {
						t.Error("outbound POST is replayable")
					}
					if tc.providerFails {
						return nil, io.ErrUnexpectedEOF
					}
					return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
				}),
			}
			var group sync.WaitGroup
			for range 8 {
				group.Go(func() {
					req, err := http.NewRequestWithContext(context.Background(), "POST", "https://api.deepseek.com/chat/completions", strings.NewReader(tc.payload))
					if err != nil {
						t.Error(err)
						return
					}
					if resp, err := transport.RoundTrip(req); err == nil {
						_ = resp.Body.Close()
					}
				})
			}
			group.Wait()
			if got := attempts.Load(); got != tc.want {
				t.Fatalf("provider attempts = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestTrialWindowRequiresShanghaiAndTwoHours(t *testing.T) {
	start := time.Date(2026, 11, 9, 18, 0, 0, 0, time.FixedZone("CST", 8*3600))
	window := trialWindow{Start: start, End: start.Add(2 * time.Hour), Users: []uint64{1, 2}}
	if !window.valid() {
		t.Fatal("valid window rejected")
	}
	window.End = window.End.Add(time.Second)
	if window.valid() {
		t.Fatal("oversized window accepted")
	}
}

// 以 Eino 实际序列化的请求验证守卫和各失败路径，桩不创建网络连接。
func TestTrialAdapterAttemptsAtMostOneProviderRequest(t *testing.T) {
	for _, scenario := range []string{"success", "redirect", "429", "503", "connection", "timeout", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var reserved, attempts int
			guard := trialTransport{
				reserve: func(context.Context) error {
					reserved++
					if reserved > 1 {
						return errors.New("already reserved")
					}
					return nil
				},
				base: trialStub(func(req *http.Request) (*http.Response, error) {
					attempts++
					status := http.StatusOK
					header := http.Header{"Content-Type": {"application/json"}}
					body := `{"id":"stub","model":"deepseek-flash","choices":[{"index":0,"message":{"role":"assistant","content":"合成回复"},"finish_reason":"stop"}],"usage":{"prompt_tokens":31,"completion_tokens":8,"total_tokens":39}}`
					switch scenario {
					case "redirect":
						status = http.StatusTemporaryRedirect
						header.Set("Location", "https://api.deepseek.com/chat/completions")
					case "429":
						status = http.StatusTooManyRequests
					case "503":
						status = http.StatusServiceUnavailable
					case "connection":
						return nil, io.ErrUnexpectedEOF
					case "timeout":
						return nil, context.DeadlineExceeded
					case "cancel":
						cancel()
						return nil, req.Context().Err()
					}
					return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
				}),
			}
			cfg := testConfig(guard)
			cfg.MaxOutputTokens, cfg.Window.MaxOutputTokens = 1024, 1024
			g, err := NewOpenAIGenerator(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			result, err := g.Generate(ctx, Request{Mode: ModeQuick3, Topic: Topic{Title: "合成辩题", Description: "合成描述"}, UserStance: StancePro, RoundNo: 1, UserContent: "合成发言"})
			if attempts != 1 || reserved != 1 {
				t.Fatalf("attempts=%d reservations=%d err=%v", attempts, reserved, err)
			}
			if scenario == "success" && (err != nil || !saneTrialUsage(result.Usage)) {
				t.Fatalf("result invalid: %v", err)
			}
			if scenario != "success" && err == nil {
				t.Fatal("failure became success")
			}
		})
	}
}

func TestTrialLedgerAtomicReservationAndPublicGate(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("目录持久同步按部署目标在 Linux 验证")
	}
	root := t.TempDir()
	start := time.Now().In(time.FixedZone("CST", 8*3600)).Truncate(time.Second).Add(-time.Minute)
	now := start.Add(time.Minute)
	window := trialWindow{Start: start, End: start.Add(2 * time.Hour), Users: []uint64{1, 2}}
	put := func(name string, value any) {
		t.Helper()
		if err := writeTrialRecord(root, name, value); err != nil {
			t.Fatal(err)
		}
	}
	put("window.json", window)
	put("monitor", struct{}{})
	if err := os.Chtimes(filepath.Join(root, "monitor"), now, now); err != nil {
		t.Fatal(err)
	}
	put("isolated-approval.json", trialApproval{BootID: "isolated-boot", WindowStart: start, MonitorAt: now, Start: now, End: window.End, Users: window.Users})
	put("public-approval.json", trialApproval{BootID: "public-boot", WindowStart: start, MonitorAt: now, Start: now, End: now.Add(30 * time.Minute), Users: []uint64{11, 12}})
	ledger := trialLedger{root: root, stage: isolatedTrial, bootID: "isolated-boot", window: window, alert: io.Discard, now: func() time.Time { return now }}
	public := trialLedger{root: root, stage: publicTrial, bootID: "public-boot", window: window, alert: io.Discard, now: ledger.now}
	request := func(user uint64) context.Context {
		return context.WithValue(context.Background(), trialCallKey{}, &trialCall{user: user})
	}
	if err := public.check(now, 11, DefaultModelTimeout); !errors.Is(err, errTrialLedger) {
		t.Fatal("public used before isolated result")
	}
	if err := ledger.reserve(request(99)); err == nil {
		t.Fatal("unapproved identity admitted")
	}
	if err := ledger.check(now, window.Users[1], DefaultModelTimeout); err == nil {
		t.Fatal("second listed account can trigger the isolated first turn")
	}
	if err := ledger.check(now, window.Users[0], DefaultModelTimeout); err != nil {
		t.Fatalf("designated isolated account denied: %v", err)
	}
	var group sync.WaitGroup
	var admitted atomic.Int32
	for i := range 16 {
		user := window.Users[i%len(window.Users)]
		group.Go(func() {
			if err := ledger.reserve(request(user)); err == nil {
				admitted.Add(1)
			}
		})
	}
	group.Wait()
	if admitted.Load() != 1 {
		t.Fatalf("admitted=%d", admitted.Load())
	}
	restarted := trialLedger{root: root, stage: isolatedTrial, bootID: "new-isolated-boot", window: window, alert: io.Discard, now: ledger.now}
	if err := restarted.check(now, window.Users[0], DefaultModelTimeout); err == nil {
		t.Fatal("restart reused old approval")
	}
	replaceApproval := func(stage string, approval trialApproval) {
		t.Helper()
		name := stage + "-approval.json"
		if err := os.Remove(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
		put(name, approval)
	}
	replaceApproval("isolated", trialApproval{BootID: restarted.bootID, WindowStart: start, MonitorAt: now, Start: now, End: window.End, Users: window.Users})
	if err := restarted.reserve(request(window.Users[0])); !errors.Is(err, os.ErrExist) {
		t.Fatalf("restart reset quota or failed unrelated check: %v", err)
	}
	record, err := readTrialRecord[trialReservation](filepath.Join(root, "isolated-reserved.json"))
	if err != nil || record.Calls != 1 || record.Input != 16384 || record.Output != 1024 || record.MaxCostMicroCNY != 40960 {
		t.Fatalf("reservation record invalid: %+v %v", record, err)
	}
	put("isolated-result.json", trialResult{WindowStart: start, Outcome: "success", Usage: Usage{
		PromptTokens: TokenCount{Known: true, Value: 5}, CompletionTokens: TokenCount{Known: true, Value: 3},
		TotalTokens: TokenCount{Known: true, Value: 8},
	}})
	if err := public.check(now, 11, DefaultModelTimeout); !errors.Is(err, errTrialLedger) {
		t.Fatal("public used before durable completion marker")
	}
	if err := publishTrialCompletion(root, isolatedTrial, start); err != nil {
		t.Fatal(err)
	}
	// 与观察过心跳的进程共用同一批准，独立模拟其首次请求。
	firstObserver := trialLedger{root: root, stage: publicTrial, bootID: public.bootID, window: window, alert: io.Discard, now: func() time.Time { return now.Add(16 * time.Second) }}
	public.now = func() time.Time { return now.Add(16 * time.Second) }
	if err := os.Chtimes(filepath.Join(root, "monitor"), public.now(), public.now()); err != nil {
		t.Fatal(err)
	}
	if err := firstObserver.check(public.now(), 11, DefaultModelTimeout); !errors.Is(err, errTrialMonitor) {
		t.Fatalf("first observer accepted heartbeat discontinuity: %v", err)
	}
	if err := public.reserve(request(11)); !errors.Is(err, errTrialMonitor) {
		t.Fatalf("fresh monitor after a gap must be rejected before watchdog poll: %v", err)
	}
	public.now = ledger.now
	if err := os.Chtimes(filepath.Join(root, "monitor"), now, now); err != nil {
		t.Fatal(err)
	}
	if err := public.reserve(request(11)); err == nil {
		t.Fatal("monitor recovery lifted persistent stop")
	}
	// 模拟负责人核对后获准恢复，应用自身没有删除 stop 的入口。
	if err := os.Remove(filepath.Join(root, "stop")); err != nil {
		t.Fatal(err)
	}
	if err := public.reserve(request(11)); err == nil {
		t.Fatal("stop marker removal alone resumed halted process")
	}
	faulted := trialLedger{root: root, stage: publicTrial, bootID: "public-fault-test", window: window, alert: io.Discard, now: ledger.now}
	replaceApproval("public", trialApproval{BootID: faulted.bootID, WindowStart: start, MonitorAt: now, Start: now, End: now.Add(30 * time.Minute), Users: []uint64{11, 12}})
	reservation := filepath.Join(root, "isolated-reserved.json")
	if err := os.Rename(reservation, reservation+".missing"); err != nil {
		t.Fatal(err)
	}
	if err := faulted.reserve(request(11)); !errors.Is(err, errTrialLedger) {
		t.Fatalf("missing isolated evidence was not a ledger failure: %v", err)
	}
	if err := os.Rename(reservation+".missing", reservation); err != nil {
		t.Fatal(err)
	}
	if err := faulted.reserve(request(11)); err == nil {
		t.Fatal("restored isolated ledger reused the same approval")
	}
	if err := os.Remove(filepath.Join(root, "stop")); err != nil {
		t.Fatal(err)
	}
	approvalFault := trialLedger{root: root, stage: publicTrial, bootID: "public-approval-fault", window: window, alert: io.Discard, now: ledger.now}
	replaceApproval("public", trialApproval{BootID: approvalFault.bootID, WindowStart: start, MonitorAt: now, Start: now, End: now.Add(30 * time.Minute), Users: []uint64{11, 12}})
	if err := approvalFault.check(now, 11, DefaultModelTimeout); err != nil {
		t.Fatal(err)
	}
	approvalPath := filepath.Join(root, "public-approval.json")
	if err := os.Rename(approvalPath, approvalPath+".missing"); err != nil {
		t.Fatal(err)
	}
	if err := approvalFault.reserve(request(11)); !errors.Is(err, errTrialLedger) {
		t.Fatalf("observed approval loss must stop: %v", err)
	}
	if err := os.Rename(approvalPath+".missing", approvalPath); err != nil {
		t.Fatal(err)
	}
	if err := approvalFault.reserve(request(11)); err == nil {
		t.Fatal("restored approval resumed process")
	}
	if err := os.Remove(filepath.Join(root, "stop")); err != nil {
		t.Fatal(err)
	}
	publicRestarted := trialLedger{root: root, stage: publicTrial, bootID: "public-restarted", window: window, alert: io.Discard, now: ledger.now}
	replaceApproval("public", trialApproval{BootID: publicRestarted.bootID, WindowStart: start, MonitorAt: now, Start: now, End: now.Add(30 * time.Minute), Users: []uint64{11, 12}})
	if err := publicRestarted.reserve(request(11)); err != nil {
		t.Fatal(err)
	}
	if err := publicRestarted.reserve(request(12)); !errors.Is(err, os.ErrExist) {
		t.Fatalf("public quota reused or failed unrelated check: %v", err)
	}
	put("stop", struct{}{})
	if err := publicRestarted.check(now, 11, DefaultModelTimeout); err == nil {
		t.Fatal("persistent stop ignored")
	}
}

func TestTrialHaltLatchesWhenLedgerCannotWrite(t *testing.T) {
	root := t.TempDir()
	ledger := trialLedger{
		root: filepath.Join(root, "missing"), stage: isolatedTrial, bootID: "stub-boot",
		alert: io.Discard, now: time.Now,
	}
	ledger.halt()
	if !ledger.halted.Load() {
		t.Fatal("ledger write failure cleared the in-memory stop")
	}
	ledger.root = root
	if err := ledger.check(time.Now(), 1, 0); !ledger.halted.Load() || err == nil {
		t.Fatal("ledger recovery cleared the in-memory stop")
	}
}

func TestTrialCompletionRequiresPublishedPreparedInode(t *testing.T) {
	root := t.TempDir()
	start := time.Now().UTC().Truncate(time.Second)
	raw, err := start.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	prepared := filepath.Join(root, "isolated-completion-prepared.json")
	completed := filepath.Join(root, "isolated-completed.json")
	if err := os.WriteFile(prepared, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readTrialCompletion(root, isolatedTrial); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unpublished preparation must not be readable: %v", err)
	}
	if err := os.WriteFile(completed, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readTrialCompletion(root, isolatedTrial); err == nil {
		t.Fatal("an independent, parseable copy is not publication evidence")
	}
	if err := os.Remove(completed); err != nil {
		t.Fatal(err)
	}
	if err := publishTrialCompletion(root, isolatedTrial, start); !errors.Is(err, os.ErrExist) {
		t.Fatalf("preexisting preparation must fail closed: %v", err)
	}
	if _, err := os.Lstat(completed); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed preparation published completion: %v", err)
	}
}

type blockingTrialAlert struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *blockingTrialAlert) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return len(p), nil
}

func TestTrialMonitorLatchesApprovalLossBeforeReturning(t *testing.T) {
	root := t.TempDir()
	approval := trialApproval{BootID: "boot"}
	alert := &blockingTrialAlert{entered: make(chan struct{}), release: make(chan struct{})}
	ledger := trialLedger{root: root, stage: isolatedTrial, bootID: approval.BootID,
		observedApproval: &approval, alert: alert, now: time.Now}
	done := make(chan bool, 1)
	go func() { done <- ledger.checkMonitor() }()
	defer close(alert.release)
	select {
	case <-alert.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("monitor did not reach failure reporting")
	}
	if !ledger.halted.Load() {
		t.Fatal("approval fault reported before shutdown latched")
	}
	if ledger.controlMu.TryLock() {
		ledger.controlMu.Unlock()
		t.Fatal("fault handling released admission lock before finishing")
	}
}
