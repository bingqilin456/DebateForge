package debate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
)

func TestNeonSharedReportRedactsPrivateModelConfigurationAndExportsSavedText(t *testing.T) {
	verdict := neonVerdict{
		Score: &neonVerdictScore{UserTotal: 75, AssistantTotal: 70, Outcome: "win", Source: "ai"},
		Seats: []neonVerdictSeat{{Status: "completed", Source: "ai", Reason: "论据扎实", ModelAlias: "private-alias", ModelID: "private-id", JudgeName: "private-name"}, {Status: "failed", Reason: "private-failure"}},
	}
	report := newNeonReport(
		Detail{Debate: Debate{TopicTitleSnapshot: "校园议题", UserStance: "con"}, Turns: []Turn{
			{RoundNo: 1, Status: "completed", ClientRequestID: "secret-request", Messages: []Message{{Role: "user", Content: "原始辩词"}, {Role: "assistant", Content: "反驳辩词"}}},
			{RoundNo: 2, Status: "failed", Messages: []Message{{Role: "user", Content: "private-draft"}}},
		}},
		&ai.Review{KeyIssues: []ai.ReviewText{{Text: "关键争点", Quote: &ai.ReviewQuote{MessageID: "private-message"}}}},
		nil,
		verdict,
		[]AnnotationItemDTO{{TurnNo: 1, Role: "user", Label: "我方亮点", Reason: "原话清晰"}},
	)
	payload, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"private-", "secret-request"} {
		if strings.Contains(string(payload), private) {
			t.Fatalf("public DTO leaked %s: %s", private, payload)
		}
	}
	text := renderNeonReportMarkdown(report)
	for _, saved := range []string{"校园议题", "反方", "原始辩词", "反驳辩词", "关键争点", "论据扎实", "原话清晰", "75"} {
		if !strings.Contains(text, saved) {
			t.Errorf("saved text missing in Markdown: %s", saved)
		}
	}
	if strings.Contains(text, "private-") {
		t.Fatalf("export leaked private content: %s", text)
	}
}
