package topic

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// repositoryTimeout 与架构中“数据库操作 3 秒”一致。topic 不 import platform，
// 以避免 platform→topic 的路由接线形成循环依赖，因此独立声明并从请求派生。
const repositoryTimeout = 3 * time.Second

// Repository 是 topic 的唯一持久化实现，只映射既有 topics 表，不迁移、不播种。
type Repository struct {
	orm *gorm.DB
}

// NewRepository 复用 platform 已建立的 *gorm.DB，不新建连接池。
func NewRepository(orm *gorm.DB) *Repository { return &Repository{orm: orm} }

// topicRow 只映射契约需要的列；enabled 由查询条件固定为 true。
type topicRow struct {
	ID          uint64 `gorm:"column:id;primaryKey"`
	Title       string `gorm:"column:title"`
	Description string `gorm:"column:description"`
}

func (topicRow) TableName() string { return "topics" }

// ListEnabled 只返回 enabled=true 的题目，按 id 升序。
// 排序放在 SQL 中，避免应用层对大结果集二次排序；每一行仍做契约校验，
// 脏数据以受控错误失败而不是静默返回给客户端。
func (repository *Repository) ListEnabled(ctx context.Context) ([]Topic, error) {
	if repository == nil || repository.orm == nil {
		return nil, storeError("查询辩题失败", ErrDependencyUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return nil, storeError("查询辩题失败", err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, repositoryTimeout)
	defer cancel()
	var rows []topicRow
	if err := repository.orm.WithContext(queryCtx).
		Select("id", "title", "description").
		Where("enabled = ?", true).
		Order("id ASC").
		Find(&rows).Error; err != nil {
		return nil, storeError("查询辩题失败", err)
	}
	topics := make([]Topic, 0, len(rows))
	for _, row := range rows {
		topic := Topic{ID: row.ID, Title: row.Title, Description: row.Description}
		if err := validateTopic(topic); err != nil {
			return nil, storeError("辩题数据不符合契约", errors.Join(ErrInvalidData, err))
		}
		topics = append(topics, topic)
	}
	return topics, nil
}

// 编译期断言：Repository 必须始终满足 Store。
var _ Store = (*Repository)(nil)
