package integration

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/bingqilin456/DebateForge/backend/internal/debate"
	"github.com/bingqilin456/DebateForge/backend/internal/platform"
	"github.com/bingqilin456/DebateForge/backend/internal/topic"
	"github.com/google/uuid"
)

func TestNeonDojoAwardsOnlyFirstCorrectAnswer(t *testing.T) {
	f := newDebateFixture(t, ai.NewFake())
	f.server.Close()
	handler := debate.NewHandler(f.service)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := platform.NewBusinessRouter(logger, f.db, f.handler, topic.NewHandler(topic.NewService(topic.NewRepository(f.db.ORM))), handler, handler.RegisterNeonDojoRoutes)
	f.server = httptest.NewServer(router)
	if reply := f.request(t, "POST", "/api/v1/dojo/history-quiz", map[string]any{}); reply.status != http.StatusOK || reply.data()["question"] != nil || reply.data()["notice"] == nil {
		t.Fatalf("无历史应提示练内置题: %d", reply.status)
	}
	if reply := f.request(t, "POST", "/api/v1/dojo/techniques", map[string]any{}); reply.status != http.StatusOK || reply.data()["notice"] == nil {
		t.Fatalf("无历史应提示查看基础技法: %d", reply.status)
	}
	answer := func(choice int) apiResult {
		return f.request(t, "POST", "/api/v1/dojo/answer", map[string]any{"question_id": "straw-man", "choice": choice})
	}
	if reply := answer(1); reply.status != http.StatusOK || string(reply.data()["elo_delta"]) != "0" {
		t.Fatalf("答错不应计分: %d", reply.status)
	}
	replies := make(chan apiResult, 2)
	for range 2 {
		go func() { replies <- answer(0) }()
	}
	rewarded := 0
	for range 2 {
		reply := <-replies
		if reply.status != http.StatusOK || string(reply.data()["rewarded"]) != "true" {
			t.Fatalf("并发答题未正确处理: %d %s", reply.status, reply.errorCode())
		}
		if string(reply.data()["elo_delta"]) == "5" {
			rewarded++
		}
	}
	if rewarded != 1 {
		t.Fatalf("并发首次答对计分次数: %d", rewarded)
	}
	state := f.request(t, "GET", "/api/v1/dojo", nil)
	var feedback []struct {
		Rewarded bool
	}
	if state.status != http.StatusOK || json.Unmarshal(state.data()["feedback"], &feedback) != nil || len(feedback) != 1 || !feedback[0].Rewarded {
		t.Fatalf("刷新后丢失答题进度: %d", state.status)
	}
	rating := f.request(t, "GET", "/api/v1/cup", nil)
	if rating.status != http.StatusOK || string(rating.data()["elo"]) != "1425" {
		t.Fatalf("杯赛积分应同时增加 5 分: %d", rating.status)
	}

	f.service.EnableAnnotations(true)
	id := f.create(t)
	const speech = "因为所有人都相信这个结论，所以它必然正确。可以调查不同群体的实际判断。"
	for round := 1; round <= 4; round++ {
		if response := f.request(t, "POST", "/api/v1/debates/"+id+"/turns", submit(speech, uuid.NewString(), round)); response.status != 201 {
			t.Fatalf("prepare history source %d: %d %s", round, response.status, response.errorCode())
		}
	}
	// Fake 的前两轮默认产生亮点。保留其真实原文锚点，换成道场可用的合成漏洞标签。
	seed := f.db.ORM.Exec(`UPDATE turn_annotations AS a JOIN debate_turns AS t ON t.id = a.turn_id
SET a.content_json = JSON_SET(a.content_json, '$.items[0].category', 'possible_logic_gap',
    '$.items[0].label', '诉诸多数', '$.items[0].reason', '相信人数不能证明结论成立',
    '$.items[0].improvement', '给出独立的可检验证据')
WHERE a.debate_id = ? AND t.round_no = 1 AND JSON_UNQUOTE(JSON_EXTRACT(a.content_json, '$.items[0].role')) = 'user'`, id)
	if seed.Error != nil || seed.RowsAffected != 1 {
		t.Fatalf("history annotation fixture: changed=%d, %v", seed.RowsAffected, seed.Error)
	}
	quiz := f.request(t, "POST", "/api/v1/dojo/history-quiz", map[string]any{})
	var question struct{ ID, Quote, Source string }
	if quiz.status != 200 || json.Unmarshal(quiz.data()["question"], &question) != nil || question.ID == "" ||
		question.Quote == "" || !strings.Contains(speech, question.Quote) || question.Source != "history" {
		t.Fatalf("history quiz should cite own saved speech: %d %+v", quiz.status, question)
	}
	technique := f.request(t, "POST", "/api/v1/dojo/techniques", map[string]any{})
	var quote string
	if technique.status != 200 || json.Unmarshal(technique.data()["quote"], &quote) != nil || quote == "" || !strings.Contains(speech, quote) {
		t.Fatalf("personal technique should cite own saved speech: %d %s", technique.status, quote)
	}
}
