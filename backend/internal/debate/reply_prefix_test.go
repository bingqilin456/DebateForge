package debate

import (
	"context"
	"strings"
	"testing"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
)

func TestCleanReplyPrefix(t *testing.T) {
	for _, tc := range []struct {
		input string
		final bool
		want  string
		ready bool
	}{
		{"[第二轮 反", false, "[第二轮 反", false},
		{"[第2轮 反方二辩]（攻辩）：请看具体依据。", false, "请看具体依据。", true},
		{"【第四轮】", false, "", false},
		{"（据现有分析）这个结论仍有争议。", true, "（据现有分析）这个结论仍有争议。", true},
		{"（对方给了多个例子）这里可以逐项比较。", true, "（对方给了多个例子）这里可以逐项比较。", true},
		{"[参考", true, "[参考", true},
	} {
		got, ready := cleanReplyPrefix(tc.input, tc.final)
		if got != tc.want || ready != tc.ready {
			t.Errorf("cleanReplyPrefix(%q, %v) = (%q, %v), want (%q, %v)", tc.input, tc.final, got, ready, tc.want, tc.ready)
		}
	}
}

type prefixedGenerator struct {
	chunks []string
}

func (g prefixedGenerator) Generate(context.Context, ai.Request) (ai.Result, error) {
	return ai.Result{Content: strings.Join(g.chunks, "")}, nil
}

func (g prefixedGenerator) Stream(ctx context.Context, req ai.Request, emit func(string) error) (ai.Result, error) {
	for _, chunk := range g.chunks {
		if err := emit(chunk); err != nil {
			return ai.Result{}, err
		}
	}
	return g.Generate(ctx, req)
}

func TestGenerateTurnKeepsCleanStreamAndReplyWithHistory(t *testing.T) {
	chunks := []string{"[第", "三轮 反方三辩]", "：请看", "依据。"}
	service := Service{generator: prefixedGenerator{chunks}}
	history := []ai.CompletedTurn{{RoundNo: 1, UserContent: "主张", AssistantContent: "反驳"}}
	for round := 2; round <= 4; round++ {
		req := ai.Request{RoundNo: round, CompletedTurns: history}
		var visible strings.Builder
		result, err := service.generateTurn(context.Background(), req, func(chunk string) error {
			visible.WriteString(chunk)
			return nil
		})
		if err != nil || result.Content != "请看依据。" || visible.String() != result.Content {
			t.Fatalf("round %d: reply=%q stream=%q err=%v", round, result.Content, visible.String(), err)
		}
		history = append(history, ai.CompletedTurn{RoundNo: round, UserContent: "继续论证", AssistantContent: result.Content})
	}
}
