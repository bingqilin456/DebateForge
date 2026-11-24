package integration

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/google/uuid"
)

func TestNeonDemoVerdictHTTPPersistence(t *testing.T) {
	f := newDebateFixture(t, ai.NewFake())
	id := f.create(t)
	t.Cleanup(func() {
		if err := f.db.ORM.Exec("DELETE FROM neon_verdicts WHERE debate_id = ?", id).Error; err != nil {
			t.Errorf("清理裁决夹具: %v", err)
		}
	})
	path := "/api/v1/debates/" + id
	verdict := path + "/verdict"
	picks := map[string]any{"seats": []map[string]string{
		{"judge_id": "preset-logic", "model_id": "demo"},
		{"judge_id": "preset-evidence", "model_id": "demo"},
	}}
	if response := f.request(t, "POST", verdict, picks); response.status != http.StatusConflict {
		t.Fatalf("未完赛裁决: %d %s", response.status, response.errorCode())
	}
	for i := 1; i <= 4; i++ {
		result := f.request(t, "POST", path+"/turns", map[string]any{
			"round_no": i, "content": "可检验的判断标准需要允许独立复查。", "client_request_id": uuid.NewString(),
		})
		if result.status != http.StatusCreated {
			t.Fatalf("第 %d 轮: %d %s", i, result.status, result.errorCode())
		}
	}
	created := f.request(t, "POST", verdict, picks)
	if created.status != http.StatusOK || string(created.data()["status"]) != `"completed"` ||
		created.data()["elo_delta"] != nil || !bytes.Contains(created.data()["score"], []byte(`"source":"local"`)) {
		t.Fatalf("裁决未完成或演示结果被误计分: %d %s", created.status, created.errorCode())
	}
	loaded := f.request(t, "GET", verdict, nil)
	if loaded.status != http.StatusOK || !bytes.Equal(created.data()["score"], loaded.data()["score"]) {
		t.Fatalf("刷新后裁决不一致: %d", loaded.status)
	}
	replayed := f.request(t, "POST", verdict, picks)
	if replayed.status != http.StatusOK || !bytes.Equal(created.data()["seats"], replayed.data()["seats"]) {
		t.Fatalf("重发覆盖了席位: %d", replayed.status)
	}
}
