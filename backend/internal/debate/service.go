package debate

import (
	"context"
	"database/sql"
	"errors"
	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"sync"
	"time"
)

type Service struct {
	store         Store
	generator     ai.Generator
	gate          *Gate
	leaseInterval time.Duration
	stopMu        sync.Mutex
	stopLease     chan struct{}
	startupDone   chan struct{}
}

func NewService(store Store, generator ai.Generator, gate *Gate) *Service {
	if gate == nil {
		gate = NewGate(nil)
	}
	return &Service{store: store, generator: generator, gate: gate, leaseInterval: LeaseScanInterval}
}

func (s *Service) SetLeaseScanInterval(d time.Duration) {
	s.stopMu.Lock()
	defer s.stopMu.Unlock()
	s.leaseInterval = d
}

func (s *Service) RecoverExpiredLeases(ctx context.Context) (int, error) {
	if s == nil || s.store == nil {
		return 0, nil
	}
	return s.store.RecoverExpiredLeases(ctx)
}

// StartLeaseRecovery 在服务启动时立即执行一次过期租约扫描，并按固定间隔（默认 30 秒）循环回收。
func (s *Service) StartLeaseRecovery(interval time.Duration) {
	s.stopMu.Lock()
	if interval <= 0 {
		interval = s.leaseInterval
	}
	if interval <= 0 {
		interval = LeaseScanInterval
	}
	if s.stopLease != nil {
		close(s.stopLease)
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	s.stopLease = stop
	s.startupDone = done
	s.stopMu.Unlock()

	go func() {
		defer func() {
			if done != nil {
				close(done)
			}
		}()
		if _, err := s.RecoverExpiredLeases(context.Background()); err != nil && errors.Is(err, sql.ErrConnDone) {
			return
		}
		close(done)
		done = nil
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if _, err := s.RecoverExpiredLeases(context.Background()); err != nil && errors.Is(err, sql.ErrConnDone) {
					return
				}
			}
		}
	}()
}

func (s *Service) StopLeaseRecovery() {
	s.stopMu.Lock()
	defer s.stopMu.Unlock()
	if s.stopLease != nil {
		close(s.stopLease)
		s.stopLease = nil
	}
}

func (s *Service) waitStartupScan(ctx context.Context) error {
	s.stopMu.Lock()
	done := s.startupDone
	s.stopMu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) Create(ctx context.Context, uid, tid uint64, stance string) (Debate, error) {
	if uid == 0 || tid == 0 || (stance != "pro" && stance != "con") {
		return Debate{}, ErrInvalidRequest
	}
	if err := s.waitStartupScan(ctx); err != nil {
		return Debate{}, dependency(err, false)
	}
	return s.store.Create(ctx, uid, tid, stance)
}
func (s *Service) List(ctx context.Context, uid uint64, page, pageSize int) (DebateList, error) {
	if uid == 0 || page < 1 || page > MaxPage || pageSize < 1 || pageSize > MaxPageSize {
		return DebateList{}, ErrInvalidRequest
	}
	if err := s.waitStartupScan(ctx); err != nil {
		return DebateList{}, dependency(err, false)
	}
	return s.store.List(ctx, uid, page, pageSize)
}
func (s *Service) Detail(ctx context.Context, uid, id uint64) (Detail, error) {
	if err := s.waitStartupScan(ctx); err != nil {
		return Detail{}, dependency(err, false)
	}
	return s.store.Detail(ctx, uid, id)
}
func turnData(d Detail, t Turn) TurnData {
	return TurnData{t.DTO(), d.Debate.CompletedRounds, "/api/v1/debates/" + formatID(d.Debate.ID)}
}
func failedDetails(d Debate, t Turn) *FailureDetails {
	return &FailureDetails{formatID(t.ID), "failed", t.AttemptVersion, "/api/v1/debates/" + formatID(d.ID)}
}

func (s *Service) Submit(ctx context.Context, uid, id uint64, in SubmitInput) (TurnData, int, error) {
	if err := s.gate.Request(uid); err != nil {
		return TurnData{}, 0, err
	}
	return s.submit(ctx, uid, id, in)
}

func (s *Service) Retry(ctx context.Context, uid, debateID, turnID uint64, in RetryInput) (TurnData, int, error) {
	if err := s.gate.Request(uid); err != nil {
		return TurnData{}, 0, err
	}
	return s.retry(ctx, uid, debateID, turnID, in)
}

// submit 由已完成 HTTP 防刷计数的 Handler 调用，避免有效请求被重复计数。
func (s *Service) submit(ctx context.Context, uid, id uint64, in SubmitInput) (TurnData, int, error) {
	in, err := ValidateSubmit(in)
	if err != nil {
		return TurnData{}, 0, err
	}
	if err := s.waitStartupScan(ctx); err != nil {
		return TurnData{}, 0, dependency(err, false)
	}
	var permit *Permit
	consumed := false
	// 覆盖认领失败、生成失败、取消、panic 和正常完成；不在锁内排队。
	defer func() { permit.Finish(consumed) }()
	d, fresh, err := s.store.Claim(ctx, uid, id, in, func() error { var e error; permit, e = s.gate.Acquire(uid); return e })
	if err != nil {
		var f *Failure
		if errors.As(err, &f) && f.CommitUnknown {
			consumed = true
		}
		return TurnData{}, 0, err
	}
	if !fresh {
		if len(d.Turns) != 1 {
			return TurnData{}, 0, dependency(nil, false)
		}
		turn := d.Turns[0]
		switch turn.Status {
		case "completed":
			return turnData(d, turn), 200, nil
		case "generating":
			return turnData(d, turn), 202, nil
		case "failed":
			return TurnData{}, 0, &Failure{Code: "turn_failed", Status: 409, Cause: ErrTurnFailed, Details: failedDetails(d.Debate, turn)}
		}
		return TurnData{}, 0, dependency(nil, false)
	}
	consumed = true
	return s.runGeneration(ctx, uid, d, 201)
}

// retry 仅在 status=failed 且 expected_attempt_version 匹配时认领并复用已存原文重新生成。
func (s *Service) retry(ctx context.Context, uid, debateID, turnID uint64, in RetryInput) (TurnData, int, error) {
	in, err := ValidateRetry(in)
	if err != nil {
		return TurnData{}, 0, err
	}
	if err := s.waitStartupScan(ctx); err != nil {
		return TurnData{}, 0, dependency(err, false)
	}
	var permit *Permit
	consumed := false
	defer func() { permit.Finish(consumed) }()
	d, fresh, err := s.store.ClaimRetry(ctx, uid, debateID, turnID, in.ExpectedAttemptVersion, func() error { var e error; permit, e = s.gate.Acquire(uid); return e })
	if err != nil {
		var f *Failure
		if errors.As(err, &f) && f.CommitUnknown {
			consumed = true
		}
		return TurnData{}, 0, err
	}
	if !fresh {
		if len(d.Turns) != 1 {
			return TurnData{}, 0, dependency(nil, false)
		}
		turn := d.Turns[0]
		switch turn.Status {
		case "completed":
			return turnData(d, turn), 200, nil
		case "generating":
			return turnData(d, turn), 202, nil
		case "failed":
			return TurnData{}, 0, &Failure{Code: "attempt_version_conflict", Status: 409, Cause: ErrAttemptVersionConflict, Details: failedDetails(d.Debate, turn)}
		}
		return TurnData{}, 0, dependency(nil, false)
	}
	consumed = true
	return s.runGeneration(ctx, uid, d, 200)
}

func (s *Service) runGeneration(ctx context.Context, uid uint64, d Detail, successStatus int) (TurnData, int, error) {
	if len(d.Turns) == 0 {
		return TurnData{}, 0, dependency(nil, false)
	}
	turn := d.Turns[len(d.Turns)-1]
	if len(d.Turns) != turn.RoundNo || len(turn.Messages) == 0 || turn.Messages[0].Role != "user" {
		return TurnData{}, 0, dependency(nil, false)
	}
	completedTurns := make([]ai.CompletedTurn, 0, turn.RoundNo-1)
	for i := 0; i < len(d.Turns)-1; i++ {
		pt := d.Turns[i]
		if pt.RoundNo != i+1 || len(pt.Messages) != 2 || pt.Messages[0].Role != "user" || pt.Messages[1].Role != "assistant" {
			return TurnData{}, 0, dependency(nil, false)
		}
		completedTurns = append(completedTurns, ai.CompletedTurn{
			RoundNo:          pt.RoundNo,
			UserContent:      pt.Messages[0].Content,
			AssistantContent: pt.Messages[1].Content,
		})
	}
	result, genErr := s.generate(ctx, ai.Request{
		UserID:         uid,
		Topic:          ai.Topic{Title: d.Debate.TopicTitleSnapshot, Description: d.Debate.TopicDescriptionSnapshot},
		UserStance:     ai.Stance(d.Debate.UserStance),
		RoundNo:        turn.RoundNo,
		CompletedTurns: completedTurns,
		UserContent:    turn.Messages[0].Content,
	})
	if genErr == nil {
		genErr = ai.ValidateReply(result.Content)
		if failure, ok := genErr.(*ai.Error); ok {
			clone := *failure
			clone.Usage = result.Usage
			clone.Metadata = result.Metadata
			genErr = &clone
		}
	}
	if ctx.Err() != nil {
		genErr = generationContextError(genErr, result, ctx.Err())
	}
	if genErr != nil {
		code, status, usage, meta := classifyFailure(ctx, genErr)
		return TurnData{}, 0, s.saveFailure(ctx, uid, d.Debate, turn, code, status, usage, meta, genErr)
	}
	completed, e := s.store.Complete(ctx, uid, d.Debate, turn, result)
	if e != nil {
		var failure *Failure
		if errors.As(e, &failure) && failure.CommitUnknown {
			// 可能已经完成，不能把未知提交结果当成确定回滚或失败。
			return TurnData{}, 0, dependency(e, true)
		}
		code, status := "dependency_unavailable", 503
		if ctx.Err() != nil {
			code, status, _, _ = classifyFailure(ctx, generationContextError(nil, result, ctx.Err()))
		}
		return TurnData{}, 0, s.saveFailure(ctx, uid, d.Debate, turn, code, status, result.Usage, result.Metadata, e)
	}
	return turnData(completed, completed.Turns[0]), successStatus, nil
}
func (s *Service) generate(ctx context.Context, req ai.Request) (result ai.Result, err error) {
	callCtx, cancel := context.WithTimeout(ctx, ai.DefaultModelTimeout)
	defer cancel()
	defer func() {
		if recover() != nil {
			result = ai.Result{}
			err = &ai.Error{Code: ai.CodeModelError, Message: "模型调用失败"}
		}
	}()
	if s.generator == nil {
		return result, &ai.Error{Code: ai.CodeModelUnavailable}
	}
	result, err = s.generator.Generate(callCtx, req)
	if callCtx.Err() != nil {
		err = generationContextError(err, result, callCtx.Err())
	}
	return
}

// 分类不能覆盖已知计费用量；Error 与成功 Result 均可能携带供应商已返回的数据。
func generationContextError(err error, result ai.Result, reason error) error {
	usage, meta := result.Usage, result.Metadata
	var original *ai.Error
	if errors.As(err, &original) {
		usage, meta = original.Usage, original.Metadata
	}
	code := ai.CodeModelCanceled
	if errors.Is(reason, context.DeadlineExceeded) {
		code = ai.CodeModelTimeout
	}
	return (&ai.Error{Code: code, Message: "生成请求已中断", Usage: usage, Metadata: meta}).WithCause(errors.Join(reason, err))
}
func (s *Service) saveFailure(ctx context.Context, uid uint64, d Debate, t Turn, code string, status int, usage ai.Usage, meta ai.Metadata, cause error) error {
	// 生成阶段和确定回滚的最终写回都在取消后尽力落库，仍由状态/版本保护。
	cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer stop()
	saved, err := s.store.Fail(cleanup, uid, d, t, code, usage, meta)
	if err != nil {
		return dependency(errors.Join(err, cause), false)
	}
	return &Failure{Code: codeForHTTP(code), Status: status, Cause: cause, Details: failedDetails(d, saved)}
}
func classifyFailure(ctx context.Context, err error) (string, int, ai.Usage, ai.Metadata) {
	code, status := "model_error", 502
	var usage ai.Usage
	var meta ai.Metadata
	var e *ai.Error
	if errors.As(err, &e) {
		usage, meta = e.Usage, e.Metadata
		switch e.Code {
		case ai.CodeModelTimeout:
			code, status = "model_timeout", 504
		case ai.CodeModelUnavailable, ai.CodeQuotaExceeded, ai.CodeRateLimited:
			code, status = "model_unavailable", 503
		case ai.CodeModelCanceled:
			code, status = "generation_cancelled", 503
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		code, status = "model_timeout", 504
	}
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		code, status = "generation_cancelled", 503
	}
	return code, status, usage, meta
}
func codeForHTTP(code string) string {
	if code == "generation_cancelled" {
		return "model_unavailable"
	}
	return code
}
