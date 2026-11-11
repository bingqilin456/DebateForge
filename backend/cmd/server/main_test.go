package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunServesHealthDuringDatabaseOutageAndShutsDown(t *testing.T) {
	t.Chdir(t.TempDir()) // 不读取开发者本地的 .env，固定验证无文件时的启动行为。
	address := func() string {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := listener.Addr().String()
		_ = listener.Close()
		return addr
	}
	httpAddr, dbAddr := address(), address()
	passwordPath := filepath.Join(t.TempDir(), "password.txt")
	if err := os.WriteFile(passwordPath, []byte("unit-test-only"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HTTP_ADDR", httpAddr)
	t.Setenv("APP_ENV", "test")
	t.Setenv("DB_ADDR", dbAddr)
	t.Setenv("DB_NAME", "siyige_test")
	t.Setenv("DB_USER", "siyige_test_app")
	t.Setenv("DB_PASSWORD_FILE", passwordPath)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run(ctx, slog.New(slog.NewJSONHandler(io.Discard, nil))) }()
	client := &http.Client{Timeout: 4 * time.Second}
	deadline := time.Now().Add(5 * time.Second)
	for {
		response, err := client.Get("http://" + httpAddr + "/api/health/live")
		if err == nil {
			body, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if readErr != nil || response.StatusCode != 200 || string(body) != `{"status":"alive"}` {
				t.Fatal("invalid live response")
			}
			break
		}
		select {
		case runErr := <-done:
			t.Fatalf("server stopped before health: %v", runErr)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("server did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	response, err := client.Get("http://" + httpAddr + "/api/health/ready")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil || response.StatusCode != 503 || string(body) != `{"status":"not_ready"}` {
		t.Fatal("unreachable database must report 503")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("server did not shut down")
	}
	if response, err := client.Get("http://" + httpAddr + "/api/health/live"); err == nil {
		_ = response.Body.Close()
		t.Fatal("listener remained open after shutdown")
	}
}

func TestRunRegistersAuthRoutes(t *testing.T) {
	t.Chdir(t.TempDir())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	httpAddr := listener.Addr().String()
	_ = listener.Close()
	dbListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dbAddr := dbListener.Addr().String()
	_ = dbListener.Close()
	passwordPath := filepath.Join(t.TempDir(), "password.txt")
	if err := os.WriteFile(passwordPath, []byte("unit-test-only"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HTTP_ADDR", httpAddr)
	t.Setenv("APP_ENV", "test")
	t.Setenv("DB_ADDR", dbAddr)
	t.Setenv("DB_NAME", "siyige_test")
	t.Setenv("DB_USER", "siyige_test_app")
	t.Setenv("DB_PASSWORD_FILE", passwordPath)
	// 未设置 AUTH_ORIGIN 时使用默认开发 Origin，启动不得失败。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run(ctx, slog.New(slog.NewJSONHandler(io.Discard, nil))) }()
	client := &http.Client{Timeout: 4 * time.Second}
	deadline := time.Now().Add(5 * time.Second)
	for {
		response, err := client.Get("http://" + httpAddr + "/api/health/live")
		if err == nil {
			_ = response.Body.Close()
			break
		}
		select {
		case runErr := <-done:
			t.Fatalf("server stopped before health: %v", runErr)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("server did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// 真实入口必须注册认证路由：无 Cookie 的 me 返回 401 而不是 404。
	response, err := client.Get("http://" + httpAddr + "/api/v1/auth/me")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("me status=%d want=401（认证路由未注册）", response.StatusCode)
	}
	if !strings.Contains(string(body), `"code":"unauthenticated"`) {
		t.Fatal("me 未返回受控 unauthenticated 错误")
	}
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("认证响应缺少 no-store")
	}
	for _, path := range []string{"/api/v1/topics", "/api/v1/debates/1"} {
		response, err := client.Get("http://" + httpAddr + path)
		if err != nil {
			t.Fatal("业务入口请求失败")
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatal("真实入口未注册受保护的 T05 路由")
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("server did not shut down")
	}
}
