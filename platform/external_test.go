package platform_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// This test guards the supported-package boundary by compiling a completely
// separate module that can only import exported platform packages.
func TestExternalModuleCanCompileAgainstPlatform(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}
	moduleRoot := filepath.Dir(filepath.Dir(filename))
	temporary := t.TempDir()
	goMod := "module consumer.example/site\n\ngo 1.24.0\n\nrequire script-php/goweb v0.0.0\nreplace script-php/goweb => " + filepath.ToSlash(moduleRoot) + "\n"
	source := `package main

import (
	"context"
	"io"
	"net/http"
	"time"

	"script-php/goweb/platform"
	configuration "script-php/goweb/platform/configuration"
)

func pages(p *platform.Pages) error {
	return p.Handle("GET /example", platform.Page{ID: "example", Path: "/example"}, platform.RequireLogin("/login", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))
}

func admin(a *platform.Admin) error {
	return a.AddDashboardWidget(platform.DashboardWidget{ID: "example", Title: "Example"})
}

var _ = platform.Options{
	DB: nil, Config: configuration.Config{}, Version: "contract",
	Resources: platform.Resources{}, Schemas: platform.Schemas{}, Features: platform.DefaultFeatures(),
	RegisterPages: pages, RegisterAdmin: admin, PublicOverrides: map[string]http.Handler{}, AdminOverrides: map[string]http.Handler{},
	MediaBackend: nil, MediaReferences: []platform.MediaReferenceRule{}, BackgroundJobs: []platform.BackgroundJob{},
	RateLimits: []platform.RateLimitPolicy{}, Backups: nil,
}
var _ = platform.RateLimitPolicy{ID: "example", Limit: 10, Window: time.Minute, Match: func(r *http.Request) bool { return r.URL.Path == "/example" }, Key: platform.RateLimitByIP}
var _ *platform.AuthenticatedUser = platform.CurrentUser(context.Background())
var _ func(string) (platform.Config, string, error) = platform.LoadConfig
var _ func(string, http.Handler) http.Handler = platform.RequireLogin
var _ func(string, http.Handler) http.Handler = platform.RequireRole
var _ func(string, http.Handler) http.Handler = platform.RequirePermission
var _ func(string, string) string = platform.SafeRedirectPath
var _ func(*platform.Pages, string, platform.Page, http.Handler) error = (*platform.Pages).Handle
var _ func(*platform.Admin, string, platform.Page, http.Handler) error = (*platform.Admin).Handle
var _ func(*platform.Admin, http.ResponseWriter, *http.Request, int, string, string, any) = (*platform.Admin).Render
var _ func(*platform.Media, context.Context, string, io.Reader, platform.MediaOptions) (platform.MediaFile, error) = (*platform.Media).Save
var _ func(*platform.Media, context.Context, uint64) (platform.MediaFile, io.ReadCloser, error) = (*platform.Media).Open
var _ func(*platform.Runtime, context.Context) error = (*platform.Runtime).Run
var _ *platform.Media

func main() {}
`
	if err := os.WriteFile(filepath.Join(temporary, "go.mod"), []byte(goMod), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(temporary, "main.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, arguments := range [][]string{{"mod", "tidy"}, {"build", "."}} {
		command := exec.Command("go", arguments...)
		command.Dir = temporary
		command.Env = append(os.Environ(), "GOWORK=off")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("external module command go %s failed: %v\n%s", strings.Join(arguments, " "), err, strings.TrimSpace(string(output)))
		}
	}
}
