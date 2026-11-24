package debate

import (
	"github.com/bingqilin456/DebateForge/backend/internal/auth"
	"github.com/gin-gonic/gin"
)

// 争点卡的 HTTP 边界，按独立模块提供注册入口。
//
// 与 V2-04 批注一致：读取只要 Session；整理与人工编辑额外要求 Origin/CSRF。
// 本文件不修改共享 router/server 装配——正式挂载点由整合负责人加入
// （见 docs/planning/v2-parallel/02-issue-cards.md 交接段）：
//   - 在 platform/business.go 的 NewBusinessRouter 里装配 IssueCardHandler；
//   - 在 platform/business.go 的 requestBudget 中把
//     POST /api/v1/debates/:id/issue-cards/analyze 加入长预算路径。
//
// 两条路由都不占用正式发言的 Gate：分析走争点卡自己的单槽，编辑只写卡片表。

// IssueCardHandler 只暴露争点卡的读取、赛后整理与人工编辑。
type IssueCardHandler struct{ service *IssueCardService }

// NewIssueCardHandler 允许 service 为空：此时路由仍注册，但请求返回受控 503，
// 与 topic 的处理方式一致，避免装配期空指针。
func NewIssueCardHandler(service *IssueCardService) *IssueCardHandler {
	return &IssueCardHandler{service: service}
}

// RegisterIssueCardRoutes 由整合方在共享 router 装配处调用；不改动既有路由。
func (h *IssueCardHandler) RegisterIssueCardRoutes(group *gin.RouterGroup, a *auth.Handler) {
	// 没有认证依赖时不注册任何入口，避免遗漏 Origin/CSRF。
	if a == nil {
		return
	}
	group.GET("/debates/:id/issue-cards", a.RequireAuth(), h.list)
	group.POST("/debates/:id/issue-cards/analyze",
		a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), h.analyze)
	group.POST("/debates/:id/issue-cards/:card_id/edit",
		a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), h.edit)
}

func (h *IssueCardHandler) list(c *gin.Context) {
	id, err := ParseID(c.Param("id"))
	if err != nil {
		serviceError(c, err)
		return
	}
	current, _ := auth.CurrentUserFrom(c)
	if h.service == nil {
		writeError(c, 503, "dependency_unavailable", nil)
		return
	}
	data, err := h.service.Get(c.Request.Context(), current.User.ID, id)
	if err != nil {
		serviceError(c, issueCardHTTPError(err))
		return
	}
	writeData(c, 200, data)
}

func (h *IssueCardHandler) analyze(c *gin.Context) {
	id, err := ParseID(c.Param("id"))
	if err != nil {
		serviceError(c, err)
		return
	}
	var in IssueCardAnalyzeInput
	if !decode(c, &in) {
		return
	}
	current, _ := auth.CurrentUserFrom(c)
	if h.service == nil {
		writeError(c, 503, "dependency_unavailable", nil)
		return
	}
	data, status, err := h.service.Analyze(c.Request.Context(), current.User.ID, id, in)
	if err != nil {
		serviceError(c, issueCardHTTPError(err))
		return
	}
	writeData(c, status, data)
}

func (h *IssueCardHandler) edit(c *gin.Context) {
	id, err := ParseID(c.Param("id"))
	if err != nil {
		serviceError(c, err)
		return
	}
	cardID := c.Param("card_id")
	if _, err := ParseID(cardID); err != nil {
		serviceError(c, err)
		return
	}
	var in IssueCardEditInput
	if !decode(c, &in) {
		return
	}
	current, _ := auth.CurrentUserFrom(c)
	if h.service == nil {
		writeError(c, 503, "dependency_unavailable", nil)
		return
	}
	data, status, err := h.service.Edit(c.Request.Context(), current.User.ID, id, cardID, in)
	if err != nil {
		serviceError(c, issueCardHTTPError(err))
		return
	}
	writeData(c, status, data)
}
