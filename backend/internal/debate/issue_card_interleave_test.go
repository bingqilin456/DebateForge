package debate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/auth"
	"github.com/gin-gonic/gin"
)

// 争点卡的真实交错序列：先认领并阻住分析，期间完成三种人工编辑，再放行分析，
// 检查晚到结果既不覆盖人工标题、也不拆回已合并卡、不复活已移除卡。
func TestIssueCardLateAnalysisLosesAgainstConcurrentManualEdits(t *testing.T) {
	_, fake, service, debate, turns := issueCardFixture(t)
	ctx := context.Background()
	originA := turns[0].Messages[0].Content
	originB := turns[1].Messages[0].Content
	originC := turns[2].Messages[0].Content

	fake.SetCandidates([]IssueCardCandidate{
		issueCardCandidate(t, turns, originA), issueCardCandidate(t, turns, originB), issueCardCandidate(t, turns, originC),
	})
	initial, _, err := service.Analyze(ctx, 1, debate.ID, IssueCardAnalyzeInput{})
	if err != nil || len(initial.Items) != 3 {
		t.Fatalf("首次整理失败: %v len=%d", err, len(initial.Items))
	}
	cardA := issueCardByTitle(initial.Items, "争点："+firstIssueCardSentence(originA))
	cardB := issueCardByTitle(initial.Items, "争点："+firstIssueCardSentence(originB))
	cardC := issueCardByTitle(initial.Items, "争点："+firstIssueCardSentence(originC))
	if cardA == nil || cardB == nil || cardC == nil {
		t.Fatalf("缺少夹具卡片: %+v", issueCardIDs(initial.Items))
	}

	// 1) 先认领一次重试，并在分析器内阻住。
	gate := make(chan struct{})
	fake.SetGate(gate)
	fake.SetCandidates([]IssueCardCandidate{
		issueCardCandidate(t, turns, originA, turns[0].Messages[1].Content),
		issueCardCandidate(t, turns, originB, turns[1].Messages[1].Content),
		issueCardCandidate(t, turns, originC, turns[2].Messages[1].Content),
	})
	type analyzeResult struct {
		list   IssueCardListDTO
		status int
		err    error
	}
	done := make(chan analyzeResult, 1)
	go func() {
		list, status, err := service.Analyze(ctx, 1, debate.ID, IssueCardAnalyzeInput{
			Retry: true, ExpectedAttemptVersion: initial.Analysis.AttemptVersion,
		})
		done <- analyzeResult{list: list, status: status, err: err}
	}()
	select {
	case <-fake.Entered():
	case <-time.After(3 * time.Second):
		t.Fatal("分析未真正开始，无法验证交错")
	}

	// 2) 分析在途时做三种人工编辑：改名、合并、移除。
	renamed, _, err := service.Edit(ctx, 1, debate.ID, cardA.ID, IssueCardEditInput{
		ExpectedVersion: cardA.Version, Action: IssueCardActionRename, Title: issueCardPtr("交错人工标题"),
	})
	if err != nil {
		t.Fatalf("分析在途时人工改名失败: %v", err)
	}
	if renamed.Title != "交错人工标题" {
		t.Fatalf("人工改名未生效: %+v", renamed)
	}
	if _, _, err := service.Edit(ctx, 1, debate.ID, cardB.ID, IssueCardEditInput{
		ExpectedVersion: cardB.Version, Action: IssueCardActionMerge, TargetCardID: issueCardPtr(cardC.ID),
	}); err != nil {
		t.Fatalf("分析在途时人工合并失败: %v", err)
	}
	// 合并会提升目标卡版本，移除必须使用重新读取后的当前版本，不能沿用旧快照。
	inFlight, err := service.Get(ctx, 1, debate.ID)
	if err != nil {
		t.Fatal(err)
	}
	var targetC *IssueCardDTO
	for index := range inFlight.Items {
		if inFlight.Items[index].ID == cardC.ID {
			targetC = &inFlight.Items[index]
		}
	}
	if targetC == nil {
		t.Fatal("合并目标卡应仍在列表中")
	}
	if targetC.Version <= cardC.Version {
		t.Fatalf("合并后目标卡版本应递增，旧=%d 新=%d", cardC.Version, targetC.Version)
	}
	// 用合并前的旧版本移除目标卡：必须是版本冲突而不是静默覆盖。
	if _, _, err := service.Edit(ctx, 1, debate.ID, cardC.ID, IssueCardEditInput{
		ExpectedVersion: cardC.Version, Action: IssueCardActionRemove,
	}); !errors.Is(err, ErrIssueCardVersionConflict) {
		t.Fatalf("过期版本移除应为版本冲突，实际 %v", err)
	}
	if _, _, err := service.Edit(ctx, 1, debate.ID, cardC.ID, IssueCardEditInput{
		ExpectedVersion: targetC.Version, Action: IssueCardActionRemove,
	}); err != nil {
		t.Fatalf("分析在途时人工移除失败: %v", err)
	}

	// 3) 放行分析，晚到结果必须让人工编辑优先。
	close(gate)
	result := <-done
	if result.err != nil || result.status != 201 {
		t.Fatalf("放行后分析应成功: %v %d", result.err, result.status)
	}
	after := result.list
	if len(after.Items) == 0 {
		t.Fatal("人工合并目标若被移除，不得凭空复活或新建卡")
	}
	for _, card := range after.Items {
		if card.Origin.Anchor.Quote == originC {
			t.Fatalf("已人工移除的卡不得复活: %+v", card)
		}
	}
	keptA := issueCardByTitle(after.Items, "交错人工标题")
	if keptA == nil {
		t.Fatalf("人工标题必须保留: %+v", issueCardIDs(after.Items))
	}
	if keptA.ID != cardA.ID {
		t.Fatal("晚到分析不得改变稳定卡 ID")
	}
	if keptA.TitleSource != IssueCardTitleSourceUser {
		t.Fatalf("人工标题来源应保留为 user: %+v", keptA)
	}
	if len(keptA.Notices) == 0 {
		t.Fatal("晚到分析覆盖冲突须在卡片上可见")
	}
	if len(keptA.Responses) == 0 {
		t.Fatal("兼容内容（相关回应）仍应追加到人工卡")
	}
	issueCardAssertUniqueAnchors(t, *keptA)

	// 4) 分析结束后的状态与卡片版本可以继续被人工编辑（版本须递增，CAS 可用）。
	updated, _, err := service.Edit(ctx, 1, debate.ID, keptA.ID, IssueCardEditInput{
		ExpectedVersion: keptA.Version, Action: IssueCardActionRename, Title: issueCardPtr("交错后再改"),
	})
	if err != nil {
		t.Fatalf("分析结束后按新版本编辑应成功: %v", err)
	}
	if updated.Title != "交错后再改" {
		t.Fatalf("新版本改名未生效: %+v", updated)
	}
}

// issueCardHTTPFixture 构造只挂争点卡路由的测试服务器，复用既有认证与 CSRF 中间件。
type issueCardHTTPFixture struct {
	router *gin.Engine
	token  string
	csrf   string
	store  *MemoryIssueCardStore
	fake   *FakeIssueCardAnalyzer
	svc    *IssueCardService
	debate uint64
}

func newIssueCardHTTPFixture(t *testing.T) *issueCardHTTPFixture {
	t.Helper()
	token := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))
	hash := sha256.Sum256([]byte(token))
	csrf := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{10}, 32))
	now := time.Now().UTC()
	authStore := reviewAuthStore{auth.Session{ID: 1, UserID: 1, TokenHash: hash[:], CSRFToken: csrf, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}}
	a, err := auth.NewHandler(auth.NewService(authStore), auth.HandlerConfig{Origin: "http://127.0.0.1:5173"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemoryIssueCardStore(func() time.Time { return now })
	debate, turns := issueCardCompletedDebate()
	store.SeedDebate(debate, turns)
	fake := NewFakeIssueCardAnalyzer()
	svc := NewIssueCardService(store, fake)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewIssueCardHandler(svc).RegisterIssueCardRoutes(router.Group("/api/v1"), a)
	return &issueCardHTTPFixture{router: router, token: token, csrf: csrf, store: store, fake: fake, svc: svc, debate: debate.ID}
}

func (f *issueCardHTTPFixture) do(t *testing.T, method, path, body string, withCSRF, withOrigin, withSession bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if withOrigin {
		req.Header.Set("Origin", "http://127.0.0.1:5173")
	}
	if withCSRF {
		req.Header.Set("X-CSRF-Token", f.csrf)
	}
	if withSession {
		req.AddCookie(&http.Cookie{Name: auth.CookieNameDev, Value: f.token})
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func issueCardDecodeList(t *testing.T, rec *httptest.ResponseRecorder) IssueCardListDTO {
	t.Helper()
	var envelope struct {
		Data IssueCardListDTO `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("响应不是约定的 JSON: %s", rec.Body.String())
	}
	return envelope.Data
}

func TestIssueCardHTTPGuardsAndOwnership(t *testing.T) {
	f := newIssueCardHTTPFixture(t)
	base := "/api/v1/debates/11/issue-cards"
	// 读取只要求 Session；缺 Session 为 401。
	if rec := f.do(t, "GET", base, "", false, false, true); rec.Code != 200 {
		t.Fatalf("读取应为 200，实际 %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.do(t, "GET", base, "", false, false, false); rec.Code != 401 {
		t.Fatalf("缺 Session 应为 401，实际 %d", rec.Code)
	}
	// 整理与编辑缺 CSRF / Origin / Session 分别按既有中间件拒绝。
	analyze := base + "/analyze"
	if rec := f.do(t, "POST", analyze, `{}`, false, true, true); rec.Code != 403 || !strings.Contains(rec.Body.String(), "csrf_failed") {
		t.Fatalf("整理缺 CSRF 应为 403，实际 %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.do(t, "POST", analyze, `{}`, true, false, true); rec.Code != 403 {
		t.Fatalf("整理缺 Origin 应为 403，实际 %d", rec.Code)
	}
	if rec := f.do(t, "POST", analyze, `{}`, true, true, false); rec.Code != 401 {
		t.Fatalf("整理缺 Session 应为 401，实际 %d", rec.Code)
	}

	// 越权：会话属于其他用户时读取 / 整理 / 编辑一律 404，不泄露存在性。
	f.store.debates[f.debate] = Debate{ID: f.debate, UserID: 2}
	if rec := f.do(t, "GET", base, "", false, false, true); rec.Code != 404 || !strings.Contains(rec.Body.String(), `"not_found"`) {
		t.Fatalf("越权读取应为 404，实际 %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.do(t, "POST", analyze, `{}`, true, true, true); rec.Code != 404 {
		t.Fatalf("越权整理应为 404，实际 %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.do(t, "POST", base+"/1001/edit", `{"expected_version":1,"action":"remove"}`, true, true, true); rec.Code != 404 {
		t.Fatalf("越权编辑应为 404，实际 %d %s", rec.Code, rec.Body.String())
	}
}

func TestIssueCardHTTPAnalyzeAndVersionConflict(t *testing.T) {
	f := newIssueCardHTTPFixture(t)
	base := "/api/v1/debates/11/issue-cards"
	rec := f.do(t, "POST", base+"/analyze", `{}`, true, true, true)
	if rec.Code != 201 {
		t.Fatalf("首次整理应为 201，实际 %d %s", rec.Code, rec.Body.String())
	}
	list := issueCardDecodeList(t, rec)
	if list.State != IssueCardStateReady || len(list.Items) == 0 || list.DisplayCap != MaxIssueCardsDisplayed {
		t.Fatalf("整理响应形状错误: %+v", list)
	}
	card := list.Items[0]

	// 重复触发不重新分析，返回 200 与同一批卡。
	if rec = f.do(t, "POST", base+"/analyze", `{}`, true, true, true); rec.Code != 200 {
		t.Fatalf("重复触发应为 200，实际 %d %s", rec.Code, rec.Body.String())
	}
	again := issueCardDecodeList(t, rec)
	if strings.Join(issueCardIDs(again.Items), ",") != strings.Join(issueCardIDs(list.Items), ",") {
		t.Fatal("重复触发不得改变卡片身份")
	}

	// 人工改名成功并递增版本。
	rec = f.do(t, "POST", base+"/"+card.ID+"/edit",
		`{"expected_version":`+formatID(card.Version)+`,"action":"rename","title":"HTTP 人工标题"}`, true, true, true)
	if rec.Code != 200 {
		t.Fatalf("改名应为 200，实际 %d %s", rec.Code, rec.Body.String())
	}
	// 用旧版本再改：服务端版本冲突必须是 409，且不得覆盖人工标题。
	rec = f.do(t, "POST", base+"/"+card.ID+"/edit",
		`{"expected_version":`+formatID(card.Version)+`,"action":"rename","title":"过期标题"}`, true, true, true)
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "issue_card_version_conflict") {
		t.Fatalf("过期版本应为 409 issue_card_version_conflict，实际 %d %s", rec.Code, rec.Body.String())
	}
	stored := f.store.Cards(f.debate)
	for _, current := range stored {
		if current.ID == card.ID && current.Title != "HTTP 人工标题" {
			t.Fatalf("版本冲突覆盖了人工标题: %+v", current)
		}
	}

	// 非法请求体与非法动作按既有约定 400。
	for _, body := range []string{
		`{"expected_version":1,"action":"rename"}`,
		`{"expected_version":1,"action":"unknown"}`,
		`{"expected_version":1,"action":"rename","title":"   "}`,
		`{"expected_version":"1","action":"rename","title":"类型错误"}`,
	} {
		rec = f.do(t, "POST", base+"/"+card.ID+"/edit", body, true, true, true)
		if rec.Code != 400 {
			t.Fatalf("body %s 应为 400，实际 %d %s", body, rec.Code, rec.Body.String())
		}
	}

	// 卡片不属于该场：按 404 处理。
	rec = f.do(t, "POST", base+"/9999/edit", `{"expected_version":1,"action":"remove"}`, true, true, true)
	if rec.Code != 404 {
		t.Fatalf("不存在的卡应为 404，实际 %d %s", rec.Code, rec.Body.String())
	}
}

func TestIssueCardHTTPIsolationStates(t *testing.T) {
	f := newIssueCardHTTPFixture(t)
	base := "/api/v1/debates/11/issue-cards"
	// 未完赛：读取 200 + not_ready，整理 409。
	active := f.store.debates[f.debate]
	active.Status = "active"
	active.CompletedRounds = 2
	f.store.debates[f.debate] = active
	rec := f.do(t, "GET", base, "", false, false, true)
	if rec.Code != 200 {
		t.Fatalf("未完赛读取应为 200，实际 %d", rec.Code)
	}
	if list := issueCardDecodeList(t, rec); list.State != IssueCardStateNotReady || len(list.Items) != 0 {
		t.Fatalf("未完赛应隔离: %+v", list)
	}
	rec = f.do(t, "POST", base+"/analyze", `{}`, true, true, true)
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "issue_card_not_ready") {
		t.Fatalf("未完赛整理应为 409 issue_card_not_ready，实际 %d %s", rec.Code, rec.Body.String())
	}
	if f.fake.Calls() != 0 {
		t.Fatal("未完赛不得调用分析")
	}

	// 历史 quick_3：即使完赛也隔离。
	quick, quickTurns := issueCardCompletedQuick3()
	f.store.SeedDebate(quick, quickTurns)
	quickBase := "/api/v1/debates/" + formatID(quick.ID) + "/issue-cards"
	rec = f.do(t, "GET", quickBase, "", false, false, true)
	if list := issueCardDecodeList(t, rec); rec.Code != 200 || list.State != IssueCardStateUnsupported || len(list.Items) != 0 {
		t.Fatalf("quick_3 应隔离: %d %+v", rec.Code, list)
	}
	rec = f.do(t, "POST", quickBase+"/analyze", `{}`, true, true, true)
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "issue_card_mode_unsupported") {
		t.Fatalf("quick_3 整理应为 409 issue_card_mode_unsupported，实际 %d %s", rec.Code, rec.Body.String())
	}
}

func TestIssueCardHTTPFailureKeepsCards(t *testing.T) {
	f := newIssueCardHTTPFixture(t)
	base := "/api/v1/debates/11/issue-cards"
	rec := f.do(t, "POST", base+"/analyze", `{}`, true, true, true)
	if rec.Code != 201 {
		t.Fatalf("首次整理应为 201，实际 %d", rec.Code)
	}
	before := issueCardDecodeList(t, rec)
	f.fake.SetError(errors.New("合成分析失败"))
	rec = f.do(t, "POST", base+"/analyze", `{"retry":true,"expected_attempt_version":`+formatID(before.Analysis.AttemptVersion)+`}`,
		true, true, true)
	if rec.Code < 400 {
		t.Fatalf("分析失败应返回受控错误，实际 %d %s", rec.Code, rec.Body.String())
	}
	rec = f.do(t, "GET", base, "", false, false, true)
	after := issueCardDecodeList(t, rec)
	if strings.Join(issueCardIDs(after.Items), ",") != strings.Join(issueCardIDs(before.Items), ",") {
		t.Fatal("分析失败不得清空或改写已成功卡片")
	}
	if after.Analysis.Status != IssueCardAnalysisFailed || after.Analysis.ErrorCode == nil {
		t.Fatalf("分析状态应记录失败与受控错误码: %+v", after.Analysis)
	}
	if *after.Analysis.ErrorCode != "model_error" {
		t.Fatalf("受控错误码应为 model_error，实际 %q", *after.Analysis.ErrorCode)
	}
}
