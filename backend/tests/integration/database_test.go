package integration

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/platform"
)

var databaseConfig platform.DatabaseConfig
var integrationEnabled bool

func TestMain(m *testing.M) {
	flag := os.Getenv("SIYIGE_INTEGRATION")
	if flag == "" {
		os.Exit(m.Run())
	}
	if flag != "1" {
		fmt.Fprintln(os.Stderr, "SIYIGE_INTEGRATION 只允许显式设置为 1")
		os.Exit(1)
	}
	cfg, err := platform.LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "集成测试配置无效:", err)
		os.Exit(1)
	}
	// 先验证配置，不读取秘密、不创建连接；禁止沿用开发环境的默认值。
	if err := cfg.Database.ValidateTestTarget(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	databaseConfig, integrationEnabled = cfg.Database, true
	os.Exit(m.Run())
}

func openTestDatabase(t *testing.T) *platform.Database {
	t.Helper()
	if !integrationEnabled {
		t.Skip("需 SIYIGE_INTEGRATION=1 及显式 APP_ENV=test；默认测试不连接 MySQL")
	}
	if err := databaseConfig.ValidateTestTarget(); err != nil {
		t.Fatal(err)
	}
	db, err := platform.OpenDatabase(databaseConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), platform.DatabaseTimeout)
	defer cancel()
	var identity struct {
		Database string
		Account  string
	}
	if err := db.ORM.WithContext(ctx).Raw("SELECT DATABASE() AS `database`, CURRENT_USER() AS account").Scan(&identity).Error; err != nil {
		t.Fatal("无法验证测试连接身份")
	}
	if identity.Database != "siyige_test" || identity.Account != "siyige_test_app@%" {
		t.Fatal("实际数据库或账户与测试白名单不符")
	}
	return db
}

func TestMySQLConnectionAndSettings(t *testing.T) {
	db := openTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), platform.DatabaseTimeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Version, Zone, Charset, Collation string
		Moment                            time.Time
		Text                              string
	}
	if err := db.ORM.WithContext(ctx).Raw("SELECT VERSION() AS version, @@session.time_zone AS zone, @@character_set_connection AS charset, @@collation_connection AS collation, CAST('2026-11-08 01:02:03' AS DATETIME) AS moment, ? AS text", "中文辩论🌱").Scan(&settings).Error; err != nil {
		t.Fatal("GORM 只读查询失败")
	}
	if settings.Version != "8.4.13" || settings.Zone != "+00:00" || settings.Charset != "utf8mb4" || settings.Collation != "utf8mb4_0900_ai_ci" || settings.Text != "中文辩论🌱" {
		t.Fatal("连接参数与冻结环境不一致")
	}
	if settings.Moment.Location() != time.UTC || settings.Moment.Format(time.RFC3339) != "2026-11-08T01:02:03Z" {
		t.Fatal("时间未按 UTC 解析")
	}
}

func assertHealth(t *testing.T, client *http.Client, base, path string, code int, body string) {
	t.Helper()
	response, err := client.Get(base + path)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	content, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != code || string(content) != body || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected health: %d %s", response.StatusCode, content)
	}
}

func TestHTTPReadinessTracksRealDatabase(t *testing.T) {
	db := openTestDatabase(t)
	server := httptest.NewServer(platform.NewRouter(slog.New(slog.NewJSONHandler(io.Discard, nil)), db))
	defer server.Close()
	client := &http.Client{Timeout: 4 * time.Second}
	assertHealth(t, client, server.URL, "/api/health/ready", 200, `{"status":"ready"}`)
	// 只关闭本测试的连接池，不停止容器，不改变账户、表或数据。
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	assertHealth(t, client, server.URL, "/api/health/ready", 503, `{"status":"not_ready"}`)
	assertHealth(t, client, server.URL, "/api/health/live", 200, `{"status":"alive"}`)
	// 新建连接证明依赖仍可达，池资源可以重新获取。
	recovered := openTestDatabase(t)
	recoveryServer := httptest.NewServer(platform.NewRouter(slog.New(slog.NewJSONHandler(io.Discard, nil)), recovered))
	defer recoveryServer.Close()
	assertHealth(t, client, recoveryServer.URL, "/api/health/ready", 200, `{"status":"ready"}`)
}
