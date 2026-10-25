package ratelimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMemoryStoreLimitsResetsAndExpires(t *testing.T) {
	store := New(nil)
	ctx := context.Background()
	now := time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)
	first, err := store.Hit(ctx, "api", "client", 2, time.Minute, now)
	if err != nil || !first.Allowed || first.Hits != 1 {
		t.Fatalf("first hit = %s, error=%v", first, err)
	}
	second, err := store.Hit(ctx, "api", "client", 2, time.Minute, now)
	if err != nil || second.Allowed || second.RetryAfter != time.Minute {
		t.Fatalf("second hit = %s, error=%v", second, err)
	}
	blocked, err := store.Check(ctx, "api", "client", now.Add(30*time.Second))
	if err != nil || blocked.Allowed || blocked.RetryAfter != 30*time.Second {
		t.Fatalf("blocked check = %s, error=%v", blocked, err)
	}
	expired, err := store.Check(ctx, "api", "client", now.Add(2*time.Minute))
	if err != nil || !expired.Allowed {
		t.Fatalf("expired check = %s, error=%v", expired, err)
	}
	if err := store.Reset(ctx, "api", "client"); err != nil {
		t.Fatal(err)
	}
	reset, err := store.Check(ctx, "api", "client", now)
	if err != nil || !reset.Allowed || reset.Hits != 0 {
		t.Fatalf("reset check = %s, error=%v", reset, err)
	}
}

func TestHTTPMiddlewareAllowsConfiguredLimit(t *testing.T) {
	store := New(nil)
	called := 0
	handler := store.Middleware([]Policy{{
		ID: "api", Limit: 2, Window: time.Minute,
		Match: func(*http.Request) bool { return true },
		Key:   func(*http.Request) string { return "client" },
	}}, nil, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called++ }))
	for attempt := 1; attempt <= 3; attempt++ {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api", nil))
		if attempt <= 2 && response.Code != http.StatusOK {
			t.Fatalf("attempt %d status = %d", attempt, response.Code)
		}
		if attempt == 3 && (response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "") {
			t.Fatalf("limited response status=%d retry-after=%q", response.Code, response.Header().Get("Retry-After"))
		}
	}
	if called != 2 {
		t.Fatalf("downstream called %d times", called)
	}
}

func TestStoreRejectsInvalidPolicy(t *testing.T) {
	if _, err := New(nil).Hit(context.Background(), "", "client", 1, time.Minute, time.Now()); err == nil {
		t.Fatal("invalid policy was accepted")
	}
}
