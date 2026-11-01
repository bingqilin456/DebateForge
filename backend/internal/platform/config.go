package platform

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
)

// Config 只包含当前骨架使用的配置，避免提前要求数据库或模型凭据。
type Config struct {
	HTTPAddr string
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
	return Config{HTTPAddr: addr}, nil
}
