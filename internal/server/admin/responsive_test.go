package admin

import (
	"os"
	"strings"
	"testing"
)

func TestAdminResponsiveLayoutContract(t *testing.T) {
	cssBytes, err := os.ReadFile("../../../platform/web/static/css/admin.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(cssBytes)
	for _, required := range []string{
		"@media (max-width:800px)",
		"@media (max-width:600px)",
		"@media (max-width:360px)",
		".admin-sidebar { position:fixed",
		".table-wrap table,.table-wrap tbody,.table-wrap tr,.table-wrap td { display:block",
		".stat-grid { grid-template-columns:repeat(2,minmax(0,1fr))",
	} {
		if !strings.Contains(css, required) {
			t.Errorf("responsive stylesheet is missing %q", required)
		}
	}
	baseBytes, err := os.ReadFile("../../../platform/web/templates/admin/base.html")
	if err != nil {
		t.Fatal(err)
	}
	base := string(baseBytes)
	for _, required := range []string{`name="viewport"`, `data-menu-open`, `aria-controls="admin-sidebar"`, `data-menu-close`} {
		if !strings.Contains(base, required) {
			t.Errorf("admin layout is missing mobile control %q", required)
		}
	}
}
