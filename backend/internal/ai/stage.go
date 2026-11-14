package ai

// Stage 是一场辩论中的环节语义。环节由 (赛制, 轮号) 唯一决定，
// 不单独存储，避免产生第二个事实源；第 3 轮是两种赛制的分叉点：
// quick_3 第 3 轮为结辩，standard_4 第 3 轮为自由辩、第 4 轮才是结辩。
type Stage string

const (
	// StageOpening 立论：双方各自建立论证框架。
	StageOpening Stage = "opening"
	// StageRebuttal 驳立论：攻击对方前提与推理，quick_3 中称攻辩。
	StageRebuttal Stage = "rebuttal"
	// StageFreeDebate 自由辩：短促高频交锋，仅 standard_4 有。
	StageFreeDebate Stage = "free_debate"
	// StageClosing 结辩：收束主线并回应核心交锋点。
	StageClosing Stage = "closing"
)

// stageByMode 是 (赛制, 轮号) 到环节的唯一映射表。
// quick_3 没有自由辩；standard_4 把自由辩插在第 3 轮，结辩后移到第 4 轮。
var stageByMode = map[Mode][]Stage{
	ModeQuick3:    {StageOpening, StageRebuttal, StageClosing},
	ModeStandard4: {StageOpening, StageRebuttal, StageFreeDebate, StageClosing},
}

// StageFor 返回该赛制指定轮号的环节。轮号越界时返回空环节，
// 调用方须先通过 ValidateRequest 或 RoundsFor 保证轮号合法。
func StageFor(m Mode, roundNo int) Stage {
	stages, ok := stageByMode[m]
	if !ok || roundNo < 1 || roundNo > len(stages) {
		return ""
	}
	return stages[roundNo-1]
}

// StagesFor 返回该赛制的完整环节序列，供前端展示进度与测试校验。
func StagesFor(m Mode) []Stage {
	stages, ok := stageByMode[m]
	if !ok {
		return nil
	}
	out := make([]Stage, len(stages))
	copy(out, stages)
	return out
}

// label 是写入中文 Prompt 与前端展示的固定措辞，不接受外部输入。
func (s Stage) label() string {
	switch s {
	case StageOpening:
		return "立论"
	case StageRebuttal:
		return "驳立论"
	case StageFreeDebate:
		return "自由辩"
	case StageClosing:
		return "结辩"
	default:
		return "未知环节"
	}
}

// Label 导出环节的中文名，供上层构造响应；未知环节返回受控占位文本。
func (s Stage) Label() string { return s.label() }

// label 是赛制的中文名。
func (m Mode) label() string {
	switch m {
	case ModeQuick3:
		return "三轮快速赛（立论/攻辩/结辩）"
	case ModeStandard4:
		return "四轮标准赛（立论/驳立论/自由辩/结辩）"
	default:
		return "未知赛制"
	}
}

// Label 导出赛制的中文名。
func (m Mode) Label() string { return m.label() }

// stageRequirement 返回该轮环节对应的额外 Prompt 指令。
// 不同环节的落点不同：立论建框架、驳立论打前提、自由辩短促交锋、结辩收束主线。
func stageRequirement(m Mode, roundNo int) string {
	switch StageFor(m, roundNo) {
	case StageOpening:
		return "【本轮环节：立论】请建立清晰的核心判断标准，给出两到三条支撑理由，并预判对方可能的攻击方向。\n\n"
	case StageRebuttal:
		return "【本轮环节：驳立论】请先正面攻击对方上一轮的核心前提或推理跳跃，再补强己方立论，不要重复已说过的内容。\n\n"
	case StageFreeDebate:
		return "【本轮环节：自由辩】请用两到三个短促交锋点直接回应对方，每个点一句话，不要展开成长篇论述。\n\n"
	case StageClosing:
		return "【本轮环节：结辩】请收束整场主线：回应对方最有力的一个交锋点，重申己方核心判断标准，并给出明确的结论。\n\n"
	default:
		return ""
	}
}
