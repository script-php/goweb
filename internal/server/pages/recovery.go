package pages

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"script-php/goweb/internal/auth"
	"script-php/goweb/internal/clientip"
)

type recoveryData struct{ Token, Error, Notice string }
type twoFactorData struct{ Challenge, Next, Error string }

func (pages *Pages) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	pages.renderer.Render(w, r, http.StatusOK, "forgot-password", "Forgot password", recoveryData{})
}
func (pages *Pages) ForgotPasswordSubmit(w http.ResponseWriter, r *http.Request) {
	if pages.auth == nil {
		http.Error(w, "Authentication is not configured", http.StatusServiceUnavailable)
		return
	}
	if err := pages.auth.RequestPasswordReset(r.Context(), r.FormValue("email")); err != nil {
		slog.Error("send password recovery email", "error", err)
	}
	pages.renderer.Render(w, r, http.StatusOK, "forgot-password", "Forgot password", recoveryData{Notice: "If that account exists, a password reset link has been sent."})
}
func (pages *Pages) ResetPassword(w http.ResponseWriter, r *http.Request) {
	pages.renderer.Render(w, r, http.StatusOK, "reset-password", "Reset password", recoveryData{Token: r.URL.Query().Get("token")})
}
func (pages *Pages) ResetPasswordSubmit(w http.ResponseWriter, r *http.Request) {
	data := recoveryData{Token: r.FormValue("token")}
	password := r.FormValue("password")
	if password != r.FormValue("password_confirmation") {
		data.Error = "Passwords do not match"
		pages.renderer.Render(w, r, http.StatusUnprocessableEntity, "reset-password", "Reset password", data)
		return
	}
	if err := pages.auth.ResetPassword(r.Context(), data.Token, password); err != nil {
		data.Error = "The reset link is invalid or expired, or the password is not valid."
		pages.renderer.Render(w, r, http.StatusUnprocessableEntity, "reset-password", "Reset password", data)
		return
	}
	http.Redirect(w, r, "/login?notice=password-reset", http.StatusSeeOther)
}
func (pages *Pages) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	if pages.auth == nil || pages.auth.VerifyEmail(r.Context(), r.URL.Query().Get("token")) != nil {
		pages.renderer.Render(w, r, http.StatusUnprocessableEntity, "verify-email", "Verify email", recoveryData{Error: "The verification link is invalid or expired."})
		return
	}
	pages.renderer.Render(w, r, http.StatusOK, "verify-email", "Verify email", recoveryData{Notice: "Your email address is now verified."})
}
func (pages *Pages) SendVerification(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := pages.auth.SendVerification(r.Context(), user.ID, user.Email); err != nil {
		http.Redirect(w, r, "/account?error=verification-email", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/account?notice=verification-sent", http.StatusSeeOther)
}
func (pages *Pages) TwoFactor(w http.ResponseWriter, r *http.Request) {
	pages.renderer.Render(w, r, http.StatusOK, "two-factor", "Two-factor authentication", twoFactorData{Challenge: r.URL.Query().Get("challenge"), Next: auth.SafeRedirectPath(r.URL.Query().Get("next"), "/admin/")})
}
func (pages *Pages) TwoFactorSubmit(w http.ResponseWriter, r *http.Request) {
	data := twoFactorData{Challenge: r.FormValue("challenge"), Next: auth.SafeRedirectPath(r.FormValue("next"), "/admin/")}
	token, _, err := pages.auth.VerifyTwoFactor(r.Context(), data.Challenge, strings.TrimSpace(r.FormValue("code")), clientip.FromRequest(r), r.UserAgent())
	if errors.Is(err, auth.ErrInvalidToken) {
		data.Error = "Invalid or expired code"
		pages.renderer.Render(w, r, http.StatusUnprocessableEntity, "two-factor", "Two-factor authentication", data)
		return
	}
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	pages.auth.SetSessionCookie(w, token)
	http.Redirect(w, r, data.Next, http.StatusSeeOther)
}
func (pages *Pages) TwoFactorUpdate(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if !pages.auth.FeatureEnabled(r.Context(), "security.two_factor_available", true) {
		auth.RespondError(w, r, http.StatusForbidden)
		return
	}
	enabled := r.FormValue("enabled") == "true"
	if enabled && !pages.auth.EmailAvailable(r.Context()) {
		http.Redirect(w, r, "/account?error=email-disabled", http.StatusSeeOther)
		return
	}
	if enabled && !user.EmailVerified {
		http.Redirect(w, r, "/account?error=verify-before-2fa", http.StatusSeeOther)
		return
	}
	if err := pages.auth.SetTwoFactor(r.Context(), user.ID, enabled); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/account?notice="+url.QueryEscape("Two-factor authentication updated"), http.StatusSeeOther)
}
