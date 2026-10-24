package auth

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestPasswordHashing(t *testing.T) {
	hash, err := HashPassword("a-long-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckPassword(hash, "a-long-test-password"); err != nil {
		t.Fatalf("correct password rejected: %v", err)
	}
	if err := CheckPassword(hash, "wrong-password"); err == nil {
		t.Fatal("wrong password accepted")
	}
}

func TestCSRFProtectionAcceptsMultipartForms(t *testing.T) {
	token := strings.Repeat("a", 64)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("_csrf", token); err != nil {
		t.Fatal(err)
	}
	file, err := writer.CreateFormFile("avatar", "avatar.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("image")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	called := false
	handler := ProtectCSRFWithMaxBytes(false, 1<<20, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	request := httptest.NewRequest(http.MethodPost, "/admin/users/1/avatar", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: token})
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if !called {
		t.Fatal("valid multipart CSRF token was rejected")
	}
}

func TestHashPasswordRejectsShortPassword(t *testing.T) {
	if _, err := HashPassword("too-short"); err == nil {
		t.Fatal("short password accepted")
	}
}

func TestLoginAttemptsAreThrottledAndExpire(t *testing.T) {
	service := NewService(nil, ServiceOptions{MaxLoginAttempts: 2, LoginLockout: time.Minute})
	key := "127.0.0.1|person@example.com"
	now := time.Now()
	if err := service.failedLogin(key, now); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("first failure = %v", err)
	}
	if err := service.failedLogin(key, now); !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("second failure = %v", err)
	}
	if !service.loginBlocked(key, now.Add(30*time.Second)) {
		t.Fatal("login was not blocked")
	}
	if service.loginBlocked(key, now.Add(2*time.Minute)) {
		t.Fatal("expired login block remained active")
	}
}

func TestLoginRetryAfterUsesConfiguredLockout(t *testing.T) {
	service := NewService(nil, ServiceOptions{LoginLockout: 1500 * time.Millisecond})
	if got := service.LoginRetryAfterSeconds(); got != 2 {
		t.Fatalf("LoginRetryAfterSeconds() = %d, want 2", got)
	}
}

func TestCSRFProtection(t *testing.T) {
	called := false
	handler := ProtectCSRF(false, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))

	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, httptest.NewRequest(http.MethodGet, "/login", nil))
	result := getResponse.Result()
	cookies := result.Cookies()
	if len(cookies) == 0 {
		t.Fatal("GET did not issue CSRF cookie")
	}
	token := cookies[0].Value

	form := url.Values{"_csrf": {token}}
	request := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(cookies[0])
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if !called {
		t.Fatal("valid CSRF token was rejected")
	}
}

func TestCSRFRejectsMissingToken(t *testing.T) {
	handler := ProtectCSRF(false, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("request without token reached handler")
	}))
	request := httptest.NewRequest(http.MethodPost, "/login", nil)
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: strings.Repeat("a", 64)})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d", response.Code)
	}
}
