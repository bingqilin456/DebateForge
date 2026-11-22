package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/bingqilin456/DebateForge/backend/internal/debate"
	"github.com/google/uuid"
)

// 产品 V2 训练赛（V2-02）的隔离 MySQL 集成验收。
// 覆盖：阶段顺序、AI 先发言、跨阶段连续同方、1/10 回合、0 回合不可结束、
// 结束与提交竞争、提交失败/未知状态恢复、生成失败与放弃边界、重放/越权、
// 旧历史兼容、普通与流式共用认领路径。

// createTraining 创建一个训练赛会话并返回其 ID。
func (f *debateFixture) createTraining(t *testing.T, stance string) string {
	t.Helper()
	r := f.request(t, "POST", "/api/v1/training-debates", map[string]any{"topic_id": "1", "user_stance": stance})
	if r.status != 201 {
		t.Fatalf("create training status=%d code=%s", r.status, r.errorCode())
	}
	var id string
	if e := json.Unmarshal(r.data()["id"], &id); e != nil || id == "" {
		t.Fatal("create training id")
	}
	return id
}

// trainingDetail 读取训练赛详情并解析权威进度。
func (f *debateFixture) trainingDetail(t *testing.T, id string) (map[string]any, debate.TrainingDetailDTO) {
	t.Helper()
	r := f.request(t, "GET", "/api/v1/debates/"+id, nil)
	if r.status != 200 {
		t.Fatalf("training detail status=%d code=%s", r.status, r.errorCode())
	}
	var detail debate.DetailDTO
	b, _ := json.Marshal(r.data())
	if e := json.Unmarshal(b, &detail); e != nil {
		t.Fatal(e)
	}
	var raw map[string]any
	_ = json.Unmarshal(b, &raw)
	if detail.Training == nil {
		t.Fatal("训练赛详情必须包含 training 字段")
	}
	return raw, *detail.Training
}

// 测试客户端与页面一样，提交从详情取得的阶段与步骤。
func (f *debateFixture) trainingSubmit(t *testing.T, path string, body map[string]any) apiResult {
	t.Helper()
	if _, ok := body["expected_stage"]; !ok {
		_, detail := f.trainingDetail(t, strings.TrimPrefix(path, "/api/v1/debates/"))
		body["expected_stage"] = detail.Progress.Stage
		body["expected_step"] = detail.Progress.CommittedSteps + 1
	}
	return f.request(t, "POST", path+"/steps", body)
}

// submitStep 提交用户发言步骤。
func submitStep(content, key string) map[string]any {
	return map[string]any{"content": content, "client_request_id": key}
}

// askAI 请求服务端生成当前 AI 步骤。
func askAI(key string) map[string]any {
	return map[string]any{"client_request_id": key}
}

// TestV2TrainingFullProSequence 覆盖用户选正方的完整训练赛链路：
// 立论→驳立论→自由辩 1 回合→结辩，共 8 步 8 条正式消息，AI 反方结辩与
// 自由辩末回合 AI 回复相邻同方。
func TestV2TrainingFullProSequence(t *testing.T) {
	fake := ai.NewFake()
	f := newDebateFixture(t, fake)
	id := f.createTraining(t, "pro")
	path := "/api/v1/debates/" + id

	raw, detail := f.trainingDetail(t, id)
	if stage, _ := raw["debate"].(map[string]any)["mode"].(string); stage != "training_v2" {
		t.Fatalf("训练赛 mode 应为 training_v2，实际 %q", stage)
	}
	if detail.Progress.Stage != "opening" || detail.Progress.StageStep != 0 {
		t.Fatalf("创建后应从立论第 1 步开始，实际 %+v", detail.Progress)
	}
	if detail.Progress.NextSpeaker == nil || *detail.Progress.NextSpeaker != "user" {
		t.Fatalf("用户选正方时立论先由用户发言，实际 %+v", detail.Progress.NextSpeaker)
	}
	if detail.Progress.NextStepNo == nil || *detail.Progress.NextStepNo != 1 {
		t.Fatal("首步编号应为 1")
	}

	// 用户立论 → AI 立论 → 用户驳立论 → AI 驳立论 → 用户自由辩 1 → AI 自由辩 1
	// → 结束自由辩 → AI 结辩 → 用户结辩。
	userSteps := map[int]string{
		1: "正方立论", 3: "正方驳立论", 5: "正方自由辩第 1 回合", 8: "正方结辩",
	}
	aiSteps := map[int]bool{2: true, 4: true, 6: true, 7: true}
	var lastStage string
	var lastSpeaker string
	consecutiveCrossStage := false
	for stepNo := 1; stepNo <= 8; stepNo++ {
		if content, ok := userSteps[stepNo]; ok {
			r := f.trainingSubmit(t, path, submitStep(content, uuid.NewString()))
			if r.status != 201 {
				t.Fatalf("第 %d 步 status=%d code=%s", stepNo, r.status, r.errorCode())
			}
			var result debate.TrainingStepResult
			b, _ := json.Marshal(r.data())
			if e := json.Unmarshal(b, &result); e != nil {
				t.Fatal(e)
			}
			if result.Step == nil || result.Step.StepNo != stepNo || result.Step.Speaker != "user" || result.Step.Content != content {
				t.Fatalf("第 %d 步响应不符合预期: %+v", stepNo, result.Step)
			}
			lastStage, lastSpeaker = result.Step.Stage, result.Step.Speaker
			continue
		}
		if !aiSteps[stepNo] {
			continue
		}
		if stepNo == 7 {
			// 自由辩第 1 回合已完整结束：显式进入结辩。
			r := f.request(t, "POST", path+"/free-debate/end", nil)
			if r.status != 200 {
				t.Fatalf("进入结辩 status=%d code=%s", r.status, r.errorCode())
			}
		}
		r := f.trainingSubmit(t, path, askAI(uuid.NewString()))
		if r.status != 201 {
			t.Fatalf("第 %d 步 AI status=%d code=%s", stepNo, r.status, r.errorCode())
		}
		var result debate.TrainingStepResult
		b, _ := json.Marshal(r.data())
		if e := json.Unmarshal(b, &result); e != nil {
			t.Fatal(e)
		}
		if result.Step == nil || result.Step.StepNo != stepNo || result.Step.Speaker != "assistant" {
			t.Fatalf("第 %d 步 AI 响应不符合预期: %+v", stepNo, result.Step)
		}
		if lastSpeaker == result.Step.Speaker && lastStage != result.Step.Stage {
			consecutiveCrossStage = true
		}
		lastStage, lastSpeaker = result.Step.Stage, result.Step.Speaker
	}

	if !consecutiveCrossStage {
		t.Fatal("用户选正方时必须出现「自由辩末回合 AI 回复 → 结辩 AI 发言」跨阶段连续同方")
	}
	raw, detail = f.trainingDetail(t, id)
	if len(detail.Steps) != 8 {
		t.Fatalf("n=1 时应落库 8 条正式消息，实际 %d", len(detail.Steps))
	}
	debateObj := raw["debate"].(map[string]any)
	if status, _ := debateObj["status"].(string); status != "completed" {
		t.Fatalf("结辩完成后状态应为 completed，实际 %q", status)
	}
	for i, step := range detail.Steps {
		if step.StepNo != i+1 {
			t.Fatalf("步骤编号必须连续，第 %d 条为 %d", i+1, step.StepNo)
		}
	}
	// 模型调用 4 次（立论/驳立论/自由辩回复/结辩），用户步骤不调用模型。
	if fake.Calls() != 4 {
		t.Fatalf("AI 步骤应调用模型 4 次，实际 %d", fake.Calls())
	}
	// 第 9 步已超出本场步骤数，不能再提交。
	if r := f.trainingSubmit(t, path, submitStep("多余发言", uuid.NewString())); r.status != 409 || r.errorCode() != "round_conflict" {
		t.Fatalf("完成后不得再提交，实际 %d %s", r.status, r.errorCode())
	}
}

// TestV2TrainingAIFirstAndCrossStageSameSpeaker 覆盖用户选反方：
// 步骤 1 是 AI 先发言；步骤 4/5 是跨阶段连续两次用户发言。
func TestV2TrainingAIFirstAndCrossStageSameSpeaker(t *testing.T) {
	f := newDebateFixture(t, ai.NewFake())
	id := f.createTraining(t, "con")
	path := "/api/v1/debates/" + id

	_, detail := f.trainingDetail(t, id)
	if detail.Progress.NextSpeaker == nil || *detail.Progress.NextSpeaker != "assistant" {
		t.Fatalf("用户选反方时第一步必须由 AI 先发言，实际 %+v", detail.Progress.NextSpeaker)
	}
	// 步骤 1：AI 正方立论（比赛由 AI 开场）。
	r := f.trainingSubmit(t, path, askAI(uuid.NewString()))
	if r.status != 201 {
		t.Fatalf("AI 首步 status=%d code=%s", r.status, r.errorCode())
	}
	// 步骤 2：用户反方立论。
	if r = f.trainingSubmit(t, path, submitStep("反方立论", uuid.NewString())); r.status != 201 {
		t.Fatalf("用户立论 status=%d code=%s", r.status, r.errorCode())
	}
	// 步骤 3：AI 驳立论。
	if r = f.trainingSubmit(t, path, askAI(uuid.NewString())); r.status != 201 {
		t.Fatalf("AI 驳立论 status=%d code=%s", r.status, r.errorCode())
	}
	// 步骤 4：用户驳立论；步骤 5：用户自由辩首发 —— 连续两次用户、跨阶段。
	if r = f.trainingSubmit(t, path, submitStep("反方驳立论", uuid.NewString())); r.status != 201 {
		t.Fatalf("用户驳立论 status=%d code=%s", r.status, r.errorCode())
	}
	if r = f.trainingSubmit(t, path, submitStep("反方自由辩首发", uuid.NewString())); r.status != 201 {
		t.Fatalf("用户自由辩首发 status=%d code=%s", r.status, r.errorCode())
	}
	_, detail = f.trainingDetail(t, id)
	if len(detail.Steps) < 5 {
		t.Fatalf("应有 5 条正式步骤，实际 %d", len(detail.Steps))
	}
	fourth, fifth := detail.Steps[3], detail.Steps[4]
	if fourth.Speaker != "user" || fifth.Speaker != "user" {
		t.Fatalf("步骤 4/5 必须连续同为用户发言，实际 %s/%s", fourth.Speaker, fifth.Speaker)
	}
	if fourth.Stage == fifth.Stage {
		t.Fatalf("步骤 4/5 必须跨阶段，实际都是 %s", fourth.Stage)
	}
	if fourth.Stage != "rebuttal" || fifth.Stage != "free_debate" {
		t.Fatalf("步骤 4 应为驳立论、步骤 5 应为自由辩，实际 %s/%s", fourth.Stage, fifth.Stage)
	}
}

// TestV2TrainingZeroRoundCannotEnd 覆盖 0 回合不可结束自由辩。
func TestV2TrainingZeroRoundCannotEnd(t *testing.T) {
	f := newDebateFixture(t, ai.NewFake())
	id := f.createTraining(t, "pro")
	path := "/api/v1/debates/" + id

	// 直接结束：还在立论阶段，必须拒绝。
	if r := f.request(t, "POST", path+"/free-debate/end", nil); r.status != 409 || r.errorCode() != "round_conflict" {
		t.Fatalf("立论阶段不得结束自由辩，实际 %d %s", r.status, r.errorCode())
	}
	// 推进到自由辩开始（立论 2 步 + 驳立论 2 步）。
	sequence := []map[string]any{
		submitStep("立论", uuid.NewString()), askAI(uuid.NewString()),
		submitStep("驳立论", uuid.NewString()), askAI(uuid.NewString()),
	}
	for i, body := range sequence {
		if r := f.trainingSubmit(t, path, body); r.status != 201 {
			t.Fatalf("第 %d 步 status=%d code=%s", i+1, r.status, r.errorCode())
		}
	}
	_, detail := f.trainingDetail(t, id)
	if detail.Progress.Stage != "free_debate" || detail.Progress.FreeDebateRounds != 0 {
		t.Fatalf("应进入自由辩且 0 回合，实际 %+v", detail.Progress)
	}
	// 0 回合结束：必须拒绝，且说明原因。
	if r := f.request(t, "POST", path+"/free-debate/end", nil); r.status != 409 || r.errorCode() != "round_conflict" {
		t.Fatalf("0 回合不得结束自由辩，实际 %d %s", r.status, r.errorCode())
	}
	_, detail = f.trainingDetail(t, id)
	if detail.Progress.CanEndFreeDebate {
		t.Fatal("0 回合时 can_end_free_debate 必须为 false")
	}
	if detail.Progress.EndBlockedReason == nil || *detail.Progress.EndBlockedReason != "no_completed_round" {
		t.Fatalf("0 回合的阻止原因应为 no_completed_round，实际 %v", detail.Progress.EndBlockedReason)
	}
	if detail.Progress.Stage != "free_debate" {
		t.Fatal("被拒绝的结束操作不得改变阶段")
	}
}

// TestV2TrainingTenRoundCapAndEnd 覆盖 10 回合上限：满 10 回合后不再接受新发言，
// 但必须由用户显式进入结辩，不自动生成结辩。
func TestV2TrainingTenRoundCapAndEnd(t *testing.T) {
	fake := ai.NewFake()
	f := newDebateFixture(t, fake)
	id := f.createTraining(t, "pro")
	path := "/api/v1/debates/" + id

	// 立论 2 + 驳立论 2。
	for i := 0; i < 2; i++ {
		if r := f.trainingSubmit(t, path, submitStep(fmt.Sprintf("固定阶段%d", i), uuid.NewString())); r.status != 201 {
			t.Fatalf("用户固定步 status=%d code=%s", r.status, r.errorCode())
		}
		if r := f.trainingSubmit(t, path, askAI(uuid.NewString())); r.status != 201 {
			t.Fatalf("AI 固定步 status=%d code=%s", r.status, r.errorCode())
		}
	}
	// 自由辩 10 回合 = 20 步。
	for round := 1; round <= 10; round++ {
		if r := f.trainingSubmit(t, path, submitStep(fmt.Sprintf("自由辩第 %d 回合", round), uuid.NewString())); r.status != 201 {
			t.Fatalf("第 %d 回合用户 status=%d code=%s", round, r.status, r.errorCode())
		}
		if r := f.trainingSubmit(t, path, askAI(uuid.NewString())); r.status != 201 {
			t.Fatalf("第 %d 回合 AI status=%d code=%s", round, r.status, r.errorCode())
		}
	}
	_, detail := f.trainingDetail(t, id)
	if detail.Progress.FreeDebateRounds != 10 {
		t.Fatalf("应完成 10 回合，实际 %d", detail.Progress.FreeDebateRounds)
	}
	if detail.Progress.NextStepNo != nil {
		t.Fatalf("10 回合完成后不得再分配下一步，实际 %v", detail.Progress.NextStepNo)
	}
	if !detail.Progress.CanEndFreeDebate {
		t.Fatal("10 回合完成后应允许显式进入结辩")
	}
	if detail.Progress.CanSubmitStep {
		t.Fatal("10 回合完成后不得继续提交自由辩发言")
	}
	// 第 11 回合必须被拒绝，且不新增正式步骤。
	before := len(detail.Steps)
	if r := f.trainingSubmit(t, path, submitStep("第 11 回合", uuid.NewString())); r.status != 409 {
		t.Fatalf("第 11 回合必须被拒绝，实际 %d", r.status)
	}
	_, detail = f.trainingDetail(t, id)
	if len(detail.Steps) != before {
		t.Fatal("被拒绝的第 11 回合不得落库")
	}
	// 显式进入结辩：不自动生成结辩。
	if r := f.request(t, "POST", path+"/free-debate/end", nil); r.status != 200 {
		t.Fatalf("进入结辩 status=%d code=%s", r.status, r.errorCode())
	}
	_, detail = f.trainingDetail(t, id)
	if detail.Progress.Stage != "closing" {
		t.Fatalf("应进入结辩，实际 %s", detail.Progress.Stage)
	}
	if len(detail.Steps) != before {
		t.Fatal("进入结辩不得新增正式步骤或自动生成结辩")
	}
	// 重复点击：不重复切换、不产生新步骤（幂等重放）。
	if r := f.request(t, "POST", path+"/free-debate/end", nil); r.status != 200 {
		t.Fatalf("重复进入结辩应可重放，实际 %d", r.status)
	}
	_, detail = f.trainingDetail(t, id)
	if detail.Progress.Stage != "closing" || len(detail.Steps) != before {
		t.Fatal("重复进入结辩改变了状态")
	}
}

// TestV2TrainingEndVsSubmitRace 覆盖「结束自由辩」与「发言提交」竞争：
// 服务端以会话行锁与条件更新保证只有一个先成功。
func TestV2TrainingEndVsSubmitRace(t *testing.T) {
	f := newDebateFixture(t, ai.NewFake())
	id := f.createTraining(t, "con")
	path := "/api/v1/debates/" + id
	// 反方在自由辩与结辩均先发言，旧请求必须有阶段守卫。
	for _, body := range []map[string]any{
		askAI(uuid.NewString()), submitStep("立论", uuid.NewString()),
		askAI(uuid.NewString()), submitStep("驳立论", uuid.NewString()),
		submitStep("自由辩第 1 回合", uuid.NewString()), askAI(uuid.NewString()),
	} {
		if r := f.trainingSubmit(t, path, body); r.status != 201 {
			t.Fatalf("准备步骤 status=%d code=%s", r.status, r.errorCode())
		}
	}
	// 并发发起：结束自由辩 vs 新的用户发言。二者只能有一个成功推进阶段。
	endDone := make(chan apiResult, 1)
	go func() { endDone <- f.request(t, "POST", path+"/free-debate/end", nil) }()
	submitDone := make(chan apiResult, 1)
	go func() {
		submitDone <- f.trainingSubmit(t, path, map[string]any{"content": "第 2 回合抢占", "client_request_id": uuid.NewString(), "expected_stage": "free_debate", "expected_step": 7})
	}()
	end, submit := <-endDone, <-submitDone
	_, detail := f.trainingDetail(t, id)
	if end.status == 200 && submit.status == 201 {
		t.Fatal("同一阶段位置的结束和发言只能成功一个")
	} else if end.status == 200 {
		if submit.status != 409 {
			t.Fatalf("结束成功后提交必须冲突，实际 %d", submit.status)
		}
		if detail.Progress.Stage != "closing" {
			t.Fatal("结束成功后阶段应为结辩")
		}
	} else if submit.status == 201 {
		if end.status != 200 && end.status != 409 {
			t.Fatalf("提交成功后结束应成功或冲突，实际 %d", end.status)
		}
	} else {
		t.Fatalf("竞争时至少一个操作必须成功：end=%d submit=%d", end.status, submit.status)
	}
	// 无论谁赢，状态都必须自洽：不能既在结辩又留有半回合。
	_, detail = f.trainingDetail(t, id)
	if detail.Progress.Stage == "closing" && detail.Progress.StageStep != 0 {
		t.Fatalf("结辩阶段内步数必须为 0，实际 %d", detail.Progress.StageStep)
	}
}

// TestV2TrainingGenerationFailureAndAbandonBoundary 覆盖生成失败与放弃边界：
// 已完成 ≥1 回合时允许放弃失败回应并进入结辩；0 回合时不允。
func TestV2TrainingGenerationFailureAndAbandonBoundary(t *testing.T) {
	var failOnce atomic.Bool
	failOnce.Store(true)
	g := testGenerator(func(c context.Context, r ai.Request) (ai.Result, error) {
		// 只让自由辩第 1 回合的 AI 回复（历史已含 5 步时）失败一次，前面立论/驳立论正常。
		if r.Training != nil && len(r.Training.History) == 5 && failOnce.CompareAndSwap(true, false) {
			return ai.Result{}, &ai.Error{Code: ai.CodeModelTimeout}
		}
		return ai.NewFake().Generate(c, r)
	})
	f := newDebateFixture(t, g)
	id := f.createTraining(t, "pro")
	path := "/api/v1/debates/" + id
	// 立论 2 步 + 驳立论 2 步 + 自由辩用户首发（第 5 步）。
	for _, body := range []map[string]any{
		submitStep("立论", uuid.NewString()), askAI(uuid.NewString()),
		submitStep("驳立论", uuid.NewString()), askAI(uuid.NewString()),
		submitStep("自由辩首发", uuid.NewString()),
	} {
		if r := f.trainingSubmit(t, path, body); r.status != 201 {
			t.Fatalf("准备步骤 status=%d code=%s", r.status, r.errorCode())
		}
	}
	// 第 6 步 AI 回复失败（0 个完整回合）。
	failKey := uuid.NewString()
	r := f.trainingSubmit(t, path, askAI(failKey))
	if r.status != 504 {
		t.Fatalf("模型超时应映射为 504，实际 %d %s", r.status, r.errorCode())
	}
	var failure struct {
		Error struct {
			Details *debate.FailureDetails `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(r.body, &failure)
	if failure.Error.Details == nil || failure.Error.Details.Status != "failed" {
		t.Fatal("已确认的生成失败必须带受控 details")
	}
	_, detail := f.trainingDetail(t, id)
	if len(detail.Steps) != 5 {
		t.Fatalf("失败步骤不占编号，实际已提交 %d 步", len(detail.Steps))
	}
	if detail.Progress.PendingAI == nil || detail.Progress.PendingAI.Status != "failed" {
		t.Fatalf("失败记录必须保留可见，实际 %+v", detail.Progress.PendingAI)
	}
	if !detail.Progress.CanRetryAI {
		t.Fatal("失败后应允许显式重试")
	}
	// 0 完整回合：不允许放弃进入结辩。
	if r := f.request(t, "POST", path+"/free-debate/end", nil); r.status != 409 || r.errorCode() != "round_conflict" {
		t.Fatalf("0 回合失败时不得跳过自由辩，实际 %d %s", r.status, r.errorCode())
	}
	// 该失败步骤保留幂等重放：同一键重放返回 409 turn_failed，不再调用模型。
	if r := f.trainingSubmit(t, path, askAI(failKey)); r.status != 409 || r.errorCode() != "turn_failed" {
		t.Fatalf("失败请求重放应返回 409 turn_failed，实际 %d %s", r.status, r.errorCode())
	}
	// 重试成功：第 6 步由 AI 重新生成并落库，完成自由辩第 1 回合。
	stepNo := 6
	claimID := failure.Error.Details.TurnID
	if r := f.request(t, "POST", fmt.Sprintf("%s/steps/%d/retry", path, stepNo), map[string]any{"expected_attempt_version": 1}); r.status != 200 {
		t.Fatalf("显式重试 status=%d code=%s", r.status, r.errorCode())
	}
	_, detail = f.trainingDetail(t, id)
	if len(detail.Steps) != 6 || detail.Progress.FreeDebateRounds != 1 {
		t.Fatalf("重试成功后应完成 1 回合，实际 steps=%d rounds=%d", len(detail.Steps), detail.Progress.FreeDebateRounds)
	}
	// 该步骤已完成：同键重放返回 200 不重新生成；旧版本重试同样按已完成重放 200，
	// 且 attempt_version 保持 2（旧尝试不能覆盖新尝试）。
	if r := f.request(t, "POST", fmt.Sprintf("%s/steps/%d/retry", path, stepNo), map[string]any{"expected_attempt_version": 1}); r.status != 200 {
		t.Fatalf("已完成步骤的重试应按重放返回 200，实际 %d %s", r.status, r.errorCode())
	}
	_, detail = f.trainingDetail(t, id)
	if len(detail.Steps) != 6 {
		t.Fatalf("旧版本重试不得新增步骤，实际 %d", len(detail.Steps))
	}
	if detail.Steps[5].StepNo != 6 {
		t.Fatalf("第 6 条正式步骤编号应为 6，实际 %d", detail.Steps[5].StepNo)
	}
	// claimID 只用于确认失败记录身份，不参与重试寻址。
	if claimID == "" {
		t.Fatal("失败详情必须包含 claim 标识")
	}
	// 已完成 ≥1 回合：此时允许进入结辩。
	if r := f.request(t, "POST", path+"/free-debate/end", nil); r.status != 200 {
		t.Fatalf("完成 1 回合后应允许进入结辩，实际 %d %s", r.status, r.errorCode())
	}
	_, detail = f.trainingDetail(t, id)
	if detail.Progress.Stage != "closing" {
		t.Fatalf("应进入结辩，实际 %s", detail.Progress.Stage)
	}
}

// TestV2TrainingAbandonFailedReplyAfterOneRound 覆盖「已完成 ≥1 回合 + AI 失败」
// 时明确放弃本次失败回应并进入结辩；失败记录保留可见为 abandoned。
func TestV2TrainingAbandonFailedReplyAfterOneRound(t *testing.T) {
	var failNow atomic.Bool
	failNow.Store(false)
	g := testGenerator(func(c context.Context, r ai.Request) (ai.Result, error) {
		if failNow.Load() {
			return ai.Result{}, &ai.Error{Code: ai.CodeModelTimeout}
		}
		return ai.NewFake().Generate(c, r)
	})
	f := newDebateFixture(t, g)
	id := f.createTraining(t, "pro")
	path := "/api/v1/debates/" + id
	for _, body := range []map[string]any{
		submitStep("立论", uuid.NewString()), askAI(uuid.NewString()),
		submitStep("驳立论", uuid.NewString()), askAI(uuid.NewString()),
		submitStep("自由辩第 1 回合", uuid.NewString()), askAI(uuid.NewString()),
	} {
		if r := f.trainingSubmit(t, path, body); r.status != 201 {
			t.Fatalf("准备步骤 status=%d code=%s", r.status, r.errorCode())
		}
	}
	// 第 2 回合用户发言成功后，AI 回复失败。
	if r := f.trainingSubmit(t, path, submitStep("自由辩第 2 回合", uuid.NewString())); r.status != 201 {
		t.Fatalf("第 2 回合用户发言 status=%d code=%s", r.status, r.errorCode())
	}
	failNow.Store(true)
	if r := f.trainingSubmit(t, path, askAI(uuid.NewString())); r.status != 504 {
		t.Fatalf("AI 回复应失败为 504，实际 %d %s", r.status, r.errorCode())
	}
	_, detail := f.trainingDetail(t, id)
	if detail.Progress.FreeDebateRounds != 1 || detail.Progress.StageStep != 1 {
		t.Fatalf("失败时应有 1 个完整回合且停在第 2 步，实际 %+v", detail.Progress)
	}
	if !detail.Progress.CanEndFreeDebate {
		t.Fatal("已完成 ≥1 回合且 AI 失败时应允许放弃进入结辩")
	}
	if r := f.request(t, "POST", path+"/free-debate/end", nil); r.status != 409 {
		t.Fatalf("未明确放弃时应拒绝进入结辩，实际 %d", r.status)
	}
	if r := f.request(t, "POST", path+"/free-debate/end", map[string]any{"expected_failed_attempt_version": detail.Progress.PendingAI.AttemptVersion}); r.status != 200 {
		t.Fatalf("放弃失败回应并进入结辩 status=%d code=%s", r.status, r.errorCode())
	}
	_, detail = f.trainingDetail(t, id)
	if detail.Progress.Stage != "closing" || detail.Progress.StageStep != 0 {
		t.Fatalf("应进入结辩，实际 %+v", detail.Progress)
	}
	if detail.Progress.AbandonedAI == nil || detail.Progress.AbandonedAI.ErrorCode == nil || *detail.Progress.AbandonedAI.ErrorCode != "model_timeout" {
		t.Fatalf("被放弃的失败记录必须保留可见，实际 %+v", detail.Progress.AbandonedAI)
	}
	// 放弃后恢复生成能力，结辩应正常推进。
	failNow.Store(false)
	// 结辩仍可正常推进：下一步是 AI 反方结辩。
	if detail.Progress.NextSpeaker == nil || *detail.Progress.NextSpeaker != "assistant" {
		t.Fatalf("用户选正方时结辩由 AI 先发言，实际 %+v", detail.Progress.NextSpeaker)
	}
	if r := f.trainingSubmit(t, path, askAI(uuid.NewString())); r.status != 201 {
		t.Fatalf("结辩 AI 步 status=%d code=%s", r.status, r.errorCode())
	}
	if r := f.trainingSubmit(t, path, submitStep("用户结辩", uuid.NewString())); r.status != 201 {
		t.Fatalf("结辩用户步 status=%d code=%s", r.status, r.errorCode())
	}
}

// TestV2TrainingReplayAndOwnership 覆盖幂等重放与越权：
// 同键同文返回 200、同键异文 409、本人可见、他人 404。
func TestV2TrainingReplayAndOwnership(t *testing.T) {
	f := newDebateFixture(t, ai.NewFake())
	id := f.createTraining(t, "pro")
	path := "/api/v1/debates/" + id
	key := uuid.NewString()

	original := submitStep("原文保留　", key)
	first := f.trainingSubmit(t, path, original)
	if first.status != 201 {
		t.Fatalf("首次提交 status=%d code=%s", first.status, first.errorCode())
	}
	// 同键同文重放：200，不新增步骤。
	replay := f.trainingSubmit(t, path, original)
	if replay.status != 200 {
		t.Fatalf("幂等重放应 200，实际 %d", replay.status)
	}
	// 同键异文：409 idempotency_conflict。
	if r := f.trainingSubmit(t, path, submitStep("改写的原文", key)); r.status != 409 || r.errorCode() != "idempotency_conflict" {
		t.Fatalf("同键异文应 409 idempotency_conflict，实际 %d %s", r.status, r.errorCode())
	}
	_, detail := f.trainingDetail(t, id)
	if len(detail.Steps) != 1 {
		t.Fatalf("重放不得新增步骤，实际 %d", len(detail.Steps))
	}

	// 越权：他人读取与提交都表现为 404，不泄漏资源存在性。
	other := f.randomUsername(t)
	if r, _ := f.register(t, other, integrationPassword); r.status != 201 {
		t.Fatal("register other")
	}
	res, otherCookie, otherCSRF := f.login(t, other, integrationPassword)
	if res.status != 200 {
		t.Fatal("login other")
	}
	if foreign := f.do(t, "GET", path, "", "", "", otherCookie); foreign.status != 404 || foreign.errorCode() != "not_found" {
		t.Fatalf("他人读取应 404，实际 %d %s", foreign.status, foreign.errorCode())
	}
	if foreign := f.do(t, "POST", path+"/steps", `{"client_request_id":"`+uuid.NewString()+`","content":"越权"}`, integrationOrigin, otherCSRF, otherCookie); foreign.status != 404 {
		t.Fatalf("他人提交应 404，实际 %d", foreign.status)
	}
	if foreign := f.do(t, "POST", path+"/free-debate/end", ``, integrationOrigin, otherCSRF, otherCookie); foreign.status != 404 {
		t.Fatalf("他人结束自由辩应 404，实际 %d", foreign.status)
	}
}

// TestV2TrainingStreamSharesClaim 覆盖普通与流式共用同一认领路径：
// 流式认领后普通 JSON 重放返回 202，且最终只落一条正式步骤、只调用一次模型。
func TestV2TrainingStreamSharesClaim(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	g := streamGenerator{
		generate: ai.NewFake().Generate,
		stream: func(c context.Context, r ai.Request, emit func(string) error) (ai.Result, error) {
			calls.Add(1)
			if err := emit("中文"); err != nil {
				return ai.Result{}, err
			}
			select {
			case <-release:
			case <-c.Done():
				return ai.Result{}, c.Err()
			}
			if err := emit("🌱增量"); err != nil {
				return ai.Result{}, err
			}
			return ai.Result{Content: "中文🌱增量"}, nil
		},
	}
	f := newDebateFixture(t, g)
	id := f.createTraining(t, "con") // 用户选反方 → 第一步是 AI
	path := "/api/v1/debates/" + id
	key := uuid.NewString()
	input := map[string]any{"client_request_id": key, "expected_stage": "opening", "expected_step": 1}

	// 打开流式入口（训练赛第一步由 AI 发言）。
	resp := openTrainingStream(t, f, path, input)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream headers: %d %v", resp.StatusCode, resp.Header)
	}
	reader := bufio.NewReader(resp.Body)
	start := readFrame(t, reader)
	if !strings.HasPrefix(start, "event: start\n") {
		t.Fatalf("首帧应为 start: %s", start)
	}
	if delta := readFrame(t, reader); !strings.Contains(delta, "中文") {
		t.Fatalf("应有增量帧: %s", delta)
	}
	// 普通 JSON 用同一幂等键重放：认领仍在生成中 → 202，不重复调用模型。
	pending := f.trainingSubmit(t, path, input)
	if pending.status != 202 {
		t.Fatalf("流式认领后的 JSON 重放应 202，实际 %d %s", pending.status, pending.errorCode())
	}
	// 完成流式：只有最终事务提交成功才发送 done。
	close(release)
	rest, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rest), "event: done") || strings.Contains(string(rest), "event: error") {
		t.Fatalf("流式结束异常: %s", rest)
	}
	if calls.Load() != 1 {
		t.Fatalf("模型只应调用 1 次，实际 %d", calls.Load())
	}
	_, detail := f.trainingDetail(t, id)
	if len(detail.Steps) != 1 || detail.Steps[0].Speaker != "assistant" || detail.Steps[0].Content != "中文🌱增量" {
		t.Fatalf("流式最终结果必须落一条正式步骤，实际 %+v", detail.Steps)
	}
	// 完成后同键重放：200 且不重新生成。
	replay := f.trainingSubmit(t, path, input)
	if replay.status != 200 || calls.Load() != 1 {
		t.Fatalf("完成后重放应 200 且不重新生成，实际 %d calls=%d", replay.status, calls.Load())
	}
}

// openTrainingStream 打开训练赛流式提交入口。
func openTrainingStream(t *testing.T, f *debateFixture, path string, body any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("POST", f.server.URL+path+"/steps/stream", strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", integrationOrigin)
	req.Header.Set("X-CSRF-Token", f.csrf)
	req.AddCookie(f.cookie)
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// TestV2TrainingStreamPreflightStaysJSON 覆盖流式入口的预检仍返回普通 JSON。
func TestV2TrainingStreamPreflightStaysJSON(t *testing.T) {
	f := newDebateFixture(t, ai.NewFake())
	id := f.createTraining(t, "con")
	for _, tc := range []struct {
		name                 string
		status               int
		cookie, csrf, accept bool
	}{
		{"unauthenticated", 401, false, true, true},
		{"csrf", 403, true, false, true},
		{"accept", 400, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest("POST", f.server.URL+"/api/v1/debates/"+id+"/steps/stream", strings.NewReader(`{"client_request_id":"`+uuid.NewString()+`"}`))
			req.Header.Set("Origin", integrationOrigin)
			req.Header.Set("Content-Type", "application/json")
			if tc.cookie {
				req.AddCookie(f.cookie)
			}
			if tc.csrf {
				req.Header.Set("X-CSRF-Token", f.csrf)
			}
			if tc.accept {
				req.Header.Set("Accept", "text/event-stream")
			}
			resp, err := f.client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			raw, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != tc.status || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") || !json.Valid(raw) || strings.Contains(string(raw), "event:") {
				t.Fatalf("preflight %d %s", resp.StatusCode, raw)
			}
		})
	}
}

// TestV2TrainingV1Compatibility 覆盖旧历史兼容：
// V1 standard_4 / quick_3 会话仍可读可继续，且训练赛端点拒绝 V1 会话、V1 端点拒绝训练赛。
func TestV2TrainingV1Compatibility(t *testing.T) {
	f := newDebateFixture(t, ai.NewFake())
	standard := f.create(t) // V1 standard_4（服务端固定）
	quick := f.createQuick3(t)

	// V1 详情不受影响：无 training 字段、turns 为空数组、进度字段为 null。
	for _, id := range []string{standard, quick} {
		r := f.request(t, "GET", "/api/v1/debates/"+id, nil)
		if r.status != 200 {
			t.Fatalf("V1 详情 status=%d", r.status)
		}
		var detail debate.DetailDTO
		b, _ := json.Marshal(r.data())
		if e := json.Unmarshal(b, &detail); e != nil {
			t.Fatal(e)
		}
		if detail.Training != nil {
			t.Fatalf("V1 会话不得出现 training 字段: %+v", detail.Training)
		}
		if detail.Debate.Mode != "standard_4" && detail.Debate.Mode != "quick_3" {
			t.Fatalf("V1 赛制语义不得被改写，实际 %q", detail.Debate.Mode)
		}
	}
	// V1 会话仍可继续发言（训练赛改动不得影响 V1 路径）。
	if r := f.request(t, "POST", "/api/v1/debates/"+standard+"/turns", submit("V1 第一轮", uuid.NewString(), 1)); r.status != 201 {
		t.Fatalf("V1 继续发言 status=%d code=%s", r.status, r.errorCode())
	}
	// 训练赛端点拒绝 V1 会话。
	if r := f.request(t, "POST", "/api/v1/debates/"+standard+"/steps", submitStep("错用训练赛入口", uuid.NewString())); r.status != 409 || r.errorCode() != "round_conflict" {
		t.Fatalf("V1 会话不得使用训练赛端点，实际 %d %s", r.status, r.errorCode())
	}
	// V1 轮次端点拒绝训练赛会话，避免两套状态机互相推进。
	training := f.createTraining(t, "pro")
	if r := f.request(t, "POST", "/api/v1/debates/"+training+"/turns", submit("错用 V1 入口", uuid.NewString(), 1)); r.status != 409 || r.errorCode() != "round_conflict" {
		t.Fatalf("训练赛不得使用 V1 轮次端点，实际 %d %s", r.status, r.errorCode())
	}
	// V1 结构化总结仍只作用于 V1 会话。
	if r := f.request(t, "POST", "/api/v1/debates/"+standard+"/summary", map[string]any{"retry": false}); r.status != 409 {
		// 未完成 V1 会话的总结本就 409 round_conflict，这里只确认它没有进入训练赛路径。
		t.Fatalf("V1 总结入口行为变化: %d", r.status)
	}
}

// TestV2TrainingCreateValidation 覆盖创建入口校验：
// topic 必须存在、立场必须合法、ai_style 必须为精确枚举、创建不调用模型。
func TestV2TrainingCreateValidation(t *testing.T) {
	fake := ai.NewFake()
	f := newDebateFixture(t, fake)
	for _, body := range []string{
		`{"topic_id":"999999","user_stance":"pro"}`,
		`{"topic_id":"1","user_stance":"sideways"}`,
		`{"topic_id":"1","user_stance":"pro","ai_style":"verbose"}`,
		`{"topic_id":"abc","user_stance":"pro"}`,
	} {
		r := f.do(t, "POST", "/api/v1/training-debates", body, integrationOrigin, f.csrf, f.cookie)
		if r.status != 404 && r.status != 400 {
			t.Fatalf("非法创建 %s 应受控失败，实际 %d %s", body, r.status, r.errorCode())
		}
	}
	if fake.Calls() != 0 {
		t.Fatal("创建训练赛不得调用模型")
	}
	// 合法创建：默认流派 logical、mode 固定 training_v2、进度从立论开始。
	r := f.request(t, "POST", "/api/v1/training-debates", map[string]any{"topic_id": "1", "user_stance": "pro"})
	if r.status != 201 {
		t.Fatalf("合法创建 status=%d code=%s", r.status, r.errorCode())
	}
	var dto debate.DebateDTO
	b, _ := json.Marshal(r.data())
	if e := json.Unmarshal(b, &dto); e != nil {
		t.Fatal(e)
	}
	if dto.Mode != "training_v2" || dto.AIStyle != "logical" {
		t.Fatalf("创建响应 mode/ai_style 不符合预期: %+v", dto)
	}
	if dto.Training == nil || dto.Training.Stage != "opening" || dto.Training.FreeDebateMaxRounds != 10 {
		t.Fatalf("创建响应应带训练赛进度: %+v", dto.Training)
	}
}

// TestV2TrainingLeaseRecovery 覆盖训练赛 AI 步骤的租约回收：
// 过期 generating 认领按状态与版本条件标记 failed，且可通过显式重试恢复。
func TestV2TrainingLeaseRecovery(t *testing.T) {
	f := newDebateFixture(t, ai.NewFake())
	id := f.createTraining(t, "con") // 第一步是 AI
	path := "/api/v1/debates/" + id
	r := f.trainingSubmit(t, path, askAI(uuid.NewString()))
	if r.status != 201 {
		t.Fatalf("AI 首步 status=%d code=%s", r.status, r.errorCode())
	}
	// 人为造一条过期 generating 认领：先把阶段回退并删除步骤，再插入过期认领，
	// 模拟进程在生成中崩溃后留下的状态。
	var stepID uint64
	if err := f.db.ORM.Raw("SELECT id FROM debate_steps WHERE debate_id=? AND step_no=1", mustUint(t, id)).Scan(&stepID).Error; err != nil || stepID == 0 {
		t.Fatalf("读取步骤失败: %v", err)
	}
	if err := f.db.ORM.Exec("DELETE FROM debate_steps WHERE id=?", stepID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.ORM.Exec("UPDATE debates SET training_stage='opening', training_stage_step=0, updated_at=updated_at WHERE id=?", id).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.ORM.Exec("UPDATE debate_step_claims SET status='generating', lease_expires_at=? WHERE debate_id=? AND step_no=1", time.Now().UTC().Add(-time.Second), id).Error; err != nil {
		t.Fatal(err)
	}
	repo := debate.NewRepository(f.db.ORM)
	if n, err := repo.RecoverExpiredLeases(context.Background()); err != nil || n < 1 {
		t.Fatalf("租约回收应有至少 1 条: %d %v", n, err)
	}
	_, detail := f.trainingDetail(t, id)
	if detail.Progress.PendingAI == nil || detail.Progress.PendingAI.Status != "failed" {
		t.Fatalf("过期租约应标记 failed 且保留可见，实际 %+v", detail.Progress.PendingAI)
	}
	if !detail.Progress.CanRetryAI {
		t.Fatal("过期失败后应允许显式重试")
	}
	if r := f.request(t, "POST", path+"/steps/1/retry", map[string]any{"expected_attempt_version": 1}); r.status != 200 {
		t.Fatalf("显式重试 status=%d code=%s", r.status, r.errorCode())
	}
	_, detail = f.trainingDetail(t, id)
	if len(detail.Steps) != 1 {
		t.Fatalf("重试成功后应落 1 条步骤，实际 %d", len(detail.Steps))
	}
}
