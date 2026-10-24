package auth

import (
	"context"
	"crypto/subtle"
	"mime"
	"net/http"
)

const csrfCookieName = "website_csrf"

type csrfContextKey struct{}

func CSRFToken(ctx context.Context) string {
	token, _ := ctx.Value(csrfContextKey{}).(string)
	return token
}

func ProtectCSRF(secureCookies bool, next http.Handler) http.Handler {
	return ProtectCSRFWithMaxBytes(secureCookies, 11<<20, next)
}

func ProtectCSRFWithMaxBytes(secureCookies bool, maxRequestBytes int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := ""
		if cookie, err := r.Cookie(csrfCookieName); err == nil {
			token = cookie.Value
		}
		if len(token) != 64 {
			var err error
			token, err = randomToken(32)
			if err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: csrfCookieName, Value: token, Path: "/", MaxAge: 86400, HttpOnly: true, Secure: secureCookies, SameSite: http.SameSiteStrictMode})
		}

		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if mediaType == "multipart/form-data" && maxRequestBytes > 0 {
				r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
			}
			var parseErr error
			if mediaType == "multipart/form-data" {
				parseErr = r.ParseMultipartForm(1 << 20)
			} else {
				parseErr = r.ParseForm()
			}
			if parseErr != nil || subtle.ConstantTimeCompare([]byte(token), []byte(r.FormValue("_csrf"))) != 1 {
				RespondError(w, r, http.StatusForbidden)
				return
			}
			if r.MultipartForm != nil {
				defer r.MultipartForm.RemoveAll()
			}
		}
		r = r.WithContext(context.WithValue(r.Context(), csrfContextKey{}, token))
		next.ServeHTTP(w, r)
	})
}
