package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/bingqilin456/DebateForge/backend/internal/auth"
	"github.com/bingqilin456/DebateForge/backend/internal/debate"
	"github.com/bingqilin456/DebateForge/backend/internal/platform"
	"github.com/bingqilin456/DebateForge/backend/internal/topic"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestNeonCatalogSharingAndSnapshots(t *testing.T) {
	f := newDebateFixture(t, ai.NewFake())
	guestName := f.randomUsername(t)
	if reply, _ := f.register(t, guestName, integrationPassword); reply.status != 201 {
		t.Fatalf("register guest: %d", reply.status)
	}
	reply, guestCookie, guestCSRF := f.login(t, guestName, integrationPassword)
	if reply.status != 200 {
		t.Fatalf("login guest: %d", reply.status)
	}
	const catalog = "/api/v1/neon-catalog"
	topicBody := map[string]any{"title": "隔离辩题原文", "background": "隔离背景", "category": "campus"}
	opponentBody := map[string]any{"name": "隔离辩手原名", "tag": "追问", "difficulty": "varsity", "persona": "给每个主张说明条件。"}
	create := func(path string, payload map[string]any) string {
		result := f.request(t, "POST", path, payload)
		var id string
		if result.status != 201 || json.Unmarshal(result.data()["id"], &id) != nil || id == "" {
			t.Fatalf("create %s: %d %s", path, result.status, result.errorCode())
		}
		return id
	}
	topicID := create(catalog+"/topics", topicBody)
	opponentID := create(catalog+"/opponents", opponentBody)
	change := func(path string, visible bool) {
		if res := f.request(t, "POST", path+"/visibility", map[string]bool{"public": visible}); res.status != 200 {
			t.Fatalf("change visibility: %d", res.status)
		}
	}
	topicPath, opponentPath := catalog+"/topics/"+topicID, catalog+"/opponents/"+opponentID
	guest := func(method, path string, body map[string]any) apiResult {
		var raw string
		if body != nil {
			data, _ := json.Marshal(body)
			raw = string(data)
		}
		return f.do(t, method, path, raw, integrationOrigin, guestCSRF, guestCookie)
	}
	type listing struct {
		ID       string
		Official bool
		Public   bool
		Heat     int
	}
	entry := func(path, id string) *listing {
		var items []listing
		res := guest("GET", path, nil)
		if res.status != 200 || json.Unmarshal(res.data()["items"], &items) != nil {
			t.Fatalf("read catalog: %d %s", res.status, res.errorCode())
		}
		for _, item := range items {
			if item.ID == id && !item.Official {
				return &item
			}
		}
		return nil
	}
	if entry(catalog+"/topics", topicID) != nil || entry(catalog+"/opponents", opponentID) != nil {
		t.Fatal("guest saw private catalog entries")
	}
	match := map[string]any{"neon_topic_id": topicID, "neon_opponent_id": opponentID, "user_stance": "pro"}
	if res := guest("POST", "/api/v1/debates", match); res.status != 404 {
		t.Fatalf("guest used private choice: %d", res.status)
	}
	change(topicPath, true)
	change(opponentPath, true)
	if item := entry(catalog+"/topics", topicID); item == nil || !item.Public || item.Heat != 0 {
		t.Fatalf("new public topic: %+v", item)
	}
	if item := entry(catalog+"/opponents", opponentID); item == nil || !item.Public || item.Heat != 0 {
		t.Fatalf("new public opponent: %+v", item)
	}
	var playedID string
	for range 2 {
		res := guest("POST", "/api/v1/debates", match)
		if res.status != 201 || json.Unmarshal(res.data()["id"], &playedID) != nil {
			t.Fatalf("guest uses public catalog: %d %s", res.status, res.errorCode())
		}
		if res := guest("POST", "/api/v1/debates/"+playedID+"/turns", submit("每局只记录独立用户的实际交锋。", uuid.NewString(), 1)); res.status != 201 {
			t.Fatalf("guest starts turn: %d", res.status)
		}
	}
	create("/api/v1/debates", match)
	if item := entry(catalog+"/topics", topicID); item == nil || item.Heat != 1 {
		t.Fatalf("topic counted duplicate starter or empty game: %+v", item)
	}
	if item := entry(catalog+"/opponents", opponentID); item == nil || item.Heat != 1 {
		t.Fatalf("opponent counted duplicate starter or empty game: %+v", item)
	}
	topicBody["title"] = "更新过的题目"
	opponentBody["name"] = "更新过的辩手"
	for path, data := range map[string]map[string]any{topicPath: topicBody, opponentPath: opponentBody} {
		if res := f.request(t, "POST", path, data); res.status != 200 {
			t.Fatalf("edit: %d", res.status)
		}
	}
	res := guest("GET", "/api/v1/debates/"+playedID, nil)
	var snapshot struct {
		Topic        struct{ Title string }
		OpponentName string `json:"opponent_name"`
	}
	if res.status != 200 || json.Unmarshal(res.data()["debate"], &snapshot) != nil || snapshot.Topic.Title != "隔离辩题原文" || snapshot.OpponentName != "隔离辩手原名" {
		t.Fatalf("debate snapshots changed: %+v / %d", snapshot, res.status)
	}
	change(topicPath, false)
	change(opponentPath, false)
	if entry(catalog+"/topics", topicID) != nil || entry(catalog+"/opponents", opponentID) != nil {
		t.Fatal("guest saw withdrawn entries")
	}
	if res := guest("POST", "/api/v1/debates", match); res.status != 404 {
		t.Fatalf("guest used withdrawn choice: %d", res.status)
	}
}

func TestNeonProfileRankAndPrivateHistoryPagination(t *testing.T) {
	f := newDebateFixture(t, ai.NewFake())
	f.server.Close()
	handler := debate.NewHandler(f.service)
	repo := debate.NewRepository(f.db.ORM)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	f.server = httptest.NewServer(platform.NewBusinessRouter(logger, f.db, f.handler,
		topic.NewHandler(topic.NewService(topic.NewRepository(f.db.ORM))), handler,
		repo.RegisterNeonProfileRoutes, handler.RegisterNeonDojoRoutes))
	profile := f.request(t, "GET", "/api/v1/profile", nil)
	if profile.status != 200 || string(profile.data()["elo"]) != "1420" || string(profile.data()["elo_history"]) != "[]" {
		t.Fatalf("empty profile: %d %s", profile.status, profile.errorCode())
	}
	uid := strconv.FormatUint(f.uid, 10)
	const nickname = "隔离测试辩手"
	data := map[string]any{"nickname": nickname, "avatar_kind": "seal", "avatar_seal": "弈", "avatar_image": ""}
	if res := f.request(t, "POST", "/api/v1/profile", data); res.status != 200 {
		t.Fatalf("save profile: %d %s", res.status, res.errorCode())
	}
	publicPath := "/api/v1/public/profiles/" + uid
	public := f.do(t, "GET", publicPath, "", "", "")
	var gotName string
	if public.status != 200 || json.Unmarshal(public.data()["nickname"], &gotName) != nil || gotName != nickname || len(public.data()) != 8 {
		t.Fatalf("public profile fields: %d %s", public.status, public.errorCode())
	}
	if res := f.request(t, "POST", "/api/v1/dojo/answer", map[string]any{"question_id": "straw-man", "choice": 0}); res.status != 200 {
		t.Fatalf("dojo reward: %d", res.status)
	}
	profile = f.request(t, "GET", "/api/v1/profile", nil)
	var points []struct{ Elo int }
	if profile.status != 200 || string(profile.data()["games"]) != "0" || json.Unmarshal(profile.data()["elo_history"], &points) != nil || len(points) != 2 || points[0].Elo != 1420 || points[1].Elo != 1425 {
		t.Fatalf("practice points or official standings inconsistent: %d %+v", profile.status, points)
	}
	var users []uint64
	var lastMatch string
	t.Cleanup(func() {
		if len(users) > 0 && f.db.ORM.Exec("DELETE FROM users WHERE id IN ?", users).Error != nil {
			t.Error("profile synthetic standings cleanup")
		}
	})
	for index := range 11 {
		row := struct {
			ID           uint64
			Username     string
			PasswordHash string
		}{Username: "rank_" + uuid.NewString()[:12], PasswordHash: "synthetic-row-only"}
		if err := f.db.ORM.Table("users").Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		users = append(users, row.ID)
		elo := 2000000000 - index
		if index == 1 {
			elo++
		}
		if err := f.db.ORM.Exec("INSERT INTO neon_cup_players (user_id, elo, unlocked_index, games, wins) VALUES (?, ?, 0, 10, ?)", row.ID, elo, index).Error; err != nil {
			t.Fatal(err)
		}
		lastMatch = f.create(t)
	}
	readRanks := func(page int) []struct {
		UserID string `json:"user_id"`
		Rank   int
	} {
		res := f.request(t, "GET", fmt.Sprintf("/api/v1/leaderboard?page=%d", page), nil)
		var items []struct {
			UserID string `json:"user_id"`
			Rank   int
		}
		if res.status != 200 || json.Unmarshal(res.data()["items"], &items) != nil {
			t.Fatalf("read leaderboard: %d %s", res.status, res.errorCode())
		}
		return items
	}
	first, second := readRanks(1), readRanks(2)
	if len(first) != 10 || len(second) < 1 || first[0].UserID != strconv.FormatUint(users[1], 10) || first[1].UserID != strconv.FormatUint(users[0], 10) ||
		second[0].UserID != strconv.FormatUint(users[10], 10) || second[0].Rank != 11 {
		t.Fatalf("unstable leaderboard or wrong win-rate tie-break: %+v / %+v", first, second)
	}
	if err := f.db.ORM.Exec("UPDATE debates SET status = 'completed', completed_rounds = 4 WHERE id = ?", lastMatch).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.ORM.Exec("INSERT INTO debate_reviews (debate_id, status) VALUES (?, 'failed')", lastMatch).Error; err != nil {
		t.Fatal(err)
	}
	firstHistory := f.request(t, "GET", "/api/v1/debates?page=1&page_size=10", nil)
	secondHistory := f.request(t, "GET", "/api/v1/debates?page=2&page_size=10", nil)
	for i, res := range []apiResult{firstHistory, secondHistory} {
		var matches []debate.DebateDTO
		if res.status != 200 || json.Unmarshal(res.data()["items"], &matches) != nil || len(matches) != 10-i*9 || string(res.data()["total"]) != "11" {
			t.Fatalf("history page %d: %d %d", i+1, res.status, len(matches))
		}
		if i == 0 && (matches[0].ID != lastMatch || matches[0].ReviewStatus != "failed" || matches[1].Status != "active") {
			t.Fatal("completed review failure and active matches are not distinguishable")
		}
	}
}

func TestNeonSameTopicPublicReportsHaveDistinctLinks(t *testing.T) {
	f := newDebateFixture(t, ai.NewFake())
	f.server.Close()
	share := debate.NewNeonShareHandler(debate.NewRepository(f.db.ORM))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	f.server = httptest.NewServer(platform.NewBusinessRouter(logger, f.db, f.handler, nil, debate.NewHandler(f.service),
		func(group *gin.RouterGroup, a *auth.Handler) {
			share.RegisterPublic(group)
			share.RegisterProtected(group, a)
		}))
	change := func(id, action string) string {
		result := f.request(t, "POST", "/api/v1/debates/"+id+"/sharing", map[string]string{"action": action})
		if result.status != 200 {
			t.Fatalf("sharing %s: %d %s", action, result.status, result.errorCode())
		}
		var link string
		if err := json.Unmarshal(result.data()["share_id"], &link); err != nil {
			t.Fatal(err)
		}
		return link
	}
	first, second := f.create(t), f.create(t)
	// 用两条合成完赛记录验证发布主键，同一账号无须消耗两场模型调用额度。
	if err := f.db.ORM.Exec("UPDATE debates SET status = 'completed', completed_rounds = 4 WHERE id IN ? AND user_id = ?",
		[]string{first, second}, f.uid).Error; err != nil {
		t.Fatal(err)
	}
	firstLink, secondLink := change(first, "publish"), change(second, "publish")
	if firstLink == "" || secondLink == "" || firstLink == secondLink || change(first, "publish") != firstLink {
		t.Fatal("same-topic reports collided or publish retry changed the link")
	}
	path := fmt.Sprintf("/api/v1/public/reports?topic_id=1&owner_id=%d", f.uid)
	list := f.do(t, "GET", path, "", "", "")
	if list.status != 200 || string(list.data()["total"]) != "2" {
		t.Fatalf("same-topic list: %d", list.status)
	}
	change(first, "withdraw")
	if f.do(t, "GET", "/api/v1/public/reports/"+firstLink, "", "", "").status != 404 ||
		f.do(t, "GET", "/api/v1/public/reports/"+secondLink, "", "", "").status != 200 {
		t.Fatal("withdraw affected a different match")
	}
	if newLink := change(first, "publish"); newLink == "" || newLink == firstLink {
		t.Fatal("republishing revived the revoked link")
	}
}
