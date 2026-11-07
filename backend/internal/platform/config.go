package platform

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
)

// Config 只包含当前已接入的 HTTP 与本地数据库配置。
type Config struct {
	HTTPAddr string
	Database DatabaseConfig
}

// DatabaseConfig 不保存口令或任意 DSN，当前只允许隔离的本地应用账户。
type DatabaseConfig struct {
	Env          string
	Addr         string
	Name         string
	User         string
	PasswordFile string
}

// LoadConfig 拒绝空值和非回环地址，防止本地骨架因配置错误意外暴露公网。
func LoadConfig() (Config, error) {
	addr, present := os.LookupEnv("HTTP_ADDR")
	if !present {
		addr = "127.0.0.1:8080"
	}
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return Config{}, fmt.Errorf("HTTP_ADDR 必须是回环 IP:端口: %w", err)
	}
	ip, ipErr := netip.ParseAddr(host)
	port, portErr := strconv.Atoi(portText)
	if ipErr != nil || !ip.IsLoopback() || portErr != nil || port < 1 || port > 65535 {
		return Config{}, fmt.Errorf("HTTP_ADDR 必须使用回环 IP 和 1–65535 端口")
	}
	database := DatabaseConfig{Env: envOrDefault("APP_ENV", "dev")}
	dbPort := "13306"
	if database.Env == "test" {
		dbPort = "13307"
	}
	database.Addr = envOrDefault("DB_ADDR", "127.0.0.1:"+dbPort)
	database.Name = envOrDefault("DB_NAME", "siyige_"+database.Env)
	database.User = envOrDefault("DB_USER", "siyige_"+database.Env+"_app")
	database.PasswordFile = os.Getenv("DB_PASSWORD_FILE")
	if err := database.validate(); err != nil {
		return Config{}, err
	}
	return Config{HTTPAddr: addr, Database: database}, nil
}

func envOrDefault(key, fallback string) string {
	if value, present := os.LookupEnv(key); present {
		return value
	}
	return fallback
}

func (cfg DatabaseConfig) validate() error {
	if cfg.Env != "dev" && cfg.Env != "test" {
		return fmt.Errorf("APP_ENV 必须为 dev 或 test；当前不支持生产配置")
	}
	if cfg.Name != "siyige_"+cfg.Env || cfg.User != "siyige_"+cfg.Env+"_app" {
		return fmt.Errorf("DB_NAME 和 DB_USER 必须匹配当前隔离环境的应用账户")
	}
	host, portText, err := net.SplitHostPort(cfg.Addr)
	ip, ipErr := netip.ParseAddr(host)
	port, portErr := strconv.Atoi(portText)
	if err != nil || ipErr != nil || !ip.IsLoopback() || portErr != nil || port < 1 || port > 65535 {
		return fmt.Errorf("DB_ADDR 必须使用回环 IP 和 1–65535 端口")
	}
	if !filepath.IsAbs(cfg.PasswordFile) {
		return fmt.Errorf("DB_PASSWORD_FILE 必须为本地秘密文件的绝对路径")
	}
	return nil
}

// ValidateTestTarget 必须在集成测试读取秘密和建立连接之前调用，防止误连开发库。
func (cfg DatabaseConfig) ValidateTestTarget() error {
	if cfg.Env != "test" || cfg.Addr != "127.0.0.1:13307" || cfg.Name != "siyige_test" || cfg.User != "siyige_test_app" {
		return fmt.Errorf("集成测试只允许固定的隔离测试库和测试应用账户")
	}
	return cfg.validate()
}
