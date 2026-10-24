package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"script-php/goweb/internal/app"
	"script-php/goweb/internal/auth"
	appdb "script-php/goweb/internal/database"
	"script-php/goweb/internal/service"
	"script-php/goweb/platform"
	"script-php/goweb/platform/configuration"
)

// Project table additions and patches are compiled separately from the
// platform-owned default schemas.
//
//go:embed database
var applicationSchemaFiles embed.FS

// Project templates and assets are a separate, higher-priority layer.
//
//go:embed app/web
var applicationFiles embed.FS

func main() {
	if err := run(); err != nil {
		slog.Error("application stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	version := applicationVersion()
	configPath := flag.String("config", "", "path to config.json (defaults to the file beside the executable)")
	register := flag.Bool("register", false, "register and enable this binary as a systemd service")
	unregister := flag.Bool("unregister", false, "stop, disable, and remove this binary's systemd service")
	showVersion := flag.Bool("version", false, "print the embedded application version")
	createAdmin := flag.String("create-admin", "", "create or reset an administrator for the given email")
	check := flag.Bool("check", false, "validate configuration and deployment readiness without serving")
	backupDirectory := flag.String("backup", "", "back up the database into this directory and exit")
	schemaPlan := flag.Bool("schema-plan", false, "print pending database schema SQL without applying it")
	verifyBackupPath := flag.String("verify-backup", "", "verify a backup archive and exit")
	restorePath := flag.String("restore", "", "restore a backup archive and exit (requires --confirm-restore)")
	confirmRestore := flag.Bool("confirm-restore", false, "confirm replacement of database data and uploaded files")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return nil
	}
	if *register && *unregister {
		return errors.New("--register and --unregister cannot be used together")
	}

	cfg, path, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	slog.Info("configuration loaded", "path", path, "version", version)
	if *verifyBackupPath != "" {
		summary, err := verifyBackup(*verifyBackupPath)
		if err != nil {
			return fmt.Errorf("verify backup: %w", err)
		}
		slog.Info("backup verified", "path", *verifyBackupPath, "entries", summary.Files, "upload_files", summary.UploadFiles, "sql_bytes", summary.SQLBytes)
		return nil
	}
	if *restorePath != "" {
		if !*confirmRestore {
			return errors.New("--restore requires --confirm-restore because it replaces database data and uploaded files")
		}
		previousUploads, err := restoreBackup(cfg, *restorePath, filepath.Dir(path))
		if err != nil {
			return fmt.Errorf("restore backup: %w", err)
		}
		slog.Info("backup restored", "path", *restorePath, "previous_uploads", previousUploads)
		return nil
	}
	if *register {
		if err := service.Register(cfg.Service, path); err != nil {
			return fmt.Errorf("register service: %w", err)
		}
		slog.Info("service registered", "name", service.Name(cfg.Service))
		return nil
	}
	if *unregister {
		if err := service.Unregister(cfg.Service); err != nil {
			return fmt.Errorf("unregister service: %w", err)
		}
		slog.Info("service unregistered", "name", service.Name(cfg.Service))
		return nil
	}

	db, err := appdb.Open(cfg.Database)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer db.Close()
	schemaOptions := platform.Schemas{FS: applicationSchemaFiles, Root: "database", Existing: platform.SchemaMergeExisting}
	if *schemaPlan {
		planContext := context.Background()
		var cancel context.CancelFunc = func() {}
		if cfg.Database.MigrationTimeout.Duration > 0 {
			planContext, cancel = context.WithTimeout(planContext, cfg.Database.MigrationTimeout.Duration)
		}
		defer cancel()
		queries, err := platform.PlanSchemas(planContext, db, schemaOptions)
		if err != nil {
			return err
		}
		if len(queries) == 0 {
			fmt.Println("No schema changes pending.")
			return nil
		}
		for _, query := range queries {
			fmt.Println(query + ";")
		}
		return nil
	}

	runtime, err := platform.New(platform.Options{
		DB: db, Config: cfg, Version: version,
		Resources: platform.Resources{FS: applicationFiles, Root: "app/web"},
		Schemas:   schemaOptions,
		Features:  platform.DefaultFeatures(), RegisterPages: app.RegisterPages, RegisterAdmin: app.RegisterAdmin,
		Backups: backupAdminOperations(cfg, filepath.Dir(path), version),
	})
	if err != nil {
		return fmt.Errorf("create platform: %w", err)
	}
	if *backupDirectory != "" {
		path, err := createBackup(cfg.Database, cfg.Storage, cfg.Backup, *backupDirectory, filepath.Dir(path), version)
		if err != nil {
			return err
		}
		slog.Info("database backup created", "path", path)
		return nil
	}
	if *createAdmin != "" {
		password, err := readNewPassword()
		if err != nil {
			return err
		}
		if err := auth.BootstrapAdministrator(context.Background(), db, *createAdmin, password); err != nil {
			return fmt.Errorf("create administrator: %w", err)
		}
		slog.Info("administrator ready", "email", *createAdmin)
		return nil
	}

	if *check {
		if err := checkDeployment(cfg.Service); err != nil {
			return fmt.Errorf("deployment check: %w", err)
		}
		if !cfg.Authentication.SecureCookies {
			slog.Warn("secure cookies are disabled; enable them for production HTTPS")
		}
		slog.Info("deployment check passed", "version", version, "database", cfg.Database.Name)
		return nil
	}
	runContext, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	slog.Info("website listening", "address", cfg.Server.Address)
	return runtime.Run(runContext)
}
