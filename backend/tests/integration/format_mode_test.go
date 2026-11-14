package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/google/uuid"
)

// TestStandard4FourRoundsEndToEnd 是本轮赛制升级的核心集成证据：
// 新会话按 standard_4 跑满 4 轮、落库 8 条正式消息、第 5 轮被拒，
// 随后总结成功且允许引用第 4 轮。它同时验证 00012 迁移放宽后的数据库约束
// 与业务状态机确实一致（若 CHECK 仍是 1–3，第 4 轮会直接 500）。
func TestStandard4FourRoundsEndToEnd(t *testing.T) {
	var calls atomic.Int32
	fake := ai.NewFake()
	// 使用组合生成器，使四轮结束后的总结也能走真实业务链路。
	f := newDebateFixture(t, testCombinedGenerator{genTurn: func(c context.Context, r ai.Request) (ai.Result, error) {
		calls.Add(1)
		return fake.Generate(c, r)
	}})
	id := f.create(t)
	path := "/api/v1/debates/" + id

	// 创建后确认赛制与流派由服务端固定为 standard_4 / logical。
	detail := f.request(t, "GET", path, nil)
	if detail.status != 200 {
		t.Fatalf("detail status=%d code=%s", detail.status, detail.errorCode())
	}
	var debateObj map[string]any
	if e := json.Unmarshal(detail.data()["debate"], &debateObj); e != nil {
		t.Fatal(e)
	}
	if mode, _ := debateObj["mode"].(string); mode != "standard_4" {
		t.Fatalf("新会话赛制应为 standard_4，实际 %q", mode)
	}
	if total, _ := debateObj["total_rounds"].(float64); int(total) != 4 {
		t.Fatalf("standard_4 的 total_rounds 应为 4，实际 %v", debateObj["total_rounds"])
	}

	// 连续 4 轮，轮次从 1 递增到 4。
	for round := 1; round <= 4; round++ {
		r := f.request(t, "POST", path+"/turns", submit("第"+string(rune('0'+round))+"轮发言", uuid.NewString(), round))
		if r.status != 201 {
			t.Fatalf("第 %d 轮 status=%d code=%s", round, r.status, r.errorCode())
		}
		var turn map[string]any
		if e := json.Unmarshal(r.data()["turn"], &turn); e != nil {
			t.Fatal(e)
		}
		if got, _ := turn["round_no"].(float64); int(got) != round {
			t.Fatalf("第 %d 轮 round_no 返回 %v", round, turn["round_no"])
		}
	}

	// 第 4 轮完成后会话应为 completed，completed_rounds=4，共 8 条消息。
	detail = f.request(t, "GET", path, nil)
	if e := json.Unmarshal(detail.data()["debate"], &debateObj); e != nil {
		t.Fatal(e)
	}
	if status, _ := debateObj["status"].(string); status != "completed" {
		t.Fatalf("四轮完成后状态应为 completed，实际 %q", status)
	}
	if rounds, _ := debateObj["completed_rounds"].(float64); int(rounds) != 4 {
		t.Fatalf("completed_rounds 应为 4，实际 %v", debateObj["completed_rounds"])
	}
	var turns []map[string]any
	if e := json.Unmarshal(detail.data()["turns"], &turns); e != nil {
		t.Fatal(e)
	}
	if len(turns) != 4 {
		t.Fatalf("应落库 4 个轮次，实际 %d", len(turns))
	}
	messages := 0
	for _, turn := range turns {
		msgs, _ := turn["messages"].([]any)
		messages += len(msgs)
	}
	if messages != 8 {
		t.Fatalf("四轮应有 8 条正式消息，实际 %d", messages)
	}

	// 第 5 轮必须被拒，且不能写库。
	before := calls.Load()
	r := f.request(t, "POST", path+"/turns", submit("第 5 轮越界", uuid.NewString(), 5))
	if r.status != 409 || r.errorCode() != "round_conflict" {
		t.Fatalf("第 5 轮应 409 round_conflict，实际 status=%d code=%s", r.status, r.errorCode())
	}
	if calls.Load() != before {
		t.Fatal("越界轮次不得调用模型")
	}

	// 总结必须成功，并允许出现第 4 轮的引用。
	summary := f.request(t, "POST", path+"/summary", map[string]any{"retry": false})
	if summary.status != 201 {
		t.Fatalf("四轮总结 status=%d code=%s", summary.status, summary.errorCode())
	}
	var schemaVersion int
	if e := json.Unmarshal(summary.data()["schema_version"], &schemaVersion); e != nil {
		t.Fatal(e)
	}
	if schemaVersion != ai.SummarySchemaVersion {
		t.Fatalf("总结 schema_version 应为 %d，实际 %d", ai.SummarySchemaVersion, schemaVersion)
	}
}

// TestQuick3HistoryStillCompletesAtThree 确认历史 quick_3 语义未被子系统改动破坏：
// 把会话切回 quick_3 后第 3 轮即完成，第 4 轮被拒。
// 赛制由服务端在创建时固定，本用例直接写库构造历史场景。
func TestQuick3HistoryStillCompletesAtThree(t *testing.T) {
	fake := ai.NewFake()
	f := newDebateFixture(t, testCombinedGenerator{genTurn: func(c context.Context, r ai.Request) (ai.Result, error) {
		return fake.Generate(c, r)
	}})
	id := f.create(t)
	// 模拟 00012 迁移前的历史行：赛制为 quick_3。
	if err := f.db.ORM.Exec("UPDATE debates SET mode = ? WHERE id = ?", "quick_3", id).Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/debates/" + id

	for round := 1; round <= 3; round++ {
		r := f.request(t, "POST", path+"/turns", submit("历史第"+string(rune('0'+round))+"轮", uuid.NewString(), round))
		if r.status != 201 {
			t.Fatalf("quick_3 第 %d 轮 status=%d code=%s", round, r.status, r.errorCode())
		}
	}
	detail := f.request(t, "GET", path, nil)
	var debateObj map[string]any
	if e := json.Unmarshal(detail.data()["debate"], &debateObj); e != nil {
		t.Fatal(e)
	}
	if status, _ := debateObj["status"].(string); status != "completed" {
		t.Fatalf("quick_3 三轮后应为 completed，实际 %q", status)
	}
	if rounds, _ := debateObj["completed_rounds"].(float64); int(rounds) != 3 {
		t.Fatalf("completed_rounds 应为 3，实际 %v", debateObj["completed_rounds"])
	}

	r := f.request(t, "POST", path+"/turns", submit("quick_3 第 4 轮越界", uuid.NewString(), 4))
	if r.status != 409 || r.errorCode() != "round_conflict" {
		t.Fatalf("quick_3 第 4 轮应 409，实际 status=%d code=%s", r.status, r.errorCode())
	}
	_ = http.StatusOK
}

// TestCreateRejectsClientSuppliedMode 确认赛制不能被客户端参数修改：
// 即使请求体带上 mode，服务端仍按 standard_4 创建。
func TestCreateRejectsClientSuppliedMode(t *testing.T) {
	f := newDebateFixture(t, testGenerator(func(c context.Context, r ai.Request) (ai.Result, error) {
		return ai.NewFake().Generate(c, r)
	}))
	r := f.request(t, "POST", "/api/v1/debates", map[string]any{
		"topic_id":    "1",
		"user_stance": "pro",
		"mode":        "quick_3",
		"ai_style":    "aggressive",
	})
	if r.status != 201 {
		t.Fatalf("create status=%d code=%s", r.status, r.errorCode())
	}
	var id string
	if e := json.Unmarshal(r.data()["id"], &id); e != nil || id == "" {
		t.Fatal("create id")
	}
	var row struct {
		Mode    string
		AIStyle string
	}
	if err := f.db.ORM.Raw("SELECT mode, ai_style FROM debates WHERE id = ?", id).Scan(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Mode != "standard_4" {
		t.Fatalf("客户端传参不得修改赛制，实际 %q", row.Mode)
	}
	if row.AIStyle != "logical" {
		t.Fatalf("客户端传参不得修改流派，实际 %q", row.AIStyle)
	}
}
