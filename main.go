package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example.com/website/internal/config"
	appdb "example.com/website/internal/database"
	"example.com/website/internal/server"
	"github.com/script-php/tables"
)

// The database schema is compiled into the binary. Rebuild after changing a
// file in database/.
//
//go:embed database/*.json
var schemaFiles embed.FS

// Templates and public assets are also compiled into the binary so release/
// only needs the executable and its config.json.
//
//go:embed web
var webFiles embed.FS

var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("application stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "", "path to config.json (defaults to the file beside the executable)")
	flag.Parse()

	cfg, path, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	slog.Info("configuration loaded", "path", path, "version", version)

	db, err := appdb.Open(cfg.Database)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer db.Close()

	// Apply the embedded table definitions before constructing or starting the
	// HTTP server. No request can be served against an outdated schema.
	migrationContext, cancelMigration := context.WithTimeout(context.Background(), cfg.Database.MigrationTimeout.Duration)
	result, err := tables.NewTables(db, schemaFiles).Apply(migrationContext)
	cancelMigration()
	if err != nil {
		return fmt.Errorf("apply database tables: %w", err)
	}
	slog.Info("database schema ready", "skipped", result.Skipped, "changes", len(result.Queries))

	webRoot, err := fs.Sub(webFiles, "web")
	if err != nil {
		return fmt.Errorf("open embedded web files: %w", err)
	}

	handler, err := server.New(webRoot, db, version)
	if err != nil {
		return fmt.Errorf("create web server: %w", err)
	}

	httpServer := &http.Server{
		Addr:              cfg.Server.Address,
		Handler:           handler,
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout.Duration,
		ReadTimeout:       cfg.Server.ReadTimeout.Duration,
		WriteTimeout:      cfg.Server.WriteTimeout.Duration,
		IdleTimeout:       cfg.Server.IdleTimeout.Duration,
	}

	serveErrors := make(chan error, 1)
	go func() {
		slog.Info("website listening", "address", cfg.Server.Address)
		serveErrors <- httpServer.ListenAndServe()
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)

	select {
	case signal := <-signals:
		slog.Info("shutdown requested", "signal", signal.String())
	case err := <-serveErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve website: %w", err)
		}
		return nil
	}

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout.Duration)
	defer cancelShutdown()
	if err := httpServer.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	select {
	case err := <-serveErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve website: %w", err)
		}
	case <-time.After(time.Second):
	}

	return nil
}
