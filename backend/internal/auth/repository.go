package auth

import (
	"context"
	"errors"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

// repositoryTimeout 与架构中“数据库操作 3 秒”一致。auth 包不 import platform，
// 以避免 platform→auth 的路由接线形成循环依赖，因此在这里独立声明并从请求派生。
const repositoryTimeout = 3 * time.Second

// Repository 是 auth 的唯一持久化实现，只映射 users 与 auth_sessions 两张既有表。
// 所有查询使用参数绑定；错误保留底层 cause 以便诊断，但 Error 文本受控。
type Repository struct {
	orm *gorm.DB
}

// NewRepository 复用 platform 已建立的 *gorm.DB，不新建连接池、不迁移表。
func NewRepository(orm *gorm.DB) *Repository { return &Repository{orm: orm} }

// userRow 只映射认证相关列，避免把整表结构暴露给上层。
type userRow struct {
	ID           uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	Username     string    `gorm:"column:username"`
	PasswordHash string    `gorm:"column:password_hash"`
	CreatedAt    time.Time `gorm:"column:created_at"`
}

func (userRow) TableName() string { return "users" }

// sessionRow 对应 auth_sessions；TokenHash 为 BINARY(32) 的 SHA-256 摘要。
type sessionRow struct {
	ID        uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	UserID    uint64    `gorm:"column:user_id"`
	TokenHash []byte    `gorm:"column:token_hash"`
	CSRFToken string    `gorm:"column:csrf_token"`
	ExpiresAt time.Time `gorm:"column:expires_at"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (sessionRow) TableName() string { return "auth_sessions" }

// withTimeout 从请求 Context 派生最长 3 秒的数据库预算，保留调用方取消语义。
func (repository *Repository) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, repositoryTimeout)
}

func (repository *Repository) CreateUser(ctx context.Context, username, passwordHash string) (User, error) {
	ctx, cancel := repository.withTimeout(ctx)
	defer cancel()
	row := userRow{Username: username, PasswordHash: passwordHash}
	if err := repository.orm.WithContext(ctx).Create(&row).Error; err != nil {
		if isDuplicateEntry(err) {
			return User{}, ErrUsernameConflict
		}
		return User{}, storeError("创建用户失败", err)
	}
	return User{ID: row.ID, Username: row.Username, PasswordHash: row.PasswordHash}, nil
}

func (repository *Repository) FindUserByUsername(ctx context.Context, username string) (User, error) {
	ctx, cancel := repository.withTimeout(ctx)
	defer cancel()
	var row userRow
	if err := repository.orm.WithContext(ctx).Where("username = ?", username).Take(&row).Error; err != nil {
		return User{}, mapRowError("查询用户失败", err)
	}
	return User{ID: row.ID, Username: row.Username, PasswordHash: row.PasswordHash}, nil
}

func (repository *Repository) FindUserByID(ctx context.Context, id uint64) (User, error) {
	ctx, cancel := repository.withTimeout(ctx)
	defer cancel()
	var row userRow
	if err := repository.orm.WithContext(ctx).Where("id = ?", id).Take(&row).Error; err != nil {
		return User{}, mapRowError("查询用户失败", err)
	}
	return User{ID: row.ID, Username: row.Username, PasswordHash: row.PasswordHash}, nil
}

func (repository *Repository) CreateSession(ctx context.Context, session Session) (uint64, error) {
	ctx, cancel := repository.withTimeout(ctx)
	defer cancel()
	row := sessionRow{
		UserID:    session.UserID,
		TokenHash: session.TokenHash,
		CSRFToken: session.CSRFToken,
		ExpiresAt: session.ExpiresAt.UTC(),
	}
	if err := repository.orm.WithContext(ctx).Create(&row).Error; err != nil {
		// token 摘要碰撞不可恢复，按依赖故障处理而不是覆盖已有会话。
		return 0, storeError("创建会话失败", err)
	}
	return row.ID, nil
}

func (repository *Repository) FindSessionByTokenHash(ctx context.Context, tokenHash []byte) (Session, error) {
	ctx, cancel := repository.withTimeout(ctx)
	defer cancel()
	var row sessionRow
	if err := repository.orm.WithContext(ctx).Where("token_hash = ?", tokenHash).Take(&row).Error; err != nil {
		return Session{}, mapRowError("查询会话失败", err)
	}
	return Session{
		ID:        row.ID,
		UserID:    row.UserID,
		TokenHash: row.TokenHash,
		CSRFToken: row.CSRFToken,
		ExpiresAt: row.ExpiresAt.UTC(),
		CreatedAt: row.CreatedAt.UTC(),
	}, nil
}

func (repository *Repository) DeleteSession(ctx context.Context, sessionID uint64) (bool, error) {
	ctx, cancel := repository.withTimeout(ctx)
	defer cancel()
	result := repository.orm.WithContext(ctx).Where("id = ?", sessionID).Delete(&sessionRow{})
	if result.Error != nil {
		return false, storeError("删除会话失败", result.Error)
	}
	return result.RowsAffected > 0, nil
}

// mapRowError 把“没有这一行”与其他存储故障区分开，后者保留 cause 但文本受控。
func mapRowError(operation string, err error) error {
	if err == gorm.ErrRecordNotFound {
		return ErrNotFound
	}
	return storeError(operation, err)
}

// isDuplicateEntry 只把 MySQL 1062（唯一索引冲突）识别为业务冲突。
func isDuplicateEntry(err error) bool {
	var mysqlError *driver.MySQLError
	return errors.As(err, &mysqlError) && mysqlError.Number == 1062
}
