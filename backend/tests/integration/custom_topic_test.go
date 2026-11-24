package integration

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/bingqilin456/DebateForge/backend/internal/debate"
	"github.com/google/uuid"
)

// 本文件是 V2-03 私有自定义辩题的固定隔离库联调用例（AC-V2-12）。
//
// 运行条件与既有集成测试一致：显式设置 SIYIGE_INTEGRATION=1 与 APP_ENV=test，
// 固定目标 127.0.0.1:13307/siyige_test、账户 siyige_test_app；默认跳过。
// 执行前须经授权在固定测试库应用 00014；夹具复用既有按随机用户 ID 的清理。

// createCustom 通过 HTTP 创建一场私有自定义辩题，返回解析后的 DTO。
func (f *debateFixture) createCustom(t *testing.T, title, background string) debate.DebateDTO {
	t.Helper()
	body := map[string]any{
		"custom_topic": map[string]any{"title": title, "background": background},
		"user_stance":  "pro",
	}
	r := f.request(t, "POST", "/api/v1/debates", body)
	if r.status != http.StatusCreated {
		t.Fatalf("创建自定义会话 status=%d code=%s", r.status, r.errorCode())
	}
	var dto debate.DebateDTO
	raw, _ := json.Marshal(r.data())
	if err := json.Unmarshal(raw, &dto); err != nil {
		t.Fatal(err)
	}
	return dto
}

// addSecondAccount 注册并登录另一个真实账号，覆盖跨账号可见性。
func (f *debateFixture) addSecondAccount(t *testing.T) (*http.Cookie, string) {
	t.Helper()
	name := f.randomUsername(t)
	result, id := f.register(t, name, integrationPassword)
	if result.status != http.StatusCreated {
		t.Fatal("注册第二个账号失败")
	}
	f.userIDs = append(f.userIDs, id)
	result, cookie, csrf := f.login(t, name, integrationPassword)
	if result.status != http.StatusOK {
		t.Fatal("登录第二个账号失败")
	}
	return cookie, csrf
}

func containsText(body []byte, needle string) bool { return strings.Contains(string(body), needle) }

func itoa(n int) string { return strconv.Itoa(n) }

// TestCustomTopicIsPrivateAndSnapshotIsStable 覆盖 AC-V2-12 的核心可见性事实：
// 另一账号看不到、题库不出现、快照固定不可改、创建时不调用模型。
func TestCustomTopicIsPrivateAndSnapshotIsStable(t *testing.T) {
	fake := ai.NewFake()
	f := newDebateFixture(t, fake)
	title := "是否应把「远程办公」设为默认？"
	background := "讨论协作成本与人才半径"

	dto := f.createCustom(t, title, background)
	if dto.Topic.ID != nil {
		t.Fatalf("自定义会话的 topic.id 必须是 null，实际 %q", *dto.Topic.ID)
	}
	if dto.Topic.Title != title || dto.Topic.Description != background {
		t.Fatalf("创建响应快照不一致: %+v", dto.Topic)
	}
	if dto.Mode != "standard_4" || dto.TotalRounds != 4 || dto.Status != "active" || dto.CompletedRounds != 0 {
		t.Fatalf("自定义会话仍须固定四轮标准赛: %+v", dto)
	}
	if fake.Calls() != 0 {
		t.Fatal("创建会话不得调用模型")
	}

	// 数据库层核验：topic_id 为 NULL，快照原文与登录用户归属落库。
	var row struct {
		TopicID     *uint64
		Title       string
		Description string
		UserID      uint64
	}
	if err := f.db.ORM.Raw("SELECT topic_id AS topic_id, topic_title_snapshot AS title, topic_description_snapshot AS `description`, user_id AS user_id FROM debates WHERE id = ?", dto.ID).Scan(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.TopicID != nil {
		t.Fatal("自定义会话在库中的 topic_id 必须为 NULL")
	}
	if row.Title != title || row.Description != background {
		t.Fatalf("库中快照被改写: %+v", row)
	}
	if row.UserID != f.uid {
		t.Fatalf("归属必须取自登录会话: got %d want %d", row.UserID, f.uid)
	}

	// 公共题库不出现该题（题库只来自 topics 表）。
	var leaked int64
	if err := f.db.ORM.Table("topics").Where("title = ?", title).Count(&leaked).Error; err != nil || leaked != 0 {
		t.Fatalf("自定义题进入了公共题库: count=%d err=%v", leaked, err)
	}
	list := f.request(t, "GET", "/api/v1/topics", nil)
	if list.status != http.StatusOK || containsText(list.body, title) {
		t.Fatal("公共题库响应出现了自定义题或查询失败")
	}

	// 本人详情与历史可读同一份快照。
	detail := f.request(t, "GET", "/api/v1/debates/"+dto.ID, nil)
	if detail.status != http.StatusOK || !containsText(detail.body, title) {
		t.Fatal("本人详情未返回快照原文")
	}
	history := f.request(t, "GET", "/api/v1/debates?page=1&page_size=20", nil)
	if history.status != http.StatusOK || !containsText(history.body, title) {
		t.Fatal("本人历史未返回快照原文")
	}

	// 另一账号：详情、历史与生成入口全部表现为不存在。
	otherCookie, otherCSRF := f.addSecondAccount(t)
	otherDetail := f.do(t, "GET", "/api/v1/debates/"+dto.ID, "", integrationOrigin, "", otherCookie)
	if otherDetail.status != http.StatusNotFound || otherDetail.errorCode() != "not_found" {
		t.Fatalf("跨账号详情必须 404，实际 %d %s", otherDetail.status, otherDetail.errorCode())
	}
	otherList := f.do(t, "GET", "/api/v1/debates?page=1&page_size=20", "", integrationOrigin, "", otherCookie)
	if otherList.status != http.StatusOK || containsText(otherList.body, title) {
		t.Fatal("跨账号历史出现了他人自定义题")
	}
	turnBody, _ := json.Marshal(map[string]any{"round_no": 1, "content": "越权发言", "client_request_id": uuid.NewString()})
	otherTurn := f.do(t, "POST", "/api/v1/debates/"+dto.ID+"/turns", string(turnBody), integrationOrigin, otherCSRF, otherCookie)
	if otherTurn.status != http.StatusNotFound {
		t.Fatalf("跨账号生成入口必须 404，实际 %d", otherTurn.status)
	}

	// 无编辑入口；快照保持不变。
	editBody, _ := json.Marshal(map[string]any{"custom_topic": map[string]any{"title": "改题"}})
	edit := f.do(t, "PATCH", "/api/v1/debates/"+dto.ID, string(editBody), integrationOrigin, f.csrf, f.cookie)
	if edit.status != http.StatusNotFound && edit.status != http.StatusMethodNotAllowed {
		t.Fatalf("不应存在编辑接口，实际 %d", edit.status)
	}
	var unchanged struct{ Title string }
	if err := f.db.ORM.Raw("SELECT topic_title_snapshot AS title FROM debates WHERE id = ?", dto.ID).Scan(&unchanged).Error; err != nil || unchanged.Title != title {
		t.Fatalf("快照被改写: %q err=%v", unchanged.Title, err)
	}

	// 预设创建路径保持可用，快照仍带字符串 ID。
	preset := f.request(t, "POST", "/api/v1/debates", map[string]any{"topic_id": "1", "user_stance": "con"})
	if preset.status != http.StatusCreated {
		t.Fatalf("预设创建应仍为 201，实际 %d", preset.status)
	}
	var presetDTO debate.DebateDTO
	raw, _ := json.Marshal(preset.data())
	if err := json.Unmarshal(raw, &presetDTO); err != nil {
		t.Fatal(err)
	}
	if presetDTO.Topic.ID == nil || *presetDTO.Topic.ID != "1" {
		t.Fatalf("预设会话 topic.id 必须仍为字符串，实际 %+v", presetDTO.Topic.ID)
	}
	if fake.Calls() != 0 {
		t.Fatal("创建与查询路径不得调用模型")
	}
}

// TestCustomTopicRunsThroughEveryGenerationPath 验证对辩、重试、SSE、策略与总结
// 都从同一份会话快照取题，且出站 system 不含用户题目原文。
func TestCustomTopicRunsThroughEveryGenerationPath(t *testing.T) {
	f := newDebateFixture(t, ai.NewFake())
	title := "自定义题是否应记住原文？"
	background := "背景：验证快照稳定"
	dto := f.createCustom(t, title, background)
	path := "/api/v1/debates/" + dto.ID

	// 策略卡只在会话尚未开始任何轮次时认领（ClaimStrategy 要求 turns = 0），
	// 因此必须先于对辩生成；它同样从会话快照取题。
	strategyFake := ai.NewFake()
	f.mount(t, strategyFake)
	strategy := f.request(t, "POST", path+"/strategy", map[string]any{})
	if strategy.status != http.StatusCreated {
		t.Fatalf("策略生成失败: %d %s", strategy.status, strategy.errorCode())
	}
	assertCustomStrategyPrompt(t, strategyFake, title, ai.CustomStrategyPromptVersion)

	// 对辩阶段换用带一次超时脚本的 Fake：第一个请求确定失败，随后重试成功，
	// 其余轮次与总结在脚本耗尽后走确定性回声。
	turnFake := ai.NewFake(ai.FakeStep{Err: &ai.Error{Code: ai.CodeModelTimeout}})
	f.mount(t, turnFake)

	// 第一轮先失败：验证失败路径也不把题目写进 system，且重试复用快照。
	failed := f.request(t, "POST", path+"/turns", map[string]any{
		"round_no": 1, "content": "首轮立论", "client_request_id": uuid.NewString(),
	})
	if failed.status != http.StatusGatewayTimeout {
		t.Fatalf("首轮应超时失败，实际 %d", failed.status)
	}
	assertCustomTurnPrompt(t, turnFake, title, ai.CustomPromptVersion)
	var failure struct {
		Error struct {
			Details struct {
				TurnID         string `json:"turn_id"`
				AttemptVersion uint64 `json:"attempt_version"`
			} `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(failed.body, &failure)
	if failure.Error.Details.TurnID == "" {
		t.Fatalf("失败响应必须带可查询的 turn_id: %s", failed.body)
	}
	retried := f.request(t, "POST", path+"/turns/"+failure.Error.Details.TurnID+"/retry", map[string]any{
		"expected_attempt_version": failure.Error.Details.AttemptVersion,
	})
	if retried.status != http.StatusOK {
		t.Fatalf("显式重试应成功，实际 %d %s", retried.status, retried.errorCode())
	}
	assertCustomTurnPrompt(t, turnFake, title, ai.CustomPromptVersion)

	// 第二至四轮走 SSE：事件契约不变，最终事务提交后才 done。
	for round := 2; round <= 4; round++ {
		resp := openTurnStream(t, f, path, map[string]any{
			"round_no": round, "content": "流式第 " + itoa(round) + " 轮", "client_request_id": uuid.NewString(),
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("第 %d 轮 SSE HTTP=%d", round, resp.StatusCode)
		}
		reader := bufio.NewReader(resp.Body)
		done := false
		for !done {
			frame := readFrame(t, reader)
			if strings.HasPrefix(frame, "event: done") {
				done = true
			}
			if strings.HasPrefix(frame, "event: error") {
				t.Fatalf("第 %d 轮 SSE 收到 error: %s", round, frame)
			}
		}
		assertCustomTurnPrompt(t, turnFake, title, ai.CustomPromptVersion)
	}

	// 四轮完成后总结复用同一会话快照。
	// 注意：本用例已用掉该账号每分钟 6 次写入口配额（策略 1 + 对辩 5），
	// 因此总结在独立用例中用新账号验证，避免把 429 误判为功能缺陷。
}

// TestCustomTopicSummaryUsesSnapshot 在同一会话完成四轮后生成总结，
// 使用独立账号以留出每分钟 6 次写入口配额（4 轮 + 总结 = 5 次）。
func TestCustomTopicSummaryUsesSnapshot(t *testing.T) {
	fake := ai.NewFake()
	f := newDebateFixture(t, fake)
	title := "自定义题总结是否复用快照？"
	background := "背景：总结阶段"
	dto := f.createCustom(t, title, background)
	path := "/api/v1/debates/" + dto.ID

	for round := 1; round <= 4; round++ {
		r := f.request(t, "POST", path+"/turns", map[string]any{
			"round_no": round, "content": "第 " + itoa(round) + " 轮发言", "client_request_id": uuid.NewString(),
		})
		if r.status != http.StatusCreated {
			t.Fatalf("第 %d 轮失败: %d %s", round, r.status, r.errorCode())
		}
		assertCustomTurnPrompt(t, fake, title, ai.CustomPromptVersion)
	}
	summary := f.request(t, "POST", path+"/summary", map[string]any{})
	if summary.status != http.StatusCreated {
		t.Fatalf("总结生成失败: %d %s", summary.status, summary.errorCode())
	}
	assertCustomSummaryPrompt(t, fake, title, ai.CustomSummaryPromptVersion)
}

// assertCustomTurnPrompt 校验最近一次对辩调用的出站结构。
func assertCustomTurnPrompt(t *testing.T, fake *ai.Fake, title, wantVersion string) {
	t.Helper()
	records := fake.Records()
	if len(records) == 0 {
		t.Fatal("没有对辩调用记录")
	}
	record := records[len(records)-1]
	if !record.Request.Custom {
		t.Fatal("自定义会话的生成请求必须标记 Custom")
	}
	if record.Prompt.Version != wantVersion {
		t.Fatalf("Prompt 版本应为 %q，实际 %q", wantVersion, record.Prompt.Version)
	}
	assertSystemWithoutTitleAndDataBlock(t, record.Prompt, title)
}

func assertCustomStrategyPrompt(t *testing.T, fake *ai.Fake, title, wantVersion string) {
	t.Helper()
	// 策略与对辩共用 Fake 的 records 列表，按版本筛选出策略调用。
	records := fake.Records()
	var found *ai.FakeRecord
	for i := range records {
		if records[i].Prompt.Version == wantVersion {
			found = &records[i]
		}
	}
	if found == nil {
		t.Fatalf("没有版本为 %q 的策略调用记录", wantVersion)
	}
	assertSystemWithoutTitleAndDataBlock(t, found.Prompt, title)
}

func assertCustomSummaryPrompt(t *testing.T, fake *ai.Fake, title, wantVersion string) {
	t.Helper()
	records := fake.SummaryRecords()
	if len(records) == 0 {
		t.Fatal("没有总结调用记录")
	}
	record := records[len(records)-1]
	if record.Prompt.Version != wantVersion {
		t.Fatalf("总结 Prompt 版本应为 %q，实际 %q", wantVersion, record.Prompt.Version)
	}
	assertSystemWithoutTitleAndDataBlock(t, record.Prompt, title)
}

// assertSystemWithoutTitleAndDataBlock 断言 system 不含题目原文、
// 第一条 user 消息带题目数据段，且角色序列未被用户文本改写。
func assertSystemWithoutTitleAndDataBlock(t *testing.T, prompt ai.Prompt, title string) {
	t.Helper()
	if len(prompt.Messages) < 2 {
		t.Fatalf("Prompt 消息不足: %+v", prompt.Messages)
	}
	if prompt.Messages[0].Role != ai.RoleSystem {
		t.Fatal("首条消息必须是 system")
	}
	if strings.Contains(prompt.Messages[0].Content, title) {
		t.Fatal("system 规则段不得包含用户题目原文")
	}
	if !strings.Contains(prompt.Messages[1].Content, title) {
		t.Fatal("题目数据段应挂在第一条 user 消息")
	}
	if prompt.Messages[1].Role != ai.RoleUser {
		t.Fatalf("数据段所在消息必须是 user，实际 %q", prompt.Messages[1].Role)
	}
}
