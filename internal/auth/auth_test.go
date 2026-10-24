package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeResolver struct {
	user *User
	err  error
}

func (resolver fakeResolver) ResolveSession(context.Context, string) (*User, error) {
	return resolver.user, resolver.err
}

func TestAuthenticateAddsUserToContext(t *testing.T) {
	want := &User{ID: 42, Email: "person@example.com"}
	var got *User
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = CurrentUser(r.Context())
	})
	handler := Authenticate("session", fakeResolver{user: want}, next)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: "session", Value: "opaque-token"})
	handler.ServeHTTP(httptest.NewRecorder(), request)

	if got != want {
		t.Fatalf("CurrentUser() = %#v, want %#v", got, want)
	}
}

func TestAuthenticateHandlesResolverError(t *testing.T) {
	handler := Authenticate("session", fakeResolver{err: errors.New("database unavailable")}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next handler ran after resolver error")
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: "session", Value: "opaque-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}

func TestRequireLogin(t *testing.T) {
	handler := RequireLogin("/login", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("protected handler ran for anonymous request")
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/account?tab=billing", nil))

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusSeeOther)
	}
	if location := response.Header().Get("Location"); location != "/login?next=%2Faccount%3Ftab%3Dbilling" {
		t.Fatalf("Location = %q", location)
	}
}

func TestRequireRole(t *testing.T) {
	nextCalled := false
	handler := RequireRole("admin", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	}))
	request := httptest.NewRequest(http.MethodGet, "/admin", nil)
	request = request.WithContext(WithUser(request.Context(), &User{Roles: []string{"admin"}}))
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if !nextCalled {
		t.Fatal("protected handler did not run for matching role")
	}
}

func TestRequirePermission(t *testing.T) {
	nextCalled := false
	handler := RequirePermission("admin.users.view", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	}))
	request := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	request = request.WithContext(WithUser(request.Context(), &User{Permissions: []string{"admin.users.view"}}))
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if !nextCalled {
		t.Fatal("protected handler did not run for matching permission")
	}
}

func TestRequirePasswordChangeRedirectsProtectedPage(t *testing.T) {
	handler := RequirePasswordChange(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("protected page ran before password change")
	}))
	request := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	request = request.WithContext(WithUser(request.Context(), &User{ID: 1, ForcePasswordChange: true}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/change-password" {
		t.Fatalf("status/location = %d %q", response.Code, response.Header().Get("Location"))
	}
}

func TestRequirePasswordChangeAllowsChangeForm(t *testing.T) {
	called := false
	handler := RequirePasswordChange(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	request := httptest.NewRequest(http.MethodPost, "/change-password", nil)
	request = request.WithContext(WithUser(request.Context(), &User{ID: 1, ForcePasswordChange: true}))
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if !called {
		t.Fatal("password change form was blocked")
	}
}

func TestSafeRedirectPath(t *testing.T) {
	tests := map[string]string{
		"/account":               "/account",
		"/account?tab=profile":   "/account?tab=profile",
		"https://example.com":    "/",
		"//example.com/redirect": "/",
		"":                       "/",
	}
	for value, want := range tests {
		if got := SafeRedirectPath(value, "/"); got != want {
			t.Errorf("SafeRedirectPath(%q) = %q, want %q", value, got, want)
		}
	}
}
