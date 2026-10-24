package main

import (
	"context"
	"embed"
	"log"
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
		MediaReferences: []platform.MediaReferenceRule{{ID: "project_logo", Label: "Project logo", Table: "projects", Column: "logo_media_id"}},
		BackgroundJobs:  []platform.BackgroundJob{{ID: "example.project_maintenance", Label: "Project maintenance", Interval: 24 * time.Hour, Enabled: false, Run: func(context.Context) error { return nil }}},
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

func registerPages(pages *platform.Pages) error {
	return pages.Template("GET /{$}", platform.Page{ID: "home", Label: "Home", Path: "/", Navigation: true}, "home", "Home", nil)
}
