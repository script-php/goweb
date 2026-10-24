//go:build integration

package server

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"script-php/goweb/internal/auth"
)

var csrfField = regexp.MustCompile(`name="_csrf" value="([^"]+)"`)

func TestBrowserLoginAdminLogoutIntegration(t *testing.T) {
	dsn := os.Getenv("WEBSITE_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("WEBSITE_INTEGRATION_DSN is not set")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	email := "browser-" + strings.ReplaceAll(time.Now().UTC().Format("20060102150405.000000000"), ".", "") + "@example.invalid"
	password := "Integration!2345"
	if err := auth.BootstrapAdministrator(context.Background(), db, email, password); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec("DELETE FROM users WHERE email = ?", email) })

	authentication := auth.NewService(db, auth.ServiceOptions{SessionLength: time.Hour, MaxLoginAttempts: 2, LoginLockout: 2 * time.Second})
	handler, err := New(os.DirFS("../../platform/web"), db, "integration", WithAuthentication(authentication))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	loginPage := browserGet(t, client, server.URL+"/login")
	csrf := extractCSRF(t, loginPage)
	response, err := client.PostForm(server.URL+"/login", url.Values{"_csrf": {csrf}, "email": {email}, "password": {password}, "next": {"/admin/"}})
	if err != nil {
		t.Fatal(err)
	}
	adminPage := readResponse(t, response)
	if response.StatusCode != http.StatusOK || !strings.Contains(adminPage, "Dashboard") || !strings.Contains(adminPage, email) {
		t.Fatalf("login did not reach dashboard: status=%d", response.StatusCode)
	}

	csrf = extractCSRF(t, adminPage)
	response, err = client.PostForm(server.URL+"/logout", url.Values{"_csrf": {csrf}})
	if err != nil {
		t.Fatal(err)
	}
	_ = readResponse(t, response)
	response, err = client.Get(server.URL + "/admin/")
	if err != nil {
		t.Fatal(err)
	}
	body := readResponse(t, response)
	if response.Request.URL.Path != "/login" || !strings.Contains(body, "Sign in") {
		t.Fatalf("logout left admin accessible: final=%s status=%d", response.Request.URL.Path, response.StatusCode)
	}

	limitedEmail := "limited-" + email
	if err := auth.BootstrapAdministrator(context.Background(), db, limitedEmail, password); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec("DELETE FROM users WHERE email = ?", limitedEmail) })
	loginPage = browserGet(t, client, server.URL+"/login")
	csrf = extractCSRF(t, loginPage)
	for attempt := 1; attempt <= 2; attempt++ {
		response, err = client.PostForm(server.URL+"/login", url.Values{"_csrf": {csrf}, "email": {limitedEmail}, "password": {"wrong password"}, "next": {"/admin/"}})
		if err != nil {
			t.Fatal(err)
		}
		body = readResponse(t, response)
		if attempt == 1 && response.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("first failed login status=%d", response.StatusCode)
		}
		if attempt == 2 && (response.StatusCode != http.StatusTooManyRequests || response.Header.Get("Retry-After") != "2" || !strings.Contains(body, "Too many attempts")) {
			t.Fatalf("locked login status=%d retry-after=%q", response.StatusCode, response.Header.Get("Retry-After"))
		}
	}
}

func browserGet(t *testing.T, client *http.Client, address string) string {
	t.Helper()
	response, err := client.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	return readResponse(t, response)
}

func readResponse(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	contents, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func extractCSRF(t *testing.T, body string) string {
	t.Helper()
	match := csrfField.FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatal("page does not contain a CSRF token")
	}
	return match[1]
}
