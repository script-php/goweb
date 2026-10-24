package platform

import (
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestOverlayPrefersApplicationAndMergesDirectories(t *testing.T) {
	application := fstest.MapFS{"templates/pages/home.html": {Data: []byte("application")}}
	base := fstest.MapFS{"templates/pages/home.html": {Data: []byte("platform")}, "templates/pages/login.html": {Data: []byte("login")}}
	files := Overlay(application, base)
	contents, err := fs.ReadFile(files, "templates/pages/home.html")
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "application" {
		t.Fatalf("override = %q", contents)
	}
	entries, err := fs.ReadDir(files, "templates/pages")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
}

func TestResourceFilesMapsCustomDirectories(t *testing.T) {
	application := fstest.MapFS{
		"bundle/views/pages/login.html": {Data: []byte("custom login")},
		"bundle/public/example.css":     {Data: []byte("custom css")},
	}
	files, err := resourceFiles(Resources{FS: application, Root: "bundle", TemplatesDir: "views", StaticDir: "public"})
	if err != nil {
		t.Fatal(err)
	}
	login, err := fs.ReadFile(files, "templates/pages/login.html")
	if err != nil {
		t.Fatal(err)
	}
	if string(login) != "custom login" {
		t.Fatalf("login override = %q", login)
	}
	stylesheet, err := fs.ReadFile(files, "static/example.css")
	if err != nil {
		t.Fatal(err)
	}
	if string(stylesheet) != "custom css" {
		t.Fatalf("stylesheet = %q", stylesheet)
	}
	if _, err := fs.ReadFile(files, "templates/base.html"); err != nil {
		t.Fatalf("platform fallback missing: %v", err)
	}
}
