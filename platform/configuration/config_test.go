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
  },
  "authentication": {
    "session_lifetime": "24h",
    "secure_cookies": false
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

func TestProductionRequiresHTTPSAndSecureCookies(t *testing.T) {
	production := strings.Replace(validConfig, `"address": ":8080",`, `"address": ":8080", "public_url": "http://example.com", "production": true,`, 1)
	path := writeConfig(t, production)
	if _, _, err := Load(path); err == nil || !strings.Contains(err.Error(), "HTTPS URL") {
		t.Fatalf("Load() error = %v, want HTTPS validation error", err)
	}

	production = strings.Replace(production, "http://example.com", "https://example.com", 1)
	path = writeConfig(t, production)
	if _, _, err := Load(path); err == nil || !strings.Contains(err.Error(), "secure_cookies") {
		t.Fatalf("Load() error = %v, want secure cookie validation error", err)
	}
}

func TestLoadResolvesSecretsFromEnvironmentAndRelativeFile(t *testing.T) {
	directory := t.TempDir()
	secretFile := filepath.Join(directory, "smtp-password")
	if err := os.WriteFile(secretFile, []byte("mail-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOWEB_TEST_DATABASE_PASSWORD", "database-secret")
	configured := strings.Replace(validConfig, `"password": "secret",`, `"password": "", "password_env": "GOWEB_TEST_DATABASE_PASSWORD",`, 1)
	configured = strings.Replace(configured, `"authentication": {`, `"email": {"password_file":"smtp-password"}, "authentication": {`, 1)
	path := filepath.Join(directory, "config.json")
	if err := os.WriteFile(path, []byte(configured), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Password != "database-secret" || cfg.Email.Password != "mail-secret" {
		t.Fatalf("secrets were not resolved")
	}
}

func TestLoadRejectsAmbiguousOrMissingSecretSources(t *testing.T) {
	ambiguous := strings.Replace(validConfig, `"password": "secret",`, `"password_env":"ONE", "password_file":"two",`, 1)
	if _, _, err := Load(writeConfig(t, ambiguous)); err == nil || !strings.Contains(err.Error(), "cannot both") {
		t.Fatalf("ambiguous secret error = %v", err)
	}
	missing := strings.Replace(validConfig, `"password": "secret",`, `"password_env":"GOWEB_TEST_MISSING_SECRET",`, 1)
	if _, _, err := Load(writeConfig(t, missing)); err == nil || !strings.Contains(err.Error(), "empty or missing") {
		t.Fatalf("missing secret error = %v", err)
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
