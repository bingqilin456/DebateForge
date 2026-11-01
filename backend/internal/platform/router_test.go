package platform

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHTTPContract(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		status       int
		code         string
	}{
		{"GET", "/health/live", 200, ""}, {"GET", "/health/ready", 404, "not_found"},
		{"GET", "/api/v1/missing", 404, "not_found"}, {"POST", "/health/live", 405, "method_not_allowed"},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			var logs bytes.Buffer
			router := NewRouter(slog.New(slog.NewJSONHandler(&logs, nil)))
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path+"?secret=do-not-log", nil)
			router.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
				t.Fatal(rec.Header())
			}
			id := rec.Header().Get("X-Request-ID")
			if id == "" {
				t.Fatal("missing request id")
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if tc.code == "" {
				if len(body) != 1 || string(body["status"]) != `"alive"` {
					t.Fatalf("body=%s", rec.Body.String())
				}
				if rec.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("health may be cached")
				}
			} else {
				var response struct {
					Error     struct{ Code string }
					RequestID string `json:"request_id"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if response.Error.Code != tc.code || response.RequestID != id {
					t.Fatalf("body=%s", rec.Body.String())
				}
			}
			if !strings.Contains(logs.String(), id) || strings.Contains(logs.String(), "do-not-log") {
				t.Fatalf("logs=%s", logs.String())
			}
		})
	}
}

func TestPanicIsControlled(t *testing.T) {
	var logs bytes.Buffer
	router := NewRouter(slog.New(slog.NewJSONHandler(&logs, nil)))
	router.GET("/panic", func(c *gin.Context) { panic("private-internal-detail") })
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/panic", nil))
	if rec.Code != 500 || !strings.Contains(rec.Body.String(), `"code":"internal_error"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String()+logs.String(), "private-internal-detail") {
		t.Fatal("panic detail leaked")
	}
}
