package platform

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"

	"github.com/bingqilin456/DebateForge/backend/internal/auth"
)

// Config 包含 HTTP、认证与数据库配置。
type Config struct {
	HTTPAddr string
	// AuthOrigin 是所有业务 POST 精确匹配的完整 Origin；默认本地开发同源地址。
	AuthOrigin string
	// TrustedProxy 只在部署模式设置为专用代理网络中的 Nginx 地址。
	TrustedProxy netip.Addr
	Database     DatabaseConfig
}

// DatabaseConfig 不保存口令或任意 DSN，只允许对应环境的应用账户。
type DatabaseConfig struct {
	Env          string
	Addr         string
	Name         string
	User         string
	PasswordFile string
}

// LoadConfig 保持本地回环保护，只有显式部署模式允许容器监听。
func LoadConfig() (Config, error) {
	database := DatabaseConfig{Env: envOrDefault("APP_ENV", "dev")}
	addr, present := os.LookupEnv("HTTP_ADDR")
	if !present {
		addr = "127.0.0.1:8080"
	}
	var trustedProxy netip.Addr
	if database.Env == "demo" {
		if addr != "0.0.0.0:8080" {
			return Config{}, fmt.Errorf("本地 Demo 仅允许固定容器监听")
		}
		switch os.Getenv("AI_MODE") {
		case "", "disabled", "fake":
		case "local-demo":
			if !filepath.IsAbs(os.Getenv("DEMO_LEDGER")) || !filepath.IsAbs(os.Getenv("DEEPSEEK_API_KEY_FILE")) {
				return Config{}, fmt.Errorf("本地 Demo 要求独立持久账本和服务端密钥文件")
			}
		default:
			return Config{}, fmt.Errorf("本地 Demo 模型模式无效")
		}
	} else if database.Env == "deploy" {
		if addr != "0.0.0.0:8080" {
			return Config{}, fmt.Errorf("部署模式要求 HTTP_ADDR=0.0.0.0:8080")
		}
		trustedProxy = netip.MustParseAddr("172.30.70.2")
		switch os.Getenv("AI_MODE") {
		case "", "disabled", "fake":
		case "deepseek":
			if !filepath.IsAbs(os.Getenv("T07_TRIAL_LEDGER")) {
				return Config{}, fmt.Errorf("真实试运行要求显式配置持久账本路径")
			}
		default:
			return Config{}, fmt.Errorf("部署准备仅允许关闭模型或使用 Fake")
		}
	} else if !isLoopbackEndpoint(addr) {
		return Config{}, fmt.Errorf("HTTP_ADDR 必须使用回环 IP 和 1–65535 端口")
	}
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
	// Origin 校验与 auth.Handler 使用同一规则；配置错误在启动期失败，
	// 不以“看起来同源”的值放行运行期请求。
	authOrigin, secure, err := auth.ParseOrigin(envOrDefault("AUTH_ORIGIN", auth.DefaultAuthOrigin))
	if err != nil {
		return Config{}, fmt.Errorf("AUTH_ORIGIN 无效: %w", err)
	}
	if database.Env == "deploy" && !secure {
		return Config{}, fmt.Errorf("部署模式要求 HTTPS AUTH_ORIGIN")
	}
	if database.Env == "demo" && authOrigin != "http://127.0.0.1:18081" {
		return Config{}, fmt.Errorf("本地 Demo 仅允许固定回环 Origin")
	}
	return Config{HTTPAddr: addr, AuthOrigin: authOrigin, TrustedProxy: trustedProxy, Database: database}, nil
}

func envOrDefault(key, fallback string) string {
	if value, present := os.LookupEnv(key); present {
		return value
	}
	return fallback
}

func isLoopbackEndpoint(addr string) bool {
	host, portText, err := net.SplitHostPort(addr)
	ip, ipErr := netip.ParseAddr(host)
	port, portErr := strconv.Atoi(portText)
	return err == nil && ipErr == nil && ip.IsLoopback() && portErr == nil && port >= 1 && port <= 65535
}

func (cfg DatabaseConfig) validate() error {
	switch cfg.Env {
	case "dev", "test":
		if !isLoopbackEndpoint(cfg.Addr) {
			return fmt.Errorf("DB_ADDR 必须使用回环 IP 和 1–65535 端口")
		}
	case "deploy", "demo":
		if cfg.Addr != "mysql:3306" {
			return fmt.Errorf("部署模式只允许内部 DB_ADDR=mysql:3306")
		}
	default:
		return fmt.Errorf("APP_ENV 必须为 dev、test、deploy 或 demo")
	}
	if cfg.Name != "siyige_"+cfg.Env || cfg.User != "siyige_"+cfg.Env+"_app" {
		return fmt.Errorf("DB_NAME 和 DB_USER 必须匹配当前环境的应用账户")
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
