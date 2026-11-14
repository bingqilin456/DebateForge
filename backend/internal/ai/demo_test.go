package ai

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func demoFixture(t *testing.T, calls int) (*demoLedger, DemoApproval) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("持久账本只支持 Linux，须在 Linux 容器实测")
	}
	now := time.Now().UTC().Truncate(time.Second)
	a := DemoApproval{ID: "fake-approval", Model: "deepseek-flash", Users: []uint64{1, 2}, Start: now.Add(-time.Minute), End: now.Add(time.Hour), MaxCalls: calls, MaxInputTokens: 1000000, MaxOutputTokens: 100000, MaxCostMicroCNY: 1000000, InputMicroCNYPerMillion: 2000000, OutputMicroCNYPerMillion: 8000000, PerCallInputTokens: 100000, PerCallOutputTokens: 1024}
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	l, err := openDemoLedger(root, a)
	if err != nil {
		t.Fatal(err)
	}
	l.now = func() time.Time { return now }
	approveDemo(t, l)
	t.Cleanup(func() { _ = l.close() })
	return l, a
}

func approveDemo(t *testing.T, l *demoLedger) {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"boot_id": l.bootID})
	if err := os.WriteFile(filepath.Join(l.root, "enable.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func demoKnownUsage() Usage {
	return Usage{PromptTokens: TokenCount{Known: true, Value: 100}, CompletionTokens: TokenCount{Known: true, Value: 10}, TotalTokens: TokenCount{Known: true, Value: 110}}
}

// 没有原子预留会让最后一槽竞争发出多次收费请求。
func TestDemoLastSlotAndRestart(t *testing.T) {
	l, a := demoFixture(t, 1)
	var won atomic.Int32
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			c := &demoCall{user: 1}
			if l.reserve(context.Background(), c, 2000) == nil {
				won.Add(1)
				if err := l.finish(c, demoKnownUsage(), nil); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
	if won.Load() != 1 {
		t.Fatalf("reserved %d calls", won.Load())
	}
	if err := l.close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openDemoLedger(l.root, a)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.close()
	approveDemo(t, reopened)
	if reopened.reserve(context.Background(), &demoCall{user: 2}, 2000) == nil {
		t.Fatal("restart restored exhausted budget")
	}
}

func TestDemoFailureAndControls(t *testing.T) {
	for _, scenario := range []string{"unknown", "failure", "timeout", "cancel", "stop_inflight", "expired_inflight", "corrupt", "unfinished", "changed", "no_approval", "identity", "window_lead", "stop_restart"} {
		t.Run(scenario, func(t *testing.T) {
			l, a := demoFixture(t, 10)
			c := &demoCall{user: 1}
			switch scenario {
			case "no_approval":
				_ = os.Remove(filepath.Join(l.root, "enable.json"))
			case "identity":
				c.user = 3
			case "window_lead":
				l.now = func() time.Time { return a.End.Add(-30 * time.Second) }
			case "corrupt":
				_ = os.WriteFile(filepath.Join(l.root, "manifest.json"), []byte("{"), 0600)
			}
			err := l.reserve(context.Background(), c, 2000)
			if scenario == "no_approval" || scenario == "identity" || scenario == "window_lead" || scenario == "corrupt" {
				if err == nil {
					t.Fatal("invalid control admitted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			usage := demoKnownUsage()
			var failure error
			switch scenario {
			case "unknown":
				usage = Usage{}
			case "failure":
				failure = &Error{Code: CodeProviderError}
			case "timeout":
				failure = context.DeadlineExceeded
			case "cancel":
				failure = context.Canceled
			case "stop_inflight", "stop_restart":
				_ = os.WriteFile(filepath.Join(l.root, "stop"), []byte("operator"), 0600)
			case "expired_inflight":
				l.now = func() time.Time { return a.End }
			case "changed":
				a.MaxCalls++
				raw, _ := json.Marshal(a)
				_ = os.WriteFile(filepath.Join(l.root, "approval.json"), raw, 0600)
			}
			if scenario != "unfinished" {
				if l.finish(c, usage, failure) == nil {
					t.Fatal("unsafe result published")
				}
			}
			_ = l.close()
			r, err := openDemoLedger(l.root, a)
			if err == nil {
				defer r.close()
				approveDemo(t, r)
				if r.reserve(context.Background(), &demoCall{user: 2}, 2000) == nil {
					t.Fatal("unsafe restart admitted")
				}
			}
		})
	}
}

func TestDemoTokenMoneyAndDailyLimits(t *testing.T) {
	for _, scenario := range []string{"input", "output", "money", "minute", "day"} {
		t.Run(scenario, func(t *testing.T) {
			l, a := demoFixture(t, 100)
			// 更换初始批准必须使用新的空账本，运行中修改会被拒绝。
			_ = l.close()
			root := t.TempDir()
			if err := os.Chmod(root, 0700); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "input":
				a.MaxInputTokens = 2000
			case "output":
				a.MaxOutputTokens = 1024
			case "money":
				a.MaxCostMicroCNY = 12192
			}
			var err error
			l, err = openDemoLedger(root, a)
			if err != nil {
				t.Fatal(err)
			}
			defer l.close()
			approveDemo(t, l)
			now := a.Start.Add(time.Minute)
			l.now = func() time.Time { return now }
			count := 1
			if scenario == "minute" {
				count = 6
			}
			if scenario == "day" {
				count = 20
			}
			for range count {
				c := &demoCall{user: 1}
				if err := l.reserve(context.Background(), c, 2000); err != nil {
					t.Fatal(err)
				}
				if err := l.finish(c, demoKnownUsage(), nil); err != nil {
					t.Fatal(err)
				}
				if scenario == "day" {
					now = now.Add(time.Minute)
				}
			}
			if l.reserve(context.Background(), &demoCall{user: 1}, 2000) == nil {
				t.Fatal("limit exceeded")
			}
		})
	}
}

func TestDemoLockAndApprovalBinding(t *testing.T) {
	l, a := demoFixture(t, 10)
	if second, err := openDemoLedger(l.root, a); err == nil {
		second.close()
		t.Fatal("second instance admitted")
	}
	_ = l.close()
	r, err := openDemoLedger(l.root, a)
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()
	if r.reserve(context.Background(), &demoCall{user: 1}, 2000) == nil {
		t.Fatal("stale boot approval admitted")
	}
}

func TestDemoPersistenceFailureRetainsStop(t *testing.T) {
	for _, phase := range []string{"reserve", "result", "missing_manifest"} {
		t.Run(phase, func(t *testing.T) {
			l, a := demoFixture(t, 10)
			c := &demoCall{user: 1}
			if phase == "reserve" {
				if err := os.Mkdir(filepath.Join(l.root, "reserve-000001.json"), 0700); err != nil {
					t.Fatal(err)
				}
				if l.reserve(context.Background(), c, 2000) == nil {
					t.Fatal("unpersisted call admitted")
				}
			} else {
				if err := l.reserve(context.Background(), c, 2000); err != nil {
					t.Fatal(err)
				}
				if phase == "result" {
					if err := os.Mkdir(filepath.Join(l.root, "result-000001.json"), 0700); err != nil {
						t.Fatal(err)
					}
					if l.finish(c, demoKnownUsage(), nil) == nil {
						t.Fatal("unpersisted result published")
					}
				} else {
					if err := l.finish(c, demoKnownUsage(), nil); err != nil {
						t.Fatal(err)
					}
					if err := os.Remove(filepath.Join(l.root, "manifest.json")); err != nil {
						t.Fatal(err)
					}
				}
			}
			_ = l.close()
			reopened, err := openDemoLedger(l.root, a)
			if err == nil {
				defer reopened.close()
				approveDemo(t, reopened)
				if reopened.reserve(context.Background(), &demoCall{user: 2}, 2000) == nil {
					t.Fatal("damaged ledger reset on restart")
				}
			}
		})
	}
}

func TestDemoUnknownUsageStoredAsNull(t *testing.T) {
	l, _ := demoFixture(t, 10)
	c := &demoCall{user: 1}
	if err := l.reserve(context.Background(), c, 2000); err != nil {
		t.Fatal(err)
	}
	_ = l.finish(c, Usage{}, context.Canceled)
	raw, err := os.ReadFile(filepath.Join(l.root, "result-000001.json"))
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if json.Unmarshal(raw, &record) != nil {
		t.Fatal("invalid result JSON")
	}
	usage, ok := record["usage"].(map[string]any)
	if !ok || len(usage) != 3 {
		t.Fatal("invalid usage record")
	}
	for _, key := range []string{"input_tokens", "output_tokens", "total_tokens"} {
		value, exists := usage[key]
		if !exists || value != nil {
			t.Fatal("unknown usage must be explicit null")
		}
	}
}

func TestDemoMissingTailNeverRestoresBudget(t *testing.T) {
	for _, scenario := range []string{"tail", "head"} {
		t.Run(scenario, func(t *testing.T) {
			l, a := demoFixture(t, 10)
			c := &demoCall{user: 1}
			if err := l.reserve(context.Background(), c, 2000); err != nil {
				t.Fatal(err)
			}
			if err := l.finish(c, demoKnownUsage(), nil); err != nil {
				t.Fatal(err)
			}
			_ = l.close()
			if scenario == "tail" {
				for _, name := range []string{"reserve-000001.json", "result-000001.json"} {
					if err := os.Remove(filepath.Join(l.root, name)); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				if err := os.Remove(filepath.Join(l.root, "head.json")); err != nil {
					t.Fatal(err)
				}
			}
			reopened, err := openDemoLedger(l.root, a)
			if err == nil {
				defer reopened.close()
				approveDemo(t, reopened)
				if reopened.reserve(context.Background(), &demoCall{user: 2}, 2000) == nil {
					t.Fatal("missing ledger records restored budget")
				}
			}
		})
	}
}
