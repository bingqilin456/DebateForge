package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/bingqilin456/DebateForge/backend/internal/debate"
	"github.com/bingqilin456/DebateForge/backend/internal/topic"
	"github.com/google/uuid"
)

// TestPhase2EndToEndAC01ToAC08 在隔离 MySQL 测试库（127.0.0.1:13307）与 Fake 模型下
// 完整串联 AC01–AC08：注册登录退出重登 → 辩题浏览与正反建会话 → 连续三轮六条正式消息与第 4 轮 409
// → 单轮失败保留原文与 expected_attempt_version 显式重试（旧版本 409 不调模型）
// → 90s 租约过期回收与迟到旧尝试拦截 → 结构化总结生成与重试 → 服务重建重读、历史分页与跨用户 404 隔离。
func TestPhase2EndToEndAC01ToAC08(t *testing.T) {
	var turnCalls atomic.Int32
	var summaryCalls atomic.Int32
	var round2Attempt atomic.Int32
	var round3Attempt atomic.Int32
	var summaryAttempt atomic.Int32

	round3Started := make(chan struct{})
	round3Release := make(chan struct{})
	defer close(round3Release)

	gen := testCombinedGenerator{
		genTurn: func(c context.Context, r ai.Request) (ai.Result, error) {
			turnCalls.Add(1)
			switch r.RoundNo {
			case 1:
				return ai.NewFake().Generate(c, r)
			case 2:
				switch round2Attempt.Add(1) {
				case 1:
					return ai.Result{}, &ai.Error{Code: ai.CodeModelTimeout}
				case 2:
					return ai.Result{}, &ai.Error{Code: ai.CodeModelError}
				default:
					return ai.NewFake().Generate(c, r)
				}
			case 3:
				if round3Attempt.Add(1) == 1 {
					close(round3Started)
					select {
					case <-round3Release:
						return ai.NewFake().Generate(c, r)
					case <-c.Done():
						return ai.Result{}, c.Err()
					}
				}
				return ai.NewFake().Generate(c, r)
			default:
				return ai.NewFake().Generate(c, r)
			}
		},
		genSummary: func(c context.Context, r ai.SummaryRequest) (ai.SummaryResult, error) {
			summaryCalls.Add(1)
			switch summaryAttempt.Add(1) {
			case 1:
				// 首次总结返回非法 Schema（空数组），验证转受控 502 model_error
				return ai.NewFake(ai.FakeStep{Result: ai.Result{Content: `{"strengths":[],"issues":[],"suggestions":[]}`}}).GenerateSummary(c, r)
			case 2:
				// 第一次显式重试模拟模型超时（504 model_timeout）
				return ai.SummaryResult{}, &ai.Error{Code: ai.CodeModelTimeout}
			default:
				return ai.NewFake().GenerateSummary(c, r)
			}
		},
	}

	f := newDebateFixture(t, gen)
	var moment atomic.Int64
	moment.Store(time.Date(2026, 11, 12, 1, 0, 0, 0, time.UTC).Unix())
	f.gate = debate.NewGate(func() time.Time { return time.Unix(moment.Load(), 0) })
	f.mount(t, gen)

	// ==================== AC01：注册、重复注册 409、错密 401、退出失效与重新登录 ====================
	userA := f.randomUsername(t)
	regRes, _ := f.register(t, userA, integrationPassword)
	assertStatus(t, regRes, http.StatusCreated, "")

	dupRes, _ := f.register(t, strings.ToUpper(userA), integrationPassword)
	assertStatus(t, dupRes, http.StatusConflict, "username_conflict")

	badLogin, _, _ := f.login(t, userA, "wrong-password-999")
	assertStatus(t, badLogin, http.StatusUnauthorized, "invalid_credentials")

	loginRes, cookieA1, csrfA1 := f.login(t, userA, integrationPassword)
	assertStatus(t, loginRes, http.StatusOK, "")
	f.sessionIDByToken(t, cookieA1.Value)

	meRes := f.do(t, "GET", "/api/v1/auth/me", "", "", "", cookieA1)
	assertStatus(t, meRes, http.StatusOK, "")

	logoutRes := f.do(t, "POST", "/api/v1/auth/logout", "", integrationOrigin, csrfA1, cookieA1)
	assertStatus(t, logoutRes, http.StatusNoContent, "")

	afterLogoutMe := f.do(t, "GET", "/api/v1/auth/me", "", "", "", cookieA1)
	assertStatus(t, afterLogoutMe, http.StatusUnauthorized, "unauthenticated")

	// 重新登录 User A 作为后续全链路操作身份
	loginRes2, cookieA, csrfA := f.login(t, userA, integrationPassword)
	assertStatus(t, loginRes2, http.StatusOK, "")
	f.sessionIDByToken(t, cookieA.Value)
	f.cookie = cookieA
	f.csrf = csrfA

	// ==================== AC02：辩题列表、正反立场创建、快照与非法输入拒绝 ====================
	topicsRes := f.request(t, "GET", "/api/v1/topics", nil)
	assertStatus(t, topicsRes, http.StatusOK, "")
	var topics []topic.TopicDTO
	if err := json.Unmarshal(topicsRes.data()["items"], &topics); err != nil || len(topics) < 5 {
		t.Fatalf("expected at least 5 topics, got %d err=%v", len(topics), err)
	}

	for _, badStance := range []string{"", "neutral", "PRO"} {
		r := f.request(t, "POST", "/api/v1/debates", map[string]any{"topic_id": "1", "user_stance": badStance})
		if r.status != http.StatusBadRequest || r.errorCode() != "invalid_request" {
			t.Fatalf("invalid stance %q status=%d code=%s", badStance, r.status, r.errorCode())
		}
	}
	notFoundTopic := f.request(t, "POST", "/api/v1/debates", map[string]any{"topic_id": "999999", "user_stance": "pro"})
	if notFoundTopic.status != http.StatusNotFound || notFoundTopic.errorCode() != "not_found" {
		t.Fatalf("non-existent topic status=%d code=%s", notFoundTopic.status, notFoundTopic.errorCode())
	}

	// 创建正方会话（Debate 1）与反方会话（Debate 2）
	createProRes := f.request(t, "POST", "/api/v1/debates", map[string]any{"topic_id": topics[0].ID, "user_stance": "pro"})
	assertStatus(t, createProRes, http.StatusCreated, "")
	var debatePro debate.DebateDTO
	rawPro, _ := json.Marshal(createProRes.data())
	if err := json.Unmarshal(rawPro, &debatePro); err != nil {
		t.Fatal(err)
	}
	if debatePro.UserStance != "pro" || debatePro.AIStance != "con" || debatePro.Status != "active" || debatePro.CompletedRounds != 0 ||
		debatePro.Topic.ID != topics[0].ID || debatePro.Topic.Title != topics[0].Title || debatePro.Topic.Description != topics[0].Description {
		t.Fatalf("unexpected pro debate DTO: %+v", debatePro)
	}
	// AC03/AC04/AC05/AC06 验证的是历史 quick_3 语义（三轮六条消息、第 4 轮 409）。
	// 新会话默认 standard_4，这里显式切回 quick_3 以保持这些验收条目原有含义；
	// 四轮赛制本身由 TestStandard4FourRoundsEndToEnd 单独覆盖。
	f.markQuick3(t, debatePro.ID)

	createConRes := f.request(t, "POST", "/api/v1/debates", map[string]any{"topic_id": topics[1].ID, "user_stance": "con"})
	assertStatus(t, createConRes, http.StatusCreated, "")
	var debateCon debate.DebateDTO
	rawCon, _ := json.Marshal(createConRes.data())
	if err := json.Unmarshal(rawCon, &debateCon); err != nil {
		t.Fatal(err)
	}
	if debateCon.UserStance != "con" || debateCon.AIStance != "pro" || debateCon.Status != "active" || debateCon.CompletedRounds != 0 ||
		debateCon.Topic.ID != topics[1].ID || debateCon.Topic.Title != topics[1].Title {
		t.Fatalf("unexpected con debate DTO: %+v", debateCon)
	}
	if turnCalls.Load() != 0 {
		t.Fatalf("creating debates must not call model, got %d calls", turnCalls.Load())
	}

	path1 := "/api/v1/debates/" + debatePro.ID
	path2 := "/api/v1/debates/" + debateCon.ID

	// ==================== AC03 & AC04 & AC05：三轮推进、单轮失败重试、旧版本 409、90s 租约回收 ====================
	// 第 1 轮成功 + 幂等重放 200 不调模型
	keyR1 := uuid.NewString()
	r1Res := f.request(t, "POST", path1+"/turns", submit("第一轮正方立论：广泛探索有助于建立跨学科视野", keyR1, 1))
	assertStatus(t, r1Res, http.StatusCreated, "")
	moment.Add(15)
	callsAfterR1 := turnCalls.Load()
	r1Replay := f.request(t, "POST", path1+"/turns", submit("第一轮正方立论：广泛探索有助于建立跨学科视野", keyR1, 1))
	if r1Replay.status != http.StatusOK || turnCalls.Load() != callsAfterR1 {
		t.Fatalf("round 1 replay status=%d calls=%d", r1Replay.status, turnCalls.Load())
	}
	moment.Add(15)

	// 第 2 轮首次超时（504 model_timeout），验证保留用户原文、完成轮数仍为 1
	keyR2 := uuid.NewString()
	const r2Content = "第二轮正方攻辩：过早锁定方向容易陷入局部最优"
	r2Fail1 := f.request(t, "POST", path1+"/turns", submit(r2Content, keyR2, 2))
	assertStatus(t, r2Fail1, http.StatusGatewayTimeout, "model_timeout")
	var failEnv struct {
		Error struct {
			Details *debate.FailureDetails `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(r2Fail1.body, &failEnv)
	if failEnv.Error.Details == nil || failEnv.Error.Details.AttemptVersion != 1 || failEnv.Error.Details.TurnID == "" {
		t.Fatalf("round 2 failure details unexpected: %+v", failEnv.Error.Details)
	}
	turn2ID := failEnv.Error.Details.TurnID
	moment.Add(15)

	detailAfterR2Fail := f.request(t, "GET", path1, nil)
	var detailDTO debate.DetailDTO
	rawDetail, _ := json.Marshal(detailAfterR2Fail.data())
	_ = json.Unmarshal(rawDetail, &detailDTO)
	if detailDTO.Debate.CompletedRounds != 1 || detailDTO.Debate.Status != "active" || len(detailDTO.Turns) != 2 ||
		detailDTO.Turns[1].Status != "failed" || detailDTO.Turns[1].AttemptVersion != 1 ||
		len(detailDTO.Turns[1].Messages) != 1 || detailDTO.Turns[1].Messages[0].Content != r2Content {
		t.Fatalf("round 2 failed state did not retain original user message: %+v", detailDTO)
	}

	// 第 2 轮第一次显式重试（expected_attempt_version=1）再次失败（502 model_error，attempt_version=2）
	retryPath2 := path1 + "/turns/" + turn2ID + "/retry"
	r2Fail2 := f.request(t, "POST", retryPath2, map[string]any{"expected_attempt_version": 1})
	assertStatus(t, r2Fail2, http.StatusBadGateway, "model_error")
	_ = json.Unmarshal(r2Fail2.body, &failEnv)
	if failEnv.Error.Details == nil || failEnv.Error.Details.AttemptVersion != 2 {
		t.Fatalf("expected attempt_version=2 after retry failure, got %+v", failEnv.Error.Details)
	}
	callsAfterR2Fail2 := turnCalls.Load()
	moment.Add(15)

	// 重发旧版本 expected_attempt_version=1 必须返回 409 attempt_version_conflict 且不调模型
	r2Stale := f.request(t, "POST", retryPath2, map[string]any{"expected_attempt_version": 1})
	assertStatus(t, r2Stale, http.StatusConflict, "attempt_version_conflict")
	_ = json.Unmarshal(r2Stale.body, &failEnv)
	if failEnv.Error.Details == nil || failEnv.Error.Details.AttemptVersion != 2 || turnCalls.Load() != callsAfterR2Fail2 {
		t.Fatalf("stale retry must return version 2 without calling model: details=%+v calls=%d", failEnv.Error.Details, turnCalls.Load())
	}
	moment.Add(15)

	// 使用最新版本 expected_attempt_version=2 显式重试成功（200 OK，completed_rounds=2，attempt_version=3）
	r2Success := f.request(t, "POST", retryPath2, map[string]any{"expected_attempt_version": 2})
	assertStatus(t, r2Success, http.StatusOK, "")
	var r2TurnData debate.TurnData
	rawR2, _ := json.Marshal(r2Success.data())
	_ = json.Unmarshal(rawR2, &r2TurnData)
	if r2TurnData.CompletedRounds != 2 || r2TurnData.Turn.AttemptVersion != 3 || r2TurnData.Turn.Status != "completed" ||
		len(r2TurnData.Turn.Messages) != 2 || r2TurnData.Turn.Messages[0].Content != r2Content {
		t.Fatalf("round 2 retry success unexpected: %+v", r2TurnData)
	}
	moment.Add(15)

	// 第 3 轮：模拟在途生成、校验 90s 租约、同键重放 202、租约过期扫描回收、迟到写回拦截、显式重试完成第 3 轮
	keyR3 := uuid.NewString()
	const r3Content = "第三轮正方总结：通识探索与专业深耕并非对立，探索是深耕的前提"
	r3Done := make(chan apiResult, 1)
	go func() {
		r3Done <- f.request(t, "POST", path1+"/turns", submit(r3Content, keyR3, 3))
	}()
	select {
	case <-round3Started:
	case <-time.After(3 * time.Second):
		t.Fatal("round 3 generator did not start")
	}

	// 核对数据库中第 3 轮的初始租约为 90 秒窗口（LeaseDuration = 90s）
	var leaseCheck struct {
		CreatedAt      time.Time
		LeaseExpiresAt *time.Time
	}
	if err := f.db.ORM.Table("debate_turns").Select("created_at, lease_expires_at").Where("debate_id = ? AND round_no = 3", debatePro.ID).First(&leaseCheck).Error; err != nil || leaseCheck.LeaseExpiresAt == nil {
		t.Fatalf("failed to read round 3 lease_expires_at: %v", err)
	}
	leaseDiff := leaseCheck.LeaseExpiresAt.Sub(leaseCheck.CreatedAt)
	if leaseDiff < 85*time.Second || leaseDiff > 95*time.Second {
		t.Fatalf("expected ~90s lease duration, got %v", leaseDiff)
	}

	// 生成中重放同一 client_request_id 返回 202 Accepted，不增加模型调用
	callsDuringR3 := turnCalls.Load()
	moment.Add(15)
	r3Pending := f.request(t, "POST", path1+"/turns", submit(r3Content, keyR3, 3))
	if r3Pending.status != http.StatusAccepted || turnCalls.Load() != callsDuringR3 {
		t.Fatalf("expected 202 during generating, got status=%d calls=%d", r3Pending.status, turnCalls.Load())
	}

	// 模拟 90s 租约已过期并执行扫描回收
	expiredAt := time.Now().UTC().Add(-2 * time.Second)
	if err := f.db.ORM.Exec("UPDATE debate_turns SET lease_expires_at = ? WHERE debate_id = ? AND round_no = 3", expiredAt, debatePro.ID).Error; err != nil {
		t.Fatal(err)
	}
	recoveredCount, err := f.service.RecoverExpiredLeases(context.Background())
	if err != nil || recoveredCount != 1 {
		t.Fatalf("expected 1 expired turn lease recovered, got %d err=%v", recoveredCount, err)
	}

	// 放行旧的第 3 轮在途请求，旧尝试因状态已非 generating 必须失败（503），不得写入 assistant 消息
	round3Release <- struct{}{}
	if oldRes := <-r3Done; oldRes.status != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when stale attempt finishes after lease recovery, got %d", oldRes.status)
	}

	detailAfterLease := f.request(t, "GET", path1, nil)
	rawDetail, _ = json.Marshal(detailAfterLease.data())
	_ = json.Unmarshal(rawDetail, &detailDTO)
	if detailDTO.Debate.CompletedRounds != 2 || detailDTO.Debate.Status != "active" || len(detailDTO.Turns) != 3 ||
		detailDTO.Turns[2].Status != "failed" || detailDTO.Turns[2].ErrorCode == nil || *detailDTO.Turns[2].ErrorCode != "model_timeout" ||
		len(detailDTO.Turns[2].Messages) != 1 {
		t.Fatalf("round 3 after lease recovery unexpected: %+v", detailDTO)
	}
	turn3ID := detailDTO.Turns[2].ID

	// 显式重试第 3 轮成功（expected_attempt_version=1 -> 200 OK，completed_rounds=3）
	moment.Add(15)
	r3Retry := f.request(t, "POST", path1+"/turns/"+turn3ID+"/retry", map[string]any{"expected_attempt_version": 1})
	assertStatus(t, r3Retry, http.StatusOK, "")

	// 第 4 轮提交被拒绝（409 round_conflict），不调用模型
	callsBeforeR4 := turnCalls.Load()
	moment.Add(15)
	r4Res := f.request(t, "POST", path1+"/turns", submit("第四轮越界发言", uuid.NewString(), 4))
	assertStatus(t, r4Res, http.StatusConflict, "round_conflict")
	if turnCalls.Load() != callsBeforeR4 {
		t.Fatal("round 4 conflict called model")
	}

	// 校验数据库与详情恰好 3 轮 6 条正式消息且会话状态为 completed
	detailCompleted := f.request(t, "GET", path1, nil)
	rawDetail, _ = json.Marshal(detailCompleted.data())
	_ = json.Unmarshal(rawDetail, &detailDTO)
	if detailDTO.Debate.Status != "completed" || detailDTO.Debate.CompletedRounds != 3 || len(detailDTO.Turns) != 3 {
		t.Fatalf("expected completed debate with 3 turns, got %+v", detailDTO.Debate)
	}
	for i, tr := range detailDTO.Turns {
		if tr.RoundNo != i+1 || tr.Status != "completed" || len(tr.Messages) != 2 || tr.Messages[0].Role != "user" || tr.Messages[1].Role != "assistant" {
			t.Fatalf("round %d formal messages invalid: %+v", i+1, tr)
		}
	}
	var dbMsgCount int64
	f.db.ORM.Table("messages").Joins("JOIN debate_turns ON messages.turn_id = debate_turns.id").Where("debate_turns.debate_id = ?", debatePro.ID).Count(&dbMsgCount)
	if dbMsgCount != 6 {
		t.Fatalf("expected 6 formal messages in DB, got %d", dbMsgCount)
	}

	// ==================== AC06：结构化总结生成、Schema 校验失败、显式重试与旧版本 409 ====================
	// 未完成会话（Debate 2）请求生成总结返回 409 round_conflict
	moment.Add(15)
	unfinishedSum := f.request(t, "POST", path2+"/summary", map[string]any{"retry": false})
	assertStatus(t, unfinishedSum, http.StatusConflict, "round_conflict")

	// 已完成会话首次总结因空数组 Schema 校验失败返回 502 model_error（attempt_version=1）
	moment.Add(15)
	sumFail1 := f.request(t, "POST", path1+"/summary", map[string]any{"retry": false})
	assertStatus(t, sumFail1, http.StatusBadGateway, "model_error")
	_ = json.Unmarshal(sumFail1.body, &failEnv)
	if failEnv.Error.Details == nil || failEnv.Error.Details.AttemptVersion != 1 || failEnv.Error.Details.QueryURL != path1+"/summary" {
		t.Fatalf("summary first failure details unexpected: %+v", failEnv.Error.Details)
	}

	// retry=false 遇到 failed 状态直接返回 200 与 failed 状态，不重调模型
	sumCallsAfterFail1 := summaryCalls.Load()
	moment.Add(15)
	sumNoRetry := f.request(t, "POST", path1+"/summary", map[string]any{"retry": false})
	assertStatus(t, sumNoRetry, http.StatusOK, "")
	if summaryCalls.Load() != sumCallsAfterFail1 {
		t.Fatal("summary retry=false on failed state called model")
	}

	// 第一次显式重试（expected_attempt_version=1）超时失败（504 model_timeout，attempt_version=2）
	moment.Add(15)
	sumFail2 := f.request(t, "POST", path1+"/summary", map[string]any{"retry": true, "expected_attempt_version": 1})
	assertStatus(t, sumFail2, http.StatusGatewayTimeout, "model_timeout")
	_ = json.Unmarshal(sumFail2.body, &failEnv)
	if failEnv.Error.Details == nil || failEnv.Error.Details.AttemptVersion != 2 {
		t.Fatalf("summary second failure details unexpected: %+v", failEnv.Error.Details)
	}
	sumCallsAfterFail2 := summaryCalls.Load()

	// 重发旧版本 expected_attempt_version=1 返回 409 attempt_version_conflict，不调模型
	moment.Add(15)
	sumStale := f.request(t, "POST", path1+"/summary", map[string]any{"retry": true, "expected_attempt_version": 1})
	assertStatus(t, sumStale, http.StatusConflict, "attempt_version_conflict")
	if summaryCalls.Load() != sumCallsAfterFail2 {
		t.Fatal("stale summary retry called model")
	}

	// 使用最新版本 expected_attempt_version=2 显式重试成功（200 OK，attempt_version=3，schema_version=1）
	moment.Add(15)
	sumSuccess := f.request(t, "POST", path1+"/summary", map[string]any{"retry": true, "expected_attempt_version": 2})
	assertStatus(t, sumSuccess, http.StatusOK, "")
	var sumStatus debate.SummaryStatus
	rawSum, _ := json.Marshal(sumSuccess.data())
	_ = json.Unmarshal(rawSum, &sumStatus)
	if sumStatus.Status != "completed" || sumStatus.AttemptVersion == nil || *sumStatus.AttemptVersion != 3 ||
		sumStatus.SchemaVersion == nil || *sumStatus.SchemaVersion != 1 || sumStatus.Content == nil ||
		len(sumStatus.Content.Strengths) == 0 || len(sumStatus.Content.Issues) == 0 || len(sumStatus.Content.Suggestions) == 0 {
		t.Fatalf("summary retry success unexpected: %+v", sumStatus)
	}

	// ==================== AC07 & AC08：服务重建重读、未完成会话继续、历史分页与跨用户 404 隔离 ====================
	f.mount(t, gen)

	// 重建后重读已完成会话 1（含 6 条消息与结构化总结）
	reRead1 := f.request(t, "GET", path1, nil)
	assertStatus(t, reRead1, http.StatusOK, "")
	rawDetail, _ = json.Marshal(reRead1.data())
	_ = json.Unmarshal(rawDetail, &detailDTO)
	if detailDTO.Debate.Status != "completed" || len(detailDTO.Turns) != 3 ||
		detailDTO.SummaryStatus.Status != "completed" || detailDTO.SummaryStatus.Content == nil {
		t.Fatalf("re-read completed debate after remount failed: %+v", detailDTO)
	}

	// 未完成会话 2 可继续提交第 1 轮
	moment.Add(15)
	conR1 := f.request(t, "POST", path2+"/turns", submit("反方首轮发言：尽早专注能更快形成核心竞争力", uuid.NewString(), 1))
	assertStatus(t, conR1, http.StatusCreated, "")

	// 本人历史分页查询：仅包含 User A 的两场会话（排除 fixture 初始空用户），支持 page/page_size
	listPage1 := f.request(t, "GET", "/api/v1/debates?page=1&page_size=1", nil)
	assertStatus(t, listPage1, http.StatusOK, "")
	var page1DTO debate.DebateListDTO
	rawPage1, _ := json.Marshal(listPage1.data())
	_ = json.Unmarshal(rawPage1, &page1DTO)
	if page1DTO.Total != 2 || page1DTO.Page != 1 || page1DTO.PageSize != 1 || len(page1DTO.Items) != 1 || page1DTO.Items[0].ID != debateCon.ID {
		t.Fatalf("unexpected page 1 history: %+v", page1DTO)
	}
	listPage2 := f.request(t, "GET", "/api/v1/debates?page=2&page_size=1", nil)
	assertStatus(t, listPage2, http.StatusOK, "")
	var page2DTO debate.DebateListDTO
	rawPage2, _ := json.Marshal(listPage2.data())
	_ = json.Unmarshal(rawPage2, &page2DTO)
	if page2DTO.Total != 2 || page2DTO.Page != 2 || page2DTO.PageSize != 1 || len(page2DTO.Items) != 1 || page2DTO.Items[0].ID != debatePro.ID {
		t.Fatalf("unexpected page 2 history: %+v", page2DTO)
	}

	// 注册并登录用户 B，验证跨用户列表为空且详情/发言/重试/总结全部统一返回 404 not_found
	userB := f.randomUsername(t)
	if r, _ := f.register(t, userB, integrationPassword); r.status != http.StatusCreated {
		t.Fatal("register user B")
	}
	loginB, cookieB, csrfB := f.login(t, userB, integrationPassword)
	assertStatus(t, loginB, http.StatusOK, "")
	f.sessionIDByToken(t, cookieB.Value)

	listB := f.do(t, "GET", "/api/v1/debates?page=1&page_size=20", "", "", "", cookieB)
	assertStatus(t, listB, http.StatusOK, "")
	var listBDTO debate.DebateListDTO
	rawListB, _ := json.Marshal(listB.data())
	_ = json.Unmarshal(rawListB, &listBDTO)
	if listBDTO.Total != 0 || len(listBDTO.Items) != 0 {
		t.Fatalf("user B history leaked user A debates: %+v", listBDTO)
	}

	for _, foreignReq := range []struct {
		method string
		path   string
		body   string
	}{
		{"GET", path1, ""},
		{"POST", path2 + "/turns", `{"round_no":2,"content":"越权发言","client_request_id":"` + uuid.NewString() + `"}`},
		{"POST", path1 + "/turns/" + turn2ID + "/retry", `{"expected_attempt_version":3}`},
		{"GET", path1 + "/summary", ""},
		{"POST", path1 + "/summary", `{"retry":false}`},
	} {
		origin, csrf := "", ""
		if foreignReq.method == "POST" {
			origin, csrf = integrationOrigin, csrfB
		}
		res := f.do(t, foreignReq.method, foreignReq.path, foreignReq.body, origin, csrf, cookieB)
		assertStatus(t, res, http.StatusNotFound, "not_found")
	}
}

// TestPhase2AdverseBoundaryCodePointsAndIdempotency 验证 2000/2001 Unicode 码点边界、
// 模型回复超长拒绝以及重复 client_request_id 携带不同内容或轮次的幂等冲突回归。
func TestPhase2AdverseBoundaryCodePointsAndIdempotency(t *testing.T) {
	var calls atomic.Int32
	fake := ai.NewFake()
	gen := testGenerator(func(c context.Context, r ai.Request) (ai.Result, error) {
		calls.Add(1)
		return fake.Generate(c, r)
	})
	f := newDebateFixture(t, gen)
	var moment atomic.Int64
	moment.Store(time.Date(2026, 11, 12, 2, 0, 0, 0, time.UTC).Unix())
	f.gate = debate.NewGate(func() time.Time { return time.Unix(moment.Load(), 0) })
	f.mount(t, gen)

	id := f.createQuick3(t) // 历史 quick_3 语义：本用例验证三轮边界与总结六条消息
	path := "/api/v1/debates/" + id

	// 1. 2001 个 Unicode 码点（ASCII、中文 BMP、4 字节扩展汉字/Emoji）均返回 400，不调用模型、不写库
	for _, sample := range []string{
		strings.Repeat("a", 2001),
		strings.Repeat("辩", 2001),
		strings.Repeat("𠮷", 2001),
	} {
		if utf8.RuneCountInString(sample) != 2001 {
			t.Fatal("invalid test rune count")
		}
		r := f.request(t, "POST", path+"/turns", submit(sample, uuid.NewString(), 1))
		assertStatus(t, r, http.StatusBadRequest, "invalid_request")
		moment.Add(15)
	}
	if calls.Load() != 0 {
		t.Fatalf("2001-rune rejections must not call model, got %d", calls.Load())
	}
	var turnCount int64
	f.db.ORM.Table("debate_turns").Where("debate_id = ?", id).Count(&turnCount)
	if turnCount != 0 {
		t.Fatalf("2001-rune rejections must not write turns, got %d", turnCount)
	}

	// 2. 恰好 2000 个多字节码点（1000 个中文 + 1000 个 4 字节字符 = 2000 码点 / 7000 UTF-8 字节）成功落库并原样重读
	exact2000 := strings.Repeat("思", 1000) + strings.Repeat("𠮷", 1000)
	if utf8.RuneCountInString(exact2000) != 2000 {
		t.Fatal("expected 2000 runes")
	}
	key1 := uuid.NewString()
	r := f.request(t, "POST", path+"/turns", submit(exact2000, key1, 1))
	assertStatus(t, r, http.StatusCreated, "")
	if calls.Load() != 1 {
		t.Fatalf("expected 1 model call, got %d", calls.Load())
	}
	moment.Add(15)

	// 3. 重复 client_request_id（含大小写变体）附带不同内容或不同轮次，统一返回 409 idempotency_conflict 且不调模型
	for _, conflictCase := range []struct {
		name    string
		key     string
		content string
		round   int
	}{
		{"same_round_diff_content", key1, "被篡改的第一轮内容", 1},
		{"upper_uuid_diff_content", strings.ToUpper(key1), "大写UUID篡改内容", 1},
		{"diff_round_same_content", key1, exact2000, 2},
		{"diff_round_diff_content", key1, "复用第一轮幂等键发第二轮", 2},
	} {
		res := f.request(t, "POST", path+"/turns", submit(conflictCase.content, conflictCase.key, conflictCase.round))
		if res.status != http.StatusConflict || res.errorCode() != "idempotency_conflict" {
			t.Fatalf("%s: expected 409 idempotency_conflict, got status=%d code=%s", conflictCase.name, res.status, res.errorCode())
		}
		if calls.Load() != 1 {
			t.Fatalf("%s: idempotency_conflict called model", conflictCase.name)
		}
		moment.Add(15)
	}

	// 核对第一轮 2000 码点原文未被篡改，且未产生第 2 轮记录
	detailRes := f.request(t, "GET", path, nil)
	var detail debate.DetailDTO
	rawDetail, _ := json.Marshal(detailRes.data())
	_ = json.Unmarshal(rawDetail, &detail)
	if len(detail.Turns) != 1 || len(detail.Turns[0].Messages) != 2 ||
		utf8.RuneCountInString(detail.Turns[0].Messages[0].Content) != 2000 ||
		detail.Turns[0].Messages[0].Content != exact2000 {
		t.Fatal("2000-rune message content was not preserved verbatim")
	}
}

// TestPhase2SameRoundConcurrentLockContention 验证同轮并发抢锁（不同幂等键同轮并发、同幂等键同轮并发、同失败轮并发显式重试）
// 在 MySQL FOR UPDATE 行锁与并发门限下不会重复计轮、不会重复写消息或重复调用模型。
func TestPhase2SameRoundConcurrentLockContention(t *testing.T) {
	var turnCalls atomic.Int32
	var failRound3Once atomic.Bool
	gen := testGenerator(func(c context.Context, r ai.Request) (ai.Result, error) {
		turnCalls.Add(1)
		time.Sleep(40 * time.Millisecond)
		if r.RoundNo == 3 && !failRound3Once.Swap(true) {
			return ai.Result{}, &ai.Error{Code: ai.CodeModelTimeout}
		}
		return ai.NewFake().Generate(c, r)
	})

	f := newDebateFixture(t, gen)
	var moment atomic.Int64
	moment.Store(time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC).Unix())
	f.gate = debate.NewGate(func() time.Time { return time.Unix(moment.Load(), 0) })
	f.mount(t, gen)

	id := f.createQuick3(t) // 历史 quick_3 语义：本用例验证三轮边界与总结六条消息
	path := "/api/v1/debates/" + id

	// 1. 第 1 轮：两个并发请求使用不同 client_request_id 同时抢占第 1 轮
	startR1 := make(chan struct{})
	resR1 := make([]apiResult, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-startR1
			resR1[idx] = f.request(t, "POST", path+"/turns", submit("并发第一轮发言", uuid.NewString(), 1))
		}(i)
	}
	close(startR1)
	wg.Wait()

	createdCount, rejectedCount := 0, 0
	for _, r := range resR1 {
		switch r.status {
		case http.StatusCreated:
			createdCount++
		case http.StatusConflict:
			if r.errorCode() == "round_conflict" {
				rejectedCount++
			}
		case http.StatusTooManyRequests:
			if r.errorCode() == "generation_busy" {
				rejectedCount++
			}
		}
	}
	if createdCount != 1 || rejectedCount != 1 || turnCalls.Load() != 1 {
		t.Fatalf("concurrent round 1 different keys: created=%d rejected=%d calls=%d (statuses=%d,%d)",
			createdCount, rejectedCount, turnCalls.Load(), resR1[0].status, resR1[1].status)
	}
	moment.Add(20)

	// 2. 第 2 轮：两个并发请求使用相同 client_request_id 与相同原文同时提交第 2 轮
	sharedKey := uuid.NewString()
	startR2 := make(chan struct{})
	resR2 := make([]apiResult, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-startR2
			resR2[idx] = f.request(t, "POST", path+"/turns", submit("并发同幂等键第二轮", sharedKey, 2))
		}(i)
	}
	close(startR2)
	wg.Wait()

	r2Created, r2Replayed := 0, 0
	for _, r := range resR2 {
		switch r.status {
		case http.StatusCreated:
			r2Created++
		case http.StatusAccepted, http.StatusOK:
			r2Replayed++
		}
	}
	if r2Created != 1 || r2Replayed != 1 || turnCalls.Load() != 2 {
		t.Fatalf("concurrent round 2 same key: created=%d replayed=%d calls=%d (statuses=%d,%d)",
			r2Created, r2Replayed, turnCalls.Load(), resR2[0].status, resR2[1].status)
	}
	moment.Add(20)

	// 3. 第 3 轮：先让第 3 轮首次失败（attempt_version=1），再并发发起两个 expected_attempt_version=1 的显式重试
	r3Fail := f.request(t, "POST", path+"/turns", submit("并发重试第三轮", uuid.NewString(), 3))
	assertStatus(t, r3Fail, http.StatusGatewayTimeout, "model_timeout")
	var failEnv struct {
		Error struct {
			Details *debate.FailureDetails `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(r3Fail.body, &failEnv)
	turn3ID := failEnv.Error.Details.TurnID
	callsBeforeConcurrentRetry := turnCalls.Load()
	moment.Add(20)

	startRetry := make(chan struct{})
	resRetry := make([]apiResult, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-startRetry
			resRetry[idx] = f.request(t, "POST", path+"/turns/"+turn3ID+"/retry", map[string]any{"expected_attempt_version": 1})
		}(i)
	}
	close(startRetry)
	wg.Wait()

	if turnCalls.Load() != callsBeforeConcurrentRetry+1 {
		t.Fatalf("concurrent retry must call model exactly once, got delta=%d", turnCalls.Load()-callsBeforeConcurrentRetry)
	}
	for i, r := range resRetry {
		if r.status != http.StatusOK && r.status != http.StatusAccepted {
			t.Fatalf("concurrent retry[%d] unexpected status=%d code=%s", i, r.status, r.errorCode())
		}
	}

	// 核对最终数据库状态：恰好 3 个轮次、6 条消息、completed_rounds=3
	detailRes := f.request(t, "GET", path, nil)
	var detail debate.DetailDTO
	rawDetail, _ := json.Marshal(detailRes.data())
	_ = json.Unmarshal(rawDetail, &detail)
	if detail.Debate.Status != "completed" || detail.Debate.CompletedRounds != 3 || len(detail.Turns) != 3 {
		t.Fatalf("unexpected debate state after concurrent tests: %+v", detail.Debate)
	}
	var turnRows, msgRows int64
	f.db.ORM.Table("debate_turns").Where("debate_id = ?", id).Count(&turnRows)
	f.db.ORM.Table("messages").Joins("JOIN debate_turns ON messages.turn_id = debate_turns.id").Where("debate_turns.debate_id = ?", id).Count(&msgRows)
	if turnRows != 3 || msgRows != 6 {
		t.Fatalf("expected 3 turns and 6 messages in DB, got turns=%d messages=%d", turnRows, msgRows)
	}
}

// TestPhase2XSSAndSpecialCharactersVerbatimStorage 验证包含 XSS 脚本标签、HTML 属性事件、
// 引号、反斜杠、SQL 注入字面量及多字节字符的发言与结构化总结在数据库中原样存取，不发生截断、二次转义或注入。
func TestPhase2XSSAndSpecialCharactersVerbatimStorage(t *testing.T) {
	const (
		xssUser1 = `<script>alert("xss-round-1")</script><img src=x onerror='alert(1)'> & "double" 'single' \backslash`
		xssUser2 = `<svg/onload=confirm(document.cookie)> '; DROP TABLE messages; -- {{constructor.constructor('alert(1)')()}}`
		xssUser3 = `</textarea><iframe src="javascript:alert('xss')"></iframe> 思弈阁🌱𠮷 &amp; &#x3C;script&#x3E;`
		xssAI    = `AI回应：<script>alert("ai")</script> & "立场反驳" '单引号'`
		xssPoint = `<img src=x onerror=alert('sum')> 论点清晰 & 结构完整`
	)

	summaryJSON := `{"strengths":[{"round_no":1,"point":` + mustJSONString(xssPoint) + `,"reason":"能够回应 <script> 边界条件"}],` +
		`"issues":[{"round_no":2,"point":"例证可进一步具体化 & 深化","reason":"第2轮包含 'SQL' 字面量测试"}],` +
		`"suggestions":[{"round_no":3,"action":"补充可核验的比较维度","example":"例如：对比 <A> 与 \"B\" 的长期成本"}]}`

	gen := testCombinedGenerator{
		genTurn: func(_ context.Context, _ ai.Request) (ai.Result, error) {
			return ai.Result{
				Content:  xssAI,
				Usage:    ai.Usage{PromptTokens: ai.KnownCount(64), CompletionTokens: ai.KnownCount(32)},
				Metadata: ai.Metadata{Model: "fake-xss", PromptVersion: ai.PromptVersion, LatencyMS: 5},
			}, nil
		},
		genSummary: func(c context.Context, r ai.SummaryRequest) (ai.SummaryResult, error) {
			return ai.NewFake(ai.FakeStep{Result: ai.Result{Content: summaryJSON}}).GenerateSummary(c, r)
		},
	}

	f := newDebateFixture(t, gen)
	var moment atomic.Int64
	moment.Store(time.Date(2026, 11, 12, 4, 0, 0, 0, time.UTC).Unix())
	f.gate = debate.NewGate(func() time.Time { return time.Unix(moment.Load(), 0) })
	f.mount(t, gen)

	id := f.createQuick3(t) // 历史 quick_3 语义：本用例验证三轮边界与总结六条消息
	path := "/api/v1/debates/" + id
	inputs := []string{xssUser1, xssUser2, xssUser3}

	for i, content := range inputs {
		r := f.request(t, "POST", path+"/turns", submit(content, uuid.NewString(), i+1))
		assertStatus(t, r, http.StatusCreated, "")
		moment.Add(15)
	}

	sumRes := f.request(t, "POST", path+"/summary", map[string]any{"retry": false})
	assertStatus(t, sumRes, http.StatusCreated, "")

	detailRes := f.request(t, "GET", path, nil)
	assertStatus(t, detailRes, http.StatusOK, "")
	var detail debate.DetailDTO
	rawDetail, _ := json.Marshal(detailRes.data())
	if err := json.Unmarshal(rawDetail, &detail); err != nil {
		t.Fatal(err)
	}
	for i, wantUser := range inputs {
		if detail.Turns[i].Messages[0].Content != wantUser {
			t.Fatalf("round %d user XSS payload altered: got=%q want=%q", i+1, detail.Turns[i].Messages[0].Content, wantUser)
		}
		if detail.Turns[i].Messages[1].Content != xssAI {
			t.Fatalf("round %d assistant XSS payload altered: got=%q want=%q", i+1, detail.Turns[i].Messages[1].Content, xssAI)
		}
	}
	if detail.SummaryStatus.Content == nil || detail.SummaryStatus.Content.Strengths[0].Point != xssPoint {
		t.Fatalf("summary XSS payload altered: %+v", detail.SummaryStatus.Content)
	}

	// 直接读取 MySQL 底层存储，确认写入的是原始 UTF-8 字符，未被二次 HTML 转义或注入破坏
	var storedUser1 string
	if err := f.db.ORM.Table("messages").Select("content").Where("id = ?", detail.Turns[0].Messages[0].ID).Scan(&storedUser1).Error; err != nil || storedUser1 != xssUser1 {
		t.Fatalf("raw DB user content mismatch: got=%q want=%q err=%v", storedUser1, xssUser1, err)
	}
}

func mustJSONString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
