package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestExplicitTargetAndCommand(t *testing.T) {
	// 服务/Goose 环境配置不能改变显式迁移目标，尤其不能注入口令或任意 DSN。
	t.Setenv("APP_ENV", "prod")
	t.Setenv("DB_ADDR", "db.example:3306")
	t.Setenv("DB_NAME", "production")
	t.Setenv("DB_USER", "root")
	t.Setenv("GOOSE_DBSTRING", "untrusted-dsn")
	secret := filepath.Join(t.TempDir(), "missing-secret")
	cfg, err := parseConfig([]string{"-env", "test", "-password-file", secret, "status"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "127.0.0.1:13307" || cfg.Name != "siyige_test" || cfg.User != "siyige_test_migrator" || cfg.Command != "status" {
		t.Fatalf("错误的测试目标: %+v", cfg)
	}
	cfg, err = parseConfig([]string{"-env", "dev", "-password-file", secret, "up"})
	if err != nil || cfg.Addr != "127.0.0.1:13306" || cfg.User != "siyige_dev_migrator" {
		t.Fatal("开发目标未固定到独立迁移账户", err)
	}
}

func TestRejectsUnsafeInputBeforeReadingSecret(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "missing-secret")
	for _, args := range [][]string{
		{"-password-file", secret, "up"},
		{"-env", "prod", "-password-file", secret, "up"},
		{"-env", "test", "-password-file", "relative.txt", "up"},
		{"-env", "test", "-password-file", secret, "down"},
		{"-env", "test", "-password-file", secret, "reset"},
		{"-env", "test", "-password-file", secret, "redo"},
		{"-env", "test", "-password-file", secret, "up", "extra"},
		{"-env", "test", "-password-file", secret, "-dsn", "secret-value", "up"},
	} {
		var output bytes.Buffer
		err := run(context.Background(), args, &output)
		if err == nil || !strings.Contains(err.Error(), "配置") || strings.Contains(err.Error(), "secret-value") {
			t.Fatalf("未在配置阶段安全拒绝 %q: %v", args, err)
		}
		if output.Len() != 0 {
			t.Fatal("拒绝路径泄露了参数或执行了操作")
		}
	}
}

func TestTargetCannotBeRedirected(t *testing.T) {
	base, err := parseConfig([]string{"-env", "test", "-password-file", filepath.Join(t.TempDir(), "secret"), "up"})
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*config){
		func(c *config) { c.Addr = "127.0.0.1:13306" },
		func(c *config) { c.Addr = "db.example:3306" },
		func(c *config) { c.Name = "siyige_dev" },
		func(c *config) { c.User = "siyige_test_app" },
		func(c *config) { c.User = "root" },
	} {
		cfg := base
		mutate(&cfg)
		if cfg.validate() == nil {
			t.Fatalf("未拒绝错误目标: %+v", cfg)
		}
	}
}

func TestDeployTargetIsFixed(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "missing-secret")
	t.Setenv("DB_ADDR", "127.0.0.1:13307")
	t.Setenv("DB_NAME", "siyige_test")
	cfg, err := parseConfig([]string{"-env", "deploy", "-password-file", secret, "up"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "mysql:3306" || cfg.Name != "siyige_deploy" || cfg.User != "siyige_deploy_migrator" {
		t.Fatal("部署迁移未固定到内部库和专用账户")
	}
	for _, mutate := range []func(*config){
		func(c *config) { c.Addr = "127.0.0.1:13307" },
		func(c *config) { c.Name = "siyige_test" },
		func(c *config) { c.User = "siyige_deploy_app" },
	} {
		invalid := cfg
		mutate(&invalid)
		if invalid.validate() == nil {
			t.Fatal("接受了错误的部署迁移目标")
		}
	}
}

func TestDemoTargetIsFixed(t *testing.T) {
	cfg, err := parseConfig([]string{"-env", "demo", "-password-file", filepath.Join(t.TempDir(), "missing"), "status"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "mysql:3306" || cfg.Name != "siyige_demo" || cfg.User != "siyige_demo_migrator" {
		t.Fatal("wrong demo target")
	}
	cfg.Name = "siyige_test"
	if cfg.validate() == nil {
		t.Fatal("demo migration redirected")
	}
}
