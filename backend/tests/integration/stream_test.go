package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/bingqilin456/DebateForge/backend/internal/debate"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type streamGenerator struct {
	generate func(context.Context, ai.Request) (ai.Result, error)
	stream   func(context.Context, ai.Request, func(string) error) (ai.Result, error)
}

func mustUint(t *testing.T, id string) uint64 {
	t.Helper()
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
func (g streamGenerator) Generate(c context.Context, r ai.Request) (ai.Result, error) {
	return g.generate(c, r)
}
func (g streamGenerator) Stream(c context.Context, r ai.Request, emit func(string) error) (ai.Result, error) {
	return g.stream(c, r, emit)
}

func openTurnStream(t *testing.T, f *debateFixture, path string, body any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("POST", f.server.URL+path+"/turns/stream", strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", integrationOrigin)
	req.Header.Set("X-CSRF-Token", f.csrf)
	req.AddCookie(f.cookie)
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func readFrame(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	var b strings.Builder
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("incomplete event: %v", err)
		}
		b.WriteString(line)
		if line == "\n" {
			return b.String()
		}
	}
}

func assertStoredSingleTurn(t *testing.T, f *debateFixture, id string, version uint64) {
	t.Helper()
	d, err := debate.NewRepository(f.db.ORM).Detail(context.Background(), f.uid, mustUint(t, id))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Turns) != 1 || len(d.Turns[0].Messages) != 2 || d.Turns[0].Status != "completed" || d.Turns[0].AttemptVersion != version || d.Debate.CompletedRounds != 1 {
		t.Fatalf("unexpected persisted state: %+v", d)
	}
}

func TestT15StreamAndJSONShareClaimWithoutDoubleWrite(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	g := streamGenerator{generate: ai.NewFake().Generate, stream: func(c context.Context, r ai.Request, emit func(string) error) (ai.Result, error) {
		calls.Add(1)
		if err := emit("中文🌱"); err != nil {
			return ai.Result{}, err
		}
		select {
		case <-release:
		case <-c.Done():
			return ai.Result{}, c.Err()
		}
		if err := emit("增量"); err != nil {
			return ai.Result{}, err
		}
		return ai.Result{Content: "中文🌱增量"}, nil
	}}
	f := newDebateFixture(t, g)
	id := f.create(t)
	path := "/api/v1/debates/" + id
	key := uuid.NewString()
	input := submit("同键原文", key, 1)
	resp := openTurnStream(t, f, path, input)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" || resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("stream headers: %d %v", resp.StatusCode, resp.Header)
	}
	reader := bufio.NewReader(resp.Body)
	start := readFrame(t, reader)
	if !strings.HasPrefix(start, "event: start\n") || strings.Contains(start, "attempt_version") {
		t.Fatal(start)
	}
	if first := readFrame(t, reader); !strings.Contains(first, "中文🌱") {
		t.Fatal(first)
	}
	if pending := f.request(t, "POST", path+"/turns", input); pending.status != 202 {
		t.Fatalf("JSON duplicate: %d", pending.status)
	}
	duplicate := openTurnStream(t, f, path, input)
	raw, _ := io.ReadAll(duplicate.Body)
	duplicate.Body.Close()
	if duplicate.StatusCode != 202 || !strings.HasPrefix(duplicate.Header.Get("Content-Type"), "application/json") || !json.Valid(raw) {
		t.Fatalf("duplicate stream: %d %s", duplicate.StatusCode, raw)
	}
	conflict := f.request(t, "POST", path+"/turns", submit("另一个键", uuid.NewString(), 1))
	if conflict.status != 409 {
		t.Fatalf("round conflict: %d", conflict.status)
	}
	close(release)
	rest, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rest), "event: delta") || !strings.Contains(string(rest), "event: done") || strings.Contains(string(rest), "event: error") {
		t.Fatal(string(rest))
	}
	assertStoredSingleTurn(t, f, id, 1)
	replay := openTurnStream(t, f, path, input)
	replayed, _ := io.ReadAll(replay.Body)
	if !strings.HasPrefix(string(replayed), "event: start") || !strings.Contains(string(replayed), "event: done") || strings.Contains(string(replayed), "event: delta") || calls.Load() != 1 {
		t.Fatalf("replay called model or changed events: %d %s", calls.Load(), replayed)
	}
	assertStoredSingleTurn(t, f, id, 1)
}

func TestT15DisconnectUsesDetailAndExplicitJSONRetry(t *testing.T) {
	var retries atomic.Int32
	upstreamStopped := make(chan struct{})
	g := streamGenerator{generate: func(c context.Context, r ai.Request) (ai.Result, error) {
		if retries.Add(1) == 1 {
			return ai.Result{}, &ai.Error{Code: ai.CodeModelTimeout}
		}
		return ai.NewFake().Generate(c, r)
	}, stream: func(c context.Context, r ai.Request, emit func(string) error) (ai.Result, error) {
		defer close(upstreamStopped)
		if err := emit("临时内容🌱"); err != nil {
			return ai.Result{}, err
		}
		<-c.Done()
		return ai.Result{}, c.Err()
	}}
	f := newDebateFixture(t, g)
	id := f.create(t)
	path := "/api/v1/debates/" + id
	key := uuid.NewString()
	resp := openTurnStream(t, f, path, submit("取消保留原文", key, 1))
	reader := bufio.NewReader(resp.Body)
	readFrame(t, reader)
	readFrame(t, reader)
	resp.Body.Close()
	select {
	case <-upstreamStopped:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream not cancelled")
	}
	var detail debate.Detail
	for deadline := time.Now().Add(4 * time.Second); ; {
		var err error
		detail, err = debate.NewRepository(f.db.ORM).Detail(context.Background(), f.uid, mustUint(t, id))
		if err != nil {
			t.Fatal(err)
		}
		if len(detail.Turns) == 1 && detail.Turns[0].Status == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cancel failure not persisted")
		}
		time.Sleep(10 * time.Millisecond)
	}
	turn := detail.Turns[0]
	if len(turn.Messages) != 1 || turn.AttemptVersion != 1 || turn.ClientRequestID != key {
		t.Fatalf("cancellation wrote partial reply: %+v", turn)
	}
	retryPath := path + "/turns/" + fmt.Sprint(turn.ID) + "/retry"
	if r := f.request(t, "POST", retryPath, map[string]any{"expected_attempt_version": 1}); r.status != 504 {
		t.Fatalf("first retry: %d", r.status)
	}
	if r := f.request(t, "POST", retryPath, map[string]any{"expected_attempt_version": 1}); r.status != 409 || r.errorCode() != "attempt_version_conflict" || retries.Load() != 1 {
		t.Fatalf("stale version retried model: %d", r.status)
	}
	queried := f.request(t, "GET", path, nil)
	var turns []debate.TurnDTO
	if err := json.Unmarshal(queried.data()["turns"], &turns); err != nil || len(turns) != 1 || turns[0].AttemptVersion != 2 {
		t.Fatalf("query version: %v %+v", err, turns)
	}
	if r := f.request(t, "POST", retryPath, map[string]any{"expected_attempt_version": turns[0].AttemptVersion}); r.status != 200 {
		t.Fatalf("latest explicit retry: %d", r.status)
	}
	assertStoredSingleTurn(t, f, id, 3)
}

type loseDoneWriter struct{ http.ResponseWriter }

func (w loseDoneWriter) Write(p []byte) (int, error) {
	if strings.HasPrefix(string(p), "event: done\n") {
		return len(p), nil
	}
	return w.ResponseWriter.Write(p)
}
func (w loseDoneWriter) Flush()                      { w.ResponseWriter.(http.Flusher).Flush() }
func (w loseDoneWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func TestT15CommittedButDoneLostRecoversWithoutRegeneration(t *testing.T) {
	var calls atomic.Int32
	g := streamGenerator{generate: ai.NewFake().Generate, stream: func(c context.Context, r ai.Request, emit func(string) error) (ai.Result, error) {
		calls.Add(1)
		if err := emit("正式回复🌱"); err != nil {
			return ai.Result{}, err
		}
		return ai.Result{Content: "正式回复🌱"}, nil
	}}
	f := newDebateFixture(t, g)
	id := f.create(t)
	path := "/api/v1/debates/" + id
	input := submit("丢失done原文", uuid.NewString(), 1)
	upstream := f.server.Config.Handler
	f.server.Close()
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { upstream.ServeHTTP(loseDoneWriter{w}, r) }))
	resp := openTurnStream(t, f, path, input)
	raw, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(raw), "event: done") || !strings.Contains(string(raw), "event: delta") {
		t.Fatalf("did not lose only done: %s", raw)
	}
	assertStoredSingleTurn(t, f, id, 1)
	detail := f.request(t, "GET", path, nil)
	if detail.status != 200 {
		t.Fatal(detail.status)
	}
	f.mount(t, g)
	replay := openTurnStream(t, f, path, input)
	raw, _ = io.ReadAll(replay.Body)
	if !strings.Contains(string(raw), "event: done") || strings.Contains(string(raw), "event: delta") || calls.Load() != 1 {
		t.Fatalf("recovery regenerated: %d %s", calls.Load(), raw)
	}
	assertStoredSingleTurn(t, f, id, 1)
}

func TestT15StreamDatabaseFailuresNeverSendDone(t *testing.T) {
	for _, failure := range []string{"claim_unknown", "complete_unknown", "rollback"} {
		t.Run(failure, func(t *testing.T) {
			g := streamGenerator{generate: ai.NewFake().Generate, stream: func(c context.Context, r ai.Request, emit func(string) error) (ai.Result, error) {
				if err := emit("临时片段"); err != nil {
					return ai.Result{}, err
				}
				return ai.Result{Content: "临时片段"}, nil
			}}
			f := newDebateFixture(t, g)
			id := f.create(t)
			pool, _ := f.db.ORM.DB()
			var orm *gorm.DB
			if failure == "rollback" {
				orm = wrappedORM(t, f, pool)
				if err := orm.Callback().Create().Before("gorm:create").Register("t15_fail_assistant", func(tx *gorm.DB) {
					if message, ok := tx.Statement.Dest.(*debate.Message); ok && message.Role == "assistant" {
						tx.AddError(errors.New("synthetic final write failure"))
					}
				}); err != nil {
					t.Fatal(err)
				}
			} else {
				loseAt := int32(2)
				if failure == "claim_unknown" {
					loseAt = 1
				}
				orm = wrappedORM(t, f, &acknowledgedCommitPool{DB: pool, loseAt: loseAt})
			}
			f.mountDB(t, g, orm)
			resp := openTurnStream(t, f, "/api/v1/debates/"+id, submit("数据库故障原文", uuid.NewString(), 1))
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(body), "event: done") {
				t.Fatalf("unconfirmed transaction sent done: %s", body)
			}
			if failure == "claim_unknown" {
				if resp.StatusCode != 503 || !json.Valid(body) || strings.Contains(string(body), "event:") {
					t.Fatalf("claim error must be ordinary JSON: %d %s", resp.StatusCode, body)
				}
			} else if resp.StatusCode != 200 || !strings.Contains(string(body), "event: error") || !strings.Contains(string(body), "dependency_unavailable") {
				t.Fatalf("stream error missing: %d %s", resp.StatusCode, body)
			}
			d, err := debate.NewRepository(f.db.ORM).Detail(context.Background(), f.uid, mustUint(t, id))
			if err != nil {
				t.Fatal(err)
			}
			want := "failed"
			messages := 1
			if failure == "claim_unknown" {
				want = "generating"
			}
			if failure == "complete_unknown" {
				want = "completed"
				messages = 2
			}
			if len(d.Turns) != 1 || d.Turns[0].Status != want || len(d.Turns[0].Messages) != messages {
				t.Fatalf("unexpected persisted outcome: %+v", d)
			}
		})
	}
}

func TestT15ExpiredStreamCannotOverwriteNewRetry(t *testing.T) {
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	g := streamGenerator{generate: ai.NewFake().Generate, stream: func(c context.Context, r ai.Request, emit func(string) error) (ai.Result, error) {
		if err := emit("旧尝试临时片段"); err != nil {
			return ai.Result{}, err
		}
		select {
		case <-release:
		case <-c.Done():
			return ai.Result{}, c.Err()
		}
		return ai.Result{Content: "旧尝试迟到结果"}, nil
	}}
	f := newDebateFixture(t, g)
	id := f.create(t)
	path := "/api/v1/debates/" + id
	resp := openTurnStream(t, f, path, submit("租约原文", uuid.NewString(), 1))
	reader := bufio.NewReader(resp.Body)
	readFrame(t, reader)
	readFrame(t, reader)
	repo := debate.NewRepository(f.db.ORM)
	d, err := repo.Detail(context.Background(), f.uid, mustUint(t, id))
	if err != nil {
		t.Fatal(err)
	}
	turn := d.Turns[0]
	if turn.LeaseExpiresAt == nil || turn.LeaseExpiresAt.Sub(turn.CreatedAt) < 89*time.Second || turn.LeaseExpiresAt.Sub(turn.CreatedAt) > 91*time.Second {
		t.Fatal("stream did not use 90s lease")
	}
	if err := f.db.ORM.Exec("UPDATE debate_turns SET lease_expires_at=? WHERE id=?", time.Now().UTC().Add(-time.Second), turn.ID).Error; err != nil {
		t.Fatal(err)
	}
	if n, err := repo.RecoverExpiredLeases(context.Background()); err != nil || n != 1 {
		t.Fatalf("recover: %d %v", n, err)
	}
	// 用独立 Service 模拟另一个进程在旧租约回收后认领 JSON retry。
	newProcess := debate.NewService(repo, ai.NewFake(), debate.NewGate(nil))
	if _, status, err := newProcess.Retry(context.Background(), f.uid, mustUint(t, id), turn.ID, debate.RetryInput{ExpectedAttemptVersion: 1}); err != nil || status != 200 {
		t.Fatalf("new retry: %d %v", status, err)
	}
	close(release)
	raw, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "event: done") || !strings.Contains(string(raw), "event: error") {
		t.Fatalf("old attempt reported success: %s", raw)
	}
	assertStoredSingleTurn(t, f, id, 2)
	d, err = repo.Detail(context.Background(), f.uid, mustUint(t, id))
	if err != nil || d.Turns[0].Messages[1].Content == "旧尝试迟到结果" {
		t.Fatal("late stream overwrote retry")
	}
}

func TestT15StreamPreflightStaysJSON(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		status               int
		body                 string
		cookie, csrf, accept bool
	}{
		{"unauthenticated", 401, `{}`, false, true, true},
		{"csrf", 403, `{}`, true, false, true},
		{"accept", 400, `{}`, true, true, false},
		{"json", 400, `{`, true, true, true},
		{"body_limit", 413, strings.Repeat(" ", 32769), true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newDebateFixture(t, ai.NewFake())
			id := f.create(t)
			req, _ := http.NewRequest("POST", f.server.URL+"/api/v1/debates/"+id+"/turns/stream", strings.NewReader(tc.body))
			req.Header.Set("Origin", integrationOrigin)
			req.Header.Set("Content-Type", "application/json")
			if tc.cookie {
				req.AddCookie(f.cookie)
			}
			if tc.csrf {
				req.Header.Set("X-CSRF-Token", f.csrf)
			}
			if tc.accept {
				req.Header.Set("Accept", "text/event-stream")
			}
			resp, err := f.client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			raw, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != tc.status || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") || !json.Valid(raw) || strings.Contains(string(raw), "event:") {
				t.Fatalf("preflight %d %s", resp.StatusCode, raw)
			}
			var count int64
			f.db.ORM.Table("debate_turns").Where("debate_id=?", id).Count(&count)
			if count != 0 {
				t.Fatal("preflight wrote turn")
			}
		})
	}
}
