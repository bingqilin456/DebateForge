package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/bingqilin456/DebateForge/backend/internal/debate"
	"github.com/google/uuid"
)

func waitTrainingAnalysis(t *testing.T, f *debateFixture, path string, ready func(apiResult) bool) apiResult {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	var result apiResult
	for time.Now().Before(deadline) {
		result = f.request(t, "GET", path, nil)
		if ready(result) {
			return result
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("分析未达到已保存状态：%s status=%d code=%s data=%s", path, result.status, result.errorCode(), result.data()["analysis"])
	return result
}

func TestV2TrainingAnalysisIntegration(t *testing.T) {
	fake := ai.NewFake()
	f := newDebateFixture(t, fake)
	issues := debate.NewFakeIssueCardAnalyzer()
	mount := func() {
		f.mountDB(t, fake, f.db.ORM, func(s *debate.Service, repo *debate.Repository) {
			s.EnableAnnotations(true)
			s.EnableReviews(true)
			s.EnableTrainingIssues(repo, issues)
			s.EnableTrainingRepractice(repo, func(_ context.Context, _ debate.RepracticeInput) (debate.RepracticeFeedback, error) {
				return debate.RepracticeFeedback{Change: "已补前提", Reason: "可复核", NewProblem: "适用边界"}, nil
			})
		})
	}
	mount()
	id := f.createTraining(t, "con")
	path := "/api/v1/debates/" + id
	run := func(n int) {
		t.Helper()
		_, detail := f.trainingDetail(t, id)
		key := uuid.NewString()
		body := askAI(key)
		if detail.Progress.NextSpeaker != nil && *detail.Progress.NextSpeaker == "user" {
			body = submitStep(fmt.Sprintf("我方发言第 %d 步，依据可以逐条核对。", n), key)
		}
		result := f.trainingSubmit(t, path, body)
		if result.status != 201 {
			t.Fatalf("step %d: %d %s", n, result.status, result.errorCode())
		}
	}
	waitIssues := func(version uint64) {
		t.Helper()
		waitTrainingAnalysis(t, f, path+"/issue-cards", func(result apiResult) bool {
			var state debate.IssueCardAnalysisDTO
			return result.status == 200 && json.Unmarshal(result.data()["analysis"], &state) == nil &&
				state.AttemptVersion == version && state.Status == "completed"
		})
	}
	for i := 1; i <= 6; i++ {
		run(i)
		if i == 1 {
			waitTrainingAnalysis(t, f, path+"/annotations", func(result apiResult) bool {
				var batches []debate.AnnotationBatchDTO
				return result.status == 200 && json.Unmarshal(result.data()["items"], &batches) == nil &&
					len(batches) == 1 && batches[0].RoundNo == 1 && batches[0].Status == "completed"
			})
		}
		if i == 4 || i == 6 {
			waitIssues(uint64(i/2 - 1))
		}
	}
	if r := f.request(t, "POST", path+"/review", map[string]any{}); r.status != 409 {
		t.Fatalf("未完赛复盘 should conflict: %d", r.status)
	}
	if r := f.request(t, "POST", path+"/free-debate/end", map[string]any{}); r.status != 200 {
		t.Fatalf("end free debate %d %s", r.status, r.errorCode())
	}
	run(7)
	run(8)
	waitIssues(3)
	if issues.Calls() != 3 {
		t.Fatalf("分析应只有三个完成位置：%d", issues.Calls())
	}
	fake.FailReviews(&ai.Error{Code: ai.CodeModelError, Message: "synthetic"}, 1)
	if r := f.request(t, "POST", path+"/review", map[string]any{}); r.status != 502 {
		t.Fatalf("复盘故障 %d %s", r.status, r.errorCode())
	}
	mount()
	waitIssues(3)
	_, detail := f.trainingDetail(t, id)
	if len(detail.Steps) != 8 || detail.Progress.NextSpeaker != nil {
		t.Fatal("复盘失败不应改变完赛进度")
	}
	failed := f.request(t, "GET", path+"/review", nil)
	if string(failed.data()["status"]) != `"failed"` {
		t.Fatalf("刷新后未恢复失败：%d", failed.status)
	}
	review := f.request(t, "POST", path+"/review", map[string]any{"retry": true, "expected_attempt_version": 1})
	if review.status != 200 && review.status != 201 {
		t.Fatalf("重试复盘 %d %s", review.status, review.errorCode())
	}
	var content struct {
		Dimensions []struct {
			Level *int `json:"level"`
		} `json:"dimensions"`
	}
	if err := json.Unmarshal(review.data()["content"], &content); err != nil || len(content.Dimensions) != 4 {
		t.Fatalf("复盘四维无效: %v", err)
	}
	missing := false
	for _, dimension := range content.Dimensions {
		missing = missing || dimension.Level == nil
	}
	if !missing {
		t.Fatal("Fake 复盘应包含观察不足")
	}
	source := detail.Steps[1]
	in := map[string]any{"client_request_id": uuid.NewString(), "message_id": source.ID, "quote": source.Content,
		"start_cp": 0, "end_cp": len([]rune(source.Content)), "goal": "补前提", "rewrite": "评价条件应当可逐条查验。"}
	if r := f.request(t, "POST", path+"/repractice", in); r.status != 200 {
		t.Fatalf("训练赛局部重练 %d %s", r.status, r.errorCode())
	}
	mount()
	if r := f.request(t, "GET", path+"/repractice", nil); r.status != 200 || !strings.Contains(string(r.body), "评价条件应当可逐条查验") {
		t.Fatalf("查询重练 %d %s", r.status, r.errorCode())
	}
}
