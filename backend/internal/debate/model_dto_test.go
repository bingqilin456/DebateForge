package debate

import (
	"testing"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
)

// TestDebateDTOCarriesModeAndRounds 确认 DTO 把赛制、流派与总轮数一并投影，
// 前端据此推导轮次上限，不再自行硬编码。
func TestDebateDTOCarriesModeAndRounds(t *testing.T) {
	cases := []struct {
		mode   string
		style  string
		want   string
		rounds int
	}{
		{string(ai.ModeStandard4), string(ai.StyleAggressive), "standard_4", 4},
		{string(ai.ModeQuick3), string(ai.StyleData), "quick_3", 3},
	}
	for _, tc := range cases {
		dto := Debate{ID: 1, UserStance: "pro", Mode: tc.mode, AIStyle: tc.style}.DTO()
		if dto.Mode != tc.want {
			t.Fatalf("模式 %q 应投影为 %q，实际 %q", tc.mode, tc.want, dto.Mode)
		}
		if dto.AIStyle != tc.style {
			t.Fatalf("流派 %q 应原样投影，实际 %q", tc.style, dto.AIStyle)
		}
		if dto.TotalRounds != tc.rounds {
			t.Fatalf("%q 的总轮数应为 %d，实际 %d", tc.mode, tc.rounds, dto.TotalRounds)
		}
	}
}

// TestDebateDTOFallsBackConsistently 覆盖历史行的防御分支：
// 赛制无法识别时必须同时回退 mode 与 total_rounds，不能只改其一，
// 否则前端会因 mode 为空而把整个响应判为格式错误。
func TestDebateDTOFallsBackConsistently(t *testing.T) {
	dto := Debate{ID: 1, UserStance: "con", Mode: "", AIStyle: "unknown-style"}.DTO()
	if dto.Mode != string(ai.ModeQuick3) {
		t.Fatalf("空赛制应回退为 quick_3，实际 %q", dto.Mode)
	}
	if dto.TotalRounds != 3 {
		t.Fatalf("空赛制回退后总轮数应为 3，实际 %d", dto.TotalRounds)
	}
	if dto.AIStyle != string(ai.StyleLogical) {
		t.Fatalf("未知流派应回退为 logical，实际 %q", dto.AIStyle)
	}
	// 回退后的组合必须是前端可校验的自洽组合。
	if !ai.Mode(dto.Mode).Valid() {
		t.Fatalf("回退后的赛制 %q 仍不合法", dto.Mode)
	}
}
