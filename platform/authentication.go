package platform

import (
	"context"
	"net/http"

	"script-php/goweb/internal/auth"
)

// AuthenticatedUser is the signed-in user available to application handlers.
type AuthenticatedUser = auth.User

// CurrentUser returns the authenticated user stored in ctx, or nil.
func CurrentUser(ctx context.Context) *AuthenticatedUser { return auth.CurrentUser(ctx) }

// RequireLogin redirects anonymous requests to loginPath.
func RequireLogin(loginPath string, next http.Handler) http.Handler {
	return auth.RequireLogin(loginPath, next)
}

// RequireRole requires an authenticated user with the supplied role slug.
func RequireRole(role string, next http.Handler) http.Handler { return auth.RequireRole(role, next) }

// RequirePermission requires an authenticated user with the supplied permission.
func RequirePermission(permission string, next http.Handler) http.Handler {
	return auth.RequirePermission(permission, next)
}

// SafeRedirectPath accepts only local absolute paths and otherwise returns fallback.
func SafeRedirectPath(value, fallback string) string { return auth.SafeRedirectPath(value, fallback) }
