package integration

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/bingqilin456/DebateForge/backend/internal/debate"
	"github.com/bingqilin456/DebateForge/backend/internal/platform"
	"github.com/bingqilin456/DebateForge/backend/internal/topic"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestT05PendingConflictAndAdmission(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var calls atomic.Int32
	f := newDebateFixture(t, testGenerator(func(c context.Context, r ai.Request) (ai.Result, error) {
		calls.Add(1)
		started <- struct{}{}
		select {
		case <-release:
			return ai.NewFake().Generate(c, r)
		case <-c.Done():
			return ai.Result{}, c.Err()
		}
	}))
	// 即使测试失败，也先放行在途模型，再关闭服务和清理夹具。
	defer close(release)
	id := f.create(t)
	second := f.create(t)
	path := "/api/v1/debates/" + id
	key := uuid.NewString()
	done := make(chan apiResult, 1)
	go func() { done <- f.request(t, "POST", path+"/turns", submit("生成中原文", key, 1)) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("generator not started")
	}
	r := f.request(t, "GET", path, nil)
	if r.status != 200 {
		t.Fatal("detail blocked by model transaction")
	}
	r = f.request(t, "POST", path+"/turns", submit("生成中原文", key, 1))
	if r.status != 202 || calls.Load() != 1 {
		t.Fatal("generating replay")
	}
	r = f.request(t, "POST", path+"/turns", submit("生成中原文", uuid.NewString(), 1))
	if r.status != 409 || r.errorCode() != "round_conflict" {
		t.Fatal("round uniqueness")
	}
	r = f.request(t, "POST", "/api/v1/debates/"+second+"/turns", submit("另一个会话", uuid.NewString(), 1))
	if r.status != 429 || r.errorCode() != "generation_busy" || r.header.Get("Retry-After") == "" {
		t.Fatal("single-user slot")
	}
	// 用另一个已登记的真实账号占第二个进程槽，第三个账号必须立即拒绝。
	credentials := func() (*http.Cookie, string) {
		name := f.randomUsername(t)
		r, _ := f.register(t, name, integrationPassword)
		if r.status != 201 {
			t.Fatal("register admission user")
		}
		r, c, s := f.login(t, name, integrationPassword)
		if r.status != 200 {
			t.Fatal("login admission user")
		}
		return c, s
	}
	otherCookie, otherCSRF := credentials()
	thirdCookie, thirdCSRF := credentials()
	createFor := func(c *http.Cookie, s string) string {
		r := f.do(t, "POST", "/api/v1/debates", `{"topic_id":"1","user_stance":"con"}`, integrationOrigin, s, c)
		var id string
		_ = json.Unmarshal(r.data()["id"], &id)
		if r.status != 201 || id == "" {
			t.Fatal("create admission debate")
		}
		return id
	}
	otherID := createFor(otherCookie, otherCSRF)
	thirdID := createFor(thirdCookie, thirdCSRF)
	doneOther := make(chan apiResult, 1)
	go func() {
		doneOther <- f.do(t, "POST", "/api/v1/debates/"+otherID+"/turns", `{"round_no":1,"content":"第二个用户","client_request_id":"`+uuid.NewString()+`"}`, integrationOrigin, otherCSRF, otherCookie)
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("second generator not started")
	}
	r = f.do(t, "POST", "/api/v1/debates/"+thirdID+"/turns", `{"round_no":1,"content":"第三个用户","client_request_id":"`+uuid.NewString()+`"}`, integrationOrigin, thirdCSRF, thirdCookie)
	if r.status != 429 || r.errorCode() != "generation_busy" || calls.Load() != 2 {
		t.Fatal("process global slot")
	}
	// 释放和等待通过通道完成，避免清理与生成写回并发。
	release <- struct{}{}
	release <- struct{}{}
	if (<-done).status != 201 || (<-doneOther).status != 201 {
		t.Fatal("admitted work did not finish")
	}
	var count int64
	f.db.ORM.Table("debate_turns").Where("debate_id IN ?", []string{second, thirdID}).Count(&count)
	if count != 0 {
		t.Fatal("busy attempt wrote rows")
	}
}

func TestT05FailureCancellationAndDatabaseFailure(t *testing.T) {
	f := newDebateFixture(t, ai.NewFake(ai.FakeStep{Err: &ai.Error{Code: ai.CodeModelTimeout}}))
	id := f.create(t)
	path := "/api/v1/debates/" + id
	key := uuid.NewString()
	r := f.request(t, "POST", path+"/turns", submit("失败原文", key, 1))
	if r.status != 504 {
		t.Fatal("model timeout status")
	}
	var failure struct {
		Error struct {
			Details *debate.FailureDetails `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(r.body, &failure)
	if failure.Error.Details == nil {
		t.Fatal("confirmed failure missing details")
	}
	r = f.request(t, "POST", path+"/turns", submit("失败原文", key, 1))
	if r.status != 409 || r.errorCode() != "turn_failed" {
		t.Fatal("failed replay")
	}
	r = f.request(t, "GET", path, nil)
	var turns []debate.TurnDTO
	_ = json.Unmarshal(r.data()["turns"], &turns)
	if len(turns) != 1 || len(turns[0].Messages) != 1 || turns[0].Messages[0].Content != "失败原文" {
		t.Fatal("failed content not retained")
	}
	// 取消真实 HTTP 后查询已持久化 generation_cancelled，不把取消响应当成功。
	started := make(chan struct{})
	ended := make(chan struct{})
	f.mount(t, testGenerator(func(c context.Context, _ ai.Request) (ai.Result, error) {
		close(started)
		<-c.Done()
		close(ended)
		return ai.Result{}, c.Err()
	}))
	id = f.create(t)
	path = "/api/v1/debates/" + id
	ctx, cancel := context.WithCancel(context.Background())
	body, _ := json.Marshal(submit("取消原文", uuid.NewString(), 1))
	req, _ := http.NewRequestWithContext(ctx, "POST", f.server.URL+path+"/turns", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", integrationOrigin)
	req.Header.Set("X-CSRF-Token", f.csrf)
	req.AddCookie(f.cookie)
	network := make(chan error, 1)
	go func() {
		resp, e := f.client.Do(req)
		if resp != nil {
			resp.Body.Close()
		}
		network <- e
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("cancel start")
	}
	cancel()
	if e := <-network; !errors.Is(e, context.Canceled) {
		t.Fatal("HTTP cancel not observed")
	}
	<-ended
	deadline := time.Now().Add(4 * time.Second)
	for {
		r = f.request(t, "GET", path, nil)
		_ = json.Unmarshal(r.data()["turns"], &turns)
		if len(turns) == 1 && turns[0].Status == "failed" && turns[0].ErrorCode != nil && *turns[0].ErrorCode == "generation_cancelled" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cancel state not persisted")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// 已关闭连接池只影响本夹具；查询/写请求返回受控 503，不停止数据库容器。
	f.db.Close()
	r = f.request(t, "GET", path, nil)
	if r.status != 503 || r.errorCode() != "dependency_unavailable" {
		t.Fatal("database failure not controlled")
	}
}

func TestT05BodyWhitespaceAndMinuteLimit(t *testing.T) {
	f := newDebateFixture(t, ai.NewFake())
	id := f.create(t)
	path := "/api/v1/debates/" + id + "/turns"
	for _, content := range []string{"", " \ufeff ", "　\n"} {
		r := f.request(t, "POST", path, submit(content, uuid.NewString(), 1))
		if r.status != 400 {
			t.Fatal("whitespace accepted")
		}
	}
	r := f.do(t, "POST", path, strings.Repeat(" ", 32*1024+1), integrationOrigin, f.csrf, f.cookie)
	if r.status != 413 {
		t.Fatal("body limit")
	}
	key := uuid.NewString()
	// 前面四次非法请求同样计入 HTTP 防刷，剩余两次允许新请求与重放。
	for i := 0; i < 2; i++ {
		r = f.request(t, "POST", path, submit("反复请求", key, 1))
		if (i == 0 && r.status != 201) || (i > 0 && r.status != 200) {
			t.Fatal("valid replay rejected early")
		}
	}
	r = f.request(t, "POST", path, submit("反复请求", key, 1))
	if r.status != 429 || r.errorCode() != "rate_limited" || r.header.Get("Retry-After") == "" {
		t.Fatal("minute limit")
	}
}

type testGenerator func(context.Context, ai.Request) (ai.Result, error)

func (g testGenerator) Generate(c context.Context, r ai.Request) (ai.Result, error) { return g(c, r) }

type debateFixture struct {
	*authFixture
	cookie *http.Cookie
	csrf   string
	uid    uint64
	gate   *debate.Gate
}

func newDebateFixture(t *testing.T, g ai.Generator) *debateFixture {
	t.Helper()
	f := &debateFixture{authFixture: newAuthFixture(t), gate: debate.NewGate(nil)}
	f.mount(t, g)
	// 先登记按本轮确切 user ID 的清理；响应异常也不会遗留 Session 或 Debate。
	t.Cleanup(func() {
		f.server.Close()
		db, err := platform.OpenDatabase(databaseConfig)
		if err != nil {
			t.Error("T05 cleanup connection")
			return
		}
		defer db.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var identity struct {
			Database string
			Account  string
		}
		if err = db.ORM.WithContext(ctx).Raw("SELECT DATABASE() AS `database`,CURRENT_USER() AS account").Scan(&identity).Error; err != nil || identity.Database != "siyige_test" || identity.Account != "siyige_test_app@%" {
			t.Error("T05 cleanup identity")
			return
		}
		for _, uid := range f.userIDs {
			for _, q := range []string{"DELETE m FROM messages m JOIN debate_turns t ON m.turn_id=t.id JOIN debates d ON t.debate_id=d.id WHERE d.user_id=?", "DELETE t FROM debate_turns t JOIN debates d ON t.debate_id=d.id WHERE d.user_id=?", "DELETE FROM debates WHERE user_id=?", "DELETE FROM auth_sessions WHERE user_id=?"} {
				if e := db.ORM.WithContext(ctx).Exec(q, uid).Error; e != nil {
					t.Error("T05 fixture cleanup failed")
				}
			}
		}
	})
	user := f.randomUsername(t)
	result, id := f.register(t, user, integrationPassword)
	if result.status != 201 {
		t.Fatal("T05 register")
	}
	f.uid = id
	result, f.cookie, f.csrf = f.login(t, user, integrationPassword)
	if result.status != 200 {
		t.Fatal("T05 login")
	}
	return f
}
func (f *debateFixture) mount(t *testing.T, g ai.Generator) {
	f.mountDB(t, g, f.db.ORM)
}
func (f *debateFixture) mountDB(t *testing.T, g ai.Generator, orm *gorm.DB) {
	t.Helper()
	f.server.Close()
	router := platform.NewBusinessRouter(slog.New(slog.NewJSONHandler(io.Discard, nil)), f.db, f.handler, topic.NewHandler(topic.NewService(topic.NewRepository(f.db.ORM))), debate.NewHandler(debate.NewService(debate.NewRepository(orm), g, f.gate)))
	f.server = httptest.NewUnstartedServer(router)
	f.server.Config.WriteTimeout = platform.HTTPWriteTimeout
	f.server.Start()
}
func (f *debateFixture) request(t *testing.T, method, path string, body any) apiResult {
	t.Helper()
	s := ""
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			t.Fatal(e)
		}
		s = string(b)
	}
	return f.do(t, method, path, s, integrationOrigin, f.csrf, f.cookie)
}
func (f *debateFixture) create(t *testing.T) string {
	t.Helper()
	r := f.request(t, "POST", "/api/v1/debates", map[string]any{"topic_id": "1", "user_stance": "pro"})
	if r.status != 201 {
		t.Fatalf("create status=%d code=%s", r.status, r.errorCode())
	}
	var id string
	if e := json.Unmarshal(r.data()["id"], &id); e != nil || id == "" {
		t.Fatal("create id")
	}
	return id
}
func submit(content, key string, round int) map[string]any {
	return map[string]any{"content": content, "client_request_id": key, "round_no": round}
}
func TestT05SlowHTTPPersistsAndReplays(t *testing.T) {
	var calls atomic.Int32
	fake := ai.NewFake()
	f := newDebateFixture(t, testGenerator(func(c context.Context, r ai.Request) (ai.Result, error) {
		calls.Add(1)
		select {
		case <-time.After(5500 * time.Millisecond):
			return fake.Generate(c, r)
		case <-c.Done():
			return ai.Result{}, c.Err()
		}
	}))
	id := f.create(t)
	path := "/api/v1/debates/" + id
	key := uuid.NewString()
	start := time.Now()
	r := f.request(t, "POST", path+"/turns", submit("原文保留　", key, 1))
	if r.status != 201 || time.Since(start) < 5*time.Second {
		t.Fatalf("slow HTTP status=%d code=%s", r.status, r.errorCode())
	}
	assertRequestID(t, r)
	assertNoStore(t, r)
	r = f.request(t, "POST", path+"/turns", submit("原文保留　", strings.ToUpper(key), 1))
	if r.status != 200 || calls.Load() != 1 {
		t.Fatal("completed replay generated")
	}
	r = f.request(t, "POST", path+"/turns", submit("改文", key, 1))
	if r.status != 409 || r.errorCode() != "idempotency_conflict" {
		t.Fatal("idempotency content conflict")
	}
	r = f.request(t, "POST", path+"/turns", submit("原文保留　", key, 2))
	if r.status != 409 || r.errorCode() != "idempotency_conflict" {
		t.Fatal("idempotency round conflict")
	}
	r = f.request(t, "POST", path+"/turns", submit("下一轮", uuid.NewString(), 2))
	if r.status != 409 {
		t.Fatal("second round admitted")
	}
	f.mount(t, ai.NewFake())
	r = f.request(t, "GET", path, nil)
	var detail debate.DetailDTO
	b, _ := json.Marshal(r.data())
	if json.Unmarshal(b, &detail) != nil || r.status != 200 || detail.Debate.CompletedRounds != 1 || detail.Debate.Status != "active" || len(detail.Turns) != 1 || detail.Turns[0].AttemptVersion != 1 || len(detail.Turns[0].Messages) != 2 || detail.Turns[0].Messages[0].Content != "原文保留　" {
		t.Fatal("refresh did not preserve formal messages")
	}
	var count int64
	f.db.ORM.Table("messages").Where("turn_id = ?", detail.Turns[0].ID).Count(&count)
	if count != 2 {
		t.Fatal("duplicate messages")
	}
}
func TestT05InputAuthOwnershipAndTopics(t *testing.T) {
	fake := ai.NewFake()
	f := newDebateFixture(t, fake)
	r := f.request(t, "GET", "/api/v1/topics", nil)
	var topics []topic.TopicDTO
	if json.Unmarshal(r.data()["items"], &topics) != nil || r.status != 200 || len(topics) < 5 {
		t.Fatal("real topics missing")
	}
	for i := 1; i < len(topics); i++ {
		a, _ := debate.ParseID(topics[i-1].ID)
		b, _ := debate.ParseID(topics[i].ID)
		if a >= b {
			t.Fatal("topics unordered")
		}
	}
	r = f.do(t, "GET", "/api/v1/topics", "", "", "")
	if r.status != 401 {
		t.Fatal("missing auth")
	}
	r = f.do(t, "POST", "/api/v1/debates", `{"topic_id":"1","user_stance":"pro"}`, integrationOrigin, "", f.cookie)
	if r.status != 403 {
		t.Fatal("CSRF not enforced")
	}
	id := f.create(t)
	if len(fake.Records()) != 0 {
		t.Fatal("create called generator")
	}
	path := "/api/v1/debates/" + id
	for _, n := range []int{2001, 2000} {
		r = f.request(t, "POST", path+"/turns", submit(strings.Repeat("𠮷", n), uuid.NewString(), 1))
		want := 400
		if n == 2000 {
			want = 201
		}
		if r.status != want {
			t.Fatalf("boundary %d status %d", n, r.status)
		}
	}
	for _, bad := range []string{"0", "01", "18446744073709551616"} {
		if r = f.request(t, "GET", "/api/v1/debates/"+bad, nil); r.status != 400 {
			t.Fatal("invalid id")
		}
	}
	other := f.randomUsername(t)
	r, _ = f.register(t, other, integrationPassword)
	if r.status != 201 {
		t.Fatal("second register")
	}
	r, cookie, csrf := f.login(t, other, integrationPassword)
	if r.status != 200 {
		t.Fatal("second login")
	}
	foreign := f.do(t, "GET", path, "", "", "", cookie)
	missing := f.do(t, "GET", "/api/v1/debates/18446744073709551615", "", "", "", cookie)
	if foreign.status != 404 || missing.status != 404 || foreign.errorCode() != missing.errorCode() {
		t.Fatal("ownership leaked")
	}
	r = f.do(t, "POST", path+"/turns", `{"round_no":1,"content":"越权","client_request_id":"`+uuid.NewString()+`"}`, integrationOrigin, csrf, cookie)
	if r.status != 404 {
		t.Fatal("foreign turn")
	}
}
