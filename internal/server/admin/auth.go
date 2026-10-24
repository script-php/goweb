package admin

import (
	"net/http"

	"script-php/goweb/internal/auth"
)

func currentUser(r *http.Request) *auth.User { return auth.CurrentUser(r.Context()) }
