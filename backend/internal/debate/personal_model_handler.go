package debate

import (
	"github.com/bingqilin456/DebateForge/backend/internal/auth"
	"github.com/gin-gonic/gin"
)

func (h *Handler) personalModel() *PersonalModel {
	if h.service == nil {
		return nil
	}
	model, _ := h.service.generator.(*PersonalModel)
	return model
}

func (h *Handler) registerPersonalModelRoutes(group *gin.RouterGroup, a *auth.Handler) {
	settings := group.Group("/settings/model")
	settings.GET("", a.RequireAuth(), func(c *gin.Context) {
		current, _ := auth.CurrentUserFrom(c)
		status, err := h.personalModel().status(c.Request.Context(), current.User.ID)
		if err != nil {
			serviceError(c, err)
			return
		}
		writeData(c, 200, status)
	})
	if h.personalModel() == nil {
		return
	}
	settings.Use(a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF())
	p := h.personalModel()
	settings.POST("", func(c *gin.Context) {
		var in personalModelInput
		if !decode(c, &in) {
			return
		}
		current, _ := auth.CurrentUserFrom(c)
		status, err := p.save(c.Request.Context(), current.User.ID, in)
		if err != nil {
			serviceError(c, err)
			return
		}
		writeData(c, 200, status)
	})
	settings.POST("/remove", func(c *gin.Context) {
		current, _ := auth.CurrentUserFrom(c)
		if err := p.remove(c.Request.Context(), current.User.ID); err != nil {
			serviceError(c, err)
			return
		}
		writeData(c, 200, gin.H{"ok": true})
	})
	settings.POST("/test", func(c *gin.Context) {
		current, _ := auth.CurrentUserFrom(c)
		if err := h.service.gate.Request(current.User.ID); err != nil {
			serviceError(c, err)
			return
		}
		if err := p.testConnection(c.Request.Context(), current.User.ID); err != nil {
			serviceError(c, err)
			return
		}
		writeData(c, 200, gin.H{"ok": true})
	})
}
