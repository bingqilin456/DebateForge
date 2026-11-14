package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"github.com/pressly/goose/v3"
)

// 显式启用才会读取秘密或联网；测试目标不可由开发环境变量改变。
func integrationDB(t *testing.T) (*sql.DB, config) {
	t.Helper()
	if os.Getenv("SIYIGE_MIGRATION_TEST") == "" {
		t.Skip("需 SIYIGE_MIGRATION_TEST=1；默认不读秘密、不连接 MySQL")
	}
	if os.Getenv("SIYIGE_MIGRATION_TEST") != "1" {
		t.Fatal("SIYIGE_MIGRATION_TEST 只允许 1")
	}
	cfg, err := parseConfig([]string{"-env", "test", "-password-file", os.Getenv("SIYIGE_MIGRATION_PASSWORD_FILE"), "up"})
	if err != nil {
		t.Fatal(err)
	}
	db, err := openDatabase(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("无法连接固定测试库 (%T): %v", err, safeError("连接失败", err))
	}
	defer conn.Close()
	if err := verifyIdentity(ctx, conn, cfg); err != nil {
		t.Fatal(err)
	}
	wrong := cfg
	wrong.Env, wrong.Addr, wrong.Name, wrong.User = "dev", "127.0.0.1:13306", "siyige_dev", "siyige_dev_migrator"
	if err := verifyIdentity(ctx, conn, wrong); err == nil {
		t.Fatal("实际测试连接被误认为开发连接")
	}
	var zone, charset, collation string
	if err := conn.QueryRowContext(ctx, "SELECT @@session.time_zone, @@character_set_connection, @@collation_connection").Scan(&zone, &charset, &collation); err != nil || zone != "+00:00" || charset != "utf8mb4" || collation != "utf8mb4_0900_ai_ci" {
		t.Fatal("迁移会话未按 UTC/utf8mb4 连接", err)
	}
	return db, cfg
}

func TestMySQLInitialSchemaAndRepeat(t *testing.T) {
	db, cfg := integrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	provider, err := newProvider(db, cfg, os.DirFS("../../migrations"), "goose_db_version")
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := execute(ctx, provider, "up", &output); err != nil {
		t.Fatal(err)
	}
	t.Log(strings.TrimSpace(output.String()))
	before := versionHistory(t, db, "goose_db_version")
	if len(before) != 14 { // Goose 初始版本 0 + 六个建表 + 三个约束 + 一个种子版本 + 一个总结表版本 + 一个轮次与赛制版本 + 一个策略表版本。
		t.Fatalf("版本历史条数 = %d，期望 14", len(before))
	}
	var tables int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name IN ('users','auth_sessions','topics','debates','debate_turns','messages','debate_summaries','debate_strategies') AND engine='InnoDB' AND table_collation='utf8mb4_0900_ai_ci'`).Scan(&tables); err != nil || tables != 8 {
		t.Fatal("八表的存储引擎/字符集不符合冻结设计", err)
	}
	var ids, foreignKeys, historyIDs int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name IN ('users','auth_sessions','topics','debates','debate_turns','messages','debate_summaries','debate_strategies') AND column_name='id' AND column_type='bigint unsigned' AND column_key='PRI' AND extra='auto_increment'`).Scan(&ids); err != nil || ids != 8 {
		t.Fatal("业务主键未使用自增 BIGINT UNSIGNED", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.referential_constraints WHERE constraint_schema=DATABASE() AND table_name IN ('auth_sessions','debates','debate_turns','messages','debate_summaries','debate_strategies') AND delete_rule='RESTRICT' AND update_rule='RESTRICT'`).Scan(&foreignKeys); err != nil || foreignKeys != 7 {
		t.Fatal("冻结的七条外键关系不完整", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(DISTINCT version_id) FROM goose_db_version WHERE is_applied=1 AND version_id BETWEEN 1 AND 13").Scan(&historyIDs); err != nil || historyIDs != 13 {
		t.Fatal("十三个版本未逐一记录成功", err)
	}
	counts := rowCounts(t, db)
	// 每次重建 provider，模拟独立进程重跑，而非复用内存缓存。
	provider, err = newProvider(db, cfg, os.DirFS("../../migrations"), "goose_db_version")
	if err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := execute(ctx, provider, "up", &output); err != nil || !strings.Contains(output.String(), "applied=0") {
		t.Fatal("重复执行没有成为空操作", err)
	}
	if fmt.Sprint(before) != fmt.Sprint(versionHistory(t, db, "goose_db_version")) || counts != rowCounts(t, db) {
		t.Fatal("重复执行改变了历史或既有数据")
	}
	output.Reset()
	if err := execute(ctx, provider, "status", &output); err != nil || strings.Count(output.String(), "applied") != 13 {
		t.Fatal("状态未显示十三个成功版本", err)
	}
	assertSchemaConstraints(t, db)
}

func versionHistory(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query("SELECT id, version_id, is_applied, tstamp FROM `" + table + "` ORDER BY id")
	if err != nil {
		t.Fatal("无法读取版本记录")
	}
	defer rows.Close()
	var history []string
	for rows.Next() {
		var id, version int64
		var applied bool
		var stamp time.Time
		if err := rows.Scan(&id, &version, &applied, &stamp); err != nil {
			t.Fatal(err)
		}
		history = append(history, fmt.Sprintf("%d/%d/%t/%s", id, version, applied, stamp.Format(time.RFC3339Nano)))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return history
}

func rowCounts(t *testing.T, db *sql.DB) [8]int64 {
	t.Helper()
	var counts [8]int64
	for i, table := range []string{"users", "auth_sessions", "topics", "debates", "debate_turns", "messages", "debate_summaries", "debate_strategies"} {
		if err := db.QueryRow("SELECT COUNT(*) FROM `" + table + "`").Scan(&counts[i]); err != nil {
			t.Fatal("无法核对业务行数")
		}
	}
	return counts
}

func assertSchemaConstraints(t *testing.T, db *sql.DB) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback() // 只回滚本测试夹具，不清空任何既有数据。
	name := fmt.Sprintf("migrate_%x", time.Now().UnixNano())
	insert := func(query string, args ...any) int64 {
		t.Helper()
		result, err := tx.Exec(query, args...)
		if err != nil {
			t.Fatalf("夹具写入失败: %v", err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	reject := func(code uint16, query string, args ...any) {
		t.Helper()
		_, err := tx.Exec(query, args...)
		var mysqlErr *driver.MySQLError
		if !errors.As(err, &mysqlErr) || mysqlErr.Number != code {
			t.Fatalf("期望 MySQL %d，得到 %v", code, err)
		}
	}
	user := insert("INSERT INTO users(username,password_hash) VALUES (?,?)", name, "fixture-hash")
	reject(1062, "INSERT INTO users(username,password_hash) VALUES (?,?)", strings.ToUpper(name), "fixture-hash")
	topic := insert("INSERT INTO topics(title,description) VALUES (?,?)", "中文辩题🌱", "只存在于回滚事务")
	debateSQL := "INSERT INTO debates(user_id,topic_id,topic_title_snapshot,topic_description_snapshot,user_stance) VALUES (?,?,?,?,?)"
	debate := insert(debateSQL, user, topic, "中文辩题🌱", "快照", "pro")
	reject(1452, debateSQL, 0, topic, "x", "y", "pro")
	reject(3819, "UPDATE debates SET completed_rounds=7 WHERE id=?", debate)
	reject(3819, "UPDATE debates SET mode='other' WHERE id=?", debate)
	reject(3819, "UPDATE debates SET ai_style='other' WHERE id=?", debate)
	reject(3819, "UPDATE debates SET user_stance='bad' WHERE id=?", debate)
	reject(3819, "UPDATE debates SET status='other' WHERE id=?", debate)
	turnSQL := "INSERT INTO debate_turns(debate_id,round_no,client_request_id,status,lease_expires_at) VALUES (?,?,?,?,'2026-11-08 00:00:00')"
	turn := insert(turnSQL, debate, 1, "550e8400-e29b-41d4-a716-446655440000", "generating")
	var unknownUsage bool
	if err := tx.QueryRow("SELECT input_tokens IS NULL AND output_tokens IS NULL FROM debate_turns WHERE id=?", turn).Scan(&unknownUsage); err != nil || !unknownUsage {
		t.Fatal("未返回用量时必须保留 NULL，而非伪造为 0", err)
	}
	reject(1062, turnSQL, debate, 1, "550e8400-e29b-41d4-a716-446655440001", "generating")
	reject(1062, turnSQL, debate, 2, "550e8400-e29b-41d4-a716-446655440000", "generating")
	reject(3819, turnSQL, debate, 7, "550e8400-e29b-41d4-a716-446655440002", "generating")
	reject(3819, "UPDATE debate_turns SET attempt_version=0 WHERE id=?", turn)
	reject(3819, "UPDATE debate_turns SET status='other' WHERE id=?", turn)
	summarySQL := "INSERT INTO debate_summaries(debate_id,status,lease_expires_at) VALUES (?,?,'2026-11-08 00:00:00')"
	summary := insert(summarySQL, debate, "generating")
	var unknownSummary bool
	if err := tx.QueryRow("SELECT schema_version IS NULL AND content_json IS NULL AND input_tokens IS NULL AND output_tokens IS NULL FROM debate_summaries WHERE id=?", summary).Scan(&unknownSummary); err != nil || !unknownSummary {
		t.Fatal("总结未完成时可选字段与未知用量必须保留 NULL，而非伪造为默认值", err)
	}
	reject(1062, summarySQL, debate, "generating")
	reject(1452, summarySQL, 0, "generating")
	reject(3819, "UPDATE debate_summaries SET attempt_version=0 WHERE id=?", summary)
	reject(3819, "UPDATE debate_summaries SET status='other' WHERE id=?", summary)
	strategySQL := "INSERT INTO debate_strategies(debate_id,status) VALUES (?,?)"
	strategy := insert(strategySQL, debate, "generating")
	var unknownStrategy bool
	if err := tx.QueryRow("SELECT schema_version IS NULL AND content_json IS NULL AND input_tokens IS NULL AND output_tokens IS NULL FROM debate_strategies WHERE id=?", strategy).Scan(&unknownStrategy); err != nil || !unknownStrategy {
		t.Fatal("策略未完成的字段与未知用量必须为 NULL", err)
	}
	reject(1062, strategySQL, debate, "generating")
	reject(1452, strategySQL, 0, "generating")
	reject(3819, "UPDATE debate_strategies SET attempt_version=0 WHERE id=?", strategy)

	insert("INSERT INTO messages(turn_id,role,content) VALUES (?,?,?)", turn, "user", "中文发言🌱")
	reject(1062, "INSERT INTO messages(turn_id,role,content) VALUES (?,?,?)", turn, "user", "重复")
	reject(3819, "INSERT INTO messages(turn_id,role,content) VALUES (?,?,?)", turn, "system", "非法角色")
	// 必须拒绝 PAD SPACE 反例，不能只测与合法值完全不同的字符串。
	for _, check := range []struct {
		table, column string
		id            int64
		valid         []string
		invalid       []string
	}{
		{"debates", "user_stance", debate, []string{"pro", "con"}, []string{"PRO", "Con", "", " pr"}},
		{"debates", "mode", debate, []string{"quick_3", "standard_4"}, []string{"QUICK_3", "Standard_4", "", " quick_3", "quick_3 "}},
		{"debates", "ai_style", debate, []string{"logical", "aggressive", "empathic", "data"}, []string{"LOGICAL", "Logical", "", " logical", "logical "}},
		{"debates", "status", debate, []string{"active", "completed"}, []string{"active ", "completed ", "ACTIVE", "Active", "", " active", "active\t"}},
		{"debate_turns", "status", turn, []string{"generating", "completed", "failed"}, []string{"generating ", "completed ", "failed ", "GENERATING", "Failed", "", " failed", "failed\t"}},
		{"debate_summaries", "status", summary, []string{"generating", "completed", "failed"}, []string{"generating ", "completed ", "failed ", "GENERATING", "Failed", "", " failed", "failed\t"}},
		{"debate_strategies", "status", strategy, []string{"generating", "completed", "failed"}, []string{"generating ", "completed ", "failed ", "GENERATING", "Failed", "", " failed", "failed\t"}},
		{"messages", "role", 0, []string{"user", "assistant"}, []string{"user ", "USER", "Assistant", "", " user", "user\t"}},
	} {
		where, id := "id", check.id
		if check.table == "messages" {
			where, id = "turn_id", turn
		}
		query := "UPDATE " + check.table + " SET " + check.column + "=? WHERE " + where + "=?"
		for _, value := range check.valid {
			if _, err := tx.Exec(query, value, id); err != nil {
				t.Fatalf("合法 %s.%s=%q 被拒: %v", check.table, check.column, value, err)
			}
		}
		for _, value := range check.invalid {
			_, err := tx.Exec(query, value, id)
			var mysqlErr *driver.MySQLError
			if !errors.As(err, &mysqlErr) || mysqlErr.Number != 3819 {
				t.Fatalf("%s.%s=%q 期望 MySQL 3819，得到 %v", check.table, check.column, value, err)
			}
		}
	}
	// MySQL 对超出 VARCHAR 宽度的空格先截断再执行 CHECK；此边界只能保证
	// 存储值精确，原始请求的词法拒绝必须在业务入口完成，不能擅自改冻结类型。
	if _, err := tx.Exec("UPDATE messages SET role='assistant ' WHERE turn_id=?", turn); err != nil {
		t.Fatal(err)
	}
	var role string
	if err := tx.QueryRow("SELECT role FROM messages WHERE turn_id=?", turn).Scan(&role); err != nil || role != "assistant" {
		t.Fatal("VARCHAR 边界留下非规范角色", err)
	}
	token := strings.Repeat("a", 32)
	insert("INSERT INTO auth_sessions(user_id,token_hash,csrf_token,expires_at) VALUES (?,?,?,'2026-11-09 00:00:00')", user, token, "fixture-csrf")
	reject(1062, "INSERT INTO auth_sessions(user_id,token_hash,csrf_token,expires_at) VALUES (?,?,?,'2026-11-09 00:00:00')", user, token, "fixture-csrf")
	var text string
	if err := tx.QueryRow("SELECT content FROM messages WHERE turn_id=?", turn).Scan(&text); err != nil || text != "中文发言🌱" {
		t.Fatal("utf8mb4 内容未保真", err)
	}
}

func TestMySQLEnumExactValues(t *testing.T) {
	db, _ := integrationDB(t)
	assertSchemaConstraints(t, db)
}

func TestMySQLFailureRecoveryWithoutDeletingData(t *testing.T) {
	db, cfg := integrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	suffix := fmt.Sprintf("%x", time.Now().UnixNano())
	history, probe := "t02_failure_versions_"+suffix, "t02_failure_probe_"+suffix
	// 探针保留供审查，绝不 DROP/TRUNCATE 或重建现有数据库。
	t.Logf("保留故障证据表: %s, %s", history, probe)
	fixture := fstest.MapFS{
		"00001_ok.sql":      {Data: []byte("-- +goose Up\nSELECT 1;\n")},
		"00002_partial.sql": {Data: []byte("-- +goose NO TRANSACTION\n-- +goose Up\nCREATE TABLE " + probe + " (id BIGINT PRIMARY KEY);\nSIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='private-failure-sentinel';\n")},
	}
	apply := func() error {
		provider, err := newProvider(db, cfg, fixture, history)
		if err != nil {
			return err
		}
		return execute(ctx, provider, "up", &bytes.Buffer{})
	}
	if err := apply(); err == nil || !strings.Contains(err.Error(), "version=2") || strings.Contains(err.Error(), "private-failure-sentinel") {
		t.Fatal("错误 SQL 未安全失败", err)
	}
	if len(versionHistory(t, db, history)) != 2 {
		t.Fatal("失败版本被误登记成功")
	}
	var rows int
	if err := db.QueryRow("SELECT COUNT(*) FROM `" + probe + "`").Scan(&rows); err != nil {
		t.Fatal("未观察到 MySQL DDL 隐式提交窗口", err)
	}
	if err := apply(); err == nil {
		t.Fatal("部分 DDL 被静默跳过")
	}
	// 仅修订隔离的内存夹具：模拟审查实际结构后批准的前向修复；不改正式迁移。
	fixture["00002_partial.sql"] = &fstest.MapFile{Data: []byte("-- +goose Up\nSELECT 1;\n")}
	if err := apply(); err != nil {
		t.Fatal("修复后不能继续", err)
	}
	if len(versionHistory(t, db, history)) != 3 {
		t.Fatal("恢复后没有正确登记成功版本")
	}
	if err := apply(); err != nil || len(versionHistory(t, db, history)) != 3 {
		t.Fatal("恢复后重复执行不安全", err)
	}
	delete(fixture, "00002_partial.sql") // 本地缺失已执行文件必须拒绝，不能只报告较低版本。
	provider, err := newProvider(db, cfg, fixture, history)
	if err != nil {
		t.Fatal(err)
	}
	if err := execute(ctx, provider, "status", &bytes.Buffer{}); err == nil {
		t.Fatal("缺失已执行迁移文件却仍报告成功")
	}
}

func TestMySQLConcurrentMigrationIsRejected(t *testing.T) {
	db, cfg := integrationDB(t)
	other, err := openDatabase(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := other.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	locker := sessionLocker{cfg: cfg}
	if err := locker.SessionLock(ctx, conn); err != nil {
		t.Fatal(err)
	}
	provider, err := newProvider(db, cfg, os.DirFS(filepath.Join("..", "..", "migrations")), "goose_db_version")
	if err != nil {
		t.Fatal(err)
	}
	err = execute(ctx, provider, "up", &bytes.Buffer{})
	if !errors.Is(err, errLocked) {
		t.Fatal("第二入口没有被数据库会话锁拒绝", err)
	}
	if err := locker.SessionUnlock(ctx, conn); err != nil {
		t.Fatal(err)
	}
	if err := execute(ctx, provider, "up", &bytes.Buffer{}); err != nil {
		t.Fatal("解锁后不能恢复", err)
	}
}

type unlockFailure struct {
	sessionLocker
	cause error
}

func (l unlockFailure) SessionUnlock(ctx context.Context, conn *sql.Conn) error {
	return errors.Join(l.sessionLocker.SessionUnlock(ctx, conn), l.cause)
}

func TestMySQLUnlockFailureIsNotSkipped(t *testing.T) {
	db, cfg := integrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	baseline, err := newProvider(db, cfg, os.DirFS("../../migrations"), "goose_db_version")
	if err != nil {
		t.Fatal(err)
	}
	if err := execute(ctx, baseline, "up", &bytes.Buffer{}); err != nil {
		t.Fatal("无法建立已执行版本的测试前提", err)
	}
	// 真实解锁之后注入传输/清理失败，验证 ErrAlreadyApplied 与清理错误合并时
	// 不能把整个错误当成可跳过的“已经执行”。
	cause := errors.New("test unlock transport failure")
	provider, err := goose.NewProvider(goose.DialectMySQL, db, os.DirFS("../../migrations"),
		goose.WithSessionLocker(unlockFailure{sessionLocker: sessionLocker{cfg: cfg}, cause: cause}))
	if err != nil {
		t.Fatal(err)
	}
	if err := execute(ctx, provider, "up", &bytes.Buffer{}); !errors.Is(err, cause) {
		t.Fatal("跳过已执行版本时吞掉了解锁错误", err)
	}
}
