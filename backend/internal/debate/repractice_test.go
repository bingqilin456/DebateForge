package debate

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRepracticeSavesIndependentVersionsAndRetries(t *testing.T) {
	calls := 0
	drill := NewRepractice(func(_ context.Context, owner, id uint64) (Detail, error) {
		if owner != 1 || id != 2 {
			return Detail{}, ErrRepracticeSource
		}
		return Detail{Debate: Debate{ID: 2, UserID: 1, Status: "completed"}, Turns: []Turn{{Status: "completed", Messages: []Message{{ID: 3, Role: "user", Content: "甲😀乙"}, {ID: 4, Role: "assistant", Content: "甲😀乙"}}}}}, nil
	}, func(_ context.Context, in RepracticeInput) (RepracticeFeedback, error) {
		calls++
		if calls == 1 {
			return RepracticeFeedback{}, errors.New("fake failure")
		}
		return RepracticeFeedback{Change: "更清楚", Reason: "补齐原因"}, nil
	})
	ctx := context.Background()
	in := RepracticeInput{RequestID: "first", MessageID: 3, Start: 1, End: 2, Quote: "😀", Rewrite: "改写"}
	unchanged := in
	unchanged.Rewrite = unchanged.Quote
	if _, err := drill.Submit(ctx, 1, 2, unchanged); !errors.Is(err, ErrRepracticeInput) || calls != 0 {
		t.Fatalf("unchanged quote must not create a version or call the generator: err=%v, calls=%d", err, calls)
	}
	if list, err := drill.List(ctx, 1, 2); err != nil || len(list) != 0 || calls != 0 {
		t.Fatal(list, err, calls)
	}
	for _, bad := range []RepracticeInput{{RequestID: "bad", MessageID: 4, Start: 1, End: 2, Quote: "😀", Rewrite: "改写"}, {RequestID: "bad", MessageID: 3, Start: 0, End: 1, Quote: "😀", Rewrite: "改写"}} {
		if _, err := drill.Submit(ctx, 1, 2, bad); !errors.Is(err, ErrRepracticeSource) {
			t.Fatal(err)
		}
	}
	if _, err := drill.Submit(ctx, 9, 2, in); !errors.Is(err, ErrRepracticeSource) {
		t.Fatal(err)
	}
	first, err := drill.Submit(ctx, 1, 2, in)
	if err != nil || first.Status != "failed" || first.Input.Rewrite != "改写" {
		t.Fatal(first, err)
	}
	if replay, err := drill.Submit(ctx, 1, 2, in); err != nil || replay.ID != first.ID || calls != 1 {
		t.Fatal(replay, err, calls)
	}
	in.Rewrite = "不同内容"
	if _, err := drill.Submit(ctx, 1, 2, in); !errors.Is(err, ErrRepracticeInput) {
		t.Fatal(err)
	}
	in.Rewrite = "改写"
	done, err := drill.Retry(ctx, 1, 2, first.ID, first.Attempt)
	if err != nil || done.ID != first.ID || done.Status != "completed" || done.Feedback.Change != "更清楚" || calls != 2 {
		t.Fatal(done, err, calls)
	}
	if _, err := drill.Retry(ctx, 1, 2, first.ID, first.Attempt); err != nil || calls != 2 {
		t.Fatal(err, calls)
	}
	in.RequestID = "second"
	in.Rewrite = "不同改写"
	if _, err := drill.Submit(ctx, 1, 2, in); err != nil {
		t.Fatal(err)
	}
	if list, err := drill.List(ctx, 1, 2); err != nil || len(list) != 2 || list[0].Input.Rewrite != "改写" || calls != 3 {
		t.Fatal(list, err, calls)
	}
}

func TestRepracticeRejectsUnfinishedOrUncommittedAndCountsCodepoints(t *testing.T) {
	ctx := context.Background()
	detail := Detail{Debate: Debate{ID: 2, UserID: 1, Status: "in_progress"}, Turns: []Turn{{Status: "generating", Messages: []Message{{ID: 3, Role: "user", Content: "原句"}}}}}
	calls := 0
	drill := NewRepractice(func(context.Context, uint64, uint64) (Detail, error) { return detail, nil }, func(context.Context, RepracticeInput) (RepracticeFeedback, error) {
		calls++
		return RepracticeFeedback{Change: "是", Reason: "说清了"}, nil
	})
	in := RepracticeInput{RequestID: "length", MessageID: 3, Start: 0, End: 2, Quote: "原句", Rewrite: "改写"}
	if _, err := drill.Submit(ctx, 1, 2, in); !errors.Is(err, ErrRepracticeSource) {
		t.Fatal(err)
	}
	detail.Debate.Status = "completed"
	if _, err := drill.Submit(ctx, 1, 2, in); !errors.Is(err, ErrRepracticeSource) {
		t.Fatal(err)
	}
	detail.Turns[0].Status = "completed"
	if _, err := drill.Submit(ctx, 9, 2, in); !errors.Is(err, ErrRepracticeSource) {
		t.Fatal(err)
	}
	in.Rewrite = "\u0085\ufeff"
	if _, err := drill.Submit(ctx, 1, 2, in); !errors.Is(err, ErrRepracticeInput) {
		t.Fatal(err)
	}
	in.Rewrite = strings.Repeat("😀", 2001)
	if _, err := drill.Submit(ctx, 1, 2, in); !errors.Is(err, ErrRepracticeInput) {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal(calls)
	}
	in.Rewrite = strings.Repeat("😀", 2000)
	if result, err := drill.Submit(ctx, 1, 2, in); err != nil || result.Status != "completed" || calls != 1 {
		t.Fatal(result.Status, err, calls)
	}
}
