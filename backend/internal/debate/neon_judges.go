package debate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/bingqilin456/DebateForge/backend/internal/auth"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type neonJudgeRow struct {
	ID, UserID uint64
	Name, Role string
	Public     bool
}

func (neonJudgeRow) TableName() string { return "neon_judges" }

type neonJudgeDTO struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	Public   bool   `json:"public"`
	Owned    bool   `json:"owned"`
	Official bool   `json:"official"`
}

var neonOfficialJudges = []neonJudgeDTO{
	{ID: "preset-logic", Name: "逻辑主审", Role: "逻辑严密性与前提拆解", Public: true, Official: true},
	{ID: "preset-evidence", Name: "证据裁判", Role: "论据充分度与事实边界", Public: true, Official: true},
	{ID: "preset-value", Name: "价值裁判", Role: "交锋回应与价值升华", Public: true, Official: true},
}

func (row neonJudgeRow) dto(uid uint64) neonJudgeDTO {
	return neonJudgeDTO{ID: formatID(row.ID), Name: row.Name, Role: row.Role, Public: row.Public, Owned: uid == row.UserID}
}

func (s *Service) neonDB(ctx context.Context) (*gorm.DB, error) {
	repo, ok := s.store.(*Repository)
	if !ok || repo.db == nil {
		return nil, dependency(nil, false)
	}
	return repo.db.WithContext(ctx), nil
}

func (s *Service) resolveNeonJudge(ctx context.Context, uid uint64, id string) (neonJudgeDTO, error) {
	for _, judge := range neonOfficialJudges {
		if judge.ID == id {
			return judge, nil
		}
	}
	number, err := ParseID(id)
	if err != nil {
		return neonJudgeDTO{}, ErrNotFound
	}
	db, err := s.neonDB(ctx)
	if err != nil {
		return neonJudgeDTO{}, err
	}
	var row neonJudgeRow
	err = db.Where("id = ? AND (user_id = ? OR `public` = TRUE)", number, uid).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return neonJudgeDTO{}, ErrNotFound
	}
	if err != nil {
		return neonJudgeDTO{}, dependency(err, false)
	}
	return row.dto(uid), nil
}

func (h *Handler) registerNeonJudgeRoutes(group *gin.RouterGroup, a *auth.Handler) {
	catalog := group.Group("/judges")
	catalog.GET("", a.RequireAuth(), h.listNeonJudges)
	catalog.GET("/models", a.RequireAuth(), h.listNeonJudgeModels)
	catalog.GET("/:id", a.RequireAuth(), func(c *gin.Context) {
		current, _ := auth.CurrentUserFrom(c)
		judge, err := h.service.resolveNeonJudge(c.Request.Context(), current.User.ID, c.Param("id"))
		if err != nil {
			serviceError(c, err)
			return
		}
		writeData(c, 200, judge)
	})
	catalog.POST("", a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), h.createNeonJudge)
	catalog.POST("/:id/visibility", a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), h.setNeonJudgeVisibility)
	catalog.POST("/models", a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), h.addNeonJudgeModel)
	catalog.POST("/models/:id/remove", a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), h.removeNeonJudgeModel)
}

func (h *Handler) listNeonJudges(c *gin.Context) {
	current, _ := auth.CurrentUserFrom(c)
	db, err := h.service.neonDB(c.Request.Context())
	if err != nil {
		serviceError(c, err)
		return
	}
	var rows []neonJudgeRow
	if err = db.Where("user_id = ? OR `public` = TRUE", current.User.ID).Order("id").Find(&rows).Error; err != nil {
		serviceError(c, dependency(err, false))
		return
	}
	items := append([]neonJudgeDTO(nil), neonOfficialJudges...)
	for _, row := range rows {
		items = append(items, row.dto(current.User.ID))
	}
	writeData(c, 200, gin.H{"items": items})
}

func (h *Handler) createNeonJudge(c *gin.Context) {
	var in struct {
		Name string `json:"name"`
		Role string `json:"role"`
	}
	if !decode(c, &in) {
		return
	}
	in.Name, in.Role = strings.TrimSpace(in.Name), strings.TrimSpace(in.Role)
	if in.Name == "" || in.Role == "" || utf8.RuneCountInString(in.Name) > 80 || utf8.RuneCountInString(in.Role) > 500 {
		serviceError(c, ErrInvalidRequest)
		return
	}
	current, _ := auth.CurrentUserFrom(c)
	db, err := h.service.neonDB(c.Request.Context())
	if err != nil {
		serviceError(c, err)
		return
	}
	row := neonJudgeRow{UserID: current.User.ID, Name: in.Name, Role: in.Role}
	if err = db.Create(&row).Error; err != nil {
		serviceError(c, dependency(err, false))
		return
	}
	writeData(c, 201, row.dto(current.User.ID))
}

func (h *Handler) setNeonJudgeVisibility(c *gin.Context) {
	var in struct {
		Public *bool `json:"public"`
	}
	if !decode(c, &in) {
		return
	}
	if in.Public == nil {
		serviceError(c, ErrInvalidRequest)
		return
	}
	id, err := ParseID(c.Param("id"))
	if err != nil {
		serviceError(c, ErrNotFound)
		return
	}
	current, _ := auth.CurrentUserFrom(c)
	db, err := h.service.neonDB(c.Request.Context())
	if err != nil {
		serviceError(c, err)
		return
	}
	owned := db.Where("id = ? AND user_id = ?", id, current.User.ID)
	var row neonJudgeRow
	if err = owned.Take(&row).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		serviceError(c, ErrNotFound)
		return
	}
	if err != nil {
		serviceError(c, dependency(err, false))
		return
	}
	if err = owned.Model(&neonJudgeRow{}).UpdateColumn("public", *in.Public).Error; err != nil {
		serviceError(c, dependency(err, false))
		return
	}
	row.Public = *in.Public
	writeData(c, 200, row.dto(current.User.ID))
}

type neonJudgeModelRow struct {
	ID, UserID                      uint64
	Ciphertext                      []byte
	ExpiresAt                       time.Time
	Provider, BaseURL, Model, Alias string
}

func (neonJudgeModelRow) TableName() string { return "neon_judge_models" }

type neonJudgeModelDTO struct {
	ID     string `json:"id"`
	Alias  string `json:"alias"`
	Source string `json:"source"`
}

func (row neonJudgeModelRow) dto() neonJudgeModelDTO {
	return neonJudgeModelDTO{formatID(row.ID), row.Alias, "ai"}
}

func neonCurrentJudgeModelID(ciphertext []byte) string {
	digest := sha256.Sum256(ciphertext)
	return "current-" + hex.EncodeToString(digest[:16])
}

func (p *PersonalModel) judgeModelRow(ctx context.Context, uid uint64, id string) (neonJudgeModelRow, error) {
	if id == "current" || strings.HasPrefix(id, "current-") {
		var row personalModelRow
		err := p.db.WithContext(ctx).Where("user_id = ? AND expires_at > ?", uid, time.Now().UTC()).Take(&row).Error
		if err == nil && id != "current" && neonCurrentJudgeModelID(row.Ciphertext) != id {
			return neonJudgeModelRow{}, gorm.ErrRecordNotFound
		}
		return neonJudgeModelRow{UserID: uid, Ciphertext: row.Ciphertext, ExpiresAt: row.ExpiresAt, Provider: row.Provider, BaseURL: row.BaseURL, Model: row.Model, Alias: row.Alias}, err
	}
	number, err := ParseID(id)
	if err != nil {
		return neonJudgeModelRow{}, gorm.ErrRecordNotFound
	}
	var row neonJudgeModelRow
	err = p.db.WithContext(ctx).Where("id = ? AND user_id = ? AND expires_at > ?", number, uid, time.Now().UTC()).Take(&row).Error
	return row, err
}

var neonDemoJudgeModel = neonJudgeModelDTO{"demo", "本地演示裁判", "local"}

func (s *Service) resolveNeonJudgeModel(ctx context.Context, uid uint64, id string) (neonJudgeModelDTO, error) {
	if id == "demo" {
		return neonDemoJudgeModel, nil
	}
	p, ok := s.generator.(*PersonalModel)
	if !ok {
		return neonJudgeModelDTO{}, ErrNotFound
	}
	row, err := p.judgeModelRow(ctx, uid, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return neonJudgeModelDTO{}, ErrNotFound
	}
	if err != nil {
		return neonJudgeModelDTO{}, dependency(err, false)
	}
	alias := row.Alias
	if alias == "" {
		alias = "个人模型"
	}
	if id == "current" {
		id = neonCurrentJudgeModelID(row.Ciphertext)
	}
	return neonJudgeModelDTO{id, alias, "ai"}, nil
}

func (s *Service) neonJudgeModel(ctx context.Context, uid uint64, id, system, data string) (ai.Result, error) {
	p, ok := s.generator.(*PersonalModel)
	if !ok || id == "demo" || id == "current" {
		return ai.Result{}, ErrNotFound
	}
	row, err := p.judgeModelRow(ctx, uid, id)
	if err != nil {
		return ai.Result{}, ErrNotFound
	}
	key, err := openPersonalKey(p.vault, uid, row.Ciphertext)
	if err != nil {
		return ai.Result{}, &ai.Error{Code: ai.CodeModelUnavailable, Message: "裁判模型配置不可用"}
	}
	adapter, err := p.adapterForConfiguration(ctx, row.BaseURL, row.Model, key)
	if err != nil {
		return ai.Result{}, err
	}
	result, err := adapter.GenerateArenaAssist(ctx, uid, system, data)
	if err == nil && strings.HasPrefix(id, "current-") {
		_ = p.mark(ctx, personalModelRow{UserID: uid, Ciphertext: row.Ciphertext}, "used")
	}
	return result, err
}

func (h *Handler) listNeonJudgeModels(c *gin.Context) {
	items := []neonJudgeModelDTO{neonDemoJudgeModel}
	if p := h.personalModel(); p != nil {
		current, _ := auth.CurrentUserFrom(c)
		uid := current.User.ID
		active, err := h.service.resolveNeonJudgeModel(c.Request.Context(), uid, "current")
		if err == nil {
			items = append(items, active)
		} else if !errors.Is(err, ErrNotFound) {
			serviceError(c, err)
			return
		}
		var rows []neonJudgeModelRow
		if err = p.db.WithContext(c.Request.Context()).Select("id", "alias").Where("user_id = ? AND expires_at > ?", uid, time.Now().UTC()).Order("id").Find(&rows).Error; err != nil {
			serviceError(c, dependency(err, false))
			return
		}
		for _, row := range rows {
			items = append(items, row.dto())
		}
	}
	writeData(c, 200, gin.H{"items": items})
}

func (h *Handler) addNeonJudgeModel(c *gin.Context) {
	p := h.personalModel()
	if p == nil {
		serviceError(c, ErrNotFound)
		return
	}
	var input personalModelInput
	if !decode(c, &input) {
		return
	}
	in, err := normalizePersonalModel(input)
	if err != nil {
		serviceError(c, ErrInvalidRequest)
		return
	}
	current, _ := auth.CurrentUserFrom(c)
	uid := current.User.ID
	row := neonJudgeModelRow{UserID: uid, Ciphertext: sealPersonalKey(p.vault, uid, in.APIKey), ExpiresAt: time.Now().UTC().Add(48 * time.Hour), Provider: in.Provider, BaseURL: in.BaseURL, Model: in.Model, Alias: in.Alias}
	if err = p.db.WithContext(c.Request.Context()).Create(&row).Error; err != nil {
		serviceError(c, dependency(err, false))
		return
	}
	writeData(c, 201, row.dto())
}

func (h *Handler) removeNeonJudgeModel(c *gin.Context) {
	p := h.personalModel()
	if p == nil {
		serviceError(c, ErrNotFound)
		return
	}
	id, err := ParseID(c.Param("id"))
	if err != nil {
		serviceError(c, ErrNotFound)
		return
	}
	current, _ := auth.CurrentUserFrom(c)
	result := p.db.WithContext(c.Request.Context()).Where("id = ? AND user_id = ?", id, current.User.ID).Delete(&neonJudgeModelRow{})
	if result.Error != nil {
		serviceError(c, dependency(result.Error, false))
		return
	}
	if result.RowsAffected == 0 {
		serviceError(c, ErrNotFound)
		return
	}
	writeData(c, 200, gin.H{"ok": true})
}
