package main

import (
	"errors"
	"flag"
	"io"
	"path/filepath"
)

type config struct {
	Env, Addr, Name, User, PasswordFile, Dir, Command string
}

func parseConfig(args []string) (config, error) {
	var cfg config
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	flags.SetOutput(io.Discard) // flag 默认错误可能包含未知参数及其秘密值。
	flags.StringVar(&cfg.Env, "env", "", "显式选择 dev/test/deploy/demo")
	flags.StringVar(&cfg.PasswordFile, "password-file", "", "迁移账户秘密文件绝对路径")
	flags.StringVar(&cfg.Dir, "dir", "../../migrations", "已审阅 SQL 迁移目录")
	if err := flags.Parse(args); err != nil || flags.NArg() != 1 {
		return config{}, errors.New("配置无效：用法 migrate -env dev|test|deploy|demo -password-file <绝对路径> [-dir <目录>] up|status")
	}
	cfg.Command = flags.Arg(0)
	cfg.Addr = targetAddress(cfg.Env)
	cfg.Name, cfg.User = "siyige_"+cfg.Env, "siyige_"+cfg.Env+"_migrator"
	return cfg, cfg.validate()
}

func targetAddress(env string) string {
	switch env {
	case "dev":
		return "127.0.0.1:13306"
	case "test":
		return "127.0.0.1:13307"
	case "deploy", "demo":
		return "mysql:3306"
	default:
		return ""
	}
}

func (cfg config) validate() error {
	addr := targetAddress(cfg.Env)
	if addr == "" || cfg.Addr != addr || cfg.Name != "siyige_"+cfg.Env || cfg.User != "siyige_"+cfg.Env+"_migrator" {
		return errors.New("配置无效：迁移只允许固定隔离目标及对应迁移账户")
	}
	if !filepath.IsAbs(cfg.PasswordFile) || cfg.Dir == "" {
		return errors.New("配置无效：秘密文件必须为绝对路径，迁移目录不能为空")
	}
	if cfg.Command != "up" && cfg.Command != "status" {
		return errors.New("配置无效：只允许 up 或 status；不提供回滚、重置或版本修复命令")
	}
	return nil
}
