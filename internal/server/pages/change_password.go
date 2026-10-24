package pages

import (
	"net/http"

	"script-php/goweb/internal/auth"
)

type changePasswordData struct {
	Error string
}

func (pages *Pages) ChangePassword(w http.ResponseWriter, r *http.Request) {
	if auth.CurrentUser(r.Context()) == nil {
		http.Redirect(w, r, "/login?next=%2Fchange-password", http.StatusSeeOther)
		return
	}
	pages.renderer.Render(w, r, http.StatusOK, "change-password", "Change password", changePasswordData{})
}

func (pages *Pages) ChangePasswordSubmit(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r.Context())
	if user == nil || pages.auth == nil {
		http.Redirect(w, r, "/login?next=%2Fchange-password", http.StatusSeeOther)
		return
	}
	password := r.FormValue("password")
	if password != r.FormValue("password_confirmation") {
		pages.renderer.Render(w, r, http.StatusUnprocessableEntity, "change-password", "Change password", changePasswordData{Error: "Passwords do not match"})
		return
	}
	cookie, err := r.Cookie(auth.SessionCookieName)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := pages.auth.ChangePassword(r.Context(), user.ID, cookie.Value, password); err != nil {
		pages.renderer.Render(w, r, http.StatusUnprocessableEntity, "change-password", "Change password", changePasswordData{Error: err.Error()})
		return
	}
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}
