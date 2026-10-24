package catalog

import "testing"

func TestCatalogSortsAndCollectsPermissions(t *testing.T) {
	pages := New()
	for _, page := range []Page{
		{ID: "second", Area: AreaAdmin, Label: "Second", Path: "/second", Permission: "content.edit", Order: 20},
		{ID: "first", Area: AreaAdmin, Label: "First", Path: "/first", Permission: "content.view", Order: 10},
		{ID: "home", Area: AreaPublic, Label: "Home", Path: "/"},
	} {
		if err := pages.Register(page); err != nil {
			t.Fatal(err)
		}
	}
	adminPages := pages.Pages(AreaAdmin)
	if len(adminPages) != 2 || adminPages[0].ID != "first" {
		t.Fatalf("unexpected pages: %#v", adminPages)
	}
	permissions := pages.Permissions()
	if len(permissions) != 2 || permissions[0] != "content.edit" || permissions[1] != "content.view" {
		t.Fatalf("unexpected permissions: %#v", permissions)
	}
}

func TestCatalogRejectsDuplicate(t *testing.T) {
	pages := New()
	page := Page{ID: "home", Area: AreaPublic, Label: "Home", Path: "/"}
	if err := pages.Register(page); err != nil {
		t.Fatal(err)
	}
	if err := pages.Register(page); err == nil {
		t.Fatal("duplicate page was accepted")
	}
}
