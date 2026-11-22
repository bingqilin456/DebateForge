package debate

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/gin-gonic/gin"
)

// V2-08 局部重练 HTTP 边界：
//   - 读取只要 Session；提交与重试要求 Origin+CSRF；
//   - 越权与不存在统一 404，不泄露会话是否存在；
//   - 未完成训练 409；空选段/AI 原文/坐标不符 400；
//   - 重复提交同请求编号幂等（不重复调用模型）；
//   - 生成失败返回失败版本并可显式重试；关闭能力时读 available=false、写 400。

// repracticeClock 是可在测试中推进的共享时钟：Gate 与存储必须看到同一时间，
// 否则 HTTP 防刷（每分钟 6 次）会把正文校验用例误判成限流。
type repracticeClock struct {
	mu    sync.Mutex
	value time.Time
}

func (c *repracticeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.value
}

func (c *repracticeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value = c.value.Add(d)
}

// repracticeHTTPFixture 组合内存存储、Fake 与已完赛会话，供边界用例复用。
type repracticeHTTPFixture struct {
	service *Service
	mem     *memoryStore
	fake    *ai.Fake
	router  *gin.Engine
	debate  Debate
	token   string
	csrf    string
	clock   *repracticeClock
}

func newRepracticeHTTPFixture(t *testing.T) *repracticeHTTPFixture {
	t.Helper()
	clock := &repracticeClock{value: time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)}
	mem := newMemoryStore(clock.now)
	// 在既有会话替身之上加独立版本表：服务通过 RepracticeStore 访问它，
	// 正式消息与复盘仍走原来的内存实现，二者的隔离由此可被直接验证。
	store := newRepracticeVersionStoreWith(mem)
	fake := ai.NewFake()
	service := NewService(store, fake, NewGate(clock.now))
	if err := service.EnableRepractice(); err != nil {
		t.Fatalf("启用局部重练失败：%v", err)
	}
	created := completeFourRoundDebate(t, mem, service)
	token := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	csrf := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{8}, 32))
	return &repracticeHTTPFixture{service: service, mem: mem, fake: fake, router: reviewHTTPRouter(t, service, token, csrf), debate: created, token: token, csrf: csrf, clock: clock}
}

// firstUserMessage 返回第 1 轮本人发言的编号与原文。
func (f *repracticeHTTPFixture) firstUserMessage(t *testing.T) (string, string) {
	t.Helper()
	detail, err := f.service.Detail(context.Background(), 1, f.debate.ID)
	if err != nil || len(detail.Turns) == 0 || len(detail.Turns[0].Messages) == 0 {
		t.Fatalf("读取会话详情失败：%+v err=%v", detail, err)
	}
	message := detail.Turns[0].Messages[0]
	if message.Role != "user" {
		t.Fatalf("第一轮首条消息应为用户发言，实际 %s", message.Role)
	}
	return formatID(message.ID), message.Content
}

func (f *repracticeHTTPFixture) base() string {
	return "/api/v1/debates/" + formatID(f.debate.ID) + "/repractice"
}

// submitBody 构造一次合法提交正文；quote 为空时按整条原文提交。
func submitBody(requestID, messageID, content, rewrite string) string {
	quote := content
	if quote == "" {
		quote = "甲"
	}
	return fmt.Sprintf(`{"client_request_id":%q,"message_id":%q,"start_cp":0,"end_cp":%d,"quote":%q,"goal":"","rewrite":%q}`,
		requestID, messageID, len([]rune(quote)), quote, rewrite)
}

func TestRepracticeHTTPReadOnlyAndWriteBoundary(t *testing.T) {
	f := newRepracticeHTTPFixture(t)
	messageID, content := f.firstUserMessage(t)

	// 读取：无版本时返回 available=true 与空列表，且不触发模型。
	rec := reviewRequest(f.router, "GET", f.base(), "", f.token, "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"available":true`) || !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("读取空历史失败：code=%d body=%s", rec.Code, rec.Body.String())
	}
	if f.fake.RepracticeCalls() != 0 {
		t.Fatal("读取不得调用模型")
	}

	// 缺少 CSRF 的提交被拒绝，且不写库、不调用模型。
	rec = reviewRequest(f.router, "POST", f.base(), submitBody("csrf-missing", messageID, content, "改写后"), f.token, "")
	if rec.Code != 403 || f.fake.RepracticeCalls() != 0 {
		t.Fatalf("缺 CSRF 应 403 且不生成：code=%d calls=%d", rec.Code, f.fake.RepracticeCalls())
	}

	// 非法正文形状一律 400：未知键、缺字段、坐标类型不符、非对象。
	for _, body := range []string{
		`{"client_request_id":"x","message_id":"1","start_cp":0,"end_cp":1,"quote":"甲","rewrite":"改写","extra":1}`,
		`{"client_request_id":"x","message_id":"1","quote":"甲","rewrite":"改写"}`,
		`{"client_request_id":"x","message_id":"1","start_cp":-1,"end_cp":1,"quote":"甲","rewrite":"改写"}`,
		`{"client_request_id":"x","message_id":"1","start_cp":"0","end_cp":1,"quote":"甲","rewrite":"改写"}`,
		`[]`,
	} {
		f.clock.advance(time.Minute)
		rec = reviewRequest(f.router, "POST", f.base(), body, f.token, f.csrf)
		if rec.Code != 400 {
			t.Fatalf("非法正文 %s 应 400，实际 %d body=%s", body, rec.Code, rec.Body.String())
		}
	}
	if f.fake.RepracticeCalls() != 0 {
		t.Fatalf("非法正文不得调用模型，实际 %d 次", f.fake.RepracticeCalls())
	}
}

func TestRepracticeHTTPSubmitIsIdempotentAndKeepsVersions(t *testing.T) {
	f := newRepracticeHTTPFixture(t)
	messageID, content := f.firstUserMessage(t)

	f.clock.advance(time.Minute)
	body := submitBody("first", messageID, content, "先说明双方采用同一套标准。")
	rec := reviewRequest(f.router, "POST", f.base(), body, f.token, f.csrf)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"completed"`) {
		t.Fatalf("首次提交失败：code=%d body=%s", rec.Code, rec.Body.String())
	}
	calls := f.fake.RepracticeCalls()
	if calls != 1 {
		t.Fatalf("首次提交应调用一次模型，实际 %d", calls)
	}

	// 同一请求编号重复提交：幂等返回同一版本，不再调用模型。
	f.clock.advance(time.Minute)
	rec = reviewRequest(f.router, "POST", f.base(), body, f.token, f.csrf)
	if rec.Code != 200 || f.fake.RepracticeCalls() != calls {
		t.Fatalf("重复提交应幂等：code=%d calls=%d/%d", rec.Code, f.fake.RepracticeCalls(), calls)
	}

	// 新请求编号产生独立版本；列表按写入顺序返回两版。
	f.clock.advance(time.Minute)
	second := submitBody("second", messageID, content, "第二版改写，补充了可检验标准。")
	rec = reviewRequest(f.router, "POST", f.base(), second, f.token, f.csrf)
	if rec.Code != 200 {
		t.Fatalf("第二次提交失败：code=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = reviewRequest(f.router, "GET", f.base(), "", f.token, "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "先说明双方采用同一套标准。") || !strings.Contains(rec.Body.String(), "第二版改写，补充了可检验标准。") {
		t.Fatalf("历史版本读取失败：code=%d body=%s", rec.Code, rec.Body.String())
	}
	// 正式原文保持原样（重练不覆盖比赛）。
	detail, err := f.service.Detail(context.Background(), 1, f.debate.ID)
	if err != nil || detail.Turns[0].Messages[0].Content != content {
		t.Fatalf("重练不得覆盖正式原文：%+v err=%v", detail.Turns[0].Messages[0], err)
	}
}

func TestRepracticeHTTPRejectsForeignIncompleteAndBadSource(t *testing.T) {
	f := newRepracticeHTTPFixture(t)
	messageID, content := f.firstUserMessage(t)

	// 空选段：按选段不成立 400，且不创建版本。
	f.clock.advance(time.Minute)
	rec := reviewRequest(f.router, "POST", f.base(),
		fmt.Sprintf(`{"client_request_id":"empty","message_id":%q,"start_cp":1,"end_cp":1,"quote":"","rewrite":"改写"}`, messageID),
		f.token, f.csrf)
	if rec.Code != 400 || f.fake.RepracticeCalls() != 0 {
		t.Fatalf("空选段应 400 且不生成：code=%d body=%s", rec.Code, rec.Body.String())
	}

	// AI 原文不可作为重练对象：取第 1 轮 AI 消息编号。
	detail, err := f.service.Detail(context.Background(), 1, f.debate.ID)
	if err != nil {
		t.Fatal(err)
	}
	aiMessage := detail.Turns[0].Messages[1]
	f.clock.advance(time.Minute)
	rec = reviewRequest(f.router, "POST", f.base(),
		submitBody("ai-source", formatID(aiMessage.ID), aiMessage.Content, "改写"),
		f.token, f.csrf)
	if rec.Code != 400 || f.fake.RepracticeCalls() != 0 {
		t.Fatalf("AI 原文应 400：code=%d body=%s", rec.Code, rec.Body.String())
	}

	// 坐标与原文不一致（按码点算错）：400。
	f.clock.advance(time.Minute)
	rec = reviewRequest(f.router, "POST", f.base(),
		fmt.Sprintf(`{"client_request_id":"bad-range","message_id":%q,"start_cp":0,"end_cp":2,"quote":%q,"rewrite":"改写"}`, messageID, string([]rune(content)[:1])),
		f.token, f.csrf)
	if rec.Code != 400 {
		t.Fatalf("坐标不符应 400：code=%d body=%s", rec.Code, rec.Body.String())
	}

	// 越权与不存在的会话统一 404（另一个用户固定 ID=2）。
	otherRouter := otherUserReviewRouter(t, f.service, f.token, f.csrf)
	f.clock.advance(time.Minute)
	rec = reviewRequest(otherRouter, "GET", f.base(), "", f.token, "")
	if rec.Code != 404 {
		t.Fatalf("跨用户读取应 404，实际 %d", rec.Code)
	}
	rec = reviewRequest(otherRouter, "POST", f.base(), submitBody("foreign", messageID, content, "改写"), f.token, f.csrf)
	if rec.Code != 404 || f.fake.RepracticeCalls() != 0 {
		t.Fatalf("跨用户提交应 404 且不生成：code=%d calls=%d", rec.Code, f.fake.RepracticeCalls())
	}

	// 未完成训练：不开放局部重练，按轮次冲突 409。
	incomplete, err := f.service.Create(context.Background(), 1, CreateInput{TopicID: topicID(1), UserStance: "pro", AIStyle: "logical"})
	if err != nil {
		t.Fatal(err)
	}
	incompleteBase := "/api/v1/debates/" + formatID(incomplete.ID) + "/repractice"
	rec = reviewRequest(f.router, "GET", incompleteBase, "", f.token, "")
	if rec.Code != 409 {
		t.Fatalf("未完成会话读取应 409，实际 %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRepracticeHTTPFailureRetainsRewriteAndRetries(t *testing.T) {
	f := newRepracticeHTTPFixture(t)
	messageID, content := f.firstUserMessage(t)
	// 首次生成失败：版本落 failed，改写保留在响应里。
	f.fake.FailRepractices(&ai.Error{Code: ai.CodeModelTimeout, Message: "合成超时"}, 1)

	f.clock.advance(time.Minute)
	rec := reviewRequest(f.router, "POST", f.base(), submitBody("failure", messageID, content, "失败的改写也要保留。"), f.token, f.csrf)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"failed"`) ||
		!strings.Contains(rec.Body.String(), "失败的改写也要保留。") ||
		!strings.Contains(rec.Body.String(), `"error_code":"model_timeout"`) {
		t.Fatalf("失败版本未按约定保存：code=%d body=%s", rec.Code, rec.Body.String())
	}
	// 读取仍能看到失败版本与改写（刷新后不丢）。
	rec = reviewRequest(f.router, "GET", f.base(), "", f.token, "")
	if !strings.Contains(rec.Body.String(), "失败的改写也要保留。") {
		t.Fatalf("刷新后应可回看失败版本：body=%s", rec.Body.String())
	}

	// 重试缺版本号：400；带错误版本：409 或幂等读回，不允许匿名重试。
	rec = reviewRequest(f.router, "POST", f.base()+"/1/retry", `{}`, f.token, f.csrf)
	if rec.Code != 400 {
		t.Fatalf("缺版本号应 400，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	f.clock.advance(time.Minute)
	rec = reviewRequest(f.router, "POST", f.base()+"/1/retry", `{"expected_attempt_version":9}`, f.token, f.csrf)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"failed"`) {
		t.Fatalf("版本不匹配应读回失败状态而不是重新生成：code=%d body=%s", rec.Code, rec.Body.String())
	}

	// 正确版本重试：同一版本、attempt=2、状态 completed。
	f.clock.advance(time.Minute)
	rec = reviewRequest(f.router, "POST", f.base()+"/1/retry", `{"expected_attempt_version":1}`, f.token, f.csrf)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"completed"`) ||
		!strings.Contains(rec.Body.String(), `"attempt_version":2`) {
		t.Fatalf("显式重试应更新同一版本：code=%d body=%s", rec.Code, rec.Body.String())
	}
	// 重试不新增版本行。
	rec = reviewRequest(f.router, "GET", f.base(), "", f.token, "")
	if strings.Count(rec.Body.String(), `"rewrite":"失败的改写也要保留。"`) != 1 {
		t.Fatalf("重试不得创建新版本：body=%s", rec.Body.String())
	}
}

// TestRepracticeDisabledDoesNotTouchVersionTable 覆盖“关闭即不访问新表”：
// 旧部署可能尚未执行 00017，读取只做既有归属校验并返回 available=false，
// 写入返回 400 且不调用模型。
func TestRepracticeDisabledDoesNotTouchVersionTable(t *testing.T) {
	clock := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	mem := newMemoryStore(func() time.Time { return clock })
	probe := &repracticeStoreProbe{memoryStore: mem}
	service := NewService(probe, ai.NewFake(), NewGate(nil))
	created := completeFourRoundDebate(t, mem, service)
	token := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))
	csrf := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{4}, 32))
	router := reviewHTTPRouter(t, service, token, csrf)
	base := "/api/v1/debates/" + formatID(created.ID) + "/repractice"

	rec := reviewRequest(router, "GET", base, "", token, "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"available":false`) || !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("关闭时应返回 available=false 与空列表：code=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = reviewRequest(router, "POST", base, `{"client_request_id":"x","message_id":"1","start_cp":0,"end_cp":1,"quote":"甲","rewrite":"改写"}`, token, csrf)
	if rec.Code != 400 {
		t.Fatalf("关闭时写入口应 400，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	// 越权与不存在仍是 404，不能对任意 ID 一律回 available=false。
	rec = reviewRequest(router, "GET", "/api/v1/debates/999999/repractice", "", token, "")
	if rec.Code != 404 {
		t.Fatalf("不存在会话应 404，实际 %d", rec.Code)
	}
	if probe.versionReads != 0 || probe.versionWrites != 0 {
		t.Fatalf("关闭时禁止读写版本表，reads=%d writes=%d", probe.versionReads, probe.versionWrites)
	}
}

// repracticeStoreProbe 记录关闭能力时是否误访问 debate_repractice_versions。
// 真实旧部署缺表会映射成 503，因此这里让相关方法直接报依赖错误。
type repracticeStoreProbe struct {
	*memoryStore
	versionReads  int
	versionWrites int
}

func (s *repracticeStoreProbe) ListRepractice(context.Context, uint64, uint64) ([]RepracticeVersion, error) {
	s.versionReads++
	return nil, dependency(errors.New("table debate_repractice_versions doesn't exist"), false)
}

func (s *repracticeStoreProbe) CreateRepractice(context.Context, uint64, uint64, RepracticeInput, time.Time) (RepracticeVersion, bool, error) {
	s.versionWrites++
	return RepracticeVersion{}, false, dependency(errors.New("table debate_repractice_versions doesn't exist"), false)
}

func (s *repracticeStoreProbe) ClaimRepracticeRetry(context.Context, uint64, uint64, uint64, uint64, time.Time) (RepracticeVersion, bool, error) {
	s.versionWrites++
	return RepracticeVersion{}, false, dependency(errors.New("table debate_repractice_versions doesn't exist"), false)
}

func (s *repracticeStoreProbe) CompleteRepractice(context.Context, uint64, uint64, uint64, uint64, RepracticeFeedback) (RepracticeVersion, error) {
	s.versionWrites++
	return RepracticeVersion{}, dependency(errors.New("table debate_repractice_versions doesn't exist"), false)
}

func (s *repracticeStoreProbe) FailRepractice(context.Context, uint64, uint64, uint64, uint64, string) (RepracticeVersion, error) {
	s.versionWrites++
	return RepracticeVersion{}, dependency(errors.New("table debate_repractice_versions doesn't exist"), false)
}

// 确保测试用的认证夹具与既有复盘用例保持同一套构造口径。
