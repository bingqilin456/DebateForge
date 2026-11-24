package debate

import (
	"testing"

	"gorm.io/gorm"
)

func SettleNeonCupForTest(tx *gorm.DB, d Debate, outcome string) (int, error) {
	settlement, err := settleNeonCup(tx, d, outcome)
	return settlement.Delta, err
}

func TestNeonCupRatingBands(t *testing.T) {
	for _, tt := range []struct {
		elo  int
		want string
	}{{1399, "prelim"}, {1400, "quarter"}, {1499, "quarter"}, {1500, "semi"}, {1599, "semi"}, {1600, "final"}} {
		if got := neonSuggestedStage(tt.elo); got.ID != tt.want {
			t.Errorf("ELO %d: got %s, want %s", tt.elo, got.ID, tt.want)
		}
	}
}

func TestNeonCupAdvanceAndScores(t *testing.T) {
	for _, tt := range []struct {
		name, stage, outcome              string
		elo, unlocked, wantElo, wantStage int
	}{
		{"当前最高赛段胜出", "quarter", "win", 1420, 1, 1440, 2},
		{"回到已解锁低阶", "prelim", "win", 1420, 1, 1435, 1},
		{"练手阶段不可晋级", "final", "win", 1420, 1, 1420, 1},
		{"平局不扣分", "quarter", "draw", 1420, 1, 1420, 1},
		{"积分下限", "quarter", "loss", 1003, 1, 1000, 1},
		{"决赛为最后一阶", "final", "win", 1600, 3, 1632, 3},
		{"普通正式对局有积分但不晋级", "", "win", 1420, 0, 1438, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := neonApplyOutcome(neonCupPlayer{Elo: tt.elo, UnlockedIndex: tt.unlocked}, tt.stage, tt.outcome)
			if got.Elo != tt.wantElo || got.UnlockedIndex != tt.wantStage {
				t.Fatalf("got elo=%d stage=%d, want elo=%d stage=%d", got.Elo, got.UnlockedIndex, tt.wantElo, tt.wantStage)
			}
		})
	}
}
