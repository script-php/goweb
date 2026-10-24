package pages

import (
	"net/http"
	"testing"
)

type renderCall struct {
	status int
	page   string
	title  string
}

type fakeRenderer struct {
	call renderCall
}

func (renderer *fakeRenderer) Render(_ http.ResponseWriter, _ *http.Request, status int, page, title string, _ any) {
	renderer.call = renderCall{status: status, page: page, title: title}
}

func TestNewUsesRenderer(t *testing.T) {
	renderer := &fakeRenderer{}
	pageHandlers := New(renderer)
	if pageHandlers.renderer != renderer {
		t.Fatal("New did not retain renderer")
	}
}
