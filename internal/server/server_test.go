package server

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"script-php/goweb/internal/server/catalog"
	"script-php/goweb/internal/server/render"
)

func TestHome(t *testing.T) {
	files := fstest.MapFS{
		"templates/base.html":       {Data: []byte(`{{define "base"}}<title>{{.Title}}</title>{{template "content" .}}<p>{{.Version}}</p>{{end}}`)},
		"templates/pages/home.html": {Data: []byte(`{{define "content"}}Home page{{end}}`)},
		"static/css/app.css":        {Data: []byte(`body { color: black; }`)},
	}
	// Admin initialization needs a database. Renderer behavior is covered in
	// focused package tests; this fixture includes all embedded templates only in
	// application-level integration tests.
	pageCatalog := catalog.New()
	if err := pageCatalog.Register(catalog.Page{ID: "home", Area: catalog.AreaPublic, Label: "Home", Path: "/", Navigation: true}); err != nil {
		t.Fatal(err)
	}
	renderer, err := render.New(fs.FS(files), "1.2.3", pageCatalog, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	renderer.Render(response, request, http.StatusOK, "home", "Home", nil)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !strings.Contains(response.Body.String(), "1.2.3") {
		t.Fatalf("body = %q, want version", response.Body.String())
	}
}

func TestStaticFiles(t *testing.T) {
	files := fstest.MapFS{
		"templates/base.html":       {Data: []byte(`{{define "base"}}{{template "content" .}}{{end}}`)},
		"templates/pages/home.html": {Data: []byte(`{{define "content"}}Home{{end}}`)},
		"static/css/app.css":        {Data: []byte(`body { color: black; }`)},
	}
	handler := http.StripPrefix("/static/", http.FileServer(http.FS(mustSub(t, files, "static"))))

	request := httptest.NewRequest(http.MethodGet, "/static/css/app.css", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Body.String() != `body { color: black; }` {
		t.Fatalf("body = %q", response.Body.String())
	}
}

func mustSub(t *testing.T, files fs.FS, directory string) fs.FS {
	t.Helper()
	sub, err := fs.Sub(files, directory)
	if err != nil {
		t.Fatal(err)
	}
	return sub
}
