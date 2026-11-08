package topic

import (
	"context"
	"errors"
	"strconv"
	"unicode/utf8"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
)

// 辩题模块对外的受控错误：HTTP 边界只按 errors.Is 分支映射，
// 底层 SQL、DSN 与账户信息不进入 Error() 文本。
var (
	// ErrDependencyUnavailable 对应 503 dependency_unavailable。
	ErrDependencyUnavailable = errors.New("依赖服务暂时不可用")
	// ErrInvalidData 表示数据库中的辩题数据违反契约（非空、UTF-8、长度或顺序），
	// HTTP 边界映射为 500 internal_error，绝不把脏数据返回给客户端。
	ErrInvalidData = errors.New("辩题数据不符合契约")
)

// Topic 是只读辩题快照；enabled 与时间列不对外暴露，查询已限定 enabled=true。
type Topic struct {
	ID          uint64
	Title       string
	Description string
}

// Store 是 topic 需要的最小持久化能力，便于 Handler/Service 定向测试注入。
// 实现必须使用调用方 Context，并在返回结果中保持 id 升序。
type Store interface {
	ListEnabled(ctx context.Context) ([]Topic, error)
}

// storageError 保留底层 cause 供 errors.Is/As 诊断，Error 文本固定受控。
type storageError struct {
	operation string
	cause     error
}

func (e storageError) Error() string { return e.operation }

func (e storageError) Unwrap() error { return e.cause }

// storeError 包装仓储层错误；operation 必须是预定义的受控短语。
func storeError(operation string, cause error) error {
	return storageError{operation: operation, cause: cause}
}

// FormatEntityID 把 BIGINT UNSIGNED 映射为契约要求的十进制字符串。
// 0 不是合法实体 ID，出现即说明数据缺陷，返回错误而不是输出 "0"。
func FormatEntityID(id uint64) (string, error) {
	if id == 0 {
		return "", ErrInvalidData
	}
	return strconv.FormatUint(id, 10), nil
}

// validateTopic 校验单条辩题快照。这里同时检查标题与描述长度：
// 它们是后续创建会话时的 Prompt 快照来源，超出 ai 包的上限会让首轮必然失败。
func validateTopic(topic Topic) error {
	if topic.ID == 0 {
		return ErrInvalidData
	}
	if !utf8.ValidString(topic.Title) || !utf8.ValidString(topic.Description) {
		return ErrInvalidData
	}
	if !hasVisibleRune(topic.Title) || !hasVisibleRune(topic.Description) {
		return ErrInvalidData
	}
	if utf8.RuneCountInString(topic.Title) > ai.MaxTopicTitleCodepoints {
		return ErrInvalidData
	}
	if utf8.RuneCountInString(topic.Description) > ai.MaxTopicDescriptionCodepoints {
		return ErrInvalidData
	}
	return nil
}

// hasVisibleRune 判断字符串是否含非空白码点；空白判定覆盖全角空格，
// 避免数据库里仅用空格占位的辩题进入 Prompt。
func hasVisibleRune(value string) bool {
	for _, r := range value {
		switch r {
		case ' ', '\t', '\n', '\v', '\f', '\r', 0x85, 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
			continue
		}
		if r >= 0x2000 && r <= 0x200A {
			continue
		}
		return true
	}
	return false
}
