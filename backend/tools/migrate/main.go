package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output io.Writer) error {
	cfg, err := parseConfig(args)
	if err != nil {
		return err
	}
	// 与 server 完全独立：不加载 .env，也不继承 APP_ENV/DB_*/GOOSE_* 的目标。
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	db, err := openDatabase(cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	provider, err := newProvider(db, cfg, os.DirFS(cfg.Dir), "goose_db_version")
	if err != nil {
		return safeError("迁移文件无效", err)
	}
	return execute(ctx, provider, cfg.Command, output)
}
