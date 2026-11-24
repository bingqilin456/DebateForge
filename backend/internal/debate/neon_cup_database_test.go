package debate_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/debate"
	"github.com/bingqilin456/DebateForge/backend/internal/platform"
	"gorm.io/gorm"
)

func TestNeonCupSettlesOnceConcurrentlyInIsolatedMySQL(t *testing.T) {
	if os.Getenv("SIYIGE_INTEGRATION") != "1" {
		t.Skip("仅在固定隔离 MySQL 测试环境执行")
	}
	cfg, err := platform.LoadConfig()
	if err != nil || cfg.Database.ValidateTestTarget() != nil {
		t.Fatal("隔离 MySQL 配置无效")
	}
	database, err := platform.OpenDatabase(cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db := database.ORM.WithContext(ctx)
	var identity struct{ DBName, Account string }
	if err := db.Raw("SELECT DATABASE() AS db_name, CURRENT_USER() AS account").Scan(&identity).Error; err != nil ||
		identity.DBName != "siyige_test" || identity.Account != "siyige_test_app@%" {
		t.Fatal("连接不是固定测试库")
	}
	user := struct {
		ID           uint64
		Username     string
		PasswordHash string
	}{Username: fmt.Sprintf("neon3_%d", time.Now().UnixNano()), PasswordHash: "synthetic-row-only"}
	if err := db.Table("users").Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	defer func() {
		database.ORM.Exec("DELETE FROM debates WHERE user_id = ?", user.ID)
		database.ORM.Exec("DELETE FROM users WHERE id = ?", user.ID)
	}()
	newDebate := func(stage string, practice bool) debate.Debate {
		d := debate.Debate{UserID: user.ID, TopicTitleSnapshot: "合成计分场", UserStance: "pro",
			Mode: "standard_4", AIStyle: "logical", Status: "completed", CompletedRounds: 4, CupStage: stage, Practice: practice}
		if err := db.Create(&d).Error; err != nil {
			t.Fatal(err)
		}
		return d
	}
	settle := func(d debate.Debate, outcome string) (int, error) {
		var delta int
		err := db.Transaction(func(tx *gorm.DB) error {
			var err error
			delta, err = debate.SettleNeonCupForTest(tx, d, outcome)
			return err
		})
		return delta, err
	}
	first := newDebate("prelim", false)
	results := make(chan error, 2)
	for range 2 {
		go func() {
			got, err := settle(first, "win")
			if err == nil && got != 15 {
				err = fmt.Errorf("积分差值 %d", got)
			}
			results <- err
		}()
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if delta, err := settle(newDebate("quarter", false), "draw"); err != nil || delta != 0 {
		t.Fatalf("平局积分差值应为 0，得 %d, %v", delta, err)
	}
	if _, err := settle(newDebate("semi", true), "win"); err == nil {
		t.Fatal("练习局不能计分")
	}
	var stats struct{ Elo, UnlockedIndex, Games, Wins int }
	if err := db.Table("neon_cup_players").Where("user_id = ?", user.ID).Take(&stats).Error; err != nil ||
		stats.Elo != 1435 || stats.UnlockedIndex != 1 || stats.Games != 2 || stats.Wins != 1 {
		t.Fatalf("积分或晋级不一致: %+v, %v", stats, err)
	}
	var count int64
	if err := db.Table("neon_elo_entries").Where("user_id = ?", user.ID).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("应只有两局流水，得 %d, %v", count, err)
	}
}
