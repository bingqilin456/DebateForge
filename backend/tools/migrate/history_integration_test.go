package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestMySQLHistoryProtection(t *testing.T) {
	db, cfg := integrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	baseline, err := newProvider(db, cfg, os.DirFS("../../migrations"), "goose_db_version")
	if err != nil {
		t.Fatal(err)
	}
	if err := execute(ctx, baseline, "up", &bytes.Buffer{}); err != nil {
		t.Fatal("无法建立正式迁移基线", err)
	}
	for _, tc := range []struct {
		name, records string
		valid         bool
	}{
		{"gap", "(0,1),(2,1)", false},
		{"duplicate", "(0,1),(1,1),(1,1)", false},
		{"non_forward", "(0,1),(1,0)", false},
		{"missing_zero", "(1,1)", false},
		{"missing_file", "(0,1),(1,1),(3,1)", false},
		{"valid_prefix", "(0,1),(1,1)", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			suffix := fmt.Sprintf("%x", time.Now().UnixNano())
			history, probe := "t02_history_"+suffix, "t02_history_probe_"+suffix
			t.Logf("保留历史保护证据: %s / %s", history, probe)
			// 仅创建本测试独有的证据表，不改正式 Goose 历史。
			if _, err := db.ExecContext(ctx, "CREATE TABLE `"+history+"` (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, version_id BIGINT NOT NULL, is_applied BOOLEAN NOT NULL, tstamp TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)"); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(ctx, "INSERT INTO `"+history+"` (version_id,is_applied) VALUES "+tc.records); err != nil {
				t.Fatal(err)
			}
			files := fstest.MapFS{
				"00001_ok.sql":    {Data: []byte("-- +goose Up\nSELECT 1;\n")},
				"00002_probe.sql": {Data: []byte("-- +goose NO TRANSACTION\n-- +goose Up\nCREATE TABLE " + probe + " (id BIGINT PRIMARY KEY);\n")},
			}
			before := versionHistory(t, db, history)
			formal := versionHistory(t, db, "goose_db_version")
			counts := rowCounts(t, db)
			for _, command := range []string{"status", "up"} {
				provider, err := newProvider(db, cfg, files, history)
				if err != nil {
					t.Fatal(err)
				}
				var output bytes.Buffer
				err = execute(ctx, provider, command, &output)
				if tc.valid {
					if err != nil {
						t.Fatal("合法前缀被拒", err)
					}
					if command == "status" && !strings.Contains(output.String(), "00002 pending") {
						t.Fatal(output.String())
					}
					if command == "up" && output.String() != "applied=1\n" {
						t.Fatal(output.String())
					}
				} else {
					if err == nil {
						t.Fatalf("%s 接受异常历史", command)
					}
					if !reflect.DeepEqual(before, versionHistory(t, db, history)) {
						t.Fatal("拒绝后仍改写历史")
					}
				}
				var exists int
				if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?", probe).Scan(&exists); err != nil {
					t.Fatal(err)
				}
				want := 0
				if tc.valid && command == "up" {
					want = 1
				}
				if exists != want {
					t.Fatal("DDL 执行边界错误")
				}
				if !reflect.DeepEqual(formal, versionHistory(t, db, "goose_db_version")) || counts != rowCounts(t, db) {
					t.Fatal("测试改写正式历史或业务数据")
				}
				// 历史拒绝后锁也必须释放，下一次命令才可安全重试。
				var free int
				if err := db.QueryRowContext(ctx, "SELECT IS_FREE_LOCK(?)", "siyige:migrate:"+cfg.Name).Scan(&free); err != nil || free != 1 {
					t.Fatal("历史检查后遗留会话锁", err)
				}
			}
			if tc.valid && len(versionHistory(t, db, history)) != 3 {
				t.Fatal("合法前缀未登记新版本")
			}
		})
	}
}
