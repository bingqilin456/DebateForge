package topic

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// recordingLogger 只记录 GORM 生成的 SQL，用来证明仓储层的筛选与排序落在查询里。
// 它不打印参数，避免测试输出携带业务数据。
type recordingLogger struct {
	mu   sync.Mutex
	sqls []string
}

func (l *recordingLogger) LogMode(logger.LogLevel) logger.Interface { return l }
func (l *recordingLogger) Info(context.Context, string, ...any)     {}
func (l *recordingLogger) Warn(context.Context, string, ...any)     {}
func (l *recordingLogger) Error(context.Context, string, ...any)    {}
func (l *recordingLogger) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	statement, _ := fc()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sqls = append(l.sqls, statement)
}

func (l *recordingLogger) all() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.sqls, "\n")
}

// failingConnector 让 sql.DB 可以构造但任何实际连接都失败。
// 配合 DryRun 时 GORM 不会真正连接，因此可以只观察 SQL 形状。
type failingConnector struct{}

func (failingConnector) Connect(context.Context) (driver.Conn, error) {
	return nil, errors.New("测试不允许真实数据库连接")
}

func (failingConnector) Driver() driver.Driver { return failingDriver{} }

type failingDriver struct{}

func (failingDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("测试不允许真实数据库连接")
}

// newRepositoryForTest 构造 DryRun 的 GORM 会话：不联网、不迁移、不写数据。
func newRepositoryForTest(t *testing.T) (*Repository, *recordingLogger) {
	t.Helper()
	pool := sql.OpenDB(failingConnector{})
	t.Cleanup(func() { _ = pool.Close() })
	record := &recordingLogger{}
	orm, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: pool, SkipInitializeWithVersion: true}), &gorm.Config{
		DisableAutomaticPing: true,
		DryRun:               true,
		Logger:               record,
	})
	if err != nil {
		t.Fatalf("无法构造 DryRun GORM 会话: %v", err)
	}
	return NewRepository(orm), record
}

func TestRepositoryListEnabledQueriesOnlyEnabledInIDOrder(t *testing.T) {
	repository, record := newRepositoryForTest(t)
	topics, err := repository.ListEnabled(context.Background())
	if err != nil {
		t.Fatalf("DryRun 查询不应失败: %v", err)
	}
	if topics == nil {
		t.Fatal("空结果必须是空切片")
	}
	statement := strings.ToLower(record.all())
	if !strings.Contains(statement, "from `topics`") && !strings.Contains(statement, "from topics") {
		t.Fatalf("查询未使用 topics 表: %s", statement)
	}
	if !strings.Contains(statement, "enabled") {
		t.Fatalf("查询未筛选 enabled: %s", statement)
	}
	if !strings.Contains(statement, "order by") || !strings.Contains(statement, "id") {
		t.Fatalf("查询未按 id 排序: %s", statement)
	}
}

func TestRepositoryListEnabledRequiresDatabase(t *testing.T) {
	_, err := NewRepository(nil).ListEnabled(context.Background())
	if !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("缺少数据库时应受控失败: %v", err)
	}
}

func TestRepositoryListEnabledMapsDriverFailure(t *testing.T) {
	// 非 DryRun：连接必然失败，验证仓储层把驱动错误包装为受控错误。
	pool := sql.OpenDB(failingConnector{})
	t.Cleanup(func() { _ = pool.Close() })
	orm, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: pool, SkipInitializeWithVersion: true}), &gorm.Config{
		DisableAutomaticPing: true,
		Logger:               logger.Discard,
	})
	if err != nil {
		t.Fatalf("无法构造 GORM 会话: %v", err)
	}
	_, listErr := NewRepository(orm).ListEnabled(context.Background())
	if listErr == nil {
		t.Fatal("连接失败时不应返回成功")
	}
	if strings.Contains(listErr.Error(), "测试不允许真实数据库连接") {
		t.Fatal("受控错误文本泄露底层驱动原因")
	}
}
