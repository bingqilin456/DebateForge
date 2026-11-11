package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestOriginCanonicalFormCanBeUsedAgain(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"http://[::1]:5173", "http://[::1]:5173"},
		{"https://[::1]:443", "https://[::1]"},
		{"https://[::1]:8443", "https://[::1]:8443"},
		{"https://siyige.example:443", "https://siyige.example"},
		{"http://127.0.0.1:80", "http://127.0.0.1"},
		{"http://127.0.0.1", "http://127.0.0.1"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			got, _, err := ParseOrigin(tc.raw)
			if err != nil || got != tc.want {
				t.Fatalf("origin normalization: got=%q want=%q err=%v", got, tc.want, err)
			}
			if _, err := NewHandler(NewService(newFakeStore()), HandlerConfig{Origin: got}); err != nil {
				t.Fatalf("normalized config cannot initialize HTTP handler: %v", err)
			}
		})
	}
}

func TestOriginRejectsEmptyQueryAndFragmentMarkers(t *testing.T) {
	for _, raw := range []string{"http://127.0.0.1:5173?", "https://siyige.example#"} {
		if _, _, err := ParseOrigin(raw); err == nil {
			t.Fatal("origin accepted a query or fragment delimiter")
		}
	}
}

func TestServicePreservesControlledStorageCause(t *testing.T) {
	cause := errors.New("private storage diagnostic")
	store := newFakeStore()
	store.createUserErr = cause
	service, _, _ := newTestService(t, store)
	_, err := service.Register(context.Background(), "127.0.0.1", "alice_01", "correct-horse-battery")
	if !errors.Is(err, ErrDependencyUnavailable) || !errors.Is(err, cause) {
		t.Fatal("service lost the dependency classification or underlying cause")
	}
	if strings.Contains(err.Error(), cause.Error()) {
		t.Fatal("controlled error string exposed the underlying diagnostic")
	}
}

func TestWithClockAlsoControlsDefaultLimiter(t *testing.T) {
	clock := newTestClock(time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC))
	service := NewService(newFakeStore(), WithClock(clock.Now))
	service.passwords.hash = func(string) (string, error) { return "fake-hash", nil }
	for i := 0; i < 5; i++ {
		if _, err := service.Register(context.Background(), "127.0.0.1", fmt.Sprintf("user_%d", i), "correct-horse-battery"); err != nil {
			t.Fatal(err)
		}
	}
	clock.Advance(time.Hour)
	if _, err := service.Register(context.Background(), "127.0.0.1", "user_after", "correct-horse-battery"); err != nil {
		t.Fatal("advancing the injected clock did not reset the default limiter")
	}
}
