package ai

import "testing"

// TestRoundsForEachMode 锁定赛制到轮数的唯一映射，
// 防止有人重新引入固定轮数常量或改错某一模式的上限。
func TestRoundsForEachMode(t *testing.T) {
	cases := map[Mode]int{
		ModeQuick3:    3,
		ModeStandard4: 4,
	}
	for mode, want := range cases {
		got, err := RoundsFor(mode)
		if err != nil {
			t.Fatalf("%s 应为合法赛制: %v", mode, err)
		}
		if got != want {
			t.Fatalf("%s 轮数上限应为 %d，实际 %d", mode, want, got)
		}
	}
}

// TestRoundsForRejectsUnknownMode 确认未知赛制不会静默降级到任何默认轮数。
func TestRoundsForRejectsUnknownMode(t *testing.T) {
	for _, mode := range []Mode{"", "quick3", "standard_5", "STANDARD_4", "standard_4 "} {
		if _, err := RoundsFor(mode); err == nil {
			t.Fatalf("未知赛制 %q 必须返回错误，不能降级", mode)
		}
	}
}

// TestModeValid 覆盖枚举合法性判断。
func TestModeValid(t *testing.T) {
	for _, mode := range []Mode{ModeQuick3, ModeStandard4} {
		if !mode.Valid() {
			t.Fatalf("%q 应为合法赛制", mode)
		}
	}
	for _, mode := range []Mode{"", "other", "quick_4"} {
		if mode.Valid() {
			t.Fatalf("%q 不应是合法赛制", mode)
		}
	}
}

// TestMaxRoundsCoversAllModes 确认最坏预算取所有赛制的最大轮数。
func TestMaxRoundsCoversAllModes(t *testing.T) {
	max := MaxRounds()
	for mode := range roundsByMode {
		rounds, err := RoundsFor(mode)
		if err != nil {
			t.Fatal(err)
		}
		if rounds > max {
			t.Fatalf("%s 的轮数 %d 超过 MaxRounds()=%d", mode, rounds, max)
		}
	}
}

// TestStageForMapsEveryRound 覆盖两种赛制下每个合法轮号的环节。
// 第 3 轮是分叉点：quick_3 为结辩，standard_4 为自由辩。
func TestStageForMapsEveryRound(t *testing.T) {
	cases := []struct {
		mode Mode
		want []Stage
	}{
		{ModeQuick3, []Stage{StageOpening, StageRebuttal, StageClosing}},
		{ModeStandard4, []Stage{StageOpening, StageRebuttal, StageFreeDebate, StageClosing}},
	}
	for _, tc := range cases {
		rounds, err := RoundsFor(tc.mode)
		if err != nil {
			t.Fatal(err)
		}
		if len(tc.want) != rounds {
			t.Fatalf("%s 的环节数 %d 与轮数 %d 不一致", tc.mode, len(tc.want), rounds)
		}
		for round := 1; round <= rounds; round++ {
			if got := StageFor(tc.mode, round); got != tc.want[round-1] {
				t.Fatalf("%s 第 %d 轮环节应为 %s，实际 %s", tc.mode, round, tc.want[round-1], got)
			}
		}
	}
}

// TestStageThirdRoundDiverges 单独锁定第 3 轮的分叉语义：
// 这是最容易在后续改动中被改错的一处。
func TestStageThirdRoundDiverges(t *testing.T) {
	if got := StageFor(ModeQuick3, 3); got != StageClosing {
		t.Fatalf("quick_3 第 3 轮应为结辩，实际 %s", got)
	}
	if got := StageFor(ModeStandard4, 3); got != StageFreeDebate {
		t.Fatalf("standard_4 第 3 轮应为自由辩，实际 %s", got)
	}
	if got := StageFor(ModeStandard4, 4); got != StageClosing {
		t.Fatalf("standard_4 第 4 轮应为结辩，实际 %s", got)
	}
}

// TestStageForOutOfRangeReturnsEmpty 确认越界轮号不会 panic 或越界读取。
func TestStageForOutOfRangeReturnsEmpty(t *testing.T) {
	for _, tc := range []struct {
		mode  Mode
		round int
	}{
		{ModeQuick3, 0}, {ModeQuick3, 4}, {ModeQuick3, 99},
		{ModeStandard4, 0}, {ModeStandard4, 5}, {ModeStandard4, -1},
		{"", 1}, {"other", 1},
	} {
		if got := StageFor(tc.mode, tc.round); got != "" {
			t.Fatalf("%s 第 %d 轮应返回空环节，实际 %q", tc.mode, tc.round, got)
		}
	}
}

// TestStagesForReturnsCopy 确认返回切片是副本，调用方修改不会污染全局映射。
func TestStagesForReturnsCopy(t *testing.T) {
	first := StagesFor(ModeStandard4)
	if len(first) != 4 {
		t.Fatalf("standard_4 应有 4 个环节，实际 %d", len(first))
	}
	first[0] = StageClosing
	if StagesFor(ModeStandard4)[0] != StageOpening {
		t.Fatal("StagesFor 返回的切片必须是副本")
	}
	if StagesFor("other") != nil {
		t.Fatal("未知赛制应返回 nil")
	}
}

// TestStageAndModeLabels 覆盖写入 Prompt 的中文措辞。
func TestStageAndModeLabels(t *testing.T) {
	if StageOpening.Label() != "立论" || StageRebuttal.Label() != "驳立论" ||
		StageFreeDebate.Label() != "自由辩" || StageClosing.Label() != "结辩" {
		t.Fatal("环节中文名不符合约定")
	}
	if Stage("other").Label() != "未知环节" {
		t.Fatal("未知环节应有受控占位文本")
	}
	if ModeStandard4.Label() == "" || ModeQuick3.Label() == "" || Mode("other").Label() != "未知赛制" {
		t.Fatal("赛制中文名不符合约定")
	}
}

// TestValidateRequestUsesModeRounds 确认请求校验的轮次上限来自赛制：
// 第 4 轮在 quick_3 下被拒、在 standard_4 下接受。
func TestValidateRequestUsesModeRounds(t *testing.T) {
	build := func(mode Mode, rounds int) Request {
		completed := make([]CompletedTurn, 0, rounds-1)
		for r := 1; r < rounds; r++ {
			completed = append(completed, CompletedTurn{RoundNo: r, UserContent: "发言", AssistantContent: "回复"})
		}
		return Request{
			Mode:           mode,
			Topic:          Topic{Title: "辩题", Description: "说明"},
			UserStance:     StancePro,
			RoundNo:        rounds,
			CompletedTurns: completed,
			UserContent:    "本轮发言",
		}
	}
	if err := ValidateRequest(build(ModeQuick3, 3)); err != nil {
		t.Fatalf("quick_3 第 3 轮应合法: %v", err)
	}
	if err := ValidateRequest(build(ModeStandard4, 4)); err != nil {
		t.Fatalf("standard_4 第 4 轮应合法: %v", err)
	}
	if err := ValidateRequest(build(ModeQuick3, 4)); err == nil {
		t.Fatal("quick_3 第 4 轮必须被拒")
	}
	if err := ValidateRequest(build(ModeStandard4, 5)); err == nil {
		t.Fatal("standard_4 第 5 轮必须被拒")
	}
}

// TestValidateRequestRejectsUnknownStyle 确认未知流派受控失败，空流派按默认处理。
func TestValidateRequestRejectsUnknownStyle(t *testing.T) {
	req := validRequest()
	req.AIStyle = "other"
	if err := ValidateRequest(req); err == nil {
		t.Fatal("未知流派必须被拒")
	}
	req.AIStyle = ""
	if err := ValidateRequest(req); err != nil {
		t.Fatalf("空流派应使用默认逻辑型: %v", err)
	}
	for _, style := range []AIStyle{StyleLogical, StyleAggressive, StyleEmpathic, StyleData} {
		req.AIStyle = style
		if err := ValidateRequest(req); err != nil {
			t.Fatalf("流派 %s 应合法: %v", style, err)
		}
	}
}

// TestStyleValidAndLabel 覆盖流派枚举与中文描述。
func TestStyleValidAndLabel(t *testing.T) {
	for _, style := range []AIStyle{StyleLogical, StyleAggressive, StyleEmpathic, StyleData} {
		if !style.Valid() || style.Label() == "" {
			t.Fatalf("流派 %s 应有合法性与中文描述", style)
		}
	}
	if AIStyle("other").Valid() {
		t.Fatal("未知流派不应合法")
	}
	if AIStyle("other").Label() != "" {
		t.Fatal("未知流派不应返回描述")
	}
}

// TestBuildPromptIncludesStageAndStyle 确认 Prompt 真的按 (赛制, 轮号) 与流派生成，
// 而不是只改了校验而未落到提示词。
func TestBuildPromptIncludesStageAndStyle(t *testing.T) {
	req := validRequest()
	req.Mode = ModeStandard4
	req.AIStyle = StyleAggressive
	req.RoundNo = 3
	req.CompletedTurns = []CompletedTurn{
		{RoundNo: 1, UserContent: "发言一", AssistantContent: "回复一"},
		{RoundNo: 2, UserContent: "发言二", AssistantContent: "回复二"},
	}
	prompt, err := BuildPrompt(req)
	if err != nil {
		t.Fatalf("合法四轮请求应能构建 Prompt: %v", err)
	}
	system := prompt.Messages[0].Content
	for _, want := range []string{"共 4 轮", "本轮环节：自由辩", "强攻型"} {
		if !contains(system, want) {
			t.Fatalf("Prompt 缺少 %q\n实际:\n%s", want, system)
		}
	}

	// 同一轮号在 quick_3 下是结辩，证明环节由 (赛制, 轮号) 共同决定。
	req.Mode = ModeQuick3
	req.RoundNo = 3
	req.CompletedTurns = req.CompletedTurns[:2]
	prompt, err = BuildPrompt(req)
	if err != nil {
		t.Fatalf("合法三轮请求应能构建 Prompt: %v", err)
	}
	system = prompt.Messages[0].Content
	for _, want := range []string{"共 3 轮", "本轮环节：结辩"} {
		if !contains(system, want) {
			t.Fatalf("Prompt 缺少 %q\n实际:\n%s", want, system)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
