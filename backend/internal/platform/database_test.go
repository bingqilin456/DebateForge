package platform

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testDatabaseConfig(t *testing.T) DatabaseConfig {
	t.Helper()
	path := filepath.Join(t.TempDir(), "password.txt")
	if err := os.WriteFile(path, []byte("unit-test-only\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return DatabaseConfig{Env: "test", Addr: "127.0.0.1:13307", Name: "siyige_test", User: "siyige_test_app", PasswordFile: path}
}

func TestTestDatabaseTargetGuard(t *testing.T) {
	valid := testDatabaseConfig(t)
	if err := valid.ValidateTestTarget(); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"environment", "address", "host", "database", "user", "root", "migrator"} {
		t.Run(field, func(t *testing.T) {
			cfg := valid
			switch field {
			case "environment":
				cfg.Env = "dev"
			case "address":
				cfg.Addr = "127.0.0.1:13306"
			case "host":
				cfg.Addr = "localhost:13307"
			case "database":
				cfg.Name = "siyige_dev"
			case "user":
				cfg.User = "siyige_dev_app"
			case "root":
				cfg.User = "root"
			case "migrator":
				cfg.User = "siyige_test_migrator"
			}
			if cfg.ValidateTestTarget() == nil {
				t.Fatal("unsafe test target accepted")
			}
		})
	}
}

func TestDatabaseSecretsAndLazyConnection(t *testing.T) {
	cfg := testDatabaseConfig(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	cfg.Addr = listener.Addr().String()
	db, err := OpenDatabase(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := db.PingContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline, got %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("ping ignored request deadline")
	}
	if db.ORM.NowFunc().Location() != time.UTC {
		t.Fatal("GORM clock must use UTC")
	}
	pool, err := db.ORM.DB()
	if err != nil {
		t.Fatal(err)
	}
	if pool.Stats().MaxOpenConnections != 10 {
		t.Fatal("unbounded connection pool")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.PingContext(context.Background()); err == nil {
		t.Fatal("closed database accepted ping")
	}
}

func TestDatabaseSecretFailuresAreSanitized(t *testing.T) {
	for _, content := range []string{"missing", "", "\r\n", strings.Repeat("x", 4097)} {
		t.Run(content[:min(len(content), 7)], func(t *testing.T) {
			cfg := testDatabaseConfig(t)
			if content == "missing" {
				cfg.PasswordFile = filepath.Join(t.TempDir(), "private-path")
			} else if err := os.WriteFile(cfg.PasswordFile, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := OpenDatabase(cfg)
			if err == nil {
				t.Fatal("bad secret accepted")
			}
			if strings.Contains(err.Error(), cfg.PasswordFile) || strings.Contains(err.Error(), "unit-test-only") {
				t.Fatal("secret detail leaked")
			}
		})
	}
}
