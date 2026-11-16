package debate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/auth"
	"github.com/gin-gonic/gin"
)

func (h *Handler) stream(c *gin.Context) {
	accepts := false
	for _, media := range strings.Split(c.GetHeader("Accept"), ",") {
		if strings.TrimSpace(media) == "text/event-stream" {
			accepts = true
		}
	}
	if !accepts {
		writeError(c, 400, "invalid_request", nil)
		return
	}
	id, err := ParseID(c.Param("id"))
	if err != nil {
		serviceError(c, err)
		return
	}
	var in SubmitInput
	if !decode(c, &in) {
		return
	}
	current, _ := auth.CurrentUserFrom(c)
	serveTurnStream(c, func(ctx context.Context, stream *turnStream) (TurnData, int, error) {
		return h.service.submitTurn(ctx, current.User.ID, id, in, stream)
	}, 15*time.Second)
}

type turnEventWriter struct {
	c       *gin.Context
	started bool
}

func newTurnEventWriter(c *gin.Context) *turnEventWriter { return &turnEventWriter{c: c} }

func (w *turnEventWriter) event(name string, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if !w.started {
		w.c.Header("Content-Type", "text/event-stream")
		w.c.Header("Cache-Control", "no-cache")
		w.c.Header("X-Accel-Buffering", "no")
		w.c.Status(http.StatusOK)
		w.started = true
	}
	if _, err = fmt.Fprintf(w.c.Writer, "event: %s\ndata: %s\n\n", name, raw); err != nil {
		return err
	}
	return w.flush()
}

func (w *turnEventWriter) heartbeat() error {
	if _, err := w.c.Writer.Write([]byte(": keepalive\n\n")); err != nil {
		return err
	}
	return w.flush()
}

func (w *turnEventWriter) flush() error {
	w.c.Writer.WriteHeaderNow()
	writer := http.ResponseWriter(w.c.Writer)
	// Gin 的 Flush 无返回值；下探其 Writer，保留 net/http 的 FlushError。
	if unwrap, ok := writer.(interface{ Unwrap() http.ResponseWriter }); ok {
		writer = unwrap.Unwrap()
	}
	return http.NewResponseController(writer).Flush()
}

// 一个响应循环独占 Writer；有界工作协程只传临时事件，断线取消模型与后续回调。
func serveTurnStream(c *gin.Context, work func(context.Context, *turnStream) (TurnData, int, error), heartbeat time.Duration) {
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	type event struct {
		name string
		data any
	}
	type outcome struct {
		data   TurnData
		status int
		err    error
	}
	events := make(chan event)
	result := make(chan outcome, 1)
	send := func(e event) error {
		select {
		case events <- e:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	requestID := c.GetString("request_id")
	go func() {
		defer func() {
			if recover() != nil {
				result <- outcome{err: dependency(nil, false)}
			}
		}()
		data, status, err := work(ctx, &turnStream{
			start: func(id string) error { return send(event{"start", gin.H{"turn_id": id, "request_id": requestID}}) },
			delta: func(text string) error { return send(event{"delta", gin.H{"text": text}}) },
		})
		result <- outcome{data, status, err}
	}()
	writer := newTurnEventWriter(c)
	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()
	for {
		select {
		case e := <-events:
			if err := writer.event(e.name, e.data); err != nil {
				return
			}
		case <-ticker.C:
			if writer.started {
				if err := writer.heartbeat(); err != nil {
					return
				}
			}
		case <-ctx.Done():
			return
		case out := <-result:
			if out.err != nil {
				if !writer.started {
					serviceError(c, out.err)
					return
				}
				code := "dependency_unavailable"
				var failure *Failure
				if errors.As(out.err, &failure) {
					code = failure.Code
				}
				_ = writer.event("error", gin.H{"error": gin.H{"code": code, "message": "请求未完成，请查看会话状态后再操作"}, "request_id": requestID})
				return
			}
			if out.status == http.StatusAccepted {
				writeData(c, out.status, out.data)
				return
			}
			if !writer.started {
				if err := writer.event("start", gin.H{"turn_id": out.data.Turn.ID, "request_id": requestID}); err != nil {
					return
				}
			}
			_ = writer.event("done", gin.H{"turn": out.data.Turn, "completed_rounds": out.data.CompletedRounds, "status": out.data.DebateStatus})
			return
		}
	}
}
