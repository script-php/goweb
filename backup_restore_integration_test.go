//go:build integration

package main

import (
	"context"
	"database/sql"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"script-php/goweb/platform/configuration"
)

// This test intentionally dumps and restores the configured database. Point
// WEBSITE_INTEGRATION_DSN only at a disposable integration database.
func TestBackupRestoreWorkflowIntegration(t *testing.T) {
	dsn := os.Getenv("WEBSITE_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("WEBSITE_INTEGRATION_DSN is not set")
	}
	if os.Getenv("WEBSITE_INTEGRATION_ALLOW_RESTORE") != "YES" {
		t.Skip("WEBSITE_INTEGRATION_ALLOW_RESTORE=YES is required for the destructive restore test")
	}
	driverConfig, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if driverConfig.Net != "tcp" {
		t.Fatalf("backup integration requires a tcp DSN, got %q", driverConfig.Net)
	}
	host, portText, err := net.SplitHostPort(driverConfig.Addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	database := config.Database{Host: host, Port: port, Name: driverConfig.DBName, User: driverConfig.User, Password: driverConfig.Passwd}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	table := "backup_restore_" + strings.ReplaceAll(time.Now().UTC().Format("20060102150405000000000"), ".", "")
	if _, err := db.ExecContext(ctx, "CREATE TABLE `"+table+"` (value VARCHAR(50) NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec("DROP TABLE IF EXISTS `" + table + "`") })
	if _, err := db.ExecContext(ctx, "INSERT INTO `"+table+"` (value) VALUES ('preserved')"); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	uploads := filepath.Join(root, "uploads")
	if err := os.MkdirAll(uploads, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(uploads, "asset.txt"), []byte("original upload"), 0o640); err != nil {
		t.Fatal(err)
	}
	archive, err := createBackup(database, config.Storage{UploadsDirectory: uploads}, config.Backup{}, filepath.Join(root, "backups"), root, "integration")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifyBackup(archive); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "DROP TABLE `"+table+"`"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(uploads, "asset.txt"), []byte("changed"), 0o640); err != nil {
		t.Fatal(err)
	}
	previous, err := restoreBackup(config.Config{Database: database, Storage: config.Storage{UploadsDirectory: uploads}}, archive, root)
	if err != nil {
		t.Fatal(err)
	}
	var value string
	if err := db.QueryRowContext(ctx, "SELECT value FROM `"+table+"` LIMIT 1").Scan(&value); err != nil || value != "preserved" {
		t.Fatalf("restored database value=%q err=%v", value, err)
	}
	contents, err := os.ReadFile(filepath.Join(uploads, "asset.txt"))
	if err != nil || string(contents) != "original upload" {
		t.Fatalf("restored upload=%q err=%v", contents, err)
	}
	if _, err := os.Stat(filepath.Join(previous, "asset.txt")); err != nil {
		t.Fatalf("previous uploads were not preserved: %v", err)
	}
}
