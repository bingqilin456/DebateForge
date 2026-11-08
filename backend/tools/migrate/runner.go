package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"github.com/pressly/goose/v3"
)

var errLocked = errors.New("其他迁移正在执行，请稍后重试")

type migrationError struct {
	message string
	cause   error
}

func (e migrationError) Error() string { return e.message }
func (e migrationError) Unwrap() error { return e.cause }

func safeError(message string, cause error) error {
	var partial *goose.PartialError
	if errors.As(cause, &partial) && partial.Failed != nil && partial.Failed.Source != nil {
		message += fmt.Sprintf(" (version=%d)", partial.Failed.Source.Version)
	}
	var mysqlErr *driver.MySQLError
	if errors.As(cause, &mysqlErr) {
		message += fmt.Sprintf(" (mysql_code=%d)", mysqlErr.Number)
	}
	if errors.Is(cause, errLocked) {
		message += "：" + errLocked.Error()
	} else if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		message += "：操作取消或超时，请核对实际结构和版本记录"
	}
	return migrationError{message: message, cause: cause}
}

func openDatabase(cfg config) (*sql.DB, error) {
	// 所有调用方都必须经过此保护，不能依赖 CLI 已校验。
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	file, err := os.Open(cfg.PasswordFile)
	if err != nil {
		return nil, safeError("读取迁移账户秘密文件失败", err)
	}
	defer file.Close()
	secret, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(secret) > 4096 || len(strings.TrimRight(string(secret), "\r\n")) == 0 {
		return nil, safeError("秘密文件不可读、为空或超长", err)
	}
	connection := driver.NewConfig()
	connection.Net, connection.Addr = "tcp", cfg.Addr
	connection.DBName, connection.User = cfg.Name, cfg.User
	connection.Passwd = strings.TrimRight(string(secret), "\r\n")
	connection.ParseTime, connection.Loc = true, time.UTC
	if err := connection.Apply(driver.Charset("utf8mb4", "utf8mb4_0900_ai_ci")); err != nil {
		return nil, safeError("配置迁移连接字符集失败", err)
	}
	connection.Params = map[string]string{"time_zone": "'+00:00'"}
	connection.Timeout = 3 * time.Second
	connection.ReadTimeout, connection.WriteTimeout = 10*time.Second, 10*time.Second
	connection.Logger = log.New(io.Discard, "", 0)
	connector, err := driver.NewConnector(connection)
	if err != nil {
		return nil, safeError("创建迁移连接失败", err)
	}
	db := sql.OpenDB(connector)
	// 仅 SQL 迁移，锁和 DDL/DML 均使用 Goose 持有的同一 Conn。
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, nil
}

func verifyIdentity(ctx context.Context, conn *sql.Conn, cfg config) error {
	var database, account, version string
	if err := conn.QueryRowContext(ctx, "SELECT DATABASE(), CURRENT_USER(), VERSION()").Scan(&database, &account, &version); err != nil {
		return safeError("验证迁移目标身份失败", err)
	}
	if database != cfg.Name || account != cfg.User+"@%" || version != "8.4.13" {
		return errors.New("实际数据库、迁移账户或 MySQL 版本不符合固定隔离目标")
	}
	return nil
}

type sessionLocker struct {
	cfg      config
	table    string
	versions []int64
}

func (l sessionLocker) SessionLock(ctx context.Context, conn *sql.Conn) error {
	if err := l.cfg.validate(); err != nil {
		return err
	}
	// Goose 创建版本表前校验实际连接；不可只相信 host、环境变量或 DSN。
	if err := verifyIdentity(ctx, conn, l.cfg); err != nil {
		return err
	}
	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 0)", "siyige:migrate:"+l.cfg.Name).Scan(&acquired); err != nil {
		return err
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		return errLocked
	}
	if l.table != "" {
		if err := l.verifyHistory(ctx, conn); err != nil {
			return errors.Join(err, l.SessionUnlock(context.WithoutCancel(ctx), conn))
		}
	}
	return nil
}

func (l sessionLocker) verifyHistory(ctx context.Context, conn *sql.Conn) error {
	var exists int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?", l.table).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return nil // 首次建版本表由 Goose 在同一把锁内完成。
	}
	rows, err := conn.QueryContext(ctx, "SELECT version_id, is_applied FROM `"+l.table+"`")
	if err != nil {
		return err
	}
	defer rows.Close()
	applied := make(map[int64]bool)
	for rows.Next() {
		var version int64
		var active bool
		if err := rows.Scan(&version, &active); err != nil {
			return err
		}
		if _, duplicate := applied[version]; duplicate || !active {
			return errors.New("版本记录重复或包含非前向记录，需人工核对")
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !applied[0] {
		return errors.New("Goose 初始版本记录缺失，需人工核对")
	}
	delete(applied, 0)
	gap := false
	for _, version := range l.versions {
		if !applied[version] {
			gap = true
			continue
		}
		if gap {
			return errors.New("版本记录存在间隙，禁止乱序补跑")
		}
		delete(applied, version)
	}
	if len(applied) != 0 {
		return errors.New("数据库存在本地缺失的迁移版本，需恢复完整迁移文件")
	}
	return nil
}

func (l sessionLocker) SessionUnlock(ctx context.Context, conn *sql.Conn) error {
	// 即使执行被取消，也给予有限的解锁预算；关闭物理连接也会释放 MySQL 锁。
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var released sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", "siyige:migrate:"+l.cfg.Name).Scan(&released); err != nil {
		return err
	}
	if !released.Valid || released.Int64 != 1 {
		return errors.New("数据库迁移锁未成功释放")
	}
	return nil
}

func newProvider(db *sql.DB, cfg config, files fs.FS, table string) (*goose.Provider, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if len(table) > 64 || !regexp.MustCompile(`^[a-z][a-z0-9_]*$`).MatchString(table) {
		return nil, errors.New("版本表名称无效")
	}
	locker := &sessionLocker{cfg: cfg, table: table}
	provider, err := goose.NewProvider(goose.DialectMySQL, db, files,
		goose.WithTableName(table),
		goose.WithDisableGlobalRegistry(true),
		goose.WithSessionLocker(locker),
		goose.WithSlog(slog.New(slog.NewTextHandler(io.Discard, nil))),
	)
	if err != nil {
		return nil, err
	}
	for _, source := range provider.ListSources() {
		locker.versions = append(locker.versions, source.Version)
	}
	return provider, nil
}

func execute(ctx context.Context, provider *goose.Provider, command string, output io.Writer) error {
	switch command {
	case "up":
		applied := 0
		// Up/HasPending 会在取得 SessionLocker 前初始化版本表；逐版本执行
		// 避免该路径，让身份、历史检查、DDL 和登记始终由同一会话锁保护。
		for _, source := range provider.ListSources() {
			_, err := provider.ApplyVersion(ctx, source.Version, true)
			// Goose 将清理错误合并到返回值；仅跳过纯粹的已执行信号，
			// 不能用 errors.Is 吞掉同时发生的解锁/关闭失败。
			if errors.Is(err, goose.ErrAlreadyApplied) {
				var combined interface{ Unwrap() []error }
				if !errors.As(err, &combined) {
					continue
				}
			}
			if err != nil {
				return safeError("迁移失败；停止执行，请核对实际结构及版本记录后恢复", err)
			}
			applied++
		}
		_, err := fmt.Fprintf(output, "applied=%d\n", applied)
		return err
	case "status":
		statuses, err := provider.Status(ctx)
		if err != nil {
			return safeError("读取迁移状态失败", err)
		}
		for _, status := range statuses {
			if _, err := fmt.Fprintf(output, "%05d %s %s %s\n", status.Source.Version, status.State, filepath.Base(status.Source.Path), status.AppliedAt.UTC().Format(time.RFC3339)); err != nil {
				return err
			}
		}
		return nil
	default:
		return errors.New("配置无效：只允许 up/status")
	}
}
