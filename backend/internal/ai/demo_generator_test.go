package ai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDemoAllCapabilitiesShareProtectedAdapter(t *testing.T) {
	l, _ := demoFixture(t, 6)
	calls := 0
	base := trialStub(func(r *http.Request) (*http.Response, error) {
		calls++
		raw, _ := io.ReadAll(r.Body)
		payload := decodeBody(t, string(raw))
		stream, _ := payload["stream"].(bool)
		content := "合成辩论回复，保留立场并回应论点。"
		if calls == 1 {
			content = `{"thesis":"主张","arguments":["论据"],"risks":["风险"]}`
		}
		if calls == 6 {
			content = validSummaryJSON()
		}
		usage := `{"prompt_tokens":100,"completion_tokens":10,"total_tokens":110}`
		body := chatCompletionJSON(content, "stop", usage)
		ct := "application/json"
		if stream {
			ct = "text/event-stream"
			stop := "stop"
			body = "data: " + streamChunkFrame("assistant", content, nil, "") + "\n\ndata: " + streamChunkFrame("", "", &stop, "") + "\n\ndata: " + streamUsageFrame(usage) + "\n\ndata: [DONE]\n\n"
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {ct}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	cfg := testConfig(nil)
	cfg.MaxOutputTokens = 1024
	cfg.Window = WindowLimits{ContextTokens: 1000000, MaxOutputTokens: 1024, SummaryPromptReserveTokens: 60000, SummaryOutputReserveTokens: 4096}
	g, err := newDemoGenerator(context.Background(), cfg, l, base)
	if err != nil {
		t.Fatal(err)
	}
	req := validRequest()
	req.UserID = 1
	req.Mode = ModeStandard4
	_, err = g.GenerateStrategy(context.Background(), StrategyRequest{UserID: 1, Mode: ModeStandard4, AIStyle: StyleLogical, Topic: req.Topic, UserStance: req.UserStance})
	if err != nil {
		t.Fatal(err)
	}
	for round := 1; round <= 4; round++ {
		req.RoundNo = round
		if round > 1 {
			req.CompletedTurns = append(req.CompletedTurns, CompletedTurn{RoundNo: round - 1, UserContent: "用户论点", AssistantContent: "助手回应"})
		}
		var text strings.Builder
		_, err := g.Stream(context.Background(), req, func(s string) error { text.WriteString(s); return nil })
		if err != nil || text.Len() == 0 {
			t.Fatalf("round %d: %v cause: %v", round, err, errors.Unwrap(err))
		}
	}
	summary := validSummaryRequest()
	summary.UserID = 1
	summary.Mode = ModeStandard4
	summary.Turns = append(summary.Turns, CompletedTurn{RoundNo: 4, UserContent: "总结立场", AssistantContent: "回应总结"})
	if _, err := g.GenerateSummary(context.Background(), summary); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Generate(context.Background(), req); err == nil {
		t.Fatal("seventh call admitted")
	}
	if calls != 6 {
		t.Fatalf("provider calls %d", calls)
	}
}

func TestDemoStopCancelsProviderAndRetainsReservation(t *testing.T) {
	l, _ := demoFixture(t, 10)
	reached := make(chan struct{})
	canceled := make(chan struct{})
	base := trialStub(func(r *http.Request) (*http.Response, error) {
		close(reached)
		<-r.Context().Done()
		close(canceled)
		return nil, r.Context().Err()
	})
	cfg := testConfig(nil)
	cfg.MaxOutputTokens = 1024
	cfg.Window.MaxOutputTokens = 1024
	g, err := newDemoGenerator(context.Background(), cfg, l, base)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		req := validRequest()
		req.UserID = 1
		_, err := g.Generate(context.Background(), req)
		done <- err
	}()
	<-reached
	if err := os.WriteFile(filepath.Join(l.root, "stop"), []byte("operator"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("stop did not cancel upstream")
	}
	if err := <-done; err == nil {
		t.Fatal("stopped result published")
	}
	if len(l.reservations) != 1 {
		t.Fatal("reservation lost")
	}
}

func TestDemoUnknownFailureAndPanicNeverRefund(t *testing.T) {
	for _, scenario := range []string{"unknown_usage", "stream_without_usage", "http_failure", "panic"} {
		t.Run(scenario, func(t *testing.T) {
			l, _ := demoFixture(t, 10)
			calls := 0
			base := trialStub(func(r *http.Request) (*http.Response, error) {
				calls++
				if scenario == "panic" {
					panic("synthetic provider panic")
				}
				status := 200
				ct := "application/json"
				body := chatCompletionJSON("合成回复", "stop", "")
				if scenario == "http_failure" {
					status = 500
					body = `{"error":{"message":"synthetic"}}`
				}
				if scenario == "stream_without_usage" {
					ct = "text/event-stream"
					stop := "stop"
					body = "data: " + streamChunkFrame("assistant", "合成回复", &stop, "") + "\n\ndata: [DONE]\n\n"
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {ct}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			cfg := testConfig(nil)
			cfg.MaxOutputTokens = 1024
			cfg.Window.MaxOutputTokens = 1024
			g, err := newDemoGenerator(context.Background(), cfg, l, base)
			if err != nil {
				t.Fatal(err)
			}
			req := validRequest()
			req.UserID = 1
			func() {
				defer func() { _ = recover() }()
				var err error
				if scenario == "stream_without_usage" {
					_, err = g.Stream(context.Background(), req, func(string) error { return nil })
				} else {
					_, err = g.Generate(context.Background(), req)
				}
				if err == nil {
					t.Error("unsafe result published")
				}
			}()
			if _, err := os.Stat(filepath.Join(l.root, "stop")); err != nil {
				t.Fatal("failure did not persist stop")
			}
			if _, err := g.Generate(context.Background(), req); err == nil {
				t.Fatal("failure automatically retried")
			}
			if calls != 1 || len(l.reservations) != 1 {
				t.Fatal("failure refunded or retried")
			}
		})
	}
}
