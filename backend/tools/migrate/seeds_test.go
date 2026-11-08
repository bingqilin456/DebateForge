package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	driver "github.com/go-sql-driver/mysql"
)

// 独立的字面量期望用于发现漏题、中文损坏或错误 enabled，不能从 SQL 生成期望。
func assertTopicSeeds(t *testing.T, query *sql.DB) {
	t.Helper()
	for i, want := range [][2]string{
		{"大学生应该广泛探索不同领域还是尽早专注一个方向", "正方主张广泛探索不同领域，反方主张尽早专注一个方向。请比较学习机会、时间成本与长期发展。"},
		{"团队决策应该优先追求共识还是效率", "正方主张优先追求共识，反方主张优先追求效率。请讨论信息质量、执行成本与成员参与。"},
		{"学习新知识应该先系统阅读还是先动手实践", "正方主张先系统阅读，反方主张先动手实践。请比较理解深度、反馈速度与试错成本。"},
		{"城市公共空间应该优先满足便利性还是保留自然环境", "正方主张优先满足便利性，反方主张优先保留自然环境。请讨论日常需求、生态价值与资源分配。"},
		{"评价一项工作应该更看重过程还是结果", "正方主张更看重过程，反方主张更看重结果。请比较评价公平性、激励效果与责任承担。"},
	} {
		var title, description string
		var enabled bool
		var created bool
		if err := query.QueryRowContext(context.Background(), "SELECT title,description,enabled,created_at IS NOT NULL FROM topics WHERE id=?", i+1).Scan(&title, &description, &enabled, &created); err != nil {
			t.Fatalf("缺失种子 id=%d: %v", i+1, err)
		}
		if title != want[0] || description != want[1] || !enabled || !created {
			t.Fatalf("种子 id=%d 内容、启用标志或时间不符", i+1)
		}
	}
}

func TestMySQLSeedCollisionPreservesData(t *testing.T) {
	db, cfg := integrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	suffix := fmt.Sprintf("%x", time.Now().UnixNano())
	table, history := "t02_seed_probe_"+suffix, "t02_seed_versions_"+suffix
	t.Logf("保留种子冲突证据: %s / %s", table, history)
	if _, err := db.ExecContext(ctx, "CREATE TABLE `"+table+"` LIKE topics"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO `"+table+"` (id,title,description,enabled) VALUES (3,'existing-topic','do-not-overwrite',0)"); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("../../migrations/00010_seed_topics.sql")
	if err != nil {
		t.Fatal(err)
	}
	files := fstest.MapFS{"00001_seeds.sql": {Data: []byte(strings.ReplaceAll(string(source), "INSERT INTO topics", "INSERT INTO `"+table+"`"))}}
	for range 2 {
		provider, err := newProvider(db, cfg, files, history)
		if err != nil {
			t.Fatal(err)
		}
		err = execute(ctx, provider, "up", &bytes.Buffer{})
		var mysqlErr *driver.MySQLError
		if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1062 {
			t.Fatal("种子 ID 冲突没有失败", err)
		}
		if len(versionHistory(t, db, history)) != 1 {
			t.Fatal("冲突版本被登记成功")
		}
		var count int
		var title, description string
		var enabled bool
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM `"+table+"`").Scan(&count); err != nil || count != 1 {
			t.Fatal("冲突留下部分种子", err)
		}
		if err := db.QueryRowContext(ctx, "SELECT title,description,enabled FROM `"+table+"` WHERE id=3").Scan(&title, &description, &enabled); err != nil || title != "existing-topic" || description != "do-not-overwrite" || enabled {
			t.Fatal("冲突覆盖既有数据", err)
		}
	}
}

func TestMySQLTopicSeeds(t *testing.T) {
	db, cfg := integrationDB(t)
	assertTopicSeeds(t, db)
	before := versionHistory(t, db, "goose_db_version")
	counts := rowCounts(t, db)
	for range 2 {
		provider, err := newProvider(db, cfg, os.DirFS("../../migrations"), "goose_db_version")
		if err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		if err := execute(context.Background(), provider, "up", &output); err != nil || output.String() != "applied=0\n" {
			t.Fatal("种子重跑不是空操作", err)
		}
		assertTopicSeeds(t, db)
		if !reflect.DeepEqual(before, versionHistory(t, db, "goose_db_version")) || counts != rowCounts(t, db) {
			t.Fatal("重跑改变历史或行数")
		}
	}
}
