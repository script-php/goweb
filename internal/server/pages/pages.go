// Package pages contains the website's page handlers. Add one Go file per page
// or closely related feature, then register its exported handler in routes.go.
package pages

import (
	"net/http"

	"script-php/goweb/internal/auth"
)

// Renderer keeps page handlers independent from the template implementation.
// Tests can replace it with a small fake without parsing HTML templates.
type Renderer interface {
	Render(w http.ResponseWriter, r *http.Request, status int, page, title string, data any)
}

type Pages struct {
	renderer Renderer
	auth     *auth.Service
}

func New(renderer Renderer, authentication ...*auth.Service) *Pages {
	pages := &Pages{renderer: renderer}
	if len(authentication) > 0 {
		pages.auth = authentication[0]
	}
	return pages
}
