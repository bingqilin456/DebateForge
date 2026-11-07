package platform

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log"
	"os"
	"strings"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DatabaseTimeout 是单次数据库操作的预算；业务层仍须从请求派生 Context。
const DatabaseTimeout = 3 * time.Second

// Database 让入口统一管理 GORM 与其底层连接池的生命周期。
type Database struct {
	ORM  *gorm.DB
	pool *sql.DB
}

// databaseError 保留 cause 供 errors.Is/As 使用，字符串可安全写入启动日志。
type databaseError struct{ cause error }

func (e databaseError) Error() string { return "数据库操作失败" }
func (e databaseError) Unwrap() error { return e.cause }

// OpenDatabase 延迟联网，使数据库故障可由 readiness 报告而不阻止 live。
// 不运行 Ping、迁移或建表；服务仅持有应用账户。
func OpenDatabase(cfg DatabaseConfig) (*Database, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	file, err := os.Open(cfg.PasswordFile)
	if err != nil {
		return nil, databaseError{err}
	}
	defer file.Close()
	secret, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return nil, databaseError{err}
	}
	password := strings.TrimRight(string(secret), "\r\n")
	if password == "" || len(secret) > 4096 {
		return nil, databaseError{errors.New("秘密文件为空或超长")}
	}
	connection := driver.NewConfig()
	connection.Net, connection.Addr = "tcp", cfg.Addr
	connection.DBName, connection.User, connection.Passwd = cfg.Name, cfg.User, password
	connection.ParseTime, connection.Loc = true, time.UTC
	connection.Collation = "utf8mb4_0900_ai_ci"
	connection.Params = map[string]string{"charset": "utf8mb4", "time_zone": "'+00:00'"}
	connection.Timeout, connection.ReadTimeout, connection.WriteTimeout = DatabaseTimeout, DatabaseTimeout, DatabaseTimeout
	// 驱动和 ORM 的原始错误可能含服务器信息或业务 SQL，统一由边界输出受控码。
	connection.Logger = log.New(io.Discard, "", 0)
	connector, err := driver.NewConnector(connection)
	if err != nil {
		return nil, databaseError{err}
	}
	pool := sql.OpenDB(connector)
	pool.SetMaxOpenConns(10)
	pool.SetMaxIdleConns(5)
	pool.SetConnMaxLifetime(3 * time.Minute)
	pool.SetConnMaxIdleTime(time.Minute)
	orm, err := gorm.Open(mysql.New(mysql.Config{Conn: pool, SkipInitializeWithVersion: true}), &gorm.Config{
		DisableAutomaticPing: true,
		Logger:               logger.Default.LogMode(logger.Silent),
		NowFunc:              func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		_ = pool.Close()
		return nil, databaseError{err}
	}
	return &Database{ORM: orm, pool: pool}, nil
}

// PingContext 保留取消语义，且不将驱动原始错误交给 HTTP 或日志。
func (db *Database) PingContext(ctx context.Context) error {
	if err := db.pool.PingContext(ctx); err != nil {
		return databaseError{err}
	}
	return nil
}

// Close 应在 HTTP 优雅退出之后调用，允许在途请求先释放连接。
func (db *Database) Close() error {
	if err := db.pool.Close(); err != nil {
		return databaseError{err}
	}
	return nil
}
