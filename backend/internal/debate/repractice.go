package debate

import (
	"context"
	"errors"
	"sync"
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

type RepracticeFeedback struct{ Change, Reason, NewProblem string }

type RepracticeVersion struct {
	ID, Attempt uint64
	Status      string
	Input       RepracticeInput
	Feedback    RepracticeFeedback
}

// Repractice 是隔离切片的版本存储；正式存储接入之前不挂载公开路由。
// 记录在模型调用前保存，重试只推进已有失败版本，不写正式发言。
type Repractice struct {
	detail   func(context.Context, uint64, uint64) (Detail, error)
	generate func(context.Context, RepracticeInput) (RepracticeFeedback, error)
	mu       sync.Mutex
	sessions map[[2]uint64][]RepracticeVersion
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
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]RepracticeVersion{}, r.sessions[[2]uint64{owner, id}]...), nil
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
	key := [2]uint64{owner, id}
	r.mu.Lock()
	versions := r.sessions[key]
	for _, v := range versions {
		if v.Input.RequestID != in.RequestID {
			continue
		}
		r.mu.Unlock()
		if v.Input != in {
			return RepracticeVersion{}, ErrRepracticeInput
		}
		return v, nil
	}
	v := RepracticeVersion{ID: uint64(len(versions)) + 1, Attempt: 1, Status: "generating", Input: in}
	r.sessions[key] = append(versions, v)
	r.mu.Unlock()
	return r.finish(ctx, key, v), nil
}

func repracticeSourceMatches(d Detail, in RepracticeInput) bool {
	if in.MessageID == 0 || isBlankText(in.Quote) || !utf8.ValidString(in.Quote) || in.End <= in.Start {
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
	key := [2]uint64{owner, id}
	r.mu.Lock()
	versions := r.sessions[key]
	if version == 0 || version > uint64(len(versions)) {
		r.mu.Unlock()
		return RepracticeVersion{}, ErrRepracticeVersion
	}
	v := versions[version-1]
	if v.Status != "failed" || v.Attempt != attempt {
		r.mu.Unlock()
		return v, nil
	}
	if !repracticeSourceMatches(d, v.Input) {
		r.mu.Unlock()
		return RepracticeVersion{}, ErrRepracticeSource
	}
	v.Attempt++
	v.Status = "generating"
	versions[version-1] = v
	r.mu.Unlock()
	return r.finish(ctx, key, v), nil
}

func (r *Repractice) finish(ctx context.Context, key [2]uint64, v RepracticeVersion) RepracticeVersion {
	feedback, err := r.generate(ctx, v.Input)
	if err != nil || isBlankText(feedback.Change) || isBlankText(feedback.Reason) {
		v.Status = "failed"
	} else {
		v.Status = "completed"
		v.Feedback = feedback
	}
	r.mu.Lock()
	r.sessions[key][v.ID-1] = v
	r.mu.Unlock()
	return v
}
