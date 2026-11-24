package debate

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/bingqilin456/DebateForge/backend/internal/auth"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type neonTopicRow struct {
	ID, UserID                  uint64
	Title, Background, Category string
	IsPublic                    bool
}

func (neonTopicRow) TableName() string { return "neon_topics" }

type neonOpponentRow struct {
	ID, UserID                     uint64
	Name, Tag, Difficulty, Persona string
	IsPublic                       bool
}

func (neonOpponentRow) TableName() string { return "neon_opponents" }

type neonTopicDraft struct {
	Title      string `json:"title"`
	Background string `json:"background"`
	Category   string `json:"category"`
}

type neonOpponentDraft struct {
	Name       string `json:"name"`
	Tag        string `json:"tag"`
	Difficulty string `json:"difficulty"`
	Persona    string `json:"persona"`
}

type neonVisibility struct {
	Public *bool `json:"public"`
}

type neonCatalogVisibility struct {
	Public   bool `json:"public"`
	Owned    bool `json:"owned"`
	Official bool `json:"official"`
	Heat     int  `json:"heat"`
}

type neonTopicDTO struct {
	ID string `json:"id"`
	neonTopicDraft
	neonCatalogVisibility
}

type neonOpponentDTO struct {
	ID    string `json:"id"`
	Style string `json:"style,omitempty"`
	neonOpponentDraft
	neonCatalogVisibility
}

var neonCatalogCategories = []string{"technology", "campus", "society", "philosophy"}

var neonOfficialOpponents = []neonOpponentDTO{
	{ID: "logical", Style: "logical", neonOpponentDraft: neonOpponentDraft{Name: "苏格拉底·诘问型", Tag: "逻辑诘问", Difficulty: "beginner", Persona: "逐层追问概念与前提，拆解推理中的跳跃。"}, neonCatalogVisibility: neonCatalogVisibility{Public: true, Official: true}},
	{ID: "data", Style: "data", neonOpponentDraft: neonOpponentDraft{Name: "数据狂魔·实证型", Tag: "事实举证", Difficulty: "varsity", Persona: "追问证据质量，区分相关性与因果关系。"}, neonCatalogVisibility: neonCatalogVisibility{Public: true, Official: true}},
	{ID: "aggressive", Style: "aggressive", neonOpponentDraft: neonOpponentDraft{Name: "凌厉国辩·压迫型", Tag: "主动进攻", Difficulty: "national", Persona: "直击对方论点中影响结论的争议，快速展开攻防。"}, neonCatalogVisibility: neonCatalogVisibility{Public: true, Official: true}},
}

func (row neonTopicRow) dto(uid uint64, heat int) neonTopicDTO {
	return neonTopicDTO{ID: formatID(row.ID), neonTopicDraft: neonTopicDraft{row.Title, row.Background, row.Category},
		neonCatalogVisibility: neonCatalogVisibility{Public: row.IsPublic, Owned: uid == row.UserID, Heat: heat}}
}

func (row neonOpponentRow) dto(uid uint64, heat int) neonOpponentDTO {
	return neonOpponentDTO{ID: formatID(row.ID), neonOpponentDraft: neonOpponentDraft{row.Name, row.Tag, row.Difficulty, row.Persona},
		neonCatalogVisibility: neonCatalogVisibility{Public: row.IsPublic, Owned: uid == row.UserID, Heat: heat}}
}

func (draft neonTopicDraft) valid() bool {
	_, err := ValidateCreateInput(CreateInput{CustomTopic: &CustomTopic{Title: draft.Title, Background: draft.Background},
		UserStance: "pro", AIStyle: string(ai.StyleLogical)})
	return err == nil && slices.Contains(neonCatalogCategories, draft.Category)
}

func (draft neonOpponentDraft) valid() bool {
	return validNeonCatalogText(draft.Name, 40) && validNeonCatalogText(draft.Tag, 30) &&
		validNeonCatalogText(draft.Persona, 1000) && ai.ValidDifficulty(draft.Difficulty)
}

func validNeonCatalogText(text string, limit int) bool {
	return utf8.ValidString(text) && utf8.RuneCountInString(text) <= limit && strings.TrimFunc(text, isBlankRune) != ""
}

func readNeonCatalogEntry(tx *gorm.DB, uid, id uint64, ownerOnly bool, row any) error {
	query := tx.Where("id = ?", id)
	if ownerOnly {
		query = query.Where("user_id = ?", uid).Clauses(clause.Locking{Strength: "UPDATE"})
	} else {
		// 与可见性、题面或人设修改串行化，开赛快照只读取当时可用的一整版内容。
		query = query.Where("(user_id = ? OR is_public = TRUE)", uid).Clauses(clause.Locking{Strength: "SHARE"})
	}
	return neonCatalogStoreError(query.Take(row).Error)
}

func neonCatalogStoreError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return dependency(err, false)
	}
	return nil
}

func (h *Handler) registerNeonCatalogRoutes(group *gin.RouterGroup, a *auth.Handler) {
	catalog := group.Group("/neon-catalog")
	catalog.GET("/topics", a.RequireAuth(), h.listNeonTopics)
	catalog.GET("/opponents", a.RequireAuth(), h.listNeonOpponents)
	for _, target := range []struct {
		path       string
		save       gin.HandlerFunc
		visibility gin.HandlerFunc
	}{
		{"/topics", h.saveNeonTopic, h.neonTopicVisibility},
		{"/opponents", h.saveNeonOpponent, h.neonOpponentVisibility},
	} {
		catalog.POST(target.path, a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), target.save)
		catalog.POST(target.path+"/:id", a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), target.save)
		catalog.POST(target.path+"/:id/visibility", a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), target.visibility)
	}
}

func (s *Service) listNeonTopics(ctx context.Context, uid uint64) ([]neonTopicDTO, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	db, err := s.neonDB(ctx)
	if err != nil {
		return nil, err
	}
	var preset []struct {
		ID                               uint64
		Title, Description, NeonCategory string
		Heat                             int
	}
	// 仅完成首轮的不同账号计入热度。同账号重复开局、只选题、观摩或刷新都不增加热度。
	err = db.Raw(`SELECT t.id, t.title, t.description, t.neon_category,
		(SELECT COUNT(DISTINCT d.user_id) FROM debates d
		 WHERE d.topic_id=t.id AND d.completed_rounds>0) AS heat
		FROM topics t WHERE t.enabled=TRUE ORDER BY t.id`).Scan(&preset).Error
	if err != nil {
		return nil, dependency(err, false)
	}
	var custom []struct {
		Row  neonTopicRow `gorm:"embedded"`
		Heat int
	}
	err = db.Raw(`SELECT t.*,
		(SELECT COUNT(DISTINCT d.user_id) FROM debates d
		 WHERE d.neon_topic_id=t.id AND d.completed_rounds>0) AS heat
		FROM neon_topics t WHERE t.user_id=? OR t.is_public=TRUE ORDER BY t.id`, uid).Scan(&custom).Error
	if err != nil {
		return nil, dependency(err, false)
	}
	items := make([]neonTopicDTO, 0, len(preset)+len(custom))
	for _, row := range preset {
		items = append(items, neonTopicDTO{ID: formatID(row.ID), neonTopicDraft: neonTopicDraft{row.Title, row.Description, row.NeonCategory},
			neonCatalogVisibility: neonCatalogVisibility{Public: true, Official: true, Heat: row.Heat}})
	}
	for _, row := range custom {
		items = append(items, row.Row.dto(uid, row.Heat))
	}
	return items, nil
}

func (s *Service) listNeonOpponents(ctx context.Context, uid uint64) ([]neonOpponentDTO, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	db, err := s.neonDB(ctx)
	if err != nil {
		return nil, err
	}
	var officialHeat []struct {
		AIStyle string
		Heat    int
	}
	err = db.Raw(`SELECT ai_style, COUNT(DISTINCT user_id) AS heat FROM debates
		WHERE neon_opponent_id IS NULL AND completed_rounds>0 GROUP BY ai_style`).Scan(&officialHeat).Error
	if err != nil {
		return nil, dependency(err, false)
	}
	var rows []struct {
		Row  neonOpponentRow `gorm:"embedded"`
		Heat int
	}
	err = db.Raw(`SELECT o.*,
		(SELECT COUNT(DISTINCT d.user_id) FROM debates d
		 WHERE d.neon_opponent_id=o.id AND d.completed_rounds>0) AS heat
		FROM neon_opponents o WHERE o.user_id=? OR o.is_public=TRUE ORDER BY o.id`, uid).Scan(&rows).Error
	if err != nil {
		return nil, dependency(err, false)
	}
	items := slices.Clone(neonOfficialOpponents)
	for _, entry := range officialHeat {
		for i := range items {
			if items[i].Style == entry.AIStyle {
				items[i].Heat = entry.Heat
			}
		}
	}
	for _, row := range rows {
		items = append(items, row.Row.dto(uid, row.Heat))
	}
	return items, nil
}

func (h *Handler) listNeonTopics(c *gin.Context) {
	current, _ := auth.CurrentUserFrom(c)
	items, err := h.service.listNeonTopics(c.Request.Context(), current.User.ID)
	if err != nil {
		serviceError(c, err)
		return
	}
	writeData(c, 200, gin.H{"items": items})
}

func (h *Handler) listNeonOpponents(c *gin.Context) {
	current, _ := auth.CurrentUserFrom(c)
	items, err := h.service.listNeonOpponents(c.Request.Context(), current.User.ID)
	if err != nil {
		serviceError(c, err)
		return
	}
	writeData(c, 200, gin.H{"items": items})
}

func (s *Service) changeNeonCatalog(ctx context.Context, uid, id uint64, row any, values map[string]any) error {
	repo, ok := s.store.(*Repository)
	if !ok || repo.db == nil {
		return dependency(nil, false)
	}
	return repo.transaction(ctx, false, func(tx *gorm.DB) error {
		if id == 0 {
			return neonCatalogStoreError(tx.Create(row).Error)
		}
		if err := readNeonCatalogEntry(tx, uid, id, true, row); err != nil {
			return err
		}
		if err := tx.Model(row).Updates(values).Error; err != nil {
			return dependency(err, false)
		}
		return neonCatalogStoreError(tx.Take(row, id).Error)
	})
}

func readNeonCatalogID(c *gin.Context) (uint64, bool) {
	if c.Param("id") == "" {
		return 0, true
	}
	id, err := ParseID(c.Param("id"))
	if err != nil {
		serviceError(c, ErrInvalidRequest)
		return 0, false
	}
	return id, true
}

func (h *Handler) saveNeonTopic(c *gin.Context) {
	id, valid := readNeonCatalogID(c)
	var draft neonTopicDraft
	if !valid || !decode(c, &draft) {
		return
	}
	if !draft.valid() {
		serviceError(c, ErrInvalidRequest)
		return
	}
	current, _ := auth.CurrentUserFrom(c)
	uid := current.User.ID
	row := neonTopicRow{UserID: uid, Title: draft.Title, Background: draft.Background, Category: draft.Category}
	err := h.service.changeNeonCatalog(c.Request.Context(), uid, id, &row, map[string]any{
		"title": draft.Title, "background": draft.Background, "category": draft.Category,
	})
	if err != nil {
		serviceError(c, err)
		return
	}
	writeData(c, neonCatalogStatus(id), row.dto(uid, 0))
}

func (h *Handler) saveNeonOpponent(c *gin.Context) {
	id, valid := readNeonCatalogID(c)
	var draft neonOpponentDraft
	if !valid || !decode(c, &draft) {
		return
	}
	if !draft.valid() {
		serviceError(c, ErrInvalidRequest)
		return
	}
	current, _ := auth.CurrentUserFrom(c)
	uid := current.User.ID
	row := neonOpponentRow{UserID: uid, Name: draft.Name, Tag: draft.Tag, Difficulty: draft.Difficulty, Persona: draft.Persona}
	err := h.service.changeNeonCatalog(c.Request.Context(), uid, id, &row, map[string]any{
		"name": draft.Name, "tag": draft.Tag, "difficulty": draft.Difficulty, "persona": draft.Persona,
	})
	if err != nil {
		serviceError(c, err)
		return
	}
	writeData(c, neonCatalogStatus(id), row.dto(uid, 0))
}

func neonCatalogStatus(id uint64) int {
	if id == 0 {
		return 201
	}
	return 200
}

func (h *Handler) neonTopicVisibility(c *gin.Context) {
	h.changeNeonVisibility(c, &neonTopicRow{})
}

func (h *Handler) neonOpponentVisibility(c *gin.Context) {
	h.changeNeonVisibility(c, &neonOpponentRow{})
}

func (h *Handler) changeNeonVisibility(c *gin.Context, row any) {
	id, valid := readNeonCatalogID(c)
	var body neonVisibility
	if !valid || !decode(c, &body) {
		return
	}
	if body.Public == nil {
		serviceError(c, ErrInvalidRequest)
		return
	}
	current, _ := auth.CurrentUserFrom(c)
	uid := current.User.ID
	err := h.service.changeNeonCatalog(c.Request.Context(), uid, id, row, map[string]any{"is_public": *body.Public})
	if err != nil {
		serviceError(c, err)
		return
	}
	writeData(c, 200, gin.H{"public": *body.Public})
}
