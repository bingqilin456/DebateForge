package debate

import (
	"context"
	"errors"
	"sync"
	"time"
	"unicode/utf8"
)

var (
	ErrRepracticeSource  = errors.New("重练必须引用本场完赛后本人已保存的原文")
	ErrRepracticeInput   = errors.New("重练输入无效或请求编号已被使用")
	ErrRepracticeVersion = errors.New("未找到该重练版本")
)

type RepracticeInput struct {
	RequestID            string
	MessageID            uint64
	Start, End           int
	Quote, Goal, Rewrite string
}

type RepracticeFeedback struct {
	Change      string   `json:"change"`
	Reason      string   `json:"reason"`
	NewProblem  string   `json:"new_problem"`
	BeforeScore *float64 `json:"before_score,omitempty"`
	AfterScore  *float64 `json:"after_score,omitempty"`
}

type RepracticeVersion struct {
	ID, Attempt    uint64
	Status         string
	Input          RepracticeInput
	Feedback       RepracticeFeedback
	LeaseExpiresAt time.Time
}

// Repractice 校验完赛原文并管理独立的改写版本。
// 记录在模型调用前保存，重试只推进已有失败版本，不写正式发言。
type Repractice struct {
	detail        func(context.Context, uint64, uint64) (Detail, error)
	generate      func(context.Context, RepracticeInput) (RepracticeFeedback, error)
	generateOwned func(context.Context, Detail, RepracticeInput) (RepracticeFeedback, error)
	mu            sync.Mutex
	sessions      map[[2]uint64][]RepracticeVersion
	store         func(context.Context, uint64, uint64, bool, func(*[]RepracticeVersion) error) error
}

func NewRepractice(detail func(context.Context, uint64, uint64) (Detail, error), generate func(context.Context, RepracticeInput) (RepracticeFeedback, error)) *Repractice {
	return &Repractice{detail: detail, generate: generate, sessions: make(map[[2]uint64][]RepracticeVersion)}
}

func (r *Repractice) completed(ctx context.Context, owner, id uint64) (Detail, error) {
	d, err := r.detail(ctx, owner, id)
	if err != nil {
		return Detail{}, err
	}
	if owner == 0 || id == 0 || d.Debate.ID != id || d.Debate.UserID != owner || d.Debate.Status != "completed" {
		return Detail{}, ErrRepracticeSource
	}
	return d, nil
}

func (r *Repractice) List(ctx context.Context, owner, id uint64) ([]RepracticeVersion, error) {
	if _, err := r.completed(ctx, owner, id); err != nil {
		return nil, err
	}
	out := []RepracticeVersion{}
	err := r.withVersions(ctx, owner, id, false, func(versions *[]RepracticeVersion) error {
		out = append(out, (*versions)...)
		return nil
	})
	return out, err
}

func (r *Repractice) withVersions(ctx context.Context, owner, id uint64, write bool, apply func(*[]RepracticeVersion) error) error {
	if r.store != nil {
		return r.store(ctx, owner, id, write, apply)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := [2]uint64{owner, id}
	versions := r.sessions[key]
	if err := apply(&versions); err != nil {
		return err
	}
	if write {
		r.sessions[key] = versions
	}
	return nil
}

func (r *Repractice) Submit(ctx context.Context, owner, id uint64, in RepracticeInput) (RepracticeVersion, error) {
	d, err := r.completed(ctx, owner, id)
	if err != nil {
		return RepracticeVersion{}, err
	}
	if isBlankText(in.RequestID) || len(in.RequestID) > 128 || !utf8.ValidString(in.Rewrite) || !utf8.ValidString(in.Goal) || isBlankText(in.Rewrite) || utf8.RuneCountInString(in.Rewrite) > 2000 || utf8.RuneCountInString(in.Goal) > 2000 {
		return RepracticeVersion{}, ErrRepracticeInput
	}
	if !repracticeSourceMatches(d, in) {
		return RepracticeVersion{}, ErrRepracticeSource
	}
	if in.Rewrite == in.Quote {
		return RepracticeVersion{}, ErrRepracticeInput
	}
	var version RepracticeVersion
	fresh := false
	err = r.withVersions(ctx, owner, id, true, func(versions *[]RepracticeVersion) error {
		for _, v := range *versions {
			if v.Input.RequestID == in.RequestID {
				if v.Input != in {
					return ErrRepracticeInput
				}
				version = v
				return nil
			}
		}
		version = RepracticeVersion{
			ID: uint64(len(*versions)) + 1, Attempt: 1, Status: "generating", Input: in,
			LeaseExpiresAt: time.Now().UTC().Add(LeaseDuration),
		}
		*versions = append(*versions, version)
		fresh = true
		return nil
	})
	if err != nil || !fresh {
		return version, err
	}
	return r.finish(ctx, d, version)
}

func repracticeSourceMatches(d Detail, in RepracticeInput) bool {
	if in.MessageID == 0 || isBlankText(in.Quote) || !utf8.ValidString(in.Quote) || in.End <= in.Start {
		return false
	}
	if d.Training != nil {
		for _, step := range d.Training.Steps {
			if step.ID == in.MessageID && step.Speaker == "user" && VerifiedQuote(step.Content, in.Start, in.End, in.Quote) {
				return true
			}
		}
		return false
	}
	for _, turn := range d.Turns {
		if turn.Status != "completed" {
			continue
		}
		for _, message := range turn.Messages {
			if message.ID == in.MessageID && message.Role == "user" && VerifiedQuote(message.Content, in.Start, in.End, in.Quote) {
				return true
			}
		}
	}
	return false
}

func (r *Repractice) Retry(ctx context.Context, owner, id, version, attempt uint64) (RepracticeVersion, error) {
	d, err := r.completed(ctx, owner, id)
	if err != nil {
		return RepracticeVersion{}, err
	}
	var v RepracticeVersion
	fresh := false
	err = r.withVersions(ctx, owner, id, true, func(versions *[]RepracticeVersion) error {
		if version == 0 || version > uint64(len(*versions)) {
			return ErrRepracticeVersion
		}
		v = (*versions)[version-1]
		if v.Status != "failed" || v.Attempt != attempt {
			return nil
		}
		if !repracticeSourceMatches(d, v.Input) {
			return ErrRepracticeSource
		}
		v.Attempt++
		v.Status = "generating"
		v.LeaseExpiresAt = time.Now().UTC().Add(LeaseDuration)
		(*versions)[version-1] = v
		fresh = true
		return nil
	})
	if err != nil || !fresh {
		return v, err
	}
	return r.finish(ctx, d, v)
}

func (r *Repractice) finish(ctx context.Context, detail Detail, v RepracticeVersion) (RepracticeVersion, error) {
	var feedback RepracticeFeedback
	var err error
	if r.generateOwned != nil {
		feedback, err = r.generateOwned(ctx, detail, v.Input)
	} else {
		feedback, err = r.generate(ctx, v.Input)
	}
	if err != nil || isBlankText(feedback.Change) || isBlankText(feedback.Reason) {
		v.Status = "failed"
	} else {
		v.Status = "completed"
		v.Feedback = feedback
	}
	v.LeaseExpiresAt = time.Time{}
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	err = r.withVersions(saveCtx, detail.Debate.UserID, detail.Debate.ID, true, func(versions *[]RepracticeVersion) error {
		current := (*versions)[v.ID-1]
		if current.Status != "generating" || current.Attempt != v.Attempt {
			return ErrAttemptVersionConflict
		}
		(*versions)[v.ID-1] = v
		return nil
	})
	return v, err
}
