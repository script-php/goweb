package server

import (
	"database/sql"
	"io/fs"
	"net/http"

	"script-php/goweb/internal/auth"
	"script-php/goweb/internal/server/catalog"
	"script-php/goweb/internal/server/pages"
)

// registerPublicRoutes is the single overview of public pages. Metadata powers
// both website navigation and the admin page catalog.
type Public struct {
	mux       *http.ServeMux
	catalog   *catalog.Catalog
	pages     *pages.Pages
	renderer  pages.Renderer
	overrides map[string]http.Handler
}

func (public *Public) Handle(pattern string, page catalog.Page, handler http.Handler) error {
	page.Area = catalog.AreaPublic
	if err := public.catalog.Register(page); err != nil {
		return err
	}
	public.mux.Handle(pattern, handler)
	return nil
}

func (public *Public) Template(pattern string, page catalog.Page, templateName, title string, data any) error {
	return public.Handle(pattern, page, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		public.renderer.Render(w, r, http.StatusOK, templateName, title, data)
	}))
}

func (public *Public) handler(pageID string, fallback http.Handler) http.Handler {
	if replacement := public.overrides[pageID]; replacement != nil {
		return replacement
	}
	return fallback
}

func registerPublicRoutes(public *Public) error {
	routes := []struct {
		pattern string
		page    catalog.Page
		handler http.HandlerFunc
	}{
		{"GET /login", catalog.Page{ID: "login", Area: catalog.AreaPublic, Label: "Login", Path: "/login", Description: "Sign in to the website"}, public.pages.Login},
		{"POST /login", catalog.Page{ID: "login.submit", Area: catalog.AreaPublic, Label: "Login action", Path: "/login"}, public.pages.LoginSubmit},
		{"POST /logout", catalog.Page{ID: "logout", Area: catalog.AreaPublic, Label: "Logout action", Path: "/logout"}, public.pages.Logout},
		{"GET /forgot-password", catalog.Page{ID: "forgot-password", Area: catalog.AreaPublic, Label: "Forgot password", Path: "/forgot-password", Description: "Request a password reset"}, public.pages.ForgotPassword},
		{"POST /forgot-password", catalog.Page{ID: "forgot-password.submit", Area: catalog.AreaPublic, Label: "Forgot password action", Path: "/forgot-password"}, public.pages.ForgotPasswordSubmit},
		{"GET /reset-password", catalog.Page{ID: "reset-password", Area: catalog.AreaPublic, Label: "Reset password", Path: "/reset-password"}, public.pages.ResetPassword},
		{"POST /reset-password", catalog.Page{ID: "reset-password.submit", Area: catalog.AreaPublic, Label: "Reset password action", Path: "/reset-password"}, public.pages.ResetPasswordSubmit},
		{"GET /verify-email", catalog.Page{ID: "verify-email", Area: catalog.AreaPublic, Label: "Verify email", Path: "/verify-email"}, public.pages.VerifyEmail},
		{"POST /send-verification", catalog.Page{ID: "verify-email.send", Area: catalog.AreaPublic, Label: "Send verification action", Path: "/send-verification"}, public.pages.SendVerification},
		{"GET /two-factor", catalog.Page{ID: "two-factor", Area: catalog.AreaPublic, Label: "Two-factor authentication", Path: "/two-factor"}, public.pages.TwoFactor},
		{"POST /two-factor", catalog.Page{ID: "two-factor.submit", Area: catalog.AreaPublic, Label: "Two-factor action", Path: "/two-factor"}, public.pages.TwoFactorSubmit},
		{"POST /account/two-factor", catalog.Page{ID: "account.two-factor", Area: catalog.AreaPublic, Label: "Update two-factor action", Path: "/account/two-factor"}, public.pages.TwoFactorUpdate},
		{"GET /change-password", catalog.Page{ID: "change-password", Area: catalog.AreaPublic, Label: "Change password", Path: "/change-password", Description: "Update the current account password"}, public.pages.ChangePassword},
		{"POST /change-password", catalog.Page{ID: "change-password.submit", Area: catalog.AreaPublic, Label: "Change password action", Path: "/change-password"}, public.pages.ChangePasswordSubmit},
	}
	for _, route := range routes {
		if err := public.Handle(route.pattern, route.page, public.handler(route.page.ID, route.handler)); err != nil {
			return err
		}
	}
	account := auth.RequireLogin("/login", public.handler("account", http.HandlerFunc(public.pages.Account)))
	if err := public.Handle("GET /account", catalog.Page{ID: "account", Area: catalog.AreaPublic, Label: "Account", Path: "/account", Description: "Current user account"}, account); err != nil {
		return err
	}
	return nil
}

func registerInfrastructureRoutes(mux *http.ServeMux, db *sql.DB, staticFiles fs.FS, version string, readiness *Readiness) {
	mux.HandleFunc("GET /health", healthSummary(db, version, readiness))
	mux.HandleFunc("GET /health/live", healthLive(version))
	mux.HandleFunc("GET /health/ready", healthReady(db, version, readiness))
	staticHandler := http.StripPrefix("/static/", http.FileServer(http.FS(staticFiles)))
	mux.Handle("GET /static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Static asset URLs include the application version. Revalidation here also
		// prevents a proxy or browser from pairing new HTML with stale CSS/JS.
		w.Header().Set("Cache-Control", "no-cache")
		staticHandler.ServeHTTP(w, r)
	}))
}
