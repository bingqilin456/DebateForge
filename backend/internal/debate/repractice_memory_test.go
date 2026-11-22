package debate

import (
	"context"
	"sync"
	"time"
)

// repracticeVersionStore 在既有内存会话替身之上加一层“独立版本表”，
// 用来验证局部重练的持久化边界：归属/完赛/原文复核、幂等、CAS 重试，
// 以及它与正式消息、会话状态、复盘完全隔离。
//
// 真实实现把所有方法放进短事务；这里用同一把锁模拟临界区语义。
type repracticeVersionStore struct {
	*memoryStore
	mu       sync.Mutex
	versions map[uint64][]RepracticeVersion
	nextID   uint64
}

func newRepracticeVersionStore(now func() time.Time) *repracticeVersionStore {
	return newRepracticeVersionStoreWith(newMemoryStore(now))
}

// newRepracticeVersionStoreWith 在既有会话替身之上加独立版本表：
// 服务通过 RepracticeStore 访问版本，正式消息与复盘仍走原实现，隔离可验证。
func newRepracticeVersionStoreWith(mem *memoryStore) *repracticeVersionStore {
	return &repracticeVersionStore{
		memoryStore: mem,
		versions:    make(map[uint64][]RepracticeVersion),
		nextID:      0,
	}
}

func (m *repracticeVersionStore) eligible(uid, id uint64) (Detail, error) {
	detail, err := m.memoryStore.Detail(context.Background(), uid, id)
	if err != nil {
		return Detail{}, err
	}
	if detail.Debate.Status != "completed" {
		return Detail{}, ErrRoundConflict
	}
	return detail, nil
}

func (m *repracticeVersionStore) ListRepractice(_ context.Context, uid, id uint64) ([]RepracticeVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.eligible(uid, id); err != nil {
		return nil, err
	}
	return append([]RepracticeVersion(nil), m.versions[id]...), nil
}

func (m *repracticeVersionStore) CreateRepractice(_ context.Context, uid, id uint64, in RepracticeInput, now time.Time) (RepracticeVersion, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	detail, err := m.eligible(uid, id)
	if err != nil {
		return RepracticeVersion{}, false, err
	}
	if _, _, ok := repracticeSourceMatches(detail, in); !ok {
		return RepracticeVersion{}, false, ErrRepracticeSource
	}
	for _, version := range m.versions[id] {
		if version.Input.RequestID != in.RequestID {
			continue
		}
		if version.Input != in {
			return RepracticeVersion{}, false, ErrRepracticeInput
		}
		return version, false, nil
	}
	m.nextID++
	version := RepracticeVersion{
		ID:           m.nextID,
		DebateID:     id,
		Attempt:      1,
		Status:       repracticeStatusGenerating,
		Input:        in,
		RestoredFrom: in.RestoredFrom,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	m.versions[id] = append(m.versions[id], version)
	return version, true, nil
}

func (m *repracticeVersionStore) ClaimRepracticeRetry(_ context.Context, uid, id, versionID, attempt uint64, now time.Time) (RepracticeVersion, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.eligible(uid, id); err != nil {
		return RepracticeVersion{}, false, err
	}
	for index, version := range m.versions[id] {
		if version.ID != versionID {
			continue
		}
		if version.Status != repracticeStatusFailed || version.Attempt != attempt {
			return version, false, nil
		}
		version.Attempt++
		version.Status = repracticeStatusGenerating
		version.ErrorCode = ""
		version.UpdatedAt = now
		m.versions[id][index] = version
		return version, true, nil
	}
	return RepracticeVersion{}, false, ErrRepracticeVersion
}

func (m *repracticeVersionStore) CompleteRepractice(_ context.Context, uid, id, versionID, attempt uint64, feedback RepracticeFeedback) (RepracticeVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.eligible(uid, id); err != nil {
		return RepracticeVersion{}, err
	}
	for index, version := range m.versions[id] {
		if version.ID != versionID {
			continue
		}
		if version.Status != repracticeStatusGenerating || version.Attempt != attempt {
			return RepracticeVersion{}, ErrAttemptVersionConflict
		}
		version.Status = repracticeStatusCompleted
		version.Feedback = feedback
		version.ErrorCode = ""
		m.versions[id][index] = version
		return version, nil
	}
	return RepracticeVersion{}, ErrRepracticeVersion
}

func (m *repracticeVersionStore) FailRepractice(_ context.Context, uid, id, versionID, attempt uint64, code string) (RepracticeVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.eligible(uid, id); err != nil {
		return RepracticeVersion{}, err
	}
	for index, version := range m.versions[id] {
		if version.ID != versionID {
			continue
		}
		if version.Status != repracticeStatusGenerating || version.Attempt != attempt {
			return RepracticeVersion{}, ErrAttemptVersionConflict
		}
		version.Status = repracticeStatusFailed
		version.ErrorCode = code
		m.versions[id][index] = version
		return version, nil
	}
	return RepracticeVersion{}, ErrRepracticeVersion
}
