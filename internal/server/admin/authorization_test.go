package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"script-php/goweb/internal/auth"
	"script-php/goweb/internal/server/catalog"
)

func TestEveryBuiltinAdminRouteRequiresItsPermission(t *testing.T) {
	area := &Admin{mux: http.NewServeMux(), catalog: catalog.New(), overrides: make(map[string]http.Handler), backups: &BackupOperations{}, features: DefaultFeatures()}
	if err := (builtinModule{}).Register(area); err != nil {
		t.Fatal(err)
	}
	methods := map[string]string{
		"dashboard": "GET", "users": "GET", "users.new": "GET", "users.create": "POST", "users.edit": "GET", "users.update": "POST", "users.delete": "POST", "users.revoke-sessions": "POST", "users.avatar": "GET", "users.avatar.update": "POST", "users.avatar.remove": "POST",
		"roles": "GET", "roles.new": "GET", "roles.create": "POST", "roles.edit": "GET", "roles.update": "POST", "roles.delete": "POST",
		"pages": "GET", "audit": "GET", "audit.export": "GET", "emails": "GET", "emails.retry": "POST", "settings": "GET", "settings.update": "POST", "settings.test-email": "POST",
		"jobs": "GET", "jobs.run": "POST", "jobs.update": "POST",
		"backups": "GET", "backups.create": "POST", "backups.download": "GET", "backups.verify": "POST",
		"media": "GET", "media.upload": "POST", "media.bulk-delete": "POST", "media.edit": "GET", "media.update": "POST", "media.download": "GET", "media.delete": "POST", "media.cleanup": "GET", "media.cleanup.settings": "POST", "media.cleanup.run": "POST",
	}
	registered := area.catalog.Pages(catalog.AreaAdmin)
	if len(registered) != len(methods) {
		t.Fatalf("registered %d routes, authorization table has %d", len(registered), len(methods))
	}
	for _, page := range registered {
		method, covered := methods[page.ID]
		if !covered {
			t.Errorf("route %q is missing from authorization test", page.ID)
			continue
		}
		if page.Permission == "" {
			t.Errorf("route %q has no permission", page.ID)
			continue
		}
		requestPath := strings.ReplaceAll(page.Path, "{id}", "1")
		request := httptest.NewRequest(method, requestPath, nil)
		request = request.WithContext(auth.WithUser(request.Context(), &auth.User{ID: 1, Email: "limited@example.test"}))
		response := httptest.NewRecorder()
		area.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Errorf("%s %s (%s) without %s returned %d, want 403", method, requestPath, page.ID, page.Permission, response.Code)
		}
	}
}

func TestAdminRouteRedirectsAnonymousUser(t *testing.T) {
	area := &Admin{mux: http.NewServeMux(), catalog: catalog.New(), overrides: make(map[string]http.Handler), features: DefaultFeatures()}
	if err := (builtinModule{}).Register(area); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	area.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/admin/", nil))
	if response.Code != http.StatusSeeOther || !strings.HasPrefix(response.Header().Get("Location"), "/login") {
		t.Fatalf("anonymous response = %d location=%q", response.Code, response.Header().Get("Location"))
	}
}
