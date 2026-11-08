package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

const balanceJSON = `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"9.94000000","granted_balance":"0","topped_up_balance":"9.94000000"}]}`
const completionJSON = `{"id":"anonymous-test-response","model":"deepseek-flash","choices":[{"index":0,"message":{"role":"assistant","content":"开放式考核也需要可比较的评价标准。"},"finish_reason":"stop"}],"usage":{"prompt_tokens":500,"completion_tokens":30,"total_tokens":530,"prompt_cache_hit_tokens":100,"prompt_cache_miss_tokens":400}}`

func TestExecutePersistsConsumptionBeforeOnlyPostAndDisablesReentry(t *testing.T) {
	dir := t.TempDir()
	var gets, posts atomic.Int32
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == "GET" {
			gets.Add(1)
			return reply(200, balanceJSON), nil
		}
		posts.Add(1)
		marker, err := os.ReadFile(filepath.Join(dir, "consumed.json"))
		if err != nil || !json.Valid(marker) {
			t.Fatal("POST reached transport before durable consumption")
		}
		if r.GetBody != nil {
			t.Fatal("request remains replayable")
		}
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 45*time.Second {
			t.Fatal("model deadline missing")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["max_tokens"] != float64(512) || body["stream"] != false || body["tool_choice"] != "none" {
			t.Fatal("wire controls missing")
		}
		return reply(200, completionJSON), nil
	})
	e := execute(context.Background(), dir, "synthetic-test-placeholder", rt)
	if e.Status != "success" || !e.Consumed || e.ModelAttempts != 1 || e.InputBound > 8192 || e.Response.Content == "" {
		t.Fatalf("unexpected evidence: %+v", e)
	}
	if gets.Load() != 2 || posts.Load() != 1 {
		t.Fatal("unexpected call count")
	}
	second := execute(context.Background(), dir, "synthetic-test-placeholder", rt)
	if second.Status != "blocked" || gets.Load() != 2 || posts.Load() != 1 {
		t.Fatal("reentry made a network request")
	}
	if _, err := os.Stat(filepath.Join(dir, "evidence.json")); err != nil {
		t.Fatal(err)
	}
}

func TestLaunchClaimIsAtomicAcrossConcurrentExecutions(t *testing.T) {
	dir := t.TempDir()
	var posts atomic.Int32
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == "GET" {
			return reply(200, balanceJSON), nil
		}
		posts.Add(1)
		return reply(200, completionJSON), nil
	})
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() { execute(context.Background(), dir, "synthetic-test-placeholder", rt) })
	}
	wg.Wait()
	if posts.Load() != 1 {
		t.Fatalf("POST count=%d", posts.Load())
	}
}

func TestInsufficientUnavailableOrMalformedBalanceNeverCallsModel(t *testing.T) {
	for _, body := range []string{
		`{"is_available":false,"balance_infos":[{"currency":"CNY","total_balance":"9.94"}]}`,
		`{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"0.001"}]}`,
		`{"balance_infos":[{"currency":"CNY","total_balance":"9.94"}]}`,
		`{"is_available":true,"balance_infos":[{"currency":"USD","total_balance":"9.94"}]}`,
		`{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"NaN"}]}`,
	} {
		t.Run(body, func(t *testing.T) {
			var posts int
			e := execute(context.Background(), t.TempDir(), "synthetic-test-placeholder", roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method == "POST" {
					posts++
				}
				return reply(200, body), nil
			}))
			if e.Status != "blocked" || e.Consumed || posts != 0 {
				t.Fatal("balance gate failed")
			}
		})
	}
}

func TestEveryFailureConsumesAttemptWithoutRetry(t *testing.T) {
	for _, status := range []int{0, 402, 429, 500, 307} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var posts int
			dir := t.TempDir()
			e := execute(context.Background(), dir, "synthetic-test-placeholder", roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method == "GET" {
					return reply(200, balanceJSON), nil
				}
				posts++
				if status == 0 {
					return nil, errors.New("synthetic transport failure")
				}
				resp := reply(status, `{"error":{"message":"private-provider-body"}}`)
				resp.Header.Set("Location", "https://other.example/chat/completions")
				return resp, nil
			}))
			if e.Status != "failed" || !e.Consumed || posts != 1 {
				t.Fatalf("failure retried or unconsumed: %+v", e)
			}
			data, _ := os.ReadFile(filepath.Join(dir, "evidence.json"))
			if strings.Contains(string(data), "private-provider-body") {
				t.Fatal("raw provider error leaked")
			}
		})
	}
}

func TestParentCancellationReachesOnlyModelAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var posts int
	e := execute(ctx, t.TempDir(), "synthetic-test-placeholder", roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == "GET" {
			return reply(200, balanceJSON), nil
		}
		posts++
		cancel()
		<-r.Context().Done()
		return nil, r.Context().Err()
	}))
	if !e.Consumed || e.Status != "failed" || e.ErrorCode != "model_canceled" || posts != 1 {
		t.Fatalf("cancel not propagated: %+v", e)
	}
}

func TestConsumePersistenceFailureNeverSends(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "consumed.json"), 0700); err != nil {
		t.Fatal(err)
	}
	var posts int
	e := execute(context.Background(), dir, "synthetic-test-placeholder", roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == "POST" {
			posts++
		}
		return reply(200, balanceJSON), nil
	}))
	if posts != 0 || e.Status != "blocked" {
		t.Fatal("failed persistence reached model")
	}
}

func TestWireGuardRejectsChangesAndOversizeWithoutConsumption(t *testing.T) {
	valid, err := anonymousWire()
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(map[string]any){
		func(v map[string]any) { v["max_tokens"] = 513 },
		func(v map[string]any) { v["thinking"] = map[string]any{"type": "enabled"} },
		func(v map[string]any) { v["tools"] = []any{} },
		func(v map[string]any) { v["stream"] = true },
		func(v map[string]any) { v["model"] = "other-model" },
		func(v map[string]any) {
			v["messages"] = []any{map[string]any{"role": "user", "content": strings.Repeat("x", 8193)}}
		},
	} {
		var v map[string]any
		if err := json.Unmarshal(valid, &v); err != nil {
			t.Fatal(err)
		}
		mutate(v)
		body, _ := json.Marshal(v)
		if _, _, err := guardWire(body); err == nil {
			t.Fatal("unsafe wire accepted")
		}
	}
}

func TestClientAndTransportDisableRedirectReplayAndProxy(t *testing.T) {
	tr := newDirectTransport()
	defer tr.CloseIdleConnections()
	if tr.Proxy != nil || !tr.DisableKeepAlives || tr.ForceAttemptHTTP2 || len(tr.TLSNextProto) != 0 {
		t.Fatal("transport may use proxy or reused HTTP/2 connection")
	}
	c := newClient(tr, 45*time.Second)
	if c.CheckRedirect == nil || c.CheckRedirect(nil, nil) != http.ErrUseLastResponse {
		t.Fatal("redirects enabled")
	}
}

func TestUnavailableBalanceRedirectIsNotFollowed(t *testing.T) {
	var calls int
	e := execute(context.Background(), t.TempDir(), "synthetic-test-placeholder", roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		resp := reply(302, "")
		resp.Header.Set("Location", baseURL+"/chat/completions")
		return resp, nil
	}))
	if calls != 1 || e.Consumed || e.Status != "blocked" {
		t.Fatal("balance redirect followed")
	}
}

func TestTotalBudgetAndShortParentDeadlineReachTransport(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	e := execute(ctx, t.TempDir(), "synthetic-test-placeholder", roundTripFunc(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 80*time.Millisecond {
			t.Fatal("parent deadline extended")
		}
		if r.Method == "GET" {
			return reply(200, balanceJSON), nil
		}
		<-r.Context().Done()
		return nil, r.Context().Err()
	}))
	if e.Status != "failed" || !e.Consumed || e.ModelAttempts != 1 || e.ErrorCode != "model_timeout" || e.ElapsedMS > 2000 {
		t.Fatalf("deadline not respected: %+v", e)
	}
}

func TestRefusalAndTruncationNeverReissueAndEvidenceRedacts(t *testing.T) {
	for _, reason := range []string{"length", "content_filter", "synthetic-test-placeholder"} {
		dir := t.TempDir()
		var posts int
		e := execute(context.Background(), dir, "synthetic-test-placeholder", roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method == "GET" {
				return reply(200, balanceJSON), nil
			}
			posts++
			return reply(200, strings.ReplaceAll(completionJSON, `"stop"`, `"`+reason+`"`)), nil
		}))
		if !e.Consumed || posts != 1 || e.Status != "failed" {
			t.Fatal("unusable response was retried or accepted")
		}
		raw, _ := os.ReadFile(filepath.Join(dir, "evidence.json"))
		if strings.Contains(string(raw), "synthetic-test-placeholder") {
			t.Fatal("credential echoed in evidence")
		}
	}
}

func TestUsageCostIsExactButNotInvoice(t *testing.T) {
	e := evidence{StartedAt: time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)}
	if json.Unmarshal([]byte(`{"usage":{"prompt_tokens":500,"completion_tokens":30,"prompt_cache_hit_tokens":100,"prompt_cache_miss_tokens":400}}`), &e.Provider) != nil {
		t.Fatal("fixture")
	}
	calculateCost(&e)
	if e.CalculatedCost != "0.00052200" || e.CostBasis != "official_rate_returned_cache_usage_not_invoice" {
		t.Fatalf("bad cost: %+v", e)
	}
}
