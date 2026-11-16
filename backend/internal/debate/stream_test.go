package debate

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/gin-gonic/gin"
)

type streamingTestGenerator struct {
	genFunc
	stream func(context.Context, ai.Request, func(string) error) (ai.Result, error)
}

func TestSSEFramesHeartbeatAndSafeJSON(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("request_id", "request-test")
	writer := newTurnEventWriter(c)
	if err := writer.event("start", map[string]string{"turn_id": "4", "request_id": "request-test"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.event("delta", map[string]string{"text": "中文🌱\n\nevent: done"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.heartbeat(); err != nil {
		t.Fatal(err)
	}
	if got := w.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatal(got)
	}
	body := w.Body.String()
	if !strings.Contains(body, "event: start\ndata: ") || !strings.Contains(body, ": keepalive\n\n") || strings.Contains(body, "\n\nevent: done") || !strings.HasSuffix(body, "\n\n") {
		t.Fatal(body)
	}
}

type failingFlushWriter struct {
	*httptest.ResponseRecorder
	err error
}

func (w failingFlushWriter) FlushError() error { return w.err }

func TestSSEFlushFailureIsReported(t *testing.T) {
	want := errors.New("connection flush failed")
	c, _ := gin.CreateTestContext(failingFlushWriter{httptest.NewRecorder(), want})
	if err := newTurnEventWriter(c).event("start", map[string]string{"turn_id": "4"}); !errors.Is(err, want) {
		t.Fatalf("flush error was lost: %v", err)
	}
}

func TestSSEHeartbeatWhileGenerationBlocked(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/stream", nil)
	c.Set("request_id", "request-test")
	work := func(ctx context.Context, stream *turnStream) (TurnData, int, error) {
		if err := stream.start("4"); err != nil {
			return TurnData{}, 0, err
		}
		select {
		case <-time.After(25 * time.Millisecond):
		case <-ctx.Done():
			return TurnData{}, 0, ctx.Err()
		}
		return TurnData{}, 0, &Failure{Status: 504, Code: "model_timeout"}
	}
	serveTurnStream(c, work, 5*time.Millisecond)
	body, _ := io.ReadAll(w.Result().Body)
	if w.Code != 200 || !strings.Contains(string(body), ": keepalive") || !strings.Contains(string(body), "event: error") || strings.Contains(string(body), "event: done") {
		t.Fatalf("status=%d body=%s", w.Code, body)
	}
}

func (g streamingTestGenerator) Stream(ctx context.Context, req ai.Request, emit func(string) error) (ai.Result, error) {
	return g.stream(ctx, req, emit)
}

func TestStreamUsesSharedClaimAndCompletion(t *testing.T) {
	store := &testStore{}
	started := false
	var text string
	gen := streamingTestGenerator{stream: func(ctx context.Context, req ai.Request, emit func(string) error) (ai.Result, error) {
		if !started || store.claiming || req.UserContent != "原文" {
			t.Fatal("stream must start after claim with stored input")
		}
		for _, s := range []string{"中文", "🌱"} {
			if err := emit(s); err != nil {
				return ai.Result{}, err
			}
		}
		return ai.Result{Content: "中文🌱"}, nil
	}}
	service := NewService(store, gen, NewGate(nil))
	in := SubmitInput{1, "原文", "550e8400-e29b-41d4-a716-446655440000"}
	data, status, err := service.submitTurn(context.Background(), 1, 3, in, &turnStream{
		start: func(id string) error {
			if id != "4" || store.completed {
				t.Fatal("invalid start")
			}
			started = true
			return nil
		},
		delta: func(s string) error { text += s; return nil },
	})
	if err != nil || status != 201 || !store.completed || text != "中文🌱" || data.Turn.Status != "completed" {
		t.Fatalf("result status=%d err=%v text=%q", status, err, text)
	}
}

func TestStreamWriteFailureCancelsAndSavesFailure(t *testing.T) {
	store := &testStore{}
	wrote := errors.New("connection closed")
	gen := streamingTestGenerator{stream: func(ctx context.Context, req ai.Request, emit func(string) error) (ai.Result, error) {
		return ai.Result{}, emit("片段")
	}}
	service := NewService(store, gen, NewGate(nil))
	_, _, err := service.submitTurn(context.Background(), 1, 3, SubmitInput{1, "原文", "550e8400-e29b-41d4-a716-446655440000"}, &turnStream{start: func(string) error { return nil }, delta: func(string) error { return wrote }})
	if err == nil || store.completed || store.failCode == "" || store.failContext != nil {
		t.Fatalf("failure not persisted safely: %v", err)
	}
}

func TestStreamGeneratingReplayStaysJSON(t *testing.T) {
	store := &testStore{replay: true}
	service := NewService(store, nil, NewGate(nil))
	_, status, err := service.submitTurn(context.Background(), 1, 3, SubmitInput{1, "原文", "550e8400-e29b-41d4-a716-446655440000"}, &turnStream{start: func(string) error { t.Fatal("202 must not start SSE"); return nil }})
	if err != nil || status != 202 {
		t.Fatalf("status=%d err=%v", status, err)
	}
}
