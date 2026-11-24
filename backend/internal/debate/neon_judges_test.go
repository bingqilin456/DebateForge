package debate

import (
	"context"
	"errors"
	"testing"
)

func TestNeonOfficialJudgesStableAndReadOnly(t *testing.T) {
	service := Service{}
	for _, id := range []string{"preset-logic", "preset-evidence", "preset-value"} {
		judge, err := service.resolveNeonJudge(context.Background(), 9, id)
		if err != nil || judge.ID != id || !judge.Official || !judge.Public || judge.Owned {
			t.Fatalf("invalid official judge %q: %+v, %v", id, judge, err)
		}
	}
	if _, err := service.resolveNeonJudge(context.Background(), 9, "unknown-preset"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown preset should be indistinguishable from absent judge: %v", err)
	}
}

func TestNeonJudgeModelWithoutPersonalConfiguration(t *testing.T) {
	service := Service{}
	model, err := service.resolveNeonJudgeModel(context.Background(), 9, "demo")
	if err != nil || model.ID != "demo" || model.Source != "local" {
		t.Fatalf("local judge option missing: %+v, %v", model, err)
	}
	if _, err := service.resolveNeonJudgeModel(context.Background(), 9, "1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unavailable stored model should be rejected: %v", err)
	}
}
