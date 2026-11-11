package topic

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// stubStore 是最小可观察 Store，用于定向验证 Service 的映射与顺序保持。
type stubStore struct {
	topics []Topic
	err    error
	calls  int
}

func (store *stubStore) ListEnabled(context.Context) ([]Topic, error) {
	store.calls++
	return store.topics, store.err
}

func TestServiceListEnabledKeepsStoreOrder(t *testing.T) {
	store := &stubStore{topics: []Topic{
		{ID: 1, Title: "题目一", Description: "说明一"},
		{ID: 9, Title: "题目九", Description: "说明九"},
	}}
	service := NewService(store)
	topics, err := service.ListEnabled(context.Background())
	if err != nil {
		t.Fatalf("服务层不应把正常结果当错误: %v", err)
	}
	if len(topics) != 2 || topics[0].ID != 1 || topics[1].ID != 9 {
		t.Fatalf("未保持存储层顺序: %+v", topics)
	}
	if store.calls != 1 {
		t.Fatalf("存储调用次数=%d want=1", store.calls)
	}
}

func TestServiceListEnabledEmptyIsNonNil(t *testing.T) {
	service := NewService(&stubStore{})
	topics, err := service.ListEnabled(context.Background())
	if err != nil {
		t.Fatalf("空列表不应报错: %v", err)
	}
	if topics == nil {
		t.Fatal("空列表必须是空切片，否则 HTTP 会输出 null")
	}
	if len(topics) != 0 {
		t.Fatalf("空列表长度=%d want=0", len(topics))
	}
}

func TestServiceListEnabledMapsStorageFailureToControlledError(t *testing.T) {
	cause := errors.New("驱动内部原因-不可外泄")
	service := NewService(&stubStore{err: storeError("查询辩题失败", cause)})
	_, err := service.ListEnabled(context.Background())
	if !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("存储失败未映射为依赖不可用: %v", err)
	}
	if !errors.Is(err, cause) {
		t.Fatal("受控错误未保留底层 cause，无法诊断")
	}
	if strings.Contains(err.Error(), "驱动内部原因") {
		t.Fatal("错误文本泄露底层原因")
	}
}

func TestServiceListEnabledMapsUnwrappedStoreError(t *testing.T) {
	// 即使注入实现返回未包装的普通错误，也不能把原始文本带到 HTTP 边界。
	service := NewService(&stubStore{err: errors.New("SELECT 失败")})
	_, err := service.ListEnabled(context.Background())
	if !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("普通存储错误未映射为依赖不可用: %v", err)
	}
	if strings.Contains(err.Error(), "SELECT") {
		t.Fatal("错误文本泄露 SQL")
	}
}

func TestServiceListEnabledRejectsNilStore(t *testing.T) {
	service := NewService(nil)
	if _, err := service.ListEnabled(context.Background()); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("缺少存储实现时应受控失败: %v", err)
	}
}
