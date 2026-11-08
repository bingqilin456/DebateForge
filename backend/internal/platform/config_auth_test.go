package platform

import (
	"os"
	"testing"
)

func TestLoadConfigRequiresAuthOrigin(t *testing.T) {
	setDatabaseEnv(t)
	t.Setenv("AUTH_ORIGIN", "") // Unsetenv 之后仍恢复调用方原来的环境。
	// 未设置时使用默认开发 Origin：只允许完整 http://127.0.0.1:5173。
	if err := os.Unsetenv("AUTH_ORIGIN"); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthOrigin != "http://127.0.0.1:5173" {
		t.Fatalf("默认 AUTH_ORIGIN 长度=%d，期望默认值", len(cfg.AuthOrigin))
	}
}

func TestLoadConfigRejectsInvalidAuthOrigin(t *testing.T) {
	for name, value := range map[string]string{
		"path":     "http://127.0.0.1:5173/",
		"query":    "http://127.0.0.1:5173/?a=1",
		"userinfo": "http://user@127.0.0.1:5173",
		"non loop": "http://192.0.2.1:5173",
		"hostname": "http://localhost:5173",
		"scheme":   "ftp://127.0.0.1:5173",
		"empty":    "",
		"wildcard": "http://0.0.0.0:5173",
		"fragment": "http://127.0.0.1:5173#x",
	} {
		t.Run(name, func(t *testing.T) {
			setDatabaseEnv(t)
			t.Setenv("AUTH_ORIGIN", value)
			if _, err := LoadConfig(); err == nil {
				t.Fatalf("接受了非法 AUTH_ORIGIN（长度=%d）", len(value))
			}
		})
	}
}

func TestLoadConfigAcceptsHTTPSOrigin(t *testing.T) {
	setDatabaseEnv(t)
	for _, origin := range []string{"https://siyige.example", "https://siyige.example:8443"} {
		t.Setenv("AUTH_ORIGIN", origin)
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.AuthOrigin != origin {
			t.Fatalf("HTTPS Origin 未被规范化保留（长度=%d）", len(cfg.AuthOrigin))
		}
		if cfg.Database.Env != "dev" {
			t.Fatal("Origin 配置不得改变数据库环境限制")
		}
	}
}

func TestLoadConfigNormalizesDefaultHTTPSPort(t *testing.T) {
	// 浏览器不会为默认端口回填 Origin，配置成 443 时必须规范化，否则永远不匹配。
	setDatabaseEnv(t)
	t.Setenv("AUTH_ORIGIN", "https://siyige.example:443")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthOrigin != "https://siyige.example" {
		t.Fatal("默认 HTTPS 端口未规范化")
	}
}

func TestDatabaseConfigStillRejectsProductionEnv(t *testing.T) {
	// AUTH_ORIGIN 引入后 APP_ENV 仍只允许 dev/test，不新增生产部署入口。
	setDatabaseEnv(t)
	t.Setenv("APP_ENV", "production")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("APP_ENV=production 必须被拒绝")
	}
}
