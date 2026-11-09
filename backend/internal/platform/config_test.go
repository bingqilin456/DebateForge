package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func setDatabaseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HTTP_ADDR", "127.0.0.1:8080")
	t.Setenv("APP_ENV", "dev")
	t.Setenv("DB_ADDR", "127.0.0.1:13306")
	t.Setenv("DB_NAME", "siyige_dev")
	t.Setenv("DB_USER", "siyige_dev_app")
	t.Setenv("DB_PASSWORD_FILE", filepath.Join(t.TempDir(), "password.txt"))
}

func TestLoadConfigDefaults(t *testing.T) {
	setDatabaseEnv(t)
	t.Setenv("HTTP_ADDR", "")
	if err := os.Unsetenv("HTTP_ADDR"); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig()
	if err != nil || cfg.HTTPAddr != "127.0.0.1:8080" {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
}

func TestLoadConfigValidatesListener(t *testing.T) {
	setDatabaseEnv(t)
	for _, tc := range []struct {
		addr  string
		valid bool
	}{
		{"127.0.0.1:8081", true}, {"[::1]:8080", true},
		{"", false}, {":8080", false}, {"0.0.0.0:8080", false}, {"192.0.2.1:8080", false},
		{"127.0.0.1:0", false}, {"127.0.0.1:65536", false}, {"127.0.0.1:abc", false},
		{"127.0.0.1", false}, {" 127.0.0.1:8080", false},
	} {
		t.Run(tc.addr, func(t *testing.T) {
			t.Setenv("HTTP_ADDR", tc.addr)
			cfg, err := LoadConfig()
			if (err == nil) != tc.valid {
				t.Fatalf("cfg=%+v err=%v valid=%v", cfg, err, tc.valid)
			}
		})
	}
}

func TestLoadDatabaseConfig(t *testing.T) {
	setDatabaseEnv(t)
	for _, name := range []string{"APP_ENV", "DB_ADDR", "DB_NAME", "DB_USER"} {
		t.Setenv(name, "")
		_ = os.Unsetenv(name)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Env != "dev" || cfg.Database.Addr != "127.0.0.1:13306" || cfg.Database.Name != "siyige_dev" || cfg.Database.User != "siyige_dev_app" {
		t.Fatal("unexpected dev target")
	}
	t.Setenv("APP_ENV", "test")
	cfg, err = LoadConfig()
	if err != nil || cfg.Database.ValidateTestTarget() != nil {
		t.Fatalf("invalid test defaults: %v", err)
	}
}

func TestDatabaseConfigRejectsUnsafeValues(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"APP_ENV", "prod"}, {"APP_ENV", ""}, {"DB_ADDR", ""}, {"DB_ADDR", "localhost:13306"},
		{"DB_ADDR", "192.0.2.1:3306"}, {"DB_ADDR", "127.0.0.1:0"}, {"DB_ADDR", "127.0.0.1:65536"},
		{"DB_NAME", ""}, {"DB_NAME", "siyige_test"}, {"DB_NAME", "prod"}, {"DB_USER", "root"},
		{"DB_USER", "siyige_dev_migrator"}, {"DB_PASSWORD_FILE", ""}, {"DB_PASSWORD_FILE", "relative.txt"},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			setDatabaseEnv(t)
			t.Setenv(tc.key, tc.value)
			if _, err := LoadConfig(); err == nil {
				t.Fatal("unsafe configuration accepted")
			}
		})
	}
}

func TestLoadDeployConfig(t *testing.T) {
	setDatabaseEnv(t)
	t.Setenv("APP_ENV", "deploy")
	t.Setenv("HTTP_ADDR", "0.0.0.0:8080")
	t.Setenv("DB_ADDR", "mysql:3306")
	t.Setenv("DB_NAME", "siyige_deploy")
	t.Setenv("DB_USER", "siyige_deploy_app")
	t.Setenv("AUTH_ORIGIN", "https://debate.example.invalid")
	t.Setenv("AI_MODE", "fake")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.ValidateTestTarget() == nil {
		t.Fatal("部署库被接受为集成测试目标")
	}
	for _, tc := range []struct{ key, value string }{
		{"HTTP_ADDR", "0.0.0.0:8081"},
		{"DB_ADDR", "127.0.0.1:13307"},
		{"DB_ADDR", "other-mysql:3306"},
		{"DB_NAME", "siyige_test"},
		{"DB_USER", "siyige_deploy_migrator"},
		{"AUTH_ORIGIN", "http://127.0.0.1:5173"},
		{"AI_MODE", "deepseek"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := LoadConfig(); err == nil {
				t.Fatal("接受了不安全的部署配置")
			}
		})
	}
}
