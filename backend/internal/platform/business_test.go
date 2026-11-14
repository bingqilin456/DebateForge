package platform

import (
	"context"
	"github.com/gin-gonic/gin"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPBudgetsAndShorterParent(t *testing.T) {
	for _, tc := range []struct {
		path, method string
		budget       time.Duration
		parent       time.Duration
	}{{"/api/v1/debates/1/strategy", "POST", 60 * time.Second, 0}, {"/api/v1/debates/1/strategy", "GET", 5 * time.Second, 0}, {"/api/v1/debates/1/strategy", "POST", 50 * time.Millisecond, 50 * time.Millisecond}, {"/plain", "GET", 5 * time.Second, 0}, {"/api/v1/debates/1/turns", "POST", 60 * time.Second, 0}, {"/api/v1/debates/1/summary", "POST", 60 * time.Second, 0}, {"/api/v1/debates/1/summary", "GET", 5 * time.Second, 0}, {"/api/v1/debates/1/turns", "POST", 50 * time.Millisecond, 50 * time.Millisecond}} {
		t.Run(tc.method+tc.path+tc.budget.String(), func(t *testing.T) {
			router := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
			handler := func(c *gin.Context) {
				deadline, ok := c.Request.Context().Deadline()
				remaining := time.Until(deadline)
				if !ok || remaining > tc.budget || remaining < tc.budget-30*time.Millisecond {
					t.Error("request budget not applied at entry")
				}
				c.Status(204)
			}
			router.POST("/api/v1/debates/:id/strategy", handler)
			router.GET("/api/v1/debates/:id/strategy", handler)
			router.GET("/plain", handler)
			router.POST("/api/v1/debates/:id/turns", handler)
			router.POST("/api/v1/debates/:id/summary", handler)
			router.GET("/api/v1/debates/:id/summary", handler)
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.parent > 0 {
				ctx, cancel := context.WithTimeout(req.Context(), tc.parent)
				defer cancel()
				req = req.WithContext(ctx)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != 204 {
				t.Fatal("budget middleware response")
			}
		})
	}
}
