package debate

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image/jpeg"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/bingqilin456/DebateForge/backend/internal/auth"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type NeonProfileFields struct {
	Nickname    string `json:"nickname"`
	AvatarKind  string `json:"avatar_kind"`
	AvatarSeal  string `json:"avatar_seal"`
	AvatarImage string `json:"avatar_image"`
}

type neonProfileInput = NeonProfileFields

type neonStoredProfile struct {
	UserID            uint64 `gorm:"primaryKey;autoIncrement:false"`
	NeonProfileFields `gorm:"embedded"`
}

func (neonStoredProfile) TableName() string { return "neon_profiles" }

type neonPublicProfile struct {
	NeonProfileFields
	Elo       int     `json:"elo"`
	RankLabel string  `json:"rank_label"`
	WinRate   float64 `json:"win_rate"`
	CupStage  string  `json:"cup_stage"`
}

type neonProfileRow struct {
	UserID            uint64
	NeonProfileFields `gorm:"embedded"`
	Elo               int
	Games             int
	Wins              int
	UnlockedIndex     int
}

func (row neonProfileRow) publicDTO() neonPublicProfile {
	profile := row.NeonProfileFields
	if profile.Nickname == "" {
		profile = NeonProfileFields{Nickname: "思弈阁辩手", AvatarKind: "seal", AvatarSeal: "辩"}
	}
	rank := [...]string{"初露锋芒", "校队主力", "国赛辩手", "思辨大师"}
	rate := float64(row.Wins) * 100 / float64(max(row.Games, 1))
	return neonPublicProfile{profile, row.Elo, rank[neonStageIndex(neonSuggestedStage(row.Elo).ID)], rate, neonCupStages[row.UnlockedIndex].Name}
}

func validNeonProfileText(value string, limit int) bool {
	length := utf8.RuneCountInString(value)
	return length >= 1 && length <= limit && utf8.ValidString(value) && strings.IndexFunc(value, func(r rune) bool {
		return unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
	}) == -1
}

func normalizeNeonProfile(in NeonProfileFields) (NeonProfileFields, error) {
	in.Nickname = strings.TrimSpace(in.Nickname)
	in.AvatarSeal = strings.TrimSpace(in.AvatarSeal)
	if !validNeonProfileText(in.Nickname, 24) {
		return NeonProfileFields{}, ErrInvalidRequest
	}
	const prefix = "data:image/jpeg;base64,"
	switch in.AvatarKind {
	case "seal":
		if !validNeonProfileText(in.AvatarSeal, 1) || in.AvatarImage != "" {
			return NeonProfileFields{}, ErrInvalidRequest
		}
	case "image":
		if in.AvatarSeal != "" || !strings.HasPrefix(in.AvatarImage, prefix) || len(in.AvatarImage) > 26000 {
			return NeonProfileFields{}, ErrInvalidRequest
		}
		raw, err := base64.StdEncoding.Strict().DecodeString(strings.TrimPrefix(in.AvatarImage, prefix))
		if err != nil {
			return NeonProfileFields{}, ErrInvalidRequest
		}
		config, err := jpeg.DecodeConfig(bytes.NewReader(raw))
		if err != nil || config.Width > 128 || config.Height > 128 {
			return NeonProfileFields{}, ErrInvalidRequest
		}
		picture, err := jpeg.Decode(bytes.NewReader(raw))
		if err != nil {
			return NeonProfileFields{}, ErrInvalidRequest
		}
		var cleaned bytes.Buffer
		if err := jpeg.Encode(&cleaned, picture, &jpeg.Options{Quality: 75}); err != nil {
			return NeonProfileFields{}, ErrInvalidRequest
		}
		// 只保存重编码后的像素，移除文件元数据与原始载荷。
		in.AvatarImage = prefix + base64.StdEncoding.EncodeToString(cleaned.Bytes())
		if len(in.AvatarImage) > 26000 {
			return NeonProfileFields{}, ErrInvalidRequest
		}
	default:
		return NeonProfileFields{}, ErrInvalidRequest
	}
	return in, nil
}

type neonEloPoint struct {
	Elo       int       `json:"elo"`
	Before    int       `json:"-" gorm:"column:prior_elo"`
	CreatedAt time.Time `json:"created_at"`
}
type neonWeakness struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}
type neonOwnProfile struct {
	neonPublicProfile
	Games           int            `json:"games"`
	Wins            int            `json:"wins"`
	EloHistory      []neonEloPoint `json:"elo_history"`
	Vulnerabilities []neonWeakness `json:"vulnerabilities"`
}

type neonLeaderboardEntry struct {
	UserID string `json:"user_id"`
	Rank   int64  `json:"rank"`
	neonPublicProfile
}
type neonLeaderboard struct {
	Items    []neonLeaderboardEntry `json:"items"`
	Page     int                    `json:"page"`
	PageSize int                    `json:"page_size"`
	Total    int64                  `json:"total"`
}

const neonProfileRateOrder = "CAST(COALESCE(p.wins, 0) AS DECIMAL(32, 16)) / GREATEST(COALESCE(p.games, 0), 1)"

func neonProfileRows(tx *gorm.DB) *gorm.DB {
	return tx.Table("users AS u").
		Select("u.id AS user_id, COALESCE(n.nickname, '') AS nickname, COALESCE(n.avatar_kind, '') AS avatar_kind, COALESCE(n.avatar_seal, '') AS avatar_seal, COALESCE(n.avatar_image, '') AS avatar_image, COALESCE(p.elo, ?) AS elo, COALESCE(p.games, 0) AS games, COALESCE(p.wins, 0) AS wins, COALESCE(p.unlocked_index, 0) AS unlocked_index", neonStartingElo).
		Joins("LEFT JOIN neon_profiles AS n ON n.user_id = u.id").
		Joins("LEFT JOIN neon_cup_players AS p ON p.user_id = u.id")
}

func readNeonProfileRow(tx *gorm.DB, uid uint64) (row neonProfileRow, err error) {
	err = neonProfileRows(tx).Where("u.id = ?", uid).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return row, ErrNotFound
	}
	return
}

func (r *Repository) readOwnNeonProfile(ctx context.Context, uid uint64) (out neonOwnProfile, err error) {
	out.EloHistory = []neonEloPoint{}
	out.Vulnerabilities = []neonWeakness{}
	err = r.transaction(ctx, true, func(tx *gorm.DB) error {
		row, err := readNeonProfileRow(tx, uid)
		if err != nil {
			return err
		}
		out.neonPublicProfile, out.Games, out.Wins = row.publicDTO(), row.Games, row.Wins
		if err := tx.Raw(`
SELECT elo, prior_elo, created_at FROM (
    SELECT elo_after AS elo, elo_before AS prior_elo, created_at, CONCAT('d:', LPAD(debate_id, 20, '0')) AS event_id
    FROM neon_elo_entries WHERE user_id = ?
    UNION ALL
    SELECT elo_after, elo_after - 5, rewarded_at, CONCAT('q:', question_id)
    FROM neon_dojo_attempts WHERE user_id = ? AND rewarded_at IS NOT NULL
) AS events ORDER BY created_at DESC, event_id DESC LIMIT 60`, uid, uid).Scan(&out.EloHistory).Error; err != nil {
			return err
		}
		slices.Reverse(out.EloHistory)
		if len(out.EloHistory) > 0 {
			first := out.EloHistory[0]
			out.EloHistory = append([]neonEloPoint{{Elo: first.Before, CreatedAt: first.CreatedAt.UTC()}}, out.EloHistory...)
		}
		for i := range out.EloHistory {
			out.EloHistory[i].CreatedAt = out.EloHistory[i].CreatedAt.UTC()
		}
		return tx.Raw(`
SELECT j.label, COUNT(*) AS count
FROM (
    SELECT a.content_json FROM turn_annotations AS a JOIN debates AS d ON d.id = a.debate_id
    WHERE d.user_id = ? AND a.status = 'completed'
    UNION ALL
    SELECT a.content_json FROM training_annotations AS a JOIN debates AS d ON d.id = a.debate_id
    WHERE d.user_id = ? AND a.status = 'completed'
) AS a
JOIN JSON_TABLE(
    IF(JSON_VALID(a.content_json), a.content_json, '{"items":[]}'), '$.items[*]'
    COLUMNS(label VARCHAR(80) PATH '$.label', category VARCHAR(40) PATH '$.category', speaker VARCHAR(8) PATH '$.role')
) AS j ON j.speaker = 'user' AND j.category IN ('possible_logic_gap', 'insufficient_support', 'unclear_scope')
WHERE j.label IS NOT NULL AND j.label <> ''
GROUP BY j.label ORDER BY count DESC, j.label LIMIT 5`, uid, uid).Scan(&out.Vulnerabilities).Error
	})
	return
}

func (r *Repository) updateNeonProfile(ctx context.Context, uid uint64, in NeonProfileFields) (NeonProfileFields, error) {
	profile, err := normalizeNeonProfile(in)
	if err != nil {
		return NeonProfileFields{}, err
	}
	err = r.transaction(ctx, false, func(tx *gorm.DB) error {
		row := neonStoredProfile{UserID: uid, NeonProfileFields: profile}
		return tx.Clauses(clause.OnConflict{DoUpdates: clause.AssignmentColumns([]string{
			"nickname", "avatar_kind", "avatar_seal", "avatar_image",
		})}).Create(&row).Error
	})
	return profile, err
}

func (r *Repository) readNeonLeaderboard(ctx context.Context, page int) (out neonLeaderboard, err error) {
	const size = 10
	out = neonLeaderboard{Items: []neonLeaderboardEntry{}, Page: page, PageSize: size}
	err = r.transaction(ctx, true, func(tx *gorm.DB) error {
		if err := tx.Table("users").Count(&out.Total).Error; err != nil {
			return err
		}
		offset := (page - 1) * size
		if int64(offset) >= out.Total {
			return nil
		}
		var rows []neonProfileRow
		if err := neonProfileRows(tx).Order("elo DESC, " + neonProfileRateOrder + " DESC, u.id ASC").Limit(size).Offset(offset).Scan(&rows).Error; err != nil {
			return err
		}
		for index, row := range rows {
			out.Items = append(out.Items, neonLeaderboardEntry{formatID(row.UserID), int64(offset + index + 1), row.publicDTO()})
		}
		return nil
	})
	return
}

func (r *Repository) RegisterNeonProfileRoutes(group *gin.RouterGroup, a *auth.Handler) {
	profileError := func(c *gin.Context, err error) {
		if errors.Is(err, ErrInvalidRequest) || errors.Is(err, ErrNotFound) {
			serviceError(c, err)
			return
		}
		serviceError(c, dependency(err, false))
	}
	group.GET("/profile", a.RequireAuth(), func(c *gin.Context) {
		current, _ := auth.CurrentUserFrom(c)
		out, err := r.readOwnNeonProfile(c.Request.Context(), current.User.ID)
		if err != nil {
			profileError(c, err)
			return
		}
		writeData(c, http.StatusOK, out)
	})
	group.POST("/profile", a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), func(c *gin.Context) {
		current, _ := auth.CurrentUserFrom(c)
		var in NeonProfileFields
		if !decode(c, &in) {
			return
		}
		out, err := r.updateNeonProfile(c.Request.Context(), current.User.ID, in)
		if err != nil {
			profileError(c, err)
			return
		}
		writeData(c, http.StatusOK, out)
	})
	group.GET("/leaderboard", a.RequireAuth(), func(c *gin.Context) {
		values := c.Request.URL.Query()
		page, _, err := ParsePagination(values["page"], []string{"10"})
		if err != nil {
			profileError(c, err)
			return
		}
		out, err := r.readNeonLeaderboard(c.Request.Context(), page)
		if err != nil {
			profileError(c, err)
			return
		}
		writeData(c, http.StatusOK, out)
	})
	group.GET("/public/profiles/:user_id", func(c *gin.Context) {
		uid, err := ParseID(c.Param("user_id"))
		if err != nil {
			profileError(c, err)
			return
		}
		var out neonProfileRow
		err = r.transaction(c.Request.Context(), true, func(tx *gorm.DB) error {
			var err error
			out, err = readNeonProfileRow(tx, uid)
			return err
		})
		if err != nil {
			profileError(c, err)
			return
		}
		writeData(c, http.StatusOK, out.publicDTO())
	})
}
