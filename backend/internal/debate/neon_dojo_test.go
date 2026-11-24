package debate

import (
	"strings"
	"testing"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
)

func TestNeonDojoQuestionIdentityAndAnswer(t *testing.T) {
	seen := map[string]bool{}
	for _, question := range builtinDojoQuestions {
		if seen[question.ID] || question.Correct < 0 || question.Correct >= len(question.Options) ||
			strings.TrimSpace(question.Explanation) == "" {
			t.Fatalf("内置题无效: %s", question.ID)
		}
		seen[question.ID] = true
	}
	evidence := dojoEvidence{"结论需要更多证据。", ai.AnnotationInsufficientSupport, "论据不足", "加可核对的例子"}
	item := dojoHistoryKey(evidence)
	if item.Quote != evidence.Quote || item.Options[item.Correct] != "补充可核对的证据" ||
		!strings.Contains(item.Explanation, evidence.Reason) {
		t.Fatalf("历史题未保留可核对的原句或既有批注: %+v", item)
	}
	evidence.Reason = "修改过评语"
	retry := dojoHistoryKey(evidence)
	if item.ID != retry.ID || item.Correct != retry.Correct {
		t.Fatalf("批注修改后题目身份不稳定: %+v", retry)
	}
}

func TestNeonDojoRejectsInvalidPersonalization(t *testing.T) {
	for _, raw := range []string{
		`{"title":" ", "guide":"填入证据"}`,
		`{"title":"论据", "guide":"填入证据", "quote":"编造的原话"}`,
		`{"title":"论据", "guide":"填入证据"} {}`,
	} {
		if _, ok := parseDojoModelOutput(raw); ok {
			t.Fatalf("接受无效个性化内容: %s", raw)
		}
	}
	if _, ok := parseDojoModelOutput(`{"title":"论据", "guide":"填入证据"}`); !ok {
		t.Fatal("有效个性化内容未通过")
	}
}
