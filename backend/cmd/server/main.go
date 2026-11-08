package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/auth"
	"github.com/bingqilin456/DebateForge/backend/internal/platform"
	"github.com/joho/godotenv"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Time(a.Key, a.Value.Time().UTC())
			}
			return a
		},
	}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil {
		logger.Error("服务退出", "error", err.Error())
		os.Exit(1)
	}
}

// loadDotEnv 用操作系统的键名规则保留已有值，兼容 Windows 大小写和显式空值。
func loadDotEnv() error {
	values, err := godotenv.Read()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		// 解析错误可能携带文件原文，不把原始错误写入启动日志。
		return errors.New("加载 .env 失败，请检查文件权限与格式")
	}
	for key, value := range values {
		if _, present := os.LookupEnv(key); present {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return errors.New("应用 .env 配置失败，请检查环境变量格式")
		}
	}
	return nil
}

func run(ctx context.Context, logger *slog.Logger) error {
	// 仅启动入口补充本地配置；CI 可不提供 .env。
	if err := loadDotEnv(); err != nil {
		return err
	}
	cfg, err := platform.LoadConfig()
	if err != nil {
		return fmt.Errorf("配置无效: %w", err)
	}
	database, err := platform.OpenDatabase(cfg.Database)
	if err != nil {
		return fmt.Errorf("初始化数据库失败: %w", err)
	}
	defer database.Close()
	// 认证依赖单例限流器与 Service：进程内计数需要跨请求累计，不能用 per-request 实例。
	authService := auth.NewService(auth.NewRepository(database.ORM), auth.WithLimiter(auth.NewLimiter(nil, 0)))
	authHandler, err := auth.NewHandler(authService, auth.HandlerConfig{Origin: cfg.AuthOrigin, Logger: logger})
	if err != nil {
		return fmt.Errorf("初始化认证失败: %w", err)
	}
	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("监听 HTTP 失败: %w", err)
	}
	defer listener.Close()
	server := &http.Server{
		Handler:           platform.NewRouter(logger, database, authHandler),
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	// 只有进程退出协调需要 goroutine；不引入后台任务或异步业务。
	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	shutdownDone := make(chan error, 1)
	go func() {
		<-serveCtx.Done()
		shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		err := server.Shutdown(shutdownCtx)
		if err != nil {
			_ = server.Close()
		}
		shutdownDone <- err
	}()
	logger.Info("HTTP 服务启动", "address", listener.Addr().String())
	err = server.Serve(listener)
	cancel()
	shutdownErr := <-shutdownDone
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("运行 HTTP 失败: %w", err)
	}
	if shutdownErr != nil {
		return fmt.Errorf("优雅退出失败: %w", shutdownErr)
	}
	logger.Info("HTTP 服务已停止")
	return nil
}
