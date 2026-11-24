package debate

import (
	"errors"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/bingqilin456/DebateForge/backend/internal/auth"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const neonStartingElo = 1420

type neonCupStage struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Difficulty  string `json:"difficulty"`
	AIStyle     string `json:"ai_style"`
	MinElo      int    `json:"min_elo"`
	WinReward   int    `json:"win_reward"`
	Explanation string `json:"explanation"`
}

var neonCupStages = [...]neonCupStage{
	{"prelim", "初赛", "beginner", "logical", 1000, 15, "从界定概念和搭建立论开始。"},
	{"quarter", "复赛", "varsity", "logical", 1400, 20, "重点练习找出论证前提和回应反例。"},
	{"semi", "半决赛", "national", "data", 1500, 25, "与重视证据和数据的对手交锋。"},
	{"final", "决赛", "national", "aggressive", 1600, 32, "适合练习高强度反击和结辩收束。"},
}

func neonStageIndex(id string) int {
	for i, stage := range neonCupStages {
		if stage.ID == id {
			return i
		}
	}
	return -1
}

func neonSuggestedStage(elo int) neonCupStage {
	for i := len(neonCupStages) - 1; i > 0; i-- {
		if elo >= neonCupStages[i].MinElo {
			return neonCupStages[i]
		}
	}
	return neonCupStages[0]
}

type neonCupPlayer struct {
	UserID        uint64 `gorm:"primaryKey;autoIncrement:false"`
	Elo           int
	UnlockedIndex int
	Games         int
	Wins          int
}

func (neonCupPlayer) TableName() string { return "neon_cup_players" }

type neonEloEntry struct {
	DebateID      uint64 `gorm:"primaryKey;autoIncrement:false"`
	UserID        uint64
	EloBefore     int
	EloAfter      int
	Delta         int
	Outcome       string
	UnlockedIndex int
}

func (neonEloEntry) TableName() string { return "neon_elo_entries" }

func neonApplyOutcome(player neonCupPlayer, cupStage, outcome string) neonCupPlayer {
	stage := neonStageIndex(cupStage)
	if stage > player.UnlockedIndex {
		return player
	}
	player.Games++
	switch outcome {
	case "win":
		player.Wins++
		if stage < 0 {
			player.Elo += 18
		} else {
			player.Elo += neonCupStages[stage].WinReward
			if stage == player.UnlockedIndex && stage < len(neonCupStages)-1 {
				player.UnlockedIndex++
			}
		}
	case "loss":
		player.Elo = max(1000, player.Elo-8)
	}
	return player
}

func readNeonCupPlayer(db *gorm.DB, uid uint64) (neonCupPlayer, error) {
	var player neonCupPlayer
	err := db.Where("user_id = ?", uid).Take(&player).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return neonCupPlayer{UserID: uid, Elo: neonStartingElo}, nil
	}
	return player, err
}

type neonSettlement struct {
	Delta         int    `json:"elo_delta"`
	Elo           int    `json:"elo"`
	UnlockedStage string `json:"unlocked_stage"`
}

// 裁决和积分流水在同一事务提交。每局只产生一条流水；锁住账号积分行，避免两场同时覆盖余额。
func settleNeonCup(tx *gorm.DB, d Debate, outcome string) (neonSettlement, error) {
	total, err := ai.RoundsFor(ai.Mode(d.Mode))
	if err != nil || d.Mode != string(ai.ModeStandard4) || d.Practice || d.Status != "completed" || d.CompletedRounds != total ||
		(outcome != "win" && outcome != "loss" && outcome != "draw") {
		return neonSettlement{}, ErrRoundConflict
	}
	seed := neonCupPlayer{UserID: d.UserID, Elo: neonStartingElo}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&seed).Error; err != nil {
		return neonSettlement{}, err
	}
	var player neonCupPlayer
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", d.UserID).Take(&player).Error; err != nil {
		return neonSettlement{}, err
	}
	var entry neonEloEntry
	err = tx.Where("debate_id = ? AND user_id = ?", d.ID, d.UserID).Take(&entry).Error
	if err == nil {
		return neonSettlement{entry.Delta, entry.EloAfter, neonCupStages[entry.UnlockedIndex].ID}, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return neonSettlement{}, err
	}
	if neonStageIndex(d.CupStage) > player.UnlockedIndex {
		return neonSettlement{}, ErrRoundConflict
	}
	next := neonApplyOutcome(player, d.CupStage, outcome)
	entry = neonEloEntry{d.ID, d.UserID, player.Elo, next.Elo, next.Elo - player.Elo, outcome, next.UnlockedIndex}
	if err := tx.Create(&entry).Error; err != nil {
		return neonSettlement{}, err
	}
	if err := tx.Save(&next).Error; err != nil {
		return neonSettlement{}, err
	}
	return neonSettlement{entry.Delta, entry.EloAfter, neonCupStages[entry.UnlockedIndex].ID}, nil
}

type neonCupStatus struct {
	Elo              int            `json:"elo"`
	Rank             int64          `json:"rank,omitempty"`
	UnlockedStage    string         `json:"unlocked_stage"`
	RecommendedStage string         `json:"recommended_stage"`
	Stages           []neonCupStage `json:"stages"`
}

func (h *Handler) registerNeonCupRoutes(group *gin.RouterGroup, a *auth.Handler) {
	group.GET("/cup", a.RequireAuth(), func(c *gin.Context) {
		current, _ := auth.CurrentUserFrom(c)
		r, ok := h.service.store.(*Repository)
		if !ok {
			serviceError(c, dependency(nil, false))
			return
		}
		var result neonCupStatus
		err := r.transaction(c.Request.Context(), true, func(tx *gorm.DB) error {
			player, err := readNeonCupPlayer(tx, current.User.ID)
			if err != nil {
				return err
			}
			result = neonCupStatus{Elo: player.Elo, UnlockedStage: neonCupStages[player.UnlockedIndex].ID,
				RecommendedStage: neonSuggestedStage(player.Elo).ID, Stages: neonCupStages[:]}
			if player.Games > 0 {
				var ahead int64
				if err := tx.Model(&neonCupPlayer{}).Where("games > 0 AND (elo > ? OR (elo = ? AND wins * ? > games * ?))", player.Elo, player.Elo, player.Games, player.Wins).Count(&ahead).Error; err != nil {
					return err
				}
				result.Rank = ahead + 1
			}
			return nil
		})
		if err != nil {
			serviceError(c, dependency(err, false))
			return
		}
		writeData(c, 200, result)
	})
}
