package platform

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type pingFunc func(context.Context) error

func (f pingFunc) PingContext(ctx context.Context) error { return f(ctx) }

func TestReadinessAndLiveness(t *testing.T) {
	for _, available := range []bool{true, false} {
		var logs bytes.Buffer
		calls := 0
		checker := pingFunc(func(ctx context.Context) error {
			calls++
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 3*time.Second {
				t.Fatal("missing database deadline")
			}
			if !available {
				return errors.New("private-dsn-password")
			}
			return nil
		})
		router := NewRouter(slog.New(slog.NewJSONHandler(&logs, nil)), checker)
		live := httptest.NewRecorder()
		router.ServeHTTP(live, httptest.NewRequest("GET", "/api/health/live", nil))
		if live.Code != 200 || calls != 0 {
			t.Fatal("live must not ping database")
		}
		ready := httptest.NewRecorder()
		router.ServeHTTP(ready, httptest.NewRequest("GET", "/api/health/ready", nil))
		status, body := 200, `{"status":"ready"}`
		if !available {
			status, body = 503, `{"status":"not_ready"}`
		}
		if ready.Code != status || ready.Body.String() != body || calls != 1 {
			t.Fatalf("code=%d body=%s calls=%d", ready.Code, ready.Body.String(), calls)
		}
		if ready.Header().Get("Cache-Control") != "no-store" || ready.Header().Get("X-Request-ID") == "" || !strings.HasPrefix(ready.Header().Get("Content-Type"), "application/json") {
			t.Fatal("missing health headers")
		}
		if strings.Contains(logs.String()+ready.Body.String(), "private-dsn-password") {
			t.Fatal("database error leaked")
		}
	}
}

func TestReadinessDatabaseHandshakeTimeout(t *testing.T) {
	cfg := testDatabaseConfig(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	// 监听但不提供 MySQL greeting，验证真实驱动而非 Fake 的超时路径。
	cfg.Addr = listener.Addr().String()
	db, err := OpenDatabase(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	router := NewRouter(slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)), db)
	rec := httptest.NewRecorder()
	started := time.Now()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/api/health/ready", nil))
	if rec.Code != 503 || rec.Body.String() != `{"status":"not_ready"}` {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if elapsed := time.Since(started); elapsed > 4*time.Second {
		t.Fatalf("database budget exceeded: %s", elapsed)
	}
	live := httptest.NewRecorder()
	router.ServeHTTP(live, httptest.NewRequest("GET", "/api/health/live", nil))
	if live.Code != 200 {
		t.Fatal("database timeout broke liveness")
	}
}

func TestReadinessCancellationAndMissingDependency(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	checker := pingFunc(func(received context.Context) error {
		if !errors.Is(received.Err(), context.Canceled) {
			t.Fatal("request context lost")
		}
		return received.Err()
	})
	for _, pinger := range []DatabasePinger{checker, nil} {
		router := NewRouter(slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)), pinger)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest("GET", "/api/health/ready", nil).WithContext(ctx))
		if rec.Code != 503 {
			t.Fatalf("want 503, got %d", rec.Code)
		}
	}
}
