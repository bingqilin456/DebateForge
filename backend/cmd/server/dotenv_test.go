package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunDotEnvConfiguration(t *testing.T) {
	keys := []string{"APP_ENV", "HTTP_ADDR", "DB_ADDR", "DB_NAME", "DB_USER", "DB_PASSWORD_FILE"}
	type testCase struct {
		name        string
		file        string
		directory   bool
		windowsOnly bool
		system      map[string]string
		want        map[string]string
		absent      []string
		wantError   string
	}
	cases := []testCase{
		{
			name: "load file before config validation",
			file: "APP_ENV=test\nHTTP_ADDR=invalid-from-file\nDB_ADDR=127.0.0.1:13307\nDB_NAME=siyige_test\nDB_USER=siyige_test_app\nDB_PASSWORD_FILE='/local-test-only/password.txt'\n",
			want: map[string]string{
				"APP_ENV": "test", "HTTP_ADDR": "invalid-from-file", "DB_ADDR": "127.0.0.1:13307",
				"DB_NAME": "siyige_test", "DB_USER": "siyige_test_app", "DB_PASSWORD_FILE": "/local-test-only/password.txt",
			},
			wantError: "配置无效: HTTP_ADDR",
		},
		{
			name: "system variables take precedence",
			file: "APP_ENV=dev\nHTTP_ADDR=127.0.0.1:8080\nDB_ADDR=127.0.0.1:13306\nDB_NAME=siyige_dev\nDB_USER=siyige_dev_app\nDB_PASSWORD_FILE='/unused-file/password.txt'\n",
			system: map[string]string{
				"APP_ENV": "test", "HTTP_ADDR": "invalid-from-system", "DB_ADDR": "127.0.0.1:13307",
				"DB_NAME": "siyige_test", "DB_USER": "siyige_test_app", "DB_PASSWORD_FILE": "/system-test-only/password.txt",
			},
			want: map[string]string{
				"APP_ENV": "test", "HTTP_ADDR": "invalid-from-system", "DB_ADDR": "127.0.0.1:13307",
				"DB_NAME": "siyige_test", "DB_USER": "siyige_test_app", "DB_PASSWORD_FILE": "/system-test-only/password.txt",
			},
			wantError: "配置无效: HTTP_ADDR",
		},
		{
			name:      "explicit empty system value is not replaced",
			file:      "HTTP_ADDR=127.0.0.1:8080\n",
			system:    map[string]string{"HTTP_ADDR": ""},
			want:      map[string]string{"HTTP_ADDR": ""},
			wantError: "配置无效: HTTP_ADDR",
		},
		{
			name:      "missing file still requires password file configuration",
			wantError: "配置无效: DB_PASSWORD_FILE",
		},
		{
			name:      "file cannot bypass database environment guard",
			file:      "APP_ENV=production\nHTTP_ADDR=127.0.0.1:8080\n",
			wantError: "配置无效: APP_ENV",
		},
		{
			name:      "malformed file fails without exposing content",
			file:      "PRIVATE_MARKER=\"sensitive-dotenv-marker\n",
			system:    map[string]string{"HTTP_ADDR": "invalid-from-system"},
			wantError: "加载 .env 失败",
		},
		{
			name:      "unreadable file is not treated as missing",
			directory: true,
			system:    map[string]string{"HTTP_ADDR": "invalid-from-system"},
			wantError: "加载 .env 失败",
		},
		{
			name:        "windows mixed case system variables take precedence",
			windowsOnly: true,
			file:        "APP_ENV=dev\nHTTP_ADDR=127.0.0.1:8080\nDB_ADDR=127.0.0.1:13306\nDB_NAME=siyige_dev\nDB_USER=siyige_dev_app\nDB_PASSWORD_FILE='/unused-file/password.txt'\n",
			system: map[string]string{
				"app_env": "test", "http_Addr": "invalid-from-system", "db_addr": "127.0.0.1:13307",
				"Db_Name": "siyige_test", "db_user": "siyige_test_app", "db_password_file": "/system-test-only/password.txt",
			},
			want: map[string]string{
				"APP_ENV": "test", "HTTP_ADDR": "invalid-from-system", "DB_ADDR": "127.0.0.1:13307",
				"DB_NAME": "siyige_test", "DB_USER": "siyige_test_app", "DB_PASSWORD_FILE": "/system-test-only/password.txt",
			},
			wantError: "配置无效: HTTP_ADDR",
		},
		{
			name:      "malformed file does not partially apply values",
			file:      "HTTP_ADDR=invalid-from-file\nPRIVATE_MARKER=\"sensitive-dotenv-marker\n",
			absent:    []string{"HTTP_ADDR", "PRIVATE_MARKER"},
			wantError: "加载 .env 失败",
		},
		{
			name:      "invalid environment value fails without exposing content",
			file:      "PRIVATE_MARKER='sensitive-dotenv-marker\x00invalid'\n",
			absent:    []string{"PRIVATE_MARKER"},
			wantError: "应用 .env 配置失败",
		},
		{
			name:      "empty file value is not replaced by default",
			file:      "APP_ENV=\n",
			wantError: "配置无效: APP_ENV",
		},
		{
			name:      "file cannot bypass secret absolute path guard",
			file:      "DB_PASSWORD_FILE='./relative-secret.txt'\n",
			wantError: "配置无效: DB_PASSWORD_FILE",
		},
		{
			name:      "file cannot bypass database address guard",
			file:      "DB_ADDR=192.0.2.1:3306\n",
			wantError: "配置无效: DB_ADDR",
		},
		{
			name:      "file cannot select privileged database user",
			file:      "DB_USER=root\n",
			wantError: "配置无效: DB_NAME",
		},
	}
	validFile := "APP_ENV=dev\nHTTP_ADDR=127.0.0.1:8080\nDB_ADDR=127.0.0.1:13306\nDB_NAME=siyige_dev\nDB_USER=siyige_dev_app\nDB_PASSWORD_FILE='" + filepath.ToSlash(filepath.Join(t.TempDir(), "unused-secret.txt")) + "'\n"
	for _, key := range keys {
		wantError := "配置无效: " + key
		if key == "DB_USER" {
			wantError = "配置无效: DB_NAME"
		}
		for _, windowsOnly := range []bool{false, true} {
			systemKey := key
			if windowsOnly {
				systemKey = strings.ToLower(key)
			}
			cases = append(cases, testCase{
				name: "explicit empty " + systemKey + " is preserved",
				file: validFile, windowsOnly: windowsOnly,
				system: map[string]string{systemKey: ""}, want: map[string]string{key: ""},
				wantError: wantError,
			})
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.windowsOnly && runtime.GOOS != "windows" {
				t.Skip("Windows 环境变量名称不区分大小写")
			}
			t.Chdir(t.TempDir())
			// godotenv 修改进程环境；逐项恢复原值且不允许并行运行这些用例。
			for _, key := range append(keys, "PRIVATE_MARKER") {
				t.Setenv(key, "")
				if err := os.Unsetenv(key); err != nil {
					t.Fatal(err)
				}
			}
			for key, value := range tc.system {
				t.Setenv(key, value)
			}
			if tc.file != "" {
				if err := os.WriteFile(".env", []byte(tc.file), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.directory {
				if err := os.Mkdir(".env", 0700); err != nil {
					t.Fatal(err)
				}
			}
			err := run(context.Background(), slog.New(slog.NewJSONHandler(io.Discard, nil)))
			if err == nil || !strings.HasPrefix(err.Error(), tc.wantError) {
				t.Fatalf("want error prefix %q, got %v", tc.wantError, err)
			}
			for key, want := range tc.want {
				if got, present := os.LookupEnv(key); !present || got != want {
					t.Errorf("%s: want %q, got %q (present=%v)", key, want, got, present)
				}
			}
			for _, key := range tc.absent {
				if _, present := os.LookupEnv(key); present {
					t.Errorf("%s was set despite loading failure", key)
				}
			}
			if strings.Contains(err.Error(), "sensitive-dotenv-marker") || strings.Contains(err.Error(), "PRIVATE_MARKER") {
				t.Fatal("startup error exposed dotenv details")
			}
		})
	}
}
