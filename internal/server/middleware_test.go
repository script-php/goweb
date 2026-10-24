package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"script-php/goweb/internal/auth"
)

func TestIdentifyRequestPreservesValidID(t *testing.T) {
	handler := identifyRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := RequestID(r.Context()); got != "client-request-123" {
			t.Fatalf("RequestID() = %q", got)
		}
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Request-ID", "client-request-123")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if got := response.Header().Get("X-Request-ID"); got != "client-request-123" {
		t.Fatalf("response request ID = %q", got)
	}
}

func TestRecoverPanicsReturnsCustom500(t *testing.T) {
	panicking := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("test panic") })
	handler := auth.WithErrorResponder(identifyRequest(recoverPanics(panicking)), func(w http.ResponseWriter, _ *http.Request, status int) {
		w.WriteHeader(status)
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestCleanRequestIDRejectsUnsafeValue(t *testing.T) {
	if got := cleanRequestID("unsafe\nvalue"); got != "" {
		t.Fatalf("cleanRequestID() = %q", got)
	}
}
