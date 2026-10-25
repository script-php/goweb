package main

import (
	"context"
	"embed"
	"html/template"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"script-php/goweb/platform"
)

// These folders belong to this application and may be named differently by
// changing Resources and Schemas below.
//
//go:embed resources
var resources embed.FS

//go:embed tables
var schemas embed.FS

func main() {
	cfg, _, err := platform.LoadConfig("")
	if err != nil {
		log.Fatal(err)
	}
	db, err := platform.OpenDatabase(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	runtime, err := platform.New(platform.Options{
		DB: db, Config: cfg, Version: "example",
		Resources:       platform.Resources{FS: resources, Root: "resources", TemplatesDir: "views", StaticDir: "public"},
		Schemas:         platform.Schemas{FS: schemas, Root: "tables", Existing: platform.SchemaMergeExisting},
		RegisterPages:   registerPages,
		RegisterAdmin:   registerAdmin,
		MediaReferences: []platform.MediaReferenceRule{{ID: "project_logo", Label: "Project logo", Table: "projects", Column: "logo_media_id"}},
		BackgroundJobs:  []platform.BackgroundJob{{ID: "example.project_maintenance", Label: "Project maintenance", Interval: 24 * time.Hour, Enabled: false, Run: func(context.Context) error { return nil }}},
		RateLimits: []platform.RateLimitPolicy{{ID: "example.home", Limit: 120, Window: time.Minute,
			Match: func(r *http.Request) bool { return r.Method == http.MethodGet && r.URL.Path == "/" }, Key: platform.RateLimitByIP}},
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := runtime.Run(ctx); err != nil {
		log.Fatal(err)
	}
}

func registerAdmin(admin *platform.Admin) error {
	if err := admin.AddDashboardWidget(platform.DashboardWidget{ID: "example.projects", Title: "Projects", Render: func(*http.Request) (template.HTML, error) {
		return platform.HTML(`<p>Project management can be added here.</p>`), nil
	}}); err != nil {
		return err
	}
	return admin.Handle("GET /admin/projects", platform.Page{ID: "projects", Label: "Projects", Path: "/admin/projects", Description: "Manage projects", Permission: "admin.projects.view", Navigation: true, Order: 70}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admin.Render(w, r, http.StatusOK, "projects", "Projects", nil)
	}))
}

func registerPages(pages *platform.Pages) error {
	return pages.Template("GET /{$}", platform.Page{ID: "home", Label: "Home", Path: "/", Navigation: true}, "home", "Home", nil)
}
