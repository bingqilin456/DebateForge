package auth

import (
	"context"
	"errors"
	"strconv"
	"time"
)

// Store 只暴露认证所需的六个最小操作，避免引入万能 CRUD 抽象。
// 实现必须使用调用方 Context，并把“不存在”映射为 ErrNotFound、
// 把唯一索引冲突映射为 ErrUsernameConflict、其余故障包装为受控存储错误。
type Store interface {
	CreateUser(ctx context.Context, username, passwordHash string) (User, error)
	FindUserByUsername(ctx context.Context, username string) (User, error)
	FindUserByID(ctx context.Context, id uint64) (User, error)

	CreateSession(ctx context.Context, session Session) (uint64, error)
	FindSessionByTokenHash(ctx context.Context, tokenHash []byte) (Session, error)
	DeleteSession(ctx context.Context, sessionID uint64) (bool, error)
}

// User 是认证域内的用户快照；PasswordHash 只在 Service 与 Repository 之间流转，
// HTTP 层必须使用 UserDTO，不能直接序列化本结构。
type User struct {
	ID           uint64
	Username     string
	PasswordHash string
}

// Session 对应 auth_sessions 行；TokenHash 是原始 token 的 SHA-256（32 字节），
// CSRFToken 与 Session token 相互独立。Session 原值只通过 Cookie 传输、不入库。
type Session struct {
	ID        uint64
	UserID    uint64
	TokenHash []byte
	CSRFToken string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// LoginResult 返回登录成功后的用户、会话行与只交付浏览器的 token 原值。
// 任何失败路径都必须返回零值，避免 token 与 CSRF 在失败时被交付。
type LoginResult struct {
	User    User
	Session Session
	Token   string
}

// FormatEntityID 把 BIGINT UNSIGNED 映射为契约要求的十进制字符串。
// 0 不是合法实体 ID，出现即说明上游缺陷，返回错误而不是输出 "0"。
func FormatEntityID(id uint64) (string, error) {
	if id == 0 {
		return "", errors.New("实体 ID 不能为 0")
	}
	return strconv.FormatUint(id, 10), nil
}
