package pages

import (
	"net/http"

	"script-php/goweb/internal/auth"
)

// Account is an example protected page. Register it through auth.RequireLogin
// only after the application provides a SessionResolver.
func (pages *Pages) Account(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r.Context())
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	pages.renderer.Render(w, r, http.StatusOK, "account", "Your account", struct{ Notice, Error string }{r.URL.Query().Get("notice"), r.URL.Query().Get("error")})
}
