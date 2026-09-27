package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validConfig = `{
  "server": {
    "address": ":8080",
    "read_header_timeout": "5s",
    "read_timeout": "15s",
    "write_timeout": "30s",
    "idle_timeout": "1m",
    "shutdown_timeout": "15s"
  },
  "database": {
    "host": "127.0.0.1",
    "port": 3306,
    "name": "website",
    "user": "website",
    "password": "secret",
    "connect_timeout": "10s",
    "migration_timeout": "2m",
    "max_open_connections": 25,
    "max_idle_connections": 10,
    "connection_max_lifetime": "5m"
  }
}`

func TestLoad(t *testing.T) {
	path := writeConfig(t, validConfig)
	cfg, loadedPath, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loadedPath != path {
		t.Fatalf("Load() path = %q, want %q", loadedPath, path)
	}
	if cfg.Database.Name != "website" || cfg.Server.Address != ":8080" {
		t.Fatalf("Load() returned unexpected configuration: %#v", cfg)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := writeConfig(t, strings.Replace(validConfig, `"address": ":8080"`, `"address": ":8080", "adress": ":9090"`, 1))
	if _, _, err := Load(path); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("Load() error = %v, want unknown field error", err)
	}
}

func TestLoadRejectsTrailingDocument(t *testing.T) {
	path := writeConfig(t, validConfig+` {}`)
	if _, _, err := Load(path); err == nil || !strings.Contains(err.Error(), "exactly one JSON object") {
		t.Fatalf("Load() error = %v, want trailing document error", err)
	}
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
