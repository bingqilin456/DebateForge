package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"io"
	"time"
)

// Session 熵与有效期冻结自架构契约：会话 token 至少 32 随机字节，
// 数据库只保存其 SHA-256 原始 32 字节摘要，固定 24 小时过期且无刷新 token。
const (
	SessionTokenBytes = 32
	SessionCSRFBytes  = 32
	DefaultSessionTTL = 24 * time.Hour
)

// newSessionRecord 生成待持久化的会话行与只交付浏览器的 token 原值。
// token 与 csrf 使用独立随机字节；任一步失败都不返回部分结果。
func newSessionRecord(userID uint64, reader io.Reader, now time.Time, ttl time.Duration) (*Session, string, error) {
	if userID == 0 || ttl <= 0 {
		return nil, "", ErrInvalidRequest
	}
	raw := make([]byte, SessionTokenBytes+SessionCSRFBytes)
	if _, err := io.ReadFull(reader, raw); err != nil {
		return nil, "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:SessionTokenBytes])
	csrf := base64.RawURLEncoding.EncodeToString(raw[SessionTokenBytes:])
	digest := sha256.Sum256([]byte(token))
	session := &Session{
		UserID:    userID,
		TokenHash: digest[:],
		CSRFToken: csrf,
		ExpiresAt: now.Add(ttl).UTC(),
		CreatedAt: now.UTC(),
	}
	return session, token, nil
}

// sessionTokenDigest 校验 Cookie 原值并返回入库用的 SHA-256 摘要。
// 只接受规范 base64url（无填充、无多余字符）且长度恰为 32 字节，
// 使格式非法或伪造的 token 在访问数据库前就被拒绝。
func sessionTokenDigest(token string) ([]byte, error) {
	if token == "" || len(token) != base64.RawURLEncoding.EncodedLen(SessionTokenBytes) {
		return nil, ErrInvalidToken
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(raw) != SessionTokenBytes {
		return nil, ErrInvalidToken
	}
	// Strict 解码已拒绝非规范尾部比特；再确认重新编码完全一致，杜绝等价改写。
	if base64.RawURLEncoding.EncodeToString(raw) != token {
		return nil, ErrInvalidToken
	}
	digest := sha256.Sum256([]byte(token))
	return digest[:], nil
}

// sessionValidAt 以固定过期时刻判断有效性：到期瞬间即失效，不做滑动续期。
func sessionValidAt(session Session, now time.Time) bool {
	return now.Before(session.ExpiresAt)
}

// equalCSRF 使用常量时间比较 CSRF token，避免逐字节计时泄露。
func equalCSRF(expected, provided string) bool {
	return subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) == 1
}

// defaultSessionReader 返回默认密码学随机源，便于测试注入确定性字节。
func defaultSessionReader() io.Reader { return rand.Reader }
