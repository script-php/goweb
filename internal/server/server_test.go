package server

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestHome(t *testing.T) {
	files := fstest.MapFS{
		"templates/index.html": {Data: []byte(`<!doctype html><title>Test</title><p>{{.Version}}</p>`)},
		"static/app.css":       {Data: []byte(`body { color: black; }`)},
	}
	handler, err := New(fs.FS(files), nil, "1.2.3")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !strings.Contains(response.Body.String(), "1.2.3") {
		t.Fatalf("body = %q, want version", response.Body.String())
	}
	if response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("security headers were not applied")
	}
}

func TestStaticFiles(t *testing.T) {
	files := fstest.MapFS{
		"templates/index.html": {Data: []byte(`<!doctype html><title>Test</title>`)},
		"static/app.css":       {Data: []byte(`body { color: black; }`)},
	}
	handler, err := New(files, nil, "test")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/static/app.css", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Body.String() != `body { color: black; }` {
		t.Fatalf("body = %q", response.Body.String())
	}
}
