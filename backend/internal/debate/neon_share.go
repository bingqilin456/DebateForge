package debate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/bingqilin456/DebateForge/backend/internal/auth"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type neonShareRow struct {
	DebateID    uint64 `gorm:"primaryKey"`
	OwnerID     uint64
	TopicKey    *string
	ShareID     *string
	Title       string
	Report      *string
	PublishedAt *time.Time
}

func (neonShareRow) TableName() string { return "neon_public_reports" }

type neonShareStatus struct {
	PromptHandled bool    `json:"prompt_handled"`
	ShowPrompt    bool    `json:"show_prompt"`
	ShareID       *string `json:"share_id"`
}

type neonPublicReportSummary struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	PublishedAt time.Time `json:"published_at"`
}

type neonReportSpeech struct {
	Side    string `json:"side"`
	Content string `json:"content"`
}

type neonReportRound struct {
	Number   int                `json:"number"`
	Speeches []neonReportSpeech `json:"speeches"`
}

type neonReportAnnotation struct {
	Round      int    `json:"round"`
	Side       string `json:"side"`
	Label      string `json:"label"`
	Quote      string `json:"quote"`
	Reason     string `json:"reason"`
	Suggestion string `json:"suggestion"`
}

// 发布只保存经过筛选的复盘快照。临时变量、私有模型与账户字段不会随详情一起公开。
type neonPublicReport struct {
	Title         string                 `json:"title"`
	UserStance    string                 `json:"user_stance"`
	AIStance      string                 `json:"ai_stance"`
	Rounds        []neonReportRound      `json:"rounds"`
	Annotations   []neonReportAnnotation `json:"annotations"`
	Feedback      []string               `json:"feedback"`
	JudgeComments []string               `json:"judge_comments"`
	Score         *neonVerdictScore      `json:"score,omitempty"`
}

func newNeonReport(detail Detail, review *ai.Review, summary *ai.Summary, verdict neonVerdict, notes []AnnotationItemDTO) neonPublicReport {
	d := detail.Debate
	report := neonPublicReport{
		Title: d.TopicTitleSnapshot, UserStance: d.UserStance, AIStance: d.DTO().AIStance,
		Rounds: []neonReportRound{}, Annotations: []neonReportAnnotation{},
		Feedback: []string{}, JudgeComments: []string{}, Score: verdict.Score,
	}
	for _, turn := range detail.Turns {
		if turn.Status != "completed" {
			continue
		}
		round := neonReportRound{Number: turn.RoundNo, Speeches: []neonReportSpeech{}}
		for _, message := range turn.Messages {
			if message.Role == "user" || message.Role == "assistant" {
				round.Speeches = append(round.Speeches, neonReportSpeech{message.Role, message.Content})
			}
		}
		report.Rounds = append(report.Rounds, round)
	}
	if review != nil {
		for _, group := range [][]ai.ReviewText{review.KeyIssues, review.Strengths, review.Improvements} {
			for _, entry := range group {
				report.Feedback = append(report.Feedback, entry.Text)
			}
		}
	}
	if summary != nil {
		for _, group := range [][]ai.SummaryPoint{summary.Strengths, summary.Issues} {
			for _, entry := range group {
				report.Feedback = append(report.Feedback, entry.Point+" · "+entry.Reason)
			}
		}
		for _, entry := range summary.Suggestions {
			report.Feedback = append(report.Feedback, entry.Action+" · "+entry.Example)
		}
	}
	for _, seat := range verdict.Seats {
		if seat.Status == "completed" && seat.Reason != "" {
			report.JudgeComments = append(report.JudgeComments, seat.Reason)
		}
	}
	for _, note := range notes {
		report.Annotations = append(report.Annotations, neonReportAnnotation{
			Round: note.TurnNo, Side: note.Role, Label: note.Label, Quote: note.Anchor.Quote,
			Reason: note.Reason, Suggestion: note.Improvement,
		})
	}
	return report
}

func readNeonReport(tx *gorm.DB, d Debate) (neonPublicReport, error) {
	detail, err := readTurns(tx, d, "")
	if err != nil {
		return neonPublicReport{}, err
	}
	review, err := readReview(tx, d.ID, false)
	if err != nil {
		return neonPublicReport{}, err
	}
	summary, err := readSummary(tx, d.ID, false)
	if err != nil {
		return neonPublicReport{}, err
	}
	verdict, err := loadNeonVerdict(tx, d.ID)
	if err != nil {
		return neonPublicReport{}, dependency(err, false)
	}
	var rows []TurnAnnotation
	if err := tx.Where("debate_id = ? AND status = ?", d.ID, AnnotationCompleted).Order("turn_id ASC").Find(&rows).Error; err != nil {
		return neonPublicReport{}, dependency(err, false)
	}
	var notes []AnnotationItemDTO
	for _, row := range rows {
		items, valid := decodeStoredAnnotations(row.ContentJSON)
		if valid {
			notes = append(notes, items...)
		}
	}
	return newNeonReport(detail, review.DTO().Content, summary.DTO().Content, verdict, notes), nil
}

func getNeonShareRow(tx *gorm.DB, id uint64) (neonShareRow, error) {
	var row neonShareRow
	err := tx.Take(&row, "debate_id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return neonShareRow{}, nil
	}
	if err != nil {
		return neonShareRow{}, dependency(err, false)
	}
	return row, nil
}

func neonCompletedReport(d Debate) error {
	if d.IsTraining() || d.Status != "completed" {
		return ErrRoundConflict
	}
	return nil
}

func (r *Repository) neonShareStatus(ctx context.Context, uid, id uint64) (out neonShareStatus, err error) {
	err = r.transaction(ctx, true, func(tx *gorm.DB) error {
		d, err := owned(tx, uid, id, false)
		if err != nil {
			return err
		}
		if err := neonCompletedReport(d); err != nil {
			return err
		}
		row, err := getNeonShareRow(tx, id)
		out = neonShareStatus{PromptHandled: row.DebateID != 0, ShareID: row.ShareID}
		return err
	})
	return
}

func (r *Repository) changeNeonShare(ctx context.Context, uid, id uint64, action string) (out neonShareStatus, err error) {
	err = r.transaction(ctx, false, func(tx *gorm.DB) error {
		// 同一场的询问、发布和撤回都串行化，避免重复链接和重复提醒。
		d, err := owned(tx, uid, id, true)
		if err != nil {
			return err
		}
		if err := neonCompletedReport(d); err != nil {
			return err
		}
		row, err := getNeonShareRow(tx, id)
		if err != nil {
			return err
		}
		first := row.DebateID == 0
		if first {
			row = neonShareRow{DebateID: id, OwnerID: uid, Title: d.TopicTitleSnapshot}
		}
		switch action {
		case "prompt":
			out.ShowPrompt = first
		case "publish":
			if row.ShareID == nil {
				report, err := readNeonReport(tx, d)
				if err != nil {
					return err
				}
				content, err := json.Marshal(report)
				if err != nil {
					return dependency(err, false)
				}
				row.TopicKey = nil
				if d.NeonTopicID != nil {
					key := "custom-" + formatID(*d.NeonTopicID)
					row.TopicKey = &key
				} else if d.TopicID != nil {
					key := formatID(*d.TopicID)
					row.TopicKey = &key
				}
				shareID, now, snapshot := uuid.NewString(), r.now(), string(content)
				row.ShareID, row.PublishedAt, row.Report = &shareID, &now, &snapshot
			}
		case "withdraw":
			row.ShareID, row.PublishedAt, row.Report = nil, nil, nil
		default:
			return ErrInvalidRequest
		}
		if first || action != "prompt" {
			if err := tx.Save(&row).Error; err != nil {
				return dependency(err, false)
			}
		}
		out.PromptHandled, out.ShareID = true, row.ShareID
		return nil
	})
	return
}

func (r *Repository) listNeonPublicReports(ctx context.Context, owner uint64, topic string, page, size int) ([]neonPublicReportSummary, int64, error) {
	items := []neonPublicReportSummary{}
	var total int64
	err := r.transaction(ctx, true, func(tx *gorm.DB) error {
		query := tx.Model(&neonShareRow{}).Where("published_at IS NOT NULL")
		if owner != 0 {
			query = query.Where("owner_id = ?", owner)
		}
		if topic != "" {
			query = query.Where("topic_key = ?", topic)
		}
		if err := query.Count(&total).Error; err != nil {
			return dependency(err, false)
		}
		var rows []neonShareRow
		if err := query.Select("share_id", "title", "published_at").Order("published_at DESC, debate_id DESC").
			Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
			return dependency(err, false)
		}
		for _, row := range rows {
			items = append(items, neonPublicReportSummary{*row.ShareID, row.Title, row.PublishedAt.UTC()})
		}
		return nil
	})
	return items, total, err
}

func (r *Repository) getNeonPublicReport(ctx context.Context, id string) (neonPublicReport, error) {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id {
		return neonPublicReport{}, ErrNotFound
	}
	var row neonShareRow
	err = r.db.WithContext(ctx).Select("report").Take(&row, "share_id = ? AND published_at IS NOT NULL", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return neonPublicReport{}, ErrNotFound
	}
	if err != nil {
		return neonPublicReport{}, dependency(err, false)
	}
	var report neonPublicReport
	if row.Report == nil {
		return report, ErrNotFound
	}
	if err := json.Unmarshal([]byte(*row.Report), &report); err != nil {
		return report, dependency(err, false)
	}
	return report, nil
}

type NeonShareHandler struct{ repo *Repository }

func NewNeonShareHandler(repo *Repository) *NeonShareHandler { return &NeonShareHandler{repo: repo} }

func (h *NeonShareHandler) RegisterPublic(api *gin.RouterGroup) {
	api.GET("/public/reports", h.list)
	api.GET("/public/reports/:share_id", h.show)
	api.GET("/public/reports/:share_id/markdown", h.exportPublic)
}

func (h *NeonShareHandler) RegisterProtected(api *gin.RouterGroup, a *auth.Handler) {
	if a == nil {
		return
	}
	api.GET("/debates/:id/sharing", a.RequireAuth(), h.status)
	api.POST("/debates/:id/sharing", a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), h.change)
	api.GET("/debates/:id/sharing/markdown", a.RequireAuth(), h.exportOwner)
}

func neonShareOwner(c *gin.Context) (uint64, uint64, bool) {
	current, _ := auth.CurrentUserFrom(c)
	id, err := ParseID(c.Param("id"))
	if err != nil {
		serviceError(c, err)
		return 0, 0, false
	}
	return current.User.ID, id, true
}

func (h *NeonShareHandler) status(c *gin.Context) {
	uid, id, ok := neonShareOwner(c)
	if ok {
		result, err := h.repo.neonShareStatus(c.Request.Context(), uid, id)
		if err != nil {
			serviceError(c, err)
			return
		}
		writeData(c, http.StatusOK, result)
	}
}

func (h *NeonShareHandler) change(c *gin.Context) {
	uid, id, ok := neonShareOwner(c)
	if !ok {
		return
	}
	var input struct {
		Action string `json:"action"`
	}
	if !decode(c, &input) {
		return
	}
	result, err := h.repo.changeNeonShare(c.Request.Context(), uid, id, input.Action)
	if err != nil {
		serviceError(c, err)
		return
	}
	writeData(c, http.StatusOK, result)
}

func (h *NeonShareHandler) list(c *gin.Context) {
	values := c.Request.URL.Query()
	owner, topic := values.Get("owner_id"), values.Get("topic_id")
	var uid uint64
	var err error
	if (owner == "" && topic == "") || len(values["owner_id"]) > 1 || len(values["topic_id"]) > 1 {
		serviceError(c, ErrInvalidRequest)
		return
	}
	if owner != "" {
		uid, err = ParseID(owner)
		if err != nil {
			serviceError(c, err)
			return
		}
	}
	if topic != "" {
		if _, err := ParseID(strings.TrimPrefix(topic, "custom-")); err != nil {
			serviceError(c, err)
			return
		}
	}
	page, size, err := ParsePagination(values["page"], values["page_size"])
	if err != nil {
		serviceError(c, err)
		return
	}
	items, total, err := h.repo.listNeonPublicReports(c.Request.Context(), uid, topic, page, size)
	if err != nil {
		serviceError(c, err)
		return
	}
	writeData(c, http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}

func (h *NeonShareHandler) show(c *gin.Context) {
	report, err := h.repo.getNeonPublicReport(c.Request.Context(), c.Param("share_id"))
	if err != nil {
		serviceError(c, err)
		return
	}
	writeData(c, http.StatusOK, report)
}

func (h *NeonShareHandler) exportPublic(c *gin.Context) {
	report, err := h.repo.getNeonPublicReport(c.Request.Context(), c.Param("share_id"))
	if err != nil {
		serviceError(c, err)
		return
	}
	writeNeonMarkdown(c, report)
}

func (h *NeonShareHandler) exportOwner(c *gin.Context) {
	uid, id, ok := neonShareOwner(c)
	if !ok {
		return
	}
	var report neonPublicReport
	err := h.repo.transaction(c.Request.Context(), true, func(tx *gorm.DB) error {
		d, err := owned(tx, uid, id, false)
		if err != nil {
			return err
		}
		if err := neonCompletedReport(d); err != nil {
			return err
		}
		report, err = readNeonReport(tx, d)
		return err
	})
	if err != nil {
		serviceError(c, err)
		return
	}
	writeNeonMarkdown(c, report)
}

func writeNeonMarkdown(c *gin.Context, report neonPublicReport) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", `attachment; filename="siyige-review.md"`)
	c.Data(http.StatusOK, "text/markdown; charset=utf-8", []byte(renderNeonReportMarkdown(report)))
}

// 动态文字在 Markdown 中以字面形式导出，避免用户发言被解释为 HTML 或外部图片。
var neonMarkdownEscapes = strings.NewReplacer(
	"\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_", "{", "\\{", "}", "\\}",
	"[", "\\[", "]", "\\]", "<", "&lt;", ">", "&gt;", "#", "\\#", "+", "\\+",
	"-", "\\-", ".", "\\.", "!", "\\!", "|", "\\|", "&", "&amp;",
)

func neonMarkdownText(value string) string {
	value = strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(value)
	lines := strings.Split(value, "\n")
	for i := range lines {
		lines[i] = neonMarkdownEscapes.Replace(strings.TrimLeft(lines[i], " \t"))
	}
	return strings.Join(lines, "\n")
}

func renderNeonReportMarkdown(report neonPublicReport) string {
	var markdown strings.Builder
	stance := map[string]string{"pro": "正方", "con": "反方"}
	side := map[string]string{"user": "我方", "assistant": "AI 对手"}
	fmt.Fprintf(&markdown, "# %s\n\n我方持方：%s · AI 对手持方：%s\n\n", strings.ReplaceAll(neonMarkdownText(report.Title), "\n", " "), stance[report.UserStance], stance[report.AIStance])
	markdown.WriteString("## 裁决\n\n")
	if score := report.Score; score != nil {
		outcome := map[string]string{"win": "我方获胜", "loss": "AI 对手获胜", "draw": "平局"}
		source := map[string]string{"ai": "模型裁决", "local": "演示裁决"}
		fmt.Fprintf(&markdown, "%s · %s · 我方 %.1f / 对手 %.1f\n\n", outcome[score.Outcome], source[score.Source], score.UserTotal, score.AssistantTotal)
		for i, dimension := range []string{"逻辑严密性", "论据充分度", "临场反击力", "价值升华", "表达感染力"} {
			fmt.Fprintf(&markdown, "- %s：%.1f / %.1f\n", dimension, score.User[i], score.Assistant[i])
		}
		markdown.WriteString("\n")
	} else {
		markdown.WriteString("暂无已保存的裁决。\n\n")
	}
	for _, comment := range report.JudgeComments {
		fmt.Fprintf(&markdown, "%s\n\n", neonMarkdownText(comment))
	}
	markdown.WriteString("## 综合评语\n\n")
	if len(report.Feedback) == 0 {
		markdown.WriteString("暂无已保存的评语。\n\n")
	}
	for _, comment := range report.Feedback {
		fmt.Fprintf(&markdown, "%s\n\n", neonMarkdownText(comment))
	}
	markdown.WriteString("## 辩词\n\n")
	for _, round := range report.Rounds {
		fmt.Fprintf(&markdown, "### 第 %d 轮\n\n", round.Number)
		for _, speech := range round.Speeches {
			fmt.Fprintf(&markdown, "#### %s\n\n%s\n\n", side[speech.Side], neonMarkdownText(speech.Content))
		}
	}
	markdown.WriteString("## 批注\n\n")
	if len(report.Annotations) == 0 {
		markdown.WriteString("暂无已保存的批注。\n")
	}
	for _, note := range report.Annotations {
		fmt.Fprintf(&markdown, "### 第 %d 轮 · %s · %s\n\n%s\n\n%s\n\n%s\n\n", note.Round, side[note.Side], neonMarkdownText(note.Label), neonMarkdownText(note.Quote), neonMarkdownText(note.Reason), neonMarkdownText(note.Suggestion))
	}
	return markdown.String()
}
