package debate

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestArenaProjectsOnlyCommittedPairs(t *testing.T) {
	detail := Detail{
		Debate: Debate{Mode: "standard_4", TopicTitleSnapshot: "辩题", UserStance: "con", CompletedRounds: 2},
		Turns: []Turn{
			{RoundNo: 1, Status: "completed", Messages: []Message{
				{ID: 10, Role: "user", Content: "因为存在证据。例如还有后续。"},
				{ID: 11, Role: "assistant", Content: "需要进一步比较。"},
			}},
			{RoundNo: 2, Status: "failed", Messages: []Message{{ID: 12, Role: "user", Content: "这轮不计入"}}},
			{RoundNo: 3, Status: "completed", Messages: []Message{{ID: 13, Role: "user"}, {ID: 14, Role: "assistant"}}},
		},
	}
	got := projectArena(detail)
	if len(got.Hints) != 4 || len(got.Rounds) != 1 {
		t.Fatalf("unexpected state: %+v", got)
	}
	round := got.Rounds[0]
	if round.ProPercent != 38 || round.UserPoint != "因为存在证据。" || round.UserMessageID != "10" || round.AssistantMessageID != "11" || round.Source != "local" {
		t.Fatalf("unexpected round: %+v", round)
	}
	if again := projectArena(detail); !reflect.DeepEqual(got, again) {
		t.Fatal("repeated reads change the projection")
	}
}

func TestParseNeonDifficultyUsesExplicitChoices(t *testing.T) {
	for _, value := range []struct {
		input string
		valid bool
	}{{"\"varsity\"", true}, {"\"national\"", true}, {"null", false}, {"\"custom\"", false}} {
		_, err := parseCreateRequest(createRequest{TopicID: []byte(`"1"`), UserStance: "pro", Difficulty: []byte(value.input)})
		if (err == nil) != value.valid {
			t.Fatalf("input %s: %v", value.input, err)
		}
	}
}

func TestArenaPracticeCountsOnlyCommittedFakeOutput(t *testing.T) {
	fake, real := "fake", "example-model"
	turns := []Turn{{Status: "failed", ModelName: &fake}, {Status: "completed", ModelName: &real}}
	if hasFakeArenaReply(turns) || !hasRealArenaReply(turns) {
		t.Fatal("failed attempts must not override a committed real reply")
	}
	turns = append(turns, Turn{Status: "completed", ModelName: &fake})
	if !projectArena(Detail{Turns: turns}).Practice {
		t.Fatal("completed Fake output must remain practice")
	}
}

func TestArenaGraphRequiresBothModelAnchors(t *testing.T) {
	start, end := 0, 2
	item := func(role, message, category string) AnnotationItemDTO {
		return AnnotationItemDTO{
			Category: category, Label: "批注", Reason: "理由", Improvement: "改进", Role: role, TurnNo: 1,
			Anchor: Anchor{MessageID: message, Quote: "原句", Resolved: true, StartCP: &start, EndCP: &end},
		}
	}
	items := []AnnotationItemDTO{
		item("user", "10", AnnotationStrength), item("assistant", "11", AnnotationOpponentWeakness),
	}
	payload, err := json.Marshal(map[string]any{"items": items})
	if err != nil {
		t.Fatal(err)
	}
	body, model := string(payload), "fake"
	record := &TurnAnnotation{Status: AnnotationCompleted, ContentJSON: &body, ModelName: &model}
	batches := []AnnotationBatch{{Turn: Turn{RoundNo: 1}, Annotation: record}}
	view := arenaState{Rounds: []arenaRound{{
		RoundNo: 1, Source: "local", GraphSource: "local", UserMessageID: "10", AssistantMessageID: "11",
	}}}
	applyArenaGraph(&view, batches)
	if got := view.Rounds[0].GraphSource; got != "local" {
		t.Fatalf("Fake mislabeled: %s", got)
	}
	model = "example-model"
	applyArenaGraph(&view, batches)
	if got := view.Rounds[0]; got.GraphSource != "ai" || got.UserPoint != "原句" || got.AssistantPoint != "原句" || got.Source != "local" {
		t.Fatalf("unexpected real annotation projection %+v", got)
	}
}
