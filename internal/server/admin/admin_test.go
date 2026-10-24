package admin

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNotificationsUseValidatedKinds(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/admin/users?notice=Careful&notice_type=warning", nil)
	items := notifications(request, "Invalid form")
	if len(items) != 2 || items[0].Kind != "error" || items[1].Kind != "warning" {
		t.Fatalf("notifications = %#v", items)
	}
	request = httptest.NewRequest(http.MethodGet, "/admin/users?notice=Done&notice_type=unexpected", nil)
	items = notifications(request, "")
	if len(items) != 1 || items[0].Kind != "success" {
		t.Fatalf("default notification = %#v", items)
	}
}

func TestAdminExtensionsRejectDuplicateIDs(t *testing.T) {
	area := &Admin{overrides: make(map[string]http.Handler)}
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	if err := area.Override("users.edit", handler); err != nil {
		t.Fatal(err)
	}
	if err := area.Override("users.edit", handler); err == nil {
		t.Fatal("duplicate override accepted")
	}
	widget := DashboardWidget{ID: "orders", Title: "Orders", Render: func(*http.Request) (template.HTML, error) { return "", nil }}
	if err := area.AddDashboardWidget(widget); err != nil {
		t.Fatal(err)
	}
	if err := area.AddDashboardWidget(widget); err == nil {
		t.Fatal("duplicate widget accepted")
	}
}

func TestSameOriginRejectsCrossSitePost(t *testing.T) {
	handler := sameOrigin(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("cross-site request reached handler")
	}))
	request := httptest.NewRequest(http.MethodPost, "https://example.com/admin/settings", nil)
	request.Header.Set("Origin", "https://attacker.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestSameOriginAllowsMatchingPost(t *testing.T) {
	called := false
	handler := sameOrigin(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	request := httptest.NewRequest(http.MethodPost, "https://example.com/admin/settings", nil)
	request.Header.Set("Origin", "https://example.com")
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if !called {
		t.Fatal("same-origin request did not reach handler")
	}
}

func TestFinalAdministratorCannotLoseRole(t *testing.T) {
	if canRemoveAdministratorRole(true, false, 1) {
		t.Fatal("final active administrator was allowed to lose role")
	}
	if !canRemoveAdministratorRole(true, false, 2) {
		t.Fatal("administrator was blocked despite another active administrator")
	}
	if !canRemoveAdministratorRole(true, true, 1) {
		t.Fatal("keeping the administrator role was blocked")
	}
}
