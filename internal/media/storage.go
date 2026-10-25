package media

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"sync"

	configuration "script-php/goweb/platform/configuration"
)

type BackendRouter interface {
	Current(context.Context) (string, Backend, error)
	Named(context.Context, string) (Backend, error)
	All(context.Context) (map[string]Backend, error)
}

type staticRouter struct{ backend Backend }

func (router staticRouter) Current(context.Context) (string, Backend, error) {
	return "local", router.backend, nil
}
func (router staticRouter) Named(_ context.Context, name string) (Backend, error) {
	if name == "" || name == "custom" || name == "local" {
		return router.backend, nil
	}
	return nil, errors.New("media storage backend is unavailable")
}
func (router staticRouter) All(context.Context) (map[string]Backend, error) {
	return map[string]Backend{"local": router.backend}, nil
}

type ConfiguredRouter struct {
	db       *sql.DB
	defaults configuration.Storage
	local    Backend
	mu       sync.Mutex
	s3Key    string
	s3       *S3Backend
}

func NewConfiguredRouter(db *sql.DB, defaults configuration.Storage, local Backend) *ConfiguredRouter {
	return &ConfiguredRouter{db: db, defaults: defaults, local: local}
}

func (router *ConfiguredRouter) config(ctx context.Context) (configuration.Storage, error) {
	cfg := router.defaults
	if cfg.Backend == "" {
		cfg.Backend = "local"
	}
	rows, err := router.db.QueryContext(ctx, `SELECT setting_key,setting_value FROM settings WHERE setting_key LIKE 'storage.%'`)
	if err != nil {
		return cfg, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return cfg, err
		}
		switch key {
		case "storage.backend":
			cfg.Backend = value
		case "storage.s3.endpoint":
			cfg.S3.Endpoint = value
		case "storage.s3.bucket":
			cfg.S3.Bucket = value
		case "storage.s3.region":
			cfg.S3.Region = value
		case "storage.s3.access_key":
			cfg.S3.AccessKey = value
		case "storage.s3.secret_key":
			if value != "" {
				cfg.S3.SecretKey = value
			}
		case "storage.s3.use_tls":
			cfg.S3.UseTLS = value == "true"
		case "storage.s3.path_style":
			cfg.S3.PathStyle = value == "true"
		case "storage.s3.create_bucket":
			cfg.S3.CreateBucket = value == "true"
		case "storage.s3.prefix":
			cfg.S3.Prefix = value
		}
	}
	return cfg, rows.Err()
}

func (router *ConfiguredRouter) Current(ctx context.Context) (string, Backend, error) {
	cfg, err := router.config(ctx)
	if err != nil {
		return "", nil, err
	}
	backend, err := router.backend(ctx, cfg.Backend, cfg)
	return cfg.Backend, backend, err
}

func (router *ConfiguredRouter) Named(ctx context.Context, name string) (Backend, error) {
	if name == "" || name == "local" {
		return router.local, nil
	}
	cfg, err := router.config(ctx)
	if err != nil {
		return nil, err
	}
	return router.backend(ctx, name, cfg)
}

func (router *ConfiguredRouter) All(ctx context.Context) (map[string]Backend, error) {
	cfg, err := router.config(ctx)
	if err != nil {
		return nil, err
	}
	result := map[string]Backend{"local": router.local}
	if cfg.S3.Endpoint != "" && cfg.S3.Bucket != "" && cfg.S3.AccessKey != "" && cfg.S3.SecretKey != "" {
		backend, err := router.backend(ctx, "s3", cfg)
		if err != nil {
			return nil, err
		}
		result["s3"] = backend
	}
	return result, nil
}

func (router *ConfiguredRouter) backend(ctx context.Context, name string, cfg configuration.Storage) (Backend, error) {
	if name == "local" || name == "" {
		return router.local, nil
	}
	if name != "s3" {
		return nil, errors.New("unknown media storage backend " + strconv.Quote(name))
	}
	key := strings.Join([]string{cfg.S3.Endpoint, cfg.S3.Bucket, cfg.S3.Region, cfg.S3.AccessKey, cfg.S3.SecretKey, strconv.FormatBool(cfg.S3.UseTLS), strconv.FormatBool(cfg.S3.PathStyle), cfg.S3.Prefix}, "\x00")
	router.mu.Lock()
	defer router.mu.Unlock()
	if router.s3 != nil && router.s3Key == key {
		return router.s3, nil
	}
	backend, err := NewS3Backend(cfg.S3)
	if err != nil {
		return nil, err
	}
	if err := backend.Check(ctx, cfg.S3.CreateBucket, cfg.S3.Region); err != nil {
		return nil, err
	}
	router.s3, router.s3Key = backend, key
	return backend, nil
}

func (router *ConfiguredRouter) Validate(ctx context.Context, cfg configuration.S3) error {
	backend, err := NewS3Backend(cfg)
	if err != nil {
		return err
	}
	return backend.Check(ctx, cfg.CreateBucket, cfg.Region)
}
