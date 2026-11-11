package topic

import (
	"context"
	"errors"
)

// Service 是辩题查询的规则边界：只做受控错误收敛，不感知 HTTP，也不 import platform。
type Service struct {
	store Store
}

// NewService 接受最小 Store 接口，保证 Handler 与 Repository 不互相依赖。
func NewService(store Store) *Service { return &Service{store: store} }

// ListEnabled 返回启用辩题；空结果固定为非 nil 空切片，供 HTTP 输出 []。
// 已取消的请求直接返回取消原因，不伪装成 503。
func (service *Service) ListEnabled(ctx context.Context) ([]Topic, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if service == nil || service.store == nil {
		return nil, storeError("查询辩题失败", ErrDependencyUnavailable)
	}
	topics, err := service.store.ListEnabled(ctx)
	if err != nil {
		return nil, mapStoreError(ctx, err)
	}
	if topics == nil {
		return []Topic{}, nil
	}
	// 仓储层已排序，这里只做一次防御性校验；不重排，避免掩盖实现缺陷。
	for index, topic := range topics {
		if err := validateTopic(topic); err != nil {
			return nil, storeError("辩题数据不符合契约", errors.Join(ErrInvalidData, err))
		}
		if index > 0 && topics[index-1].ID >= topic.ID {
			return nil, storeError("辩题数据不符合契约", ErrInvalidData)
		}
	}
	return topics, nil
}

// mapStoreError 收敛存储错误：调用方取消优先，数据契约错误原样上抛，
// 其余（含未包装的普通错误）统一为受控依赖错误。
func mapStoreError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if errors.Is(err, ErrInvalidData) {
		return storeError(ErrInvalidData.Error(), err)
	}
	return storeError(ErrDependencyUnavailable.Error(), errors.Join(ErrDependencyUnavailable, err))
}
