package debate

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/auth"
	"github.com/gin-gonic/gin"
)

// 产品 V2 训练赛的 HTTP 入口。它们与 V1 轮次端点共用同一认证、Origin/CSRF、
// 并发门禁与 SSE 传输层，只是状态机换成「阶段 + 发言步骤」。

// limitTrainingGeneration 是训练赛生成入口的防刷计数。
// 它复用 Gate 的同一计数器与每日额度，只是分钟上限高于 V1：
// 一场训练赛最多 26 个正式步骤，沿用每分钟 6 次会把正常对局卡死。
func (h *Handler) limitTrainingGeneration(c *gin.Context) {
	current, _ := auth.CurrentUserFrom(c)
	if err := h.service.gate.TrainingRequest(current.User.ID); err != nil {
		serviceError(c, err)
		return
	}
	c.Next()
}

// createTraining 创建训练赛会话（mode 由服务端固定，客户端不可指定）。
func (h *Handler) createTraining(c *gin.Context) {
	var in struct {
		TopicID    string `json:"topic_id"`
		UserStance string `json:"user_stance"`
		AIStyle    string `json:"ai_style"`
	}
	if !decode(c, &in) {
		return
	}
	style := in.AIStyle
	if style == "" {
		style = "logical"
	}
	id, e := ParseID(in.TopicID)
	if e != nil {
		serviceError(c, e)
		return
	}
	current, ok := auth.CurrentUserFrom(c)
	if !ok {
		writeError(c, 401, "unauthenticated", nil)
		return
	}
	d, e := h.service.CreateTraining(c.Request.Context(), current.User.ID, id, in.UserStance, style)
	if e != nil {
		serviceError(c, e)
		return
	}
	writeData(c, 201, d.DTO())
}

// submitStep 提交下一步：Content 为空表示请服务端生成当前 AI 步骤，
// 非空表示用户发言。发言方与阶段由服务端权威进度判定。
func (h *Handler) submitStep(c *gin.Context) {
	id, e := ParseID(c.Param("id"))
	if e != nil {
		serviceError(c, e)
		return
	}
	var in TrainingStepInput
	if !decode(c, &in) {
		return
	}
	current, _ := auth.CurrentUserFrom(c)
	data, status, e := h.service.SubmitTrainingStep(c.Request.Context(), current.User.ID, id, in, nil)
	if e != nil {
		serviceError(c, e)
		return
	}
	writeData(c, status, data)
}

// retryStep 显式重试失败的 AI 步骤；须携带期望版本号防止覆盖新尝试。
func (h *Handler) retryStep(c *gin.Context) {
	id, e := ParseID(c.Param("id"))
	if e != nil {
		serviceError(c, e)
		return
	}
	stepNo, e := ParseID(c.Param("step_no"))
	if e != nil || stepNo > trainingMaxSteps {
		serviceError(c, ErrInvalidRequest)
		return
	}
	var in TrainingRetryInput
	if !decode(c, &in) {
		return
	}
	current, _ := auth.CurrentUserFrom(c)
	data, status, e := h.service.RetryTrainingStep(c.Request.Context(), current.User.ID, id, int(stepNo), in)
	if e != nil {
		serviceError(c, e)
		return
	}
	writeData(c, status, data)
}

// endFreeDebate 结束自由辩并进入结辩；服务端确认，不调用模型。
func (h *Handler) endFreeDebate(c *gin.Context) {
	id, e := ParseID(c.Param("id"))
	if e != nil {
		serviceError(c, e)
		return
	}
	current, _ := auth.CurrentUserFrom(c)
	var in TrainingEndInput
	if c.Request.ContentLength != 0 && !decode(c, &in) {
		return
	}
	data, status, e := h.service.EndFreeDebate(c.Request.Context(), current.User.ID, id, in)
	if e != nil {
		serviceError(c, e)
		return
	}
	writeData(c, status, data)
}

// streamStep 是训练赛的流式提交入口：复用 V1 的 SSE 传输与同一认领内核，
// 只有最终事务提交成功才发送 done；202 与幂等重放仍返回普通 JSON。
func (h *Handler) streamStep(c *gin.Context) {
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
	var in TrainingStepInput
	if !decode(c, &in) {
		return
	}
	current, _ := auth.CurrentUserFrom(c)
	serveTrainingStream(c, func(ctx context.Context, stream *turnStream) (TrainingStepResult, int, error) {
		return h.service.SubmitTrainingStep(ctx, current.User.ID, id, in, stream)
	}, 15*time.Second)
}

// serveTrainingStream 与 serveTurnStream 同构：一个响应循环独占 Writer，
// 有界工作协程只传临时事件；done 只在最终事务提交成功后发送。
func serveTrainingStream(c *gin.Context, work func(context.Context, *turnStream) (TrainingStepResult, int, error), heartbeat time.Duration) {
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	type event struct {
		name string
		data any
	}
	type outcome struct {
		data   TrainingStepResult
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
			start: func(id string) error {
				return send(event{"start", gin.H{"claim_id": id, "request_id": requestID}})
			},
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
				claimID := out.data.Step.ID
				if err := writer.event("start", gin.H{"claim_id": claimID, "request_id": requestID}); err != nil {
					return
				}
			}
			_ = writer.event("done", gin.H{"step": out.data.Step, "progress": out.data.Progress, "debate_status": out.data.DebateStatus})
			return
		}
	}
}
