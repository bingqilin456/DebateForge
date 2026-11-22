package debate

import (
	"testing"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
)

// trainingDebate 构造一个只用于纯函数校验的训练赛行。
func trainingDebate(stance string) Debate {
	stage := string(TrainingStageOpening)
	return Debate{ID: 1, UserStance: stance, Mode: string(TrainingMode), TrainingStage: &stage, Status: "active"}
}

// simulatedStep 是一次模拟提交的正式步骤：阶段、阶段内步、自由辩回合、发言方
// 与它在会话内的 1-based 全局步骤编号。
type simulatedStep struct {
	Stage   TrainingStage
	StageNo int
	Round   int
	Speaker ai.Speaker
	StepNo  int
}

// simulateTraining 按服务端权威推进模拟整场训练赛：
// 提交一个正式步骤 → 记录其序号与发言方 → 推进阶段 → 重复。
// 自由辩满 maxRounds 回合后模拟用户显式「进入结辩」（结辩不由提交自动触发）。
func simulateTraining(d Debate, maxRounds int) ([]simulatedStep, Debate) {
	steps := []simulatedStep{}
	for guard := 0; guard <= trainingMaxSteps+2; guard++ {
		pos, err := trainingPosition(d, len(steps))
		if err != nil {
			panic(err)
		}
		if pos.Stage == TrainingStageFreeDebate && pos.StageStep == 0 && pos.FreeRounds >= maxRounds {
			d.TrainingStage = ptr(string(TrainingStageClosing))
			d.TrainingStageStep = 0
			continue
		}
		speaker, ok := pos.nextSpeaker()
		if !ok {
			return steps, d
		}
		round := 0
		if pos.Stage == TrainingStageFreeDebate {
			round = pos.FreeRounds + 1
		}
		steps = append(steps, simulatedStep{Stage: pos.Stage, StageNo: pos.StageStep + 1, Round: round, Speaker: speaker, StepNo: pos.nextStepNo()})
		if err := advanceTraining(&d); err != nil {
			panic(err)
		}
	}
	panic("training simulation did not finish")
}

// TestTrainingSpeakerSequenceMatchesPRD 锁定 PRD V2 §3.2 的两种立场发言序列。
func TestTrainingSpeakerSequenceMatchesPRD(t *testing.T) {
	pro, _ := simulateTraining(trainingDebate("pro"), 1)
	wantPro := []simulatedStep{
		{TrainingStageOpening, 1, 0, ai.SpeakerUser, 1},
		{TrainingStageOpening, 2, 0, ai.SpeakerAssistant, 2},
		{TrainingStageRebuttal, 1, 0, ai.SpeakerUser, 3},
		{TrainingStageRebuttal, 2, 0, ai.SpeakerAssistant, 4},
		{TrainingStageFreeDebate, 1, 1, ai.SpeakerUser, 5},
		{TrainingStageFreeDebate, 2, 1, ai.SpeakerAssistant, 6},
		{TrainingStageClosing, 1, 0, ai.SpeakerAssistant, 7},
		{TrainingStageClosing, 2, 0, ai.SpeakerUser, 8},
	}
	assertSimulation(t, "用户选正方", pro, wantPro)
	// 自由辩末回合 AI 回复（步骤 6）与结辩 AI 发言（步骤 7）相邻同方且跨阶段。
	if pro[5].Speaker != ai.SpeakerAssistant || pro[6].Speaker != ai.SpeakerAssistant || pro[5].Stage == pro[6].Stage {
		t.Fatalf("用户选正方时自由辩末回合 AI 回复应与结辩 AI 发言相邻同方，实际 %v", pro)
	}

	con, _ := simulateTraining(trainingDebate("con"), 1)
	wantCon := []simulatedStep{
		{TrainingStageOpening, 1, 0, ai.SpeakerAssistant, 1},
		{TrainingStageOpening, 2, 0, ai.SpeakerUser, 2},
		{TrainingStageRebuttal, 1, 0, ai.SpeakerAssistant, 3},
		{TrainingStageRebuttal, 2, 0, ai.SpeakerUser, 4},
		{TrainingStageFreeDebate, 1, 1, ai.SpeakerUser, 5},
		{TrainingStageFreeDebate, 2, 1, ai.SpeakerAssistant, 6},
		{TrainingStageClosing, 1, 0, ai.SpeakerUser, 7},
		{TrainingStageClosing, 2, 0, ai.SpeakerAssistant, 8},
	}
	assertSimulation(t, "用户选反方", con, wantCon)
	if con[0].Speaker != ai.SpeakerAssistant {
		t.Fatal("用户选反方时第一步必须由 AI 先发言")
	}
	// 步骤 4（用户驳立论）与步骤 5（用户自由辩首发）相邻同方且跨阶段。
	if con[3].Speaker != ai.SpeakerUser || con[4].Speaker != ai.SpeakerUser || con[3].Stage == con[4].Stage {
		t.Fatalf("用户选反方时驳立论末步与自由辩首步应连续同方，实际 %v", con)
	}
}

func assertSimulation(t *testing.T, name string, got, want []simulatedStep) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s：步数 %d，期望 %d（%v）", name, len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s：第 %d 步 %+v，期望 %+v", name, i+1, got[i], want[i])
		}
	}
}

// TestTrainingStepNumberFormula 校验 PRD V2 §3.2 的步骤编号公式：
// 自由辩第 k 回合占 2k+3 与 2k+4，结辩占 2n+5 与 2n+6，共 2n+6 步。
func TestTrainingStepNumberFormula(t *testing.T) {
	for _, rounds := range []int{1, 2, 5, 10} {
		steps, final := simulateTraining(trainingDebate("pro"), rounds)
		if want := 2*rounds + 6; len(steps) != want {
			t.Fatalf("自由辩 %d 回合应有 %d 步，实际 %d", rounds, want, len(steps))
		}
		if final.Status != "completed" {
			t.Fatalf("自由辩 %d 回合后状态应为 completed，实际 %q", rounds, final.Status)
		}
		if final.CompletedRounds != rounds {
			t.Fatalf("已完成自由辩回合数应为 %d，实际 %d", rounds, final.CompletedRounds)
		}
		for k := 1; k <= rounds; k++ {
			userStep, aiStep := steps[2*k+2], steps[2*k+3]
			if userStep.StepNo != 2*k+3 || aiStep.StepNo != 2*k+4 {
				t.Fatalf("第 %d 回合步骤编号应为 %d/%d，实际 %d/%d", k, 2*k+3, 2*k+4, userStep.StepNo, aiStep.StepNo)
			}
			if userStep.Speaker != ai.SpeakerUser || aiStep.Speaker != ai.SpeakerAssistant {
				t.Fatalf("第 %d 回合必须用户先发言、AI 后回复，实际 %v/%v", k, userStep.Speaker, aiStep.Speaker)
			}
		}
		// 结辩固定占用最后两步 2n+5、2n+6。
		if steps[len(steps)-2].StepNo != 2*rounds+5 || steps[len(steps)-1].StepNo != 2*rounds+6 {
			t.Fatalf("结辩步骤编号应为 %d/%d，实际 %d/%d", 2*rounds+5, 2*rounds+6, steps[len(steps)-2].StepNo, steps[len(steps)-1].StepNo)
		}
	}
}

// TestTrainingMaxRoundsAndBoundaries 覆盖 0/1/10 回合的结束边界。
func TestTrainingMaxRoundsAndBoundaries(t *testing.T) {
	freeDebate := func(rounds, stageStep int) Debate {
		d := trainingDebate("pro")
		d.TrainingStage = ptr(string(TrainingStageFreeDebate))
		d.TrainingStageStep = stageStep
		d.CompletedRounds = rounds
		return d
	}

	// 0 回合：不允许结束自由辩。
	pos0, err := trainingPosition(freeDebate(0, 0), 4)
	if err != nil {
		t.Fatal(err)
	}
	if can, reason := endFreeDebateAvailability(pos0, nil); can || reason == nil || *reason != "no_completed_round" {
		t.Fatalf("0 回合必须被拒绝结束，实际 can=%v reason=%v", can, reason)
	}

	// 完成 1 回合：允许结束。
	pos1, err := trainingPosition(freeDebate(1, 0), 6)
	if err != nil {
		t.Fatal(err)
	}
	if can, reason := endFreeDebateAvailability(pos1, nil); !can || reason != nil {
		t.Fatalf("完成 1 回合后应允许结束，实际 can=%v reason=%v", can, reason)
	}

	// 10 回合完成：不再接受新发言，但仍可显式结束。
	pos10, err := trainingPosition(freeDebate(ai.TrainingMaxFreeDebateRounds, 0), 24)
	if err != nil {
		t.Fatal(err)
	}
	if speaker, ok := pos10.nextSpeaker(); ok {
		t.Fatalf("10 回合完成后不得再接受新发言，实际发言方 %v", speaker)
	}
	if can, reason := endFreeDebateAvailability(pos10, nil); !can || reason != nil {
		t.Fatalf("10 回合完成后应允许显式结束，实际 can=%v reason=%v", can, reason)
	}

	// 第 11 回合不存在：即便脏数据把 rounds 推过 10 也无法构造合法位置。
	if _, err := trainingPosition(freeDebate(11, 0), 26); err == nil {
		t.Fatal("自由辩回合数超过 10 必须被拒绝")
	}
}

// TestTrainingEndWhileReplyInProgress 覆盖 AI 回复中、未完成回合与失败放弃的结束判定。
func TestTrainingEndWhileReplyInProgress(t *testing.T) {
	d := trainingDebate("pro")
	d.TrainingStage = ptr(string(TrainingStageFreeDebate))
	d.TrainingStageStep = 1 // 用户已发言，等待 AI
	d.CompletedRounds = 1
	// committed=5 表示自由辩第 1 回合的 AI 回复（第 6 步）失败、未占编号。
	pos, err := trainingPosition(d, 5)
	if err != nil {
		t.Fatal(err)
	}
	// AI 回复生成中：不可结束。
	generating := []TrainingClaim{{ID: 1, StepNo: 6, Status: "generating"}}
	if can, reason := endFreeDebateAvailability(pos, generating); can || reason == nil || *reason != "reply_in_progress" {
		t.Fatalf("AI 回复中必须拒绝结束，实际 can=%v reason=%v", can, reason)
	}
	// AI 回复失败且已完成 ≥1 回合：允许放弃并进入结辩。
	failed := []TrainingClaim{{ID: 1, StepNo: 6, Status: "failed"}}
	if can, reason := endFreeDebateAvailability(pos, failed); !can || reason != nil {
		t.Fatalf("失败回复且已有完整回合时应允许放弃进入结辩，实际 can=%v reason=%v", can, reason)
	}
	// 失败但 0 完整回合：仍不可结束。
	d0 := d
	d0.CompletedRounds = 0
	pos0, err := trainingPosition(d0, 4)
	if err != nil {
		t.Fatal(err)
	}
	if can, reason := endFreeDebateAvailability(pos0, failed); can || reason == nil || *reason != "no_completed_round" {
		t.Fatalf("0 回合失败也不可跳过自由辩，实际 can=%v reason=%v", can, reason)
	}
	// 非自由辩阶段一律不可结束。
	opening, err := trainingPosition(trainingDebate("pro"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if can, reason := endFreeDebateAvailability(opening, nil); can || reason == nil || *reason != "not_free_debate" {
		t.Fatalf("立论阶段不可结束自由辩，实际 can=%v reason=%v", can, reason)
	}
}

// TestTrainingPositionRejectsInconsistentRows 覆盖脏数据防御：自洽校验必须失败而非静默纠正。
func TestTrainingPositionRejectsInconsistentRows(t *testing.T) {
	stage := string(TrainingStageOpening)
	base := Debate{ID: 1, UserStance: "pro", Mode: string(TrainingMode), TrainingStage: &stage, Status: "active"}
	cases := []struct {
		name      string
		mutate    func(*Debate)
		committed int
	}{
		{"未知阶段", func(d *Debate) { d.TrainingStage = ptr("other") }, 0},
		{"阶段内步数越界", func(d *Debate) { d.TrainingStageStep = 3 }, 0},
		{"自由辩回合越界", func(d *Debate) { d.CompletedRounds = 11 }, 0},
		{"已提交步数超过名义值", func(*Debate) {}, 5},
		{"结辩但无自由辩回合", func(d *Debate) {
			d.TrainingStage = ptr(string(TrainingStageClosing))
			d.TrainingStageStep = 0
		}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := base
			tc.mutate(&d)
			if _, err := trainingPosition(d, tc.committed); err == nil {
				t.Fatal("脏数据必须返回错误，不能静默纠正")
			}
		})
	}
}

// TestAdvanceTrainingStages 校验阶段自动推进顺序与结辩完成条件。
func TestAdvanceTrainingStages(t *testing.T) {
	stage := string(TrainingStageOpening)
	d := Debate{Mode: string(TrainingMode), TrainingStage: &stage, UserStance: "pro", Status: "active"}
	order := []struct {
		stage string
		step  int
	}{
		{string(TrainingStageOpening), 1},
		{string(TrainingStageRebuttal), 0},
		{string(TrainingStageRebuttal), 1},
		{string(TrainingStageFreeDebate), 0},
	}
	for _, want := range order {
		if err := advanceTraining(&d); err != nil {
			t.Fatal(err)
		}
		if d.TrainingStageValue() != want.stage || d.TrainingStageStep != want.step {
			t.Fatalf("推进后应为 %s/%d，实际 %s/%d", want.stage, want.step, d.TrainingStageValue(), d.TrainingStageStep)
		}
	}
	// 自由辩：stage_step 0→1（用户发言）→回合 +1 且回到 0（AI 回复）。
	if err := advanceTraining(&d); err != nil {
		t.Fatal(err)
	}
	if d.TrainingStageStep != 1 || d.CompletedRounds != 0 {
		t.Fatalf("用户自由辩发言后应为 step=1/rounds=0，实际 %d/%d", d.TrainingStageStep, d.CompletedRounds)
	}
	if err := advanceTraining(&d); err != nil {
		t.Fatal(err)
	}
	if d.TrainingStageStep != 0 || d.CompletedRounds != 1 {
		t.Fatalf("AI 回复后应完成 1 回合，实际 %d/%d", d.TrainingStageStep, d.CompletedRounds)
	}
	// 自由辩→结辩必须由显式结束触发，advanceTraining 不会自动切换。
	before := d.TrainingStageValue()
	if err := advanceTraining(&d); err != nil {
		t.Fatal(err)
	}
	if d.TrainingStageValue() != before {
		t.Fatal("advanceTraining 不得自动离开自由辩")
	}
	// 手工切换后验证结辩两步完成。
	d.TrainingStage = ptr(string(TrainingStageClosing))
	d.TrainingStageStep = 0
	if err := advanceTraining(&d); err != nil {
		t.Fatal(err)
	}
	if d.Status != "active" || d.TrainingStageStep != 1 {
		t.Fatalf("结辩第一步后仍应 active，实际 %q/%d", d.Status, d.TrainingStageStep)
	}
	if err := advanceTraining(&d); err != nil {
		t.Fatal(err)
	}
	if d.Status != "completed" || d.TrainingStageStep != 2 {
		t.Fatalf("结辩第二步后应 completed，实际 %q/%d", d.Status, d.TrainingStageStep)
	}
}

// TestValidateTrainingStepInput 覆盖输入边界：用户发言 1–2000 码点、全空白拒绝、
// AI 步骤必须空内容、幂等键必须是 UUID 且规范化为小写。
func TestValidateTrainingStepInput(t *testing.T) {
	key := "550e8400-e29b-41d4-a716-446655440000"
	if _, err := ValidateTrainingStepInput(TrainingStepInput{ClientRequestID: key, Content: "  "}); err == nil {
		t.Fatal("纯空白用户发言必须被拒绝")
	}
	if _, err := ValidateTrainingStepInput(TrainingStepInput{ClientRequestID: key, Content: "有效发言"}); err != nil {
		t.Fatalf("合法用户发言应通过: %v", err)
	}
	if _, err := ValidateTrainingStepInput(TrainingStepInput{ClientRequestID: key}); err != nil {
		t.Fatalf("空内容应按 AI 步骤通过: %v", err)
	}
	if _, err := ValidateTrainingStepInput(TrainingStepInput{ClientRequestID: "not-a-uuid", Content: "原文"}); err == nil {
		t.Fatal("非 UUID 幂等键必须被拒绝")
	}
	long := make([]rune, ai.MaxUserContentCodepoints+1)
	for i := range long {
		long[i] = '好'
	}
	if _, err := ValidateTrainingStepInput(TrainingStepInput{ClientRequestID: key, Content: string(long)}); err == nil {
		t.Fatal("超过 2000 码点必须被拒绝")
	}
	in, err := ValidateTrainingStepInput(TrainingStepInput{ClientRequestID: "550E8400-E29B-41D4-A716-446655440000", Content: "原文保留　"})
	if err != nil || in.ClientRequestID != key || in.Content != "原文保留　" {
		t.Fatalf("幂等键应规范化为小写且正文保留原文: %+v %v", in, err)
	}
}

// TestTrainingRejectsV1ModeConfusion 确认训练赛不会被 RoundsFor 当成 V1 固定轮数。
func TestTrainingRejectsV1ModeConfusion(t *testing.T) {
	if ai.ModeTrainingV2.Valid() {
		t.Fatal("training_v2 不能登记为 V1 赛制，否则会被轮数推导误用")
	}
	if _, err := ai.RoundsFor(ai.ModeTrainingV2); err == nil {
		t.Fatal("RoundsFor 对 training_v2 必须返回错误")
	}
	if !ai.ModeTrainingV2.IsTraining() {
		t.Fatal("training_v2 必须被识别为训练赛")
	}
}
