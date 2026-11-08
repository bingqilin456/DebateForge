package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	driver "github.com/go-sql-driver/mysql"
)

func TestMySQLExactEnumMigrationRejectsExistingInvalidData(t *testing.T) {
	db, cfg := integrationDB(t)
	for _, tc := range []struct{ file, table, columns, values string }{
		{"00007_exact_debate_enums.sql", "debates", "user_stance VARCHAR(3) CHARACTER SET ascii COLLATE ascii_bin, status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin", "'pro','active '"},
		{"00008_exact_turn_status.sql", "debate_turns", "status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin", "'generating '"},
		{"00009_exact_message_role.sql", "messages", "role VARCHAR(9) CHARACTER SET ascii COLLATE ascii_bin", "'user '"},
	} {
		t.Run(tc.table, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			suffix := fmt.Sprintf("%x", time.Now().UnixNano())
			table, history := "t02_enum_probe_"+suffix, "t02_enum_versions_"+suffix
			t.Logf("保留异常枚举证据: %s / %s", table, history)
			if _, err := db.ExecContext(ctx, "CREATE TABLE `"+table+"` ("+tc.columns+") ENGINE=InnoDB"); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(ctx, "INSERT INTO `"+table+"` VALUES ("+tc.values+")"); err != nil {
				t.Fatal(err)
			}
			source, err := os.ReadFile("../../migrations/" + tc.file)
			if err != nil {
				t.Fatal(err)
			}
			// 只重定向表名/约束名，执行实际迁移中的 CHECK 表达式。
			sqlText := strings.ReplaceAll(string(source), "ALTER TABLE "+tc.table, "ALTER TABLE `"+table+"`")
			sqlText = strings.ReplaceAll(sqlText, "ck_"+tc.table+"_", "ck_"+suffix+"_")
			provider, err := newProvider(db, cfg, fstest.MapFS{"00001_exact.sql": {Data: []byte(sqlText)}}, history)
			if err != nil {
				t.Fatal(err)
			}
			err = execute(ctx, provider, "up", &bytes.Buffer{})
			var mysqlErr *driver.MySQLError
			if !errors.As(err, &mysqlErr) || mysqlErr.Number != 3819 {
				t.Fatal("已有异常数据未阻止迁移", err)
			}
			if len(versionHistory(t, db, history)) != 1 {
				t.Fatal("失败约束被登记成功")
			}
			var count int
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.table_constraints WHERE table_schema=DATABASE() AND table_name=? AND constraint_type='CHECK'", table).Scan(&count); err != nil || count != 0 {
				t.Fatal("失败 ALTER 留下部分 CHECK", err)
			}
			column := "status"
			if tc.table == "messages" {
				column = "role"
			}
			var value string
			if err := db.QueryRowContext(ctx, "SELECT "+column+" FROM `"+table+"`").Scan(&value); err != nil || !strings.HasSuffix(value, " ") {
				t.Fatal("迁移擅自修复了异常数据", err)
			}
		})
	}
}
