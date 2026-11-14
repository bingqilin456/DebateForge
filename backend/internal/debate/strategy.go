package debate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/bingqilin456/DebateForge/backend/internal/auth"
	"github.com/gin-gonic/gin"
)

// 策略与总结的持久字段一致，表、业务边界和版本相互独立。
type Strategy Summary

func (Strategy) TableName() string { return "debate_strategies" }

type StrategyState struct {
	Debate   Debate
	Strategy *Strategy
}
type StrategyStatus struct {
	Status         string       `json:"status"`
	AttemptVersion *uint64      `json:"attempt_version"`
	ErrorCode      *string      `json:"error_code,omitempty"`
	SchemaVersion  *int         `json:"schema_version,omitempty"`
	PromptVersion  *string      `json:"prompt_version,omitempty"`
	AIStyle        string       `json:"ai_style,omitempty"`
	Content        *ai.Strategy `json:"content,omitempty"`
}

func (s *Strategy) DTO(style string) StrategyStatus {
	if s == nil {
		return StrategyStatus{Status: "not_started"}
	}
	out := StrategyStatus{Status: s.Status, AttemptVersion: &s.AttemptVersion}
	if s.Status == "failed" {
		out.ErrorCode = s.ErrorCode
	}
	if s.Status == "completed" && s.ContentJSON != nil {
		if parsed, err := ai.ParseStrategy(*s.ContentJSON); err == nil {
			out.Content = &parsed
			out.SchemaVersion = s.SchemaVersion
			out.PromptVersion = s.PromptVersion
			out.AIStyle = style
		}
	}
	return out
}

type StrategyInput struct {
	Retry                  bool
	ExpectedAttemptVersion *uint64
}

// 仅策略入口严格拒绝未知键、重复键和 null；保留旧接口解码行为。
func (in *StrategyInput) UnmarshalJSON(raw []byte) error {
	*in = StrategyInput{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return ErrInvalidRequest
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return ErrInvalidRequest
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return ErrInvalidRequest
		}
		seen[key] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return ErrInvalidRequest
		}
		switch key {
		case "retry":
			if bytes.Equal(value, []byte("null")) || json.Unmarshal(value, &in.Retry) != nil {
				return ErrInvalidRequest
			}
		case "expected_attempt_version":
			var version uint64
			if json.Unmarshal(value, &version) != nil || version == 0 || version == ^uint64(0) {
				return ErrInvalidRequest
			}
			in.ExpectedAttemptVersion = &version
		default:
			return ErrInvalidRequest
		}
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return ErrInvalidRequest
	}
	if _, err = decoder.Token(); !errors.Is(err, io.EOF) {
		return ErrInvalidRequest
	}
	return validateStrategyInput(*in)
}
func validateStrategyInput(in StrategyInput) error {
	if in.Retry != (in.ExpectedAttemptVersion != nil) {
		return ErrInvalidRequest
	}
	if in.ExpectedAttemptVersion != nil && (*in.ExpectedAttemptVersion == 0 || *in.ExpectedAttemptVersion == ^uint64(0)) {
		return ErrInvalidRequest
	}
	return nil
}
func (h *Handler) getStrategy(c *gin.Context) {
	id, err := ParseID(c.Param("id"))
	if err != nil {
		serviceError(c, err)
		return
	}
	current, _ := auth.CurrentUserFrom(c)
	data, err := h.service.GetStrategy(c.Request.Context(), current.User.ID, id)
	if err != nil {
		serviceError(c, err)
		return
	}
	writeData(c, 200, data)
}
func (h *Handler) strategy(c *gin.Context) {
	id, err := ParseID(c.Param("id"))
	if err != nil {
		serviceError(c, err)
		return
	}
	var in StrategyInput
	if !decode(c, &in) {
		return
	}
	current, _ := auth.CurrentUserFrom(c)
	data, status, err := h.service.strategy(c.Request.Context(), current.User.ID, id, in)
	if err != nil {
		serviceError(c, err)
		return
	}
	writeData(c, status, data)
}
func (s *Service) GetStrategy(ctx context.Context, uid, id uint64) (StrategyStatus, error) {
	if err := s.waitStartupScan(ctx); err != nil {
		return StrategyStatus{}, dependency(err, false)
	}
	state, err := s.store.GetStrategy(ctx, uid, id)
	if err != nil {
		return StrategyStatus{}, err
	}
	return state.Strategy.DTO(state.Debate.AIStyle), nil
}
func (s *Service) Strategy(ctx context.Context, uid, id uint64, in StrategyInput) (StrategyStatus, int, error) {
	if err := s.gate.Request(uid); err != nil {
		return StrategyStatus{}, 0, err
	}
	return s.strategy(ctx, uid, id, in)
}
func (s *Service) strategy(ctx context.Context, uid, id uint64, in StrategyInput) (StrategyStatus, int, error) {
	if err := validateStrategyInput(in); err != nil {
		return StrategyStatus{}, 0, err
	}
	if err := s.waitStartupScan(ctx); err != nil {
		return StrategyStatus{}, 0, dependency(err, false)
	}
	var permit *Permit
	consumed := false
	defer func() { permit.Finish(consumed) }()
	state, fresh, err := s.store.ClaimStrategy(ctx, uid, id, in, func() error { var e error; permit, e = s.gate.Acquire(uid); return e })
	if err != nil {
		var f *Failure
		if errors.As(err, &f) && f.CommitUnknown {
			consumed = true
		}
		return StrategyStatus{}, 0, err
	}
	if state.Strategy == nil {
		return StrategyStatus{}, 0, dependency(nil, false)
	}
	current := *state.Strategy
	if !fresh {
		if current.Status == "failed" && in.Retry {
			return StrategyStatus{}, 0, &Failure{Code: "attempt_version_conflict", Status: 409, Cause: ErrAttemptVersionConflict, Details: failedStrategyDetails(state.Debate, current)}
		}
		status := 200
		if current.Status == "generating" {
			status = 202
		}
		return current.DTO(state.Debate.AIStyle), status, nil
	}
	consumed = true
	return s.runStrategyGeneration(ctx, uid, state, in.Retry)
}
func failedStrategyDetails(d Debate, s Strategy) *FailureDetails {
	return &FailureDetails{Status: "failed", AttemptVersion: s.AttemptVersion, QueryURL: "/api/v1/debates/" + formatID(d.ID) + "/strategy"}
}
func (s *Service) runStrategyGeneration(ctx context.Context, uid uint64, state StrategyState, retry bool) (StrategyStatus, int, error) {
	d, current := state.Debate, *state.Strategy
	result, genErr := s.generateStrategy(ctx, ai.StrategyRequest{UserID: uid, Mode: ai.Mode(d.Mode), AIStyle: ai.AIStyle(d.AIStyle), Topic: ai.Topic{Title: d.TopicTitleSnapshot, Description: d.TopicDescriptionSnapshot}, UserStance: ai.Stance(d.UserStance)})
	validating := genErr == nil
	// 不信任 Generator 输出，包括 Fake/未来适配器；落库前再次执行 schema 校验。
	if genErr == nil {
		raw := result.ContentJSON
		if raw == "" {
			b, e := json.Marshal(result.Strategy)
			if e != nil {
				genErr = e
			} else {
				raw = string(b)
			}
		}
		if genErr == nil {
			parsed, e := ai.ParseStrategy(raw)
			genErr = e
			if e == nil {
				b, e := json.Marshal(parsed)
				genErr = e
				result.Strategy = parsed
				result.ContentJSON = string(b)
				result.SchemaVersion = ai.StrategySchemaVersion
				if len(b) > ai.MaxStrategyJSONBytes {
					genErr = &ai.Error{Code: ai.CodeModelError, Message: "策略 JSON 超过大小上限"}
				}
			}
		}
	}
	if validating && genErr != nil {
		genErr = (&ai.Error{Code: ai.CodeModelError, Message: "策略结构无效", Usage: result.Usage, Metadata: result.Metadata}).WithCause(genErr)
	}
	// Prompt 版本由本次服务端生成路径确定，失败也留下可追踪版本。
	result.Metadata.PromptVersion = ai.StrategyPromptVersion
	if genErr != nil {
		var ae *ai.Error
		if errors.As(genErr, &ae) {
			clone := *ae
			clone.Metadata.PromptVersion = ai.StrategyPromptVersion
			genErr = &clone
		}
	}
	if ctx.Err() != nil {
		genErr = strategyContextError(genErr, result, ctx.Err())
	}
	if genErr != nil {
		code, status, usage, meta := classifyFailure(ctx, genErr)
		if meta.PromptVersion == "" {
			meta = result.Metadata
			usage = result.Usage
		}
		return StrategyStatus{}, 0, s.saveStrategyFailure(ctx, uid, d, current, code, status, usage, meta, genErr)
	}
	completed, err := s.store.CompleteStrategy(ctx, uid, d, current, result)
	if err != nil {
		var failure *Failure
		if errors.As(err, &failure) && failure.CommitUnknown {
			return StrategyStatus{}, 0, dependency(err, true)
		}
		code, status := "dependency_unavailable", 503
		if ctx.Err() != nil {
			code, status, _, _ = classifyFailure(ctx, strategyContextError(nil, result, ctx.Err()))
		}
		return StrategyStatus{}, 0, s.saveStrategyFailure(ctx, uid, d, current, code, status, result.Usage, result.Metadata, err)
	}
	status := 201
	if retry {
		status = 200
	}
	return completed.DTO(d.AIStyle), status, nil
}
func (s *Service) generateStrategy(ctx context.Context, req ai.StrategyRequest) (result ai.StrategyResult, err error) {
	callCtx, cancel := context.WithTimeout(ctx, ai.DefaultModelTimeout)
	defer cancel()
	defer func() {
		if recover() != nil {
			result = ai.StrategyResult{}
			err = &ai.Error{Code: ai.CodeModelError}
		}
	}()
	// 只有显式具备策略能力的生成器才能调用。现有真实试运行包装器不暴露此能力，
	// 避免绕过其专用费用账本；真实 Demo 的装配在另行授权任务实现。
	generator, ok := s.generator.(ai.StrategyGenerator)
	if !ok {
		return result, &ai.Error{Code: ai.CodeModelUnavailable}
	}
	result, err = generator.GenerateStrategy(callCtx, req)
	if callCtx.Err() != nil {
		err = strategyContextError(err, result, callCtx.Err())
	}
	return
}
func strategyContextError(err error, result ai.StrategyResult, reason error) error {
	return generationContextError(err, ai.Result{Usage: result.Usage, Metadata: result.Metadata}, reason)
}
func (s *Service) saveStrategyFailure(ctx context.Context, uid uint64, d Debate, current Strategy, code string, status int, usage ai.Usage, meta ai.Metadata, cause error) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	saved, err := s.store.FailStrategy(cleanup, uid, d, current, code, usage, meta)
	if err != nil {
		return dependency(errors.Join(err, cause), false)
	}
	return &Failure{Code: codeForHTTP(code), Status: status, Cause: cause, Details: failedStrategyDetails(d, saved)}
}
