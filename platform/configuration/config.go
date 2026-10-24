package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Server         Server         `json:"server"`
	Database       Database       `json:"database"`
	Authentication Authentication `json:"authentication"`
	Service        Service        `json:"service"`
	Storage        Storage        `json:"storage"`
	Backup         Backup         `json:"backup"`
	Email          Email          `json:"email"`
}

type Email struct {
	Enabled     bool     `json:"enabled"`
	Host        string   `json:"host"`
	Port        int      `json:"port"`
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	FromName    string   `json:"from_name"`
	FromAddress string   `json:"from_address"`
	Encryption  string   `json:"encryption"`
	Timeout     Duration `json:"timeout"`
}

type Storage struct {
	UploadsDirectory string   `json:"uploads_directory"`
	MaxUploadBytes   int64    `json:"max_upload_bytes"`
	AllowedMIMETypes []string `json:"allowed_mime_types"`
}

type Backup struct {
	RetentionCount int `json:"retention_count"`
}

type Authentication struct {
	SessionLifetime  Duration `json:"session_lifetime"`
	SecureCookies    bool     `json:"secure_cookies"`
	LoginMaxAttempts int      `json:"login_max_attempts"`
	LoginLockout     Duration `json:"login_lockout"`
}

type Server struct {
	Address           string   `json:"address"`
	PublicURL         string   `json:"public_url"`
	Production        bool     `json:"production"`
	ReadHeaderTimeout Duration `json:"read_header_timeout"`
	ReadTimeout       Duration `json:"read_timeout"`
	WriteTimeout      Duration `json:"write_timeout"`
	IdleTimeout       Duration `json:"idle_timeout"`
	ShutdownTimeout   Duration `json:"shutdown_timeout"`
	TrustedProxies    []string `json:"trusted_proxies"`
}

type Database struct {
	Host             string            `json:"host"`
	Port             int               `json:"port"`
	Name             string            `json:"name"`
	User             string            `json:"user"`
	Password         string            `json:"password"`
	Parameters       map[string]string `json:"parameters"`
	ConnectTimeout   Duration          `json:"connect_timeout"`
	MigrationTimeout Duration          `json:"migration_timeout"`
	MaxOpenConns     int               `json:"max_open_connections"`
	MaxIdleConns     int               `json:"max_idle_connections"`
	ConnMaxLifetime  Duration          `json:"connection_max_lifetime"`
}

// Service controls the systemd unit produced by --register. Name and
// description have safe defaults; user and group are optional.
type Service struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	User        string `json:"user"`
	Group       string `json:"group"`
}

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return errors.New("duration must be a string such as \"10s\" or \"2m\"")
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", value, err)
	}
	d.Duration = parsed
	return nil
}

func Load(requestedPath string) (Config, string, error) {
	path, err := resolvePath(requestedPath)
	if err != nil {
		return Config{}, "", err
	}

	file, err := os.Open(path)
	if err != nil {
		return Config{}, path, fmt.Errorf("open configuration %s: %w", path, err)
	}
	defer file.Close()

	var cfg Config
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, path, fmt.Errorf("decode configuration %s: %w", path, err)
	}
	if err := ensureEndOfFile(decoder); err != nil {
		return Config{}, path, fmt.Errorf("decode configuration %s: %w", path, err)
	}
	if err := cfg.validate(); err != nil {
		return Config{}, path, fmt.Errorf("validate configuration %s: %w", path, err)
	}
	return cfg, path, nil
}

func ensureEndOfFile(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("configuration must contain exactly one JSON object")
}

func resolvePath(requestedPath string) (string, error) {
	if requestedPath != "" {
		return filepath.Abs(requestedPath)
	}

	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("find executable path: %w", err)
	}
	besideExecutable := filepath.Join(filepath.Dir(executable), "config.json")
	if _, err := os.Stat(besideExecutable); err == nil {
		return besideExecutable, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect configuration %s: %w", besideExecutable, err)
	}

	// This fallback makes `go run .` convenient. A built release always uses
	// config.json beside its executable.
	fromWorkingDirectory, err := filepath.Abs("config.json")
	if err != nil {
		return "", fmt.Errorf("resolve config.json: %w", err)
	}
	return fromWorkingDirectory, nil
}

func (c Config) validate() error {
	var missing []string
	if c.Server.Address == "" {
		missing = append(missing, "server.address")
	}
	if c.Database.Host == "" {
		missing = append(missing, "database.host")
	}
	if c.Database.Port < 1 || c.Database.Port > 65535 {
		return errors.New("database.port must be between 1 and 65535")
	}
	if c.Database.Name == "" {
		missing = append(missing, "database.name")
	}
	if c.Database.User == "" {
		missing = append(missing, "database.user")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required values: %s", strings.Join(missing, ", "))
	}
	if c.Database.ConnectTimeout.Duration <= 0 || c.Database.MigrationTimeout.Duration <= 0 {
		return errors.New("database timeouts must be greater than zero")
	}
	if c.Authentication.SessionLifetime.Duration <= 0 {
		return errors.New("authentication.session_lifetime must be greater than zero")
	}
	if c.Authentication.LoginMaxAttempts < 0 {
		return errors.New("authentication.login_max_attempts cannot be negative")
	}
	if c.Backup.RetentionCount < 0 {
		return errors.New("backup.retention_count cannot be negative")
	}
	if c.Storage.MaxUploadBytes < 0 {
		return errors.New("storage.max_upload_bytes cannot be negative")
	}
	if c.Email.Enabled {
		if c.Email.Host == "" || c.Email.Port < 1 || c.Email.Port > 65535 || c.Email.FromAddress == "" {
			return errors.New("enabled email requires host, a valid port, and from_address")
		}
		if c.Email.Encryption != "none" && c.Email.Encryption != "starttls" && c.Email.Encryption != "tls" {
			return errors.New("email.encryption must be none, starttls, or tls")
		}
		if c.Email.Timeout.Duration <= 0 {
			return errors.New("email.timeout must be greater than zero")
		}
	}
	if c.Server.ReadHeaderTimeout.Duration <= 0 || c.Server.ShutdownTimeout.Duration <= 0 {
		return errors.New("server read_header_timeout and shutdown_timeout must be greater than zero")
	}
	if c.Server.Production {
		publicURL, err := url.Parse(c.Server.PublicURL)
		if err != nil || publicURL.Scheme != "https" || publicURL.Host == "" {
			return errors.New("server.public_url must be a valid HTTPS URL in production")
		}
		if !c.Authentication.SecureCookies {
			return errors.New("authentication.secure_cookies must be true in production")
		}
	}
	return nil
}
