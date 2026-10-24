// Package auth provides authentication and authorization helpers without
// prescribing how a website stores users or sessions.
package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

type User struct {
	ID                  uint64
	AvatarMediaID       uint64
	Email               string
	DisplayName         string
	Roles               []string
	Permissions         []string
	ForcePasswordChange bool
	EmailVerified       bool
	TwoFactorEnabled    bool
}

func (user *User) HasPermission(permission string) bool {
	for _, assignedPermission := range user.Permissions {
		if assignedPermission == permission || assignedPermission == "*" {
			return true
		}
	}
	return false
}

func (user *User) HasRole(role string) bool {
	for _, assignedRole := range user.Roles {
		if assignedRole == role {
			return true
		}
	}
	return false
}

// SessionResolver connects these HTTP helpers to an application's session
// storage. Return (nil, nil) for an unknown or expired token.
type SessionResolver interface {
	ResolveSession(ctx context.Context, token string) (*User, error)
}

type contextKey struct{}
type errorResponderKey struct{}

type ErrorResponder func(http.ResponseWriter, *http.Request, int)

func WithErrorResponder(next http.Handler, responder ErrorResponder) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), errorResponderKey{}, responder)))
	})
}

func RespondError(w http.ResponseWriter, r *http.Request, status int) {
	if responder, ok := r.Context().Value(errorResponderKey{}).(ErrorResponder); ok {
		responder(w, r, status)
		return
	}
	http.Error(w, http.StatusText(status), status)
}

func CurrentUser(ctx context.Context) *User {
	user, _ := ctx.Value(contextKey{}).(*User)
	return user
}

func WithUser(ctx context.Context, user *User) context.Context {
	return context.WithValue(ctx, contextKey{}, user)
}

// Authenticate resolves a session cookie once and makes its user available to
// all downstream handlers through CurrentUser.
func Authenticate(cookieName string, resolver SessionResolver, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(cookieName)
		if errors.Is(err, http.ErrNoCookie) || cookie.Value == "" {
			next.ServeHTTP(w, r)
			return
		}
		if err != nil {
			RespondError(w, r, http.StatusBadRequest)
			return
		}

		user, err := resolver.ResolveSession(r.Context(), cookie.Value)
		if err != nil {
			slog.Error("resolve user session", "error", err)
			RespondError(w, r, http.StatusInternalServerError)
			return
		}
		if user != nil {
			r = r.WithContext(WithUser(r.Context(), user))
		}
		next.ServeHTTP(w, r)
	})
}

func RequireLogin(loginPath string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if CurrentUser(r.Context()) != nil {
			next.ServeHTTP(w, r)
			return
		}

		target := loginPath
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			target += "?next=" + url.QueryEscape(r.URL.RequestURI())
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
	})
}

func RequireRole(role string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := CurrentUser(r.Context())
		if user == nil {
			RespondError(w, r, http.StatusUnauthorized)
			return
		}
		if !user.HasRole(role) {
			RespondError(w, r, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func RequirePermission(permission string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := CurrentUser(r.Context())
		if user == nil {
			RespondError(w, r, http.StatusUnauthorized)
			return
		}
		if !user.HasPermission(permission) {
			RespondError(w, r, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func RequirePasswordChange(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := CurrentUser(r.Context())
		if user != nil && user.ForcePasswordChange && r.URL.Path != "/change-password" && r.URL.Path != "/logout" && !strings.HasPrefix(r.URL.Path, "/static/") {
			http.Redirect(w, r, "/change-password", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// SafeRedirectPath validates a login form's next value and prevents redirects
// to another host. Use fallback when the value is absent or unsafe.
func SafeRedirectPath(value, fallback string) string {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err != nil || value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || parsed.IsAbs() || parsed.Host != "" {
		return fallback
	}
	return value
}
