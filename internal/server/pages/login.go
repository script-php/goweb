package pages

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"script-php/goweb/internal/auth"
	"script-php/goweb/internal/clientip"
)

type loginData struct {
	Email  string
	Next   string
	Error  string
	Notice string
}

func (pages *Pages) Login(w http.ResponseWriter, r *http.Request) {
	if auth.CurrentUser(r.Context()) != nil {
		http.Redirect(w, r, "/admin/", http.StatusSeeOther)
		return
	}
	next := auth.SafeRedirectPath(r.URL.Query().Get("next"), "/admin/")
	pages.renderer.Render(w, r, http.StatusOK, "login", "Sign in", loginData{Next: next, Notice: r.URL.Query().Get("notice")})
}

func (pages *Pages) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	if pages.auth == nil {
		http.Error(w, "Authentication is not configured", http.StatusServiceUnavailable)
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	next := auth.SafeRedirectPath(r.FormValue("next"), "/admin/")
	token, _, err := pages.auth.Login(r.Context(), email, r.FormValue("password"), clientip.FromRequest(r), r.UserAgent())
	if errors.Is(err, auth.ErrTwoFactorRequired) {
		http.Redirect(w, r, "/two-factor?challenge="+url.QueryEscape(token)+"&next="+url.QueryEscape(next), http.StatusSeeOther)
		return
	}
	if errors.Is(err, auth.ErrInvalidCredentials) {
		pages.renderer.Render(w, r, http.StatusUnprocessableEntity, "login", "Sign in", loginData{Email: email, Next: next, Error: "Invalid email or password"})
		return
	}
	if errors.Is(err, auth.ErrTooManyAttempts) {
		w.Header().Set("Retry-After", strconv.Itoa(pages.auth.LoginRetryAfterSeconds()))
		pages.renderer.Render(w, r, http.StatusTooManyRequests, "login", "Sign in", loginData{Email: email, Next: next, Error: "Too many attempts. Please wait before trying again."})
		return
	}
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	pages.auth.SetSessionCookie(w, token)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (pages *Pages) Logout(w http.ResponseWriter, r *http.Request) {
	if pages.auth == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil {
		_ = pages.auth.Logout(r.Context(), cookie.Value, clientip.FromRequest(r))
	}
	pages.auth.ClearSessionCookie(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
