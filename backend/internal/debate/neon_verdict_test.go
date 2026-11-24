package debate

import (
	"testing"
	"time"
)

func TestNeonVerdictPartialAndSettledScore(t *testing.T) {
	high := []float64{100, 80, 60, 80, 80}
	low := []float64{20, 20, 20, 20, 20}
	now := time.Now()
	view := neonVerdict{Seats: []neonVerdictSeat{
		{Status: "completed", Source: "ai", User: high, Assistant: low},
		{Status: "completed", Source: "ai", User: high, Assistant: low},
		{Status: "generating", Source: "ai", Lease: now.Add(-time.Second)},
	}}
	view.refresh(now)
	if view.Status != "partial" || view.Score.Source != "ai" || view.Score.UserTotal != 80 || view.Score.Outcome != "win" {
		t.Fatalf("unexpected partial verdict: %+v, score=%+v", view, view.Score)
	}
	delta := 16
	view.EloDelta = &delta
	view.Seats[2] = neonVerdictSeat{Status: "completed", Source: "ai", User: low, Assistant: high}
	view.refresh(now)
	if view.Status != "completed" || view.Score.Outcome != "win" || view.Score.UserTotal != 80 || *view.EloDelta != 16 {
		t.Fatalf("retry overwrote settled score: %+v", view)
	}
}

func TestNeonVerdictDemoSeatDoesNotInfluenceRealScore(t *testing.T) {
	high := []float64{80, 80, 80, 80, 80}
	low := []float64{20, 20, 20, 20, 20}
	view := neonVerdict{Seats: []neonVerdictSeat{
		{Status: "completed", Source: "ai", User: high, Assistant: low},
		{Status: "completed", Source: "local", User: low, Assistant: high},
	}}
	view.refresh(time.Now())
	if view.Score.Source != "ai" || view.Score.Outcome != "win" {
		t.Fatalf("demo vote influenced score: %+v", view.Score)
	}
}

func TestNeonJudgeRejectsInvalidScore(t *testing.T) {
	ok := `{"user":[1,2,3,4,5],"assistant":[6,7,8,9,10],"reason":"依据"}`
	if _, err := parseNeonJudgeOutput(ok); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{ok + "{}", `{"user":[1,2,3,4,101],"assistant":[6,7,8,9,10],"reason":"依据"}`,
		`{"user":[1,2,3,4,null],"assistant":[6,7,8,9,10],"reason":"依据"}`} {
		if _, err := parseNeonJudgeOutput(raw); err == nil {
			t.Fatalf("invalid score accepted: %s", raw)
		}
	}
}
