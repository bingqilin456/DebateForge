package debate

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"unicode/utf8"

	"github.com/bingqilin456/DebateForge/backend/internal/auth"
	"github.com/gin-gonic/gin"
)

// V2-08 局部重练的 HTTP 边界。
//
// 路径与既有轮次/总结/复盘入口并列，不改动任何既有路由：
//
//	GET  /api/v1/debates/:id/repractice          读取本场全部历史版本（只要 Session）
//	POST /api/v1/debates/:id/repractice          显式提交一次改写（Origin+CSRF+防刷）
//	POST /api/v1/debates/:id/repractice/:version/retry  显式重试失败版本
//
// 读接口不产生模型调用；写接口在能力关闭时返回 400（与复盘一致），
// 不产生任何版本行，也不访问尚未部署的新表。
//
// 请求正文用严格解析（拒绝未知键、重复键、null 与类型不符）：坐标按 Unicode
// 码点半开区间传输，前端不得换算成 UTF-16 下标。

// decodeRepracticeSubmit 严格解析提交正文并转换为领域输入。
func decodeRepracticeSubmit(raw []byte) (RepracticeInput, error) {
	out := RepracticeInput{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return out, ErrInvalidRequest
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return out, ErrInvalidRequest
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return out, ErrInvalidRequest
		}
		seen[key] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil || isJSONNull(value) {
			return out, ErrInvalidRequest
		}
		switch key {
		case "client_request_id":
			if json.Unmarshal(value, &out.RequestID) != nil {
				return out, ErrInvalidRequest
			}
		case "message_id":
			var id string
			if json.Unmarshal(value, &id) != nil {
				return out, ErrInvalidRequest
			}
			parsed, parseErr := ParseID(id)
			if parseErr != nil {
				return out, ErrInvalidRequest
			}
			out.MessageID = parsed
		case "start_cp":
			var point int
			if json.Unmarshal(value, &point) != nil || point < 0 {
				return out, ErrInvalidRequest
			}
			out.Start = point
		case "end_cp":
			var point int
			if json.Unmarshal(value, &point) != nil || point < 0 {
				return out, ErrInvalidRequest
			}
			out.End = point
		case "quote":
			if json.Unmarshal(value, &out.Quote) != nil {
				return out, ErrInvalidRequest
			}
		case "goal":
			if json.Unmarshal(value, &out.Goal) != nil {
				return out, ErrInvalidRequest
			}
		case "rewrite":
			if json.Unmarshal(value, &out.Rewrite) != nil {
				return out, ErrInvalidRequest
			}
		case "restored_from":
			var id string
			if json.Unmarshal(value, &id) != nil {
				return out, ErrInvalidRequest
			}
			parsed, parseErr := ParseID(id)
			if parseErr != nil {
				return out, ErrInvalidRequest
			}
			out.RestoredFrom = parsed
		default:
			return out, ErrInvalidRequest
		}
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return out, ErrInvalidRequest
	}
	if _, err = decoder.Token(); !errors.Is(err, io.EOF) {
		return out, ErrInvalidRequest
	}
	// 选段字段必须显式出现：缺字段是协议错误（400），空区间才是“要重练却没有选原文”。
	for _, key := range []string{"client_request_id", "message_id", "start_cp", "end_cp", "quote", "rewrite"} {
		if !seen[key] {
			return out, ErrInvalidRequest
		}
	}
	if out.End <= out.Start || isBlankText(out.Quote) {
		// 空选段交给服务层统一按 ErrRepracticeSource 处理（对外 400）。
		return RepracticeInput{RequestID: out.RequestID}, ErrRepracticeSource
	}
	return out, nil
}

// decodeRepracticeRetry 严格解析重试正文：必须且只能带 expected_attempt_version。
func decodeRepracticeRetry(raw []byte) (uint64, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return 0, ErrInvalidRequest
	}
	seen := map[string]bool{}
	var version uint64
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return 0, ErrInvalidRequest
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return 0, ErrInvalidRequest
		}
		seen[key] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil || isJSONNull(value) {
			return 0, ErrInvalidRequest
		}
		if key != "expected_attempt_version" {
			return 0, ErrInvalidRequest
		}
		var parsed uint64
		if json.Unmarshal(value, &parsed) != nil || parsed == 0 || parsed == ^uint64(0) {
			return 0, ErrInvalidRequest
		}
		version = parsed
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return 0, ErrInvalidRequest
	}
	if _, err = decoder.Token(); !errors.Is(err, io.EOF) {
		return 0, ErrInvalidRequest
	}
	if !seen["expected_attempt_version"] {
		return 0, ErrInvalidRequest
	}
	return version, nil
}

// readBody 读取并预检请求正文：与既有 decode 使用同一套上限、媒体类型与
// UTF-8 规则，但把原始字节交给本文件的严格解析器，以便区分“缺字段”与“类型不符”。
// 预检失败时已写出受控错误响应，返回 false。
func readBody(c *gin.Context) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, auth.MaxRequestBodyBytes))
	if err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			writeError(c, 413, "request_too_large", nil)
		} else {
			writeError(c, 400, "invalid_request", nil)
		}
		return nil, false
	}
	media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || media != "application/json" || !utf8.Valid(body) {
		writeError(c, 400, "invalid_request", nil)
		return nil, false
	}
	return body, true
}

// registerRepracticeRoutes 挂载局部重练入口；不改动既有路由。
func (h *Handler) registerRepracticeRoutes(group *gin.RouterGroup, a *auth.Handler) {
	group.GET("/debates/:id/repractice", a.RequireAuth(), h.listRepractice)
	group.POST("/debates/:id/repractice", a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), h.limitGeneration, h.submitRepractice)
	group.POST("/debates/:id/repractice/:version/retry", a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), h.limitGeneration, h.retryRepractice)
}

func (h *Handler) listRepractice(c *gin.Context) {
	id, err := ParseID(c.Param("id"))
	if err != nil {
		serviceError(c, err)
		return
	}
	current, _ := auth.CurrentUserFrom(c)
	data, err := h.service.ListRepractice(c.Request.Context(), current.User.ID, id)
	if err != nil {
		serviceError(c, err)
		return
	}
	writeData(c, 200, data.DTO())
}

func (h *Handler) submitRepractice(c *gin.Context) {
	id, err := ParseID(c.Param("id"))
	if err != nil {
		serviceError(c, err)
		return
	}
	raw, ok := readBody(c)
	if !ok {
		return
	}
	in, err := decodeRepracticeSubmit(raw)
	if err != nil {
		serviceError(c, err)
		return
	}
	current, _ := auth.CurrentUserFrom(c)
	data, err := h.service.SubmitRepractice(c.Request.Context(), current.User.ID, id, in)
	if err != nil {
		serviceError(c, err)
		return
	}
	writeData(c, 200, data.DTO())
}

func (h *Handler) retryRepractice(c *gin.Context) {
	id, err := ParseID(c.Param("id"))
	if err != nil {
		serviceError(c, err)
		return
	}
	versionID, err := ParseID(c.Param("version"))
	if err != nil {
		serviceError(c, err)
		return
	}
	raw, ok := readBody(c)
	if !ok {
		return
	}
	attempt, err := decodeRepracticeRetry(raw)
	if err != nil {
		serviceError(c, err)
		return
	}
	current, _ := auth.CurrentUserFrom(c)
	data, err := h.service.RetryRepractice(c.Request.Context(), current.User.ID, id, versionID, attempt)
	if err != nil {
		serviceError(c, err)
		return
	}
	writeData(c, 200, data.DTO())
}
