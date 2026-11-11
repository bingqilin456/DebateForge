package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

// 冻结的 HTTP 契约常量：请求体 32 KiB、Cookie 名按传输安全策略区分。
const (
	// MaxRequestBodyBytes 是所有业务 POST 的上限，含未知 Content-Length 与分块请求。
	MaxRequestBodyBytes = 32 << 10

	// CookieNameDev 只用于 HTTP 本地开发：Secure 关闭，因此不能带 __Host- 前缀。
	CookieNameDev = "siyige_session_dev"
	// CookieNameSecure 用于 HTTPS：__Host- 前缀强制 Secure、Path=/、无 Domain。
	CookieNameSecure = "__Host-siyige_session"

	// CurrentUserKey 是 Gin Context 中 typed 当前用户的键，供后续业务模块复用。
	CurrentUserKey = "current_user"

	// DefaultAuthOrigin 是本地开发的默认同源地址。
	DefaultAuthOrigin = "http://127.0.0.1:5173"

	csrfHeaderName = "X-CSRF-Token"
	originHeader   = "Origin"
)

// HandlerConfig 使用精确 Origin；Cookie 策略由 Origin 的 scheme 决定，
// 使开发（HTTP 回环）与 HTTPS 使用不同名称，避免复用同一 Cookie。
type HandlerConfig struct {
	Origin string
	Logger *slog.Logger
	// TrustedProxy 为唯一允许传入客户端地址的 TCP 对端。零值保留直连策略。
	TrustedProxy netip.Addr
}

// Handler 承载四条认证路由与可复用中间件；不直接访问数据库。
type Handler struct {
	service      *Service
	origin       string
	secure       bool
	logger       *slog.Logger
	trustedProxy netip.Addr
}

// CurrentUser 是认证中间件写入 Gin Context 的 typed 身份。
// PasswordHash 始终为空，Session 供后续业务校验 CSRF 与所有权。
type CurrentUser struct {
	User    User
	Session Session
}

// NewHandler 在启动期校验 Origin，任何不精确或不安全的值都立即失败，
// 避免运行期以“看起来同源”的配置放行请求。
func NewHandler(service *Service, config HandlerConfig) (*Handler, error) {
	if service == nil {
		return nil, errors.New("认证服务不能为空")
	}
	origin, secure, err := ParseOrigin(config.Origin)
	if err != nil {
		return nil, err
	}
	logger := config.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Handler{service: service, origin: origin, secure: secure, logger: logger, trustedProxy: config.TrustedProxy}, nil
}

// ParseOrigin 校验并规范化配置中的完整 Origin，返回用于精确比较的规范形式。
// 规则：仅 http/https；拒绝 userinfo、path、query、fragment；
// HTTPS 允许域名或 IP；HTTP 只允许回环 IP。省略端口表示协议默认端口。
func ParseOrigin(raw string) (string, bool, error) {
	if raw == "" {
		return "", false, errors.New("AUTH_ORIGIN 不能为空")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false, errors.New("AUTH_ORIGIN 不是合法 URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false, errors.New("AUTH_ORIGIN 只支持 http 或 https")
	}
	if parsed.User != nil || strings.ContainsAny(raw, "?#") || parsed.Path != "" || parsed.RawPath != "" || parsed.Opaque != "" {
		return "", false, errors.New("AUTH_ORIGIN 不能包含 userinfo、path、query 或 fragment")
	}
	host, port := parsed.Hostname(), parsed.Port()
	if host == "" {
		return "", false, errors.New("AUTH_ORIGIN 缺少主机")
	}
	secure := parsed.Scheme == "https"
	if !secure {
		// HTTP 只允许本地开发回环地址，避免明文 Origin 被当成部署配置。
		address, addrErr := netip.ParseAddr(host)
		if addrErr != nil || !address.IsLoopback() {
			return "", false, errors.New("HTTP AUTH_ORIGIN 只允许回环 IP")
		}
	} else if err := validateSecureHost(host); err != nil {
		return "", false, err
	}
	if port != "" {
		number, convErr := strconv.Atoi(port)
		if convErr != nil || number < 1 || number > 65535 {
			return "", false, errors.New("AUTH_ORIGIN 端口必须在 1–65535")
		}
		port = strconv.Itoa(number)
	}
	// IPv6 字面地址在 URL 中必须保留方括号，且规范结果必须能再次通过配置校验。
	hostText := host
	if strings.Contains(host, ":") {
		hostText = "[" + host + "]"
	}
	normalized := parsed.Scheme + "://" + hostText
	// 浏览器不会为默认端口回填 Origin，因此配置成默认端口时也规范化掉，
	// 保证与服务端收到的头逐字节相等。
	defaultPort := "80"
	if secure {
		defaultPort = "443"
	}
	if port != "" && port != defaultPort {
		normalized += ":" + port
	}
	return normalized, secure, nil
}

// validateSecureHost 允许域名或 IP；域名必须是常规 LDH 主机名（不支持通配符）。
func validateSecureHost(host string) error {
	if _, err := netip.ParseAddr(host); err == nil {
		return nil
	}
	if len(host) > 253 || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return errors.New("AUTH_ORIGIN 主机名不合法")
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return errors.New("AUTH_ORIGIN 需要完整域名或 IP")
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return errors.New("AUTH_ORIGIN 主机名不合法")
		}
		for index := 0; index < len(label); index++ {
			character := label[index]
			if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' {
				continue
			}
			return errors.New("AUTH_ORIGIN 主机名不合法")
		}
	}
	return nil
}

// cookieName 根据传输安全策略返回冻结的名称，二者不可互换。
func (h *Handler) cookieName() string {
	if h.secure {
		return CookieNameSecure
	}
	return CookieNameDev
}

// RegisterRoutes 把四条认证路由挂到 /api/v1/auth 组上。
// 注册、登录只校验 Origin；退出额外要求有效 Session 与当前 Session 的 CSRF。
func (h *Handler) RegisterRoutes(group *gin.RouterGroup) {
	group.POST("/register", h.RequireOrigin(), h.register)
	group.POST("/login", h.RequireOrigin(), h.login)
	group.POST("/logout", h.RequireOrigin(), h.RequireAuth(), h.RequireCSRF(), h.logout)
	group.GET("/me", h.RequireAuth(), h.me)
}

// RequireOrigin 精确比较完整 Origin，供所有业务 POST 复用。
// 缺失、null、重复头、被代理合并成逗号分隔值或不匹配一律 403 csrf_failed。
func (h *Handler) RequireOrigin() gin.HandlerFunc {
	return func(c *gin.Context) {
		values := c.Request.Header.Values(originHeader)
		if len(values) != 1 || values[0] != h.origin {
			writeError(c, http.StatusForbidden, "csrf_failed", "请求来源校验失败")
			return
		}
		c.Next()
	}
}

// RequireAuth 是可复用认证中间件：解析唯一 Session Cookie、恢复当前用户，
// 并以 typed CurrentUser 写入 Gin Context。凭据无效为 401，存储故障为受控 503。
func (h *Handler) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := h.sessionTokenFromRequest(c)
		if err != nil {
			writeError(c, http.StatusUnauthorized, "unauthenticated", "未登录或登录已失效")
			return
		}
		session, user, authErr := h.service.Authenticate(c.Request.Context(), token)
		if authErr != nil {
			h.writeServiceError(c, authErr)
			return
		}
		c.Set(CurrentUserKey, CurrentUser{User: user, Session: session})
		c.Next()
	}
}

// RequireCSRF 校验当前 Session 的独立 CSRF token，使用常量时间比较。
// 必须在 RequireAuth 之后使用；重复或逗号合并的 token 头按校验失败处理。
func (h *Handler) RequireCSRF() gin.HandlerFunc {
	return func(c *gin.Context) {
		current, ok := CurrentUserFrom(c)
		if !ok {
			writeError(c, http.StatusUnauthorized, "unauthenticated", "未登录或登录已失效")
			return
		}
		provided := c.Request.Header.Values(csrfHeaderName)
		if len(provided) != 1 || strings.Contains(provided[0], ",") || !equalCSRF(current.Session.CSRFToken, provided[0]) {
			writeError(c, http.StatusForbidden, "csrf_failed", "CSRF 校验失败")
			return
		}
		c.Next()
	}
}

// CurrentUserFrom 读取 RequireAuth 写入的 typed 当前用户。
func CurrentUserFrom(c *gin.Context) (CurrentUser, bool) {
	value, exists := c.Get(CurrentUserKey)
	if !exists {
		return CurrentUser{}, false
	}
	current, ok := value.(CurrentUser)
	return current, ok
}

func (h *Handler) register(c *gin.Context) {
	request, ok := h.decodeCredentials(c)
	if !ok {
		return
	}
	user, err := h.service.Register(c.Request.Context(), h.clientIP(c), request.username, request.password)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	dto, err := newUserDTO(user)
	if err != nil {
		h.writeServiceError(c, ErrDependencyUnavailable)
		return
	}
	writeData(c, http.StatusCreated, dto)
}

func (h *Handler) login(c *gin.Context) {
	request, ok := h.decodeCredentials(c)
	if !ok {
		return
	}
	result, err := h.service.Login(c.Request.Context(), h.clientIP(c), request.username, request.password)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	dto, err := newUserDTO(result.User)
	if err != nil {
		h.writeServiceError(c, ErrDependencyUnavailable)
		return
	}
	h.setSessionCookie(c, result.Token, result.Session.ExpiresAt)
	writeData(c, http.StatusOK, AuthDTO{User: dto, CSRFToken: result.Session.CSRFToken})
}

func (h *Handler) logout(c *gin.Context) {
	// 退出无请求体，但仍限制体积，避免客户端用大请求占用连接。
	if _, ok := readLimitedBody(c); !ok {
		return
	}
	current, ok := CurrentUserFrom(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "unauthenticated", "未登录或登录已失效")
		return
	}
	if err := h.service.Logout(c.Request.Context(), current.Session); err != nil {
		h.writeServiceError(c, err)
		return
	}
	h.clearSessionCookie(c)
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusNoContent)
}

func (h *Handler) me(c *gin.Context) {
	current, ok := CurrentUserFrom(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "unauthenticated", "未登录或登录已失效")
		return
	}
	dto, err := newUserDTO(current.User)
	if err != nil {
		h.writeServiceError(c, ErrDependencyUnavailable)
		return
	}
	writeData(c, http.StatusOK, AuthDTO{User: dto, CSRFToken: current.Session.CSRFToken})
}

// credentialsRequest 用指针区分“字段缺失”和“字段为空”，两者都必须 400。
type credentialsRequest struct {
	Username *string `json:"username"`
	Password *string `json:"password"`
}

// decodedCredentials 是内部解码结果，避免把指针语义带到 Service。
type decodedCredentials struct {
	username string
	password string
}

// decodeCredentials 依次执行体积、媒体类型、UTF-8、单 JSON 文档与字段校验。
// 未知字段被忽略：契约未裁定拒绝策略，不能自行加 400。
func (h *Handler) decodeCredentials(c *gin.Context) (decodedCredentials, bool) {
	var request decodedCredentials
	body, ok := readLimitedBody(c)
	if !ok {
		return request, false
	}
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(c, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return request, false
	}
	if !utf8.Valid(body) {
		writeError(c, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return request, false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	var payload credentialsRequest
	if err := decoder.Decode(&payload); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return request, false
	}
	// 单文档：最后一个值之后只允许空白，任何尾随内容都是 400。
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(c, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return request, false
	}
	if payload.Username == nil || payload.Password == nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "用户名或密码缺失")
		return request, false
	}
	request.username, request.password = *payload.Username, *payload.Password
	return request, true
}

// readLimitedBody 用 MaxBytesReader 强制 32 KiB 上限并读出正文。
// 该读法同时覆盖未知 Content-Length 与分块传输；超限返回 413 并中止。
func readLimitedBody(c *gin.Context) ([]byte, bool) {
	if c.Request.Body == nil {
		return nil, true
	}
	limited := http.MaxBytesReader(c.Writer, c.Request.Body, MaxRequestBodyBytes)
	body, err := io.ReadAll(limited)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(c, http.StatusRequestEntityTooLarge, "request_too_large", "请求体超过限制")
			return nil, false
		}
		writeError(c, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return nil, false
	}
	return body, true
}

// sessionTokenFromRequest 只读配置的 Cookie 名，并拒绝同名重复 Cookie：
// 同时出现多个候选值时无法判断哪一个才是当前 Session，必须按未认证处理。
func (h *Handler) sessionTokenFromRequest(c *gin.Context) (string, error) {
	name := h.cookieName()
	found := 0
	token := ""
	for _, cookie := range c.Request.Cookies() {
		if cookie.Name != name {
			continue
		}
		found++
		token = cookie.Value
	}
	if found != 1 || token == "" {
		return "", ErrUnauthenticated
	}
	return token, nil
}

// setSessionCookie 只写一个 Set-Cookie，属性固定为 HttpOnly、Lax、Path=/、
// 无 Domain、24 小时 Max-Age 与 Expires；Secure 与名称由传输策略决定。
func (h *Handler) setSessionCookie(c *gin.Context, token string, expiresAt time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     h.cookieName(),
		Value:    token,
		Path:     "/",
		Expires:  expiresAt.UTC(),
		MaxAge:   int(DefaultSessionTTL / time.Second),
		HttpOnly: true,
		Secure:   h.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// clearSessionCookie 用同名同路径、立即过期的方式删除 Cookie。
func (h *Handler) clearSessionCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     h.cookieName(),
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// clientIP 只接受专用 Nginx 对端重写的单一 X-Real-IP；其它请求使用 TCP 地址。
func (h *Handler) clientIP(c *gin.Context) string {
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		host = c.Request.RemoteAddr
	}
	if address, addrErr := netip.ParseAddr(host); addrErr == nil {
		if address.Unmap() == h.trustedProxy {
			values := c.Request.Header.Values("X-Real-IP")
			if len(values) == 1 {
				if client, err := netip.ParseAddr(values[0]); err == nil && client.Zone() == "" {
					return client.Unmap().String()
				}
			}
		}
		return address.String()
	}
	return "unknown"
}

// writeServiceError 把领域错误映射为冻结的契约状态码与受控提示。
// 日志只写 request_id 与受控 error_code，绝不记录原因、正文或凭据。
func (h *Handler) writeServiceError(c *gin.Context, err error) {
	var rateLimit *RateLimitError
	switch {
	case errors.Is(err, ErrInvalidRequest):
		writeError(c, http.StatusBadRequest, "invalid_request", "请求参数不符合契约")
	case errors.Is(err, ErrUsernameConflict):
		writeError(c, http.StatusConflict, "username_conflict", "用户名已被占用")
	case errors.Is(err, ErrInvalidCredentials):
		writeError(c, http.StatusUnauthorized, "invalid_credentials", "用户名或密码错误")
	case errors.Is(err, ErrUnauthenticated), errors.Is(err, ErrNotFound):
		writeError(c, http.StatusUnauthorized, "unauthenticated", "未登录或登录已失效")
	case errors.As(err, &rateLimit):
		c.Header("Retry-After", strconv.Itoa(RetryAfterSeconds(rateLimit.RetryAfter)))
		writeError(c, http.StatusTooManyRequests, "rate_limited", "请求过于频繁，请稍后再试")
	case errors.Is(err, ErrDependencyUnavailable), errors.Is(err, ErrAuthBusy):
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "服务暂时不可用")
	default:
		h.logger.Error("认证请求处理失败", "request_id", c.GetString("request_id"), "error_code", "internal_error")
		writeError(c, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
	}
}

// writeData 统一成功信封：data + request_id，并禁止缓存。
func writeData(c *gin.Context, status int, data any) {
	c.Header("Cache-Control", "no-store")
	c.JSON(status, gin.H{"data": data, "request_id": c.GetString("request_id")})
}

// writeError 统一错误信封：error{code,message} + request_id，并禁止缓存。
func writeError(c *gin.Context, status int, code, message string) {
	c.Header("Cache-Control", "no-store")
	c.AbortWithStatusJSON(status, gin.H{
		"error":      gin.H{"code": code, "message": message},
		"request_id": c.GetString("request_id"),
	})
}
