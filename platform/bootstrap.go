package platform

import (
	"database/sql"

	appdb "script-php/goweb/internal/database"
	"script-php/goweb/platform/configuration"
)

func LoadConfig(path string) (Config, string, error) { return config.Load(path) }
func OpenDatabase(cfg Config) (*sql.DB, error)       { return appdb.Open(cfg.Database) }
