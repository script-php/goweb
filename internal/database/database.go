package database

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strconv"

	"github.com/go-sql-driver/mysql"
	"script-php/goweb/platform/configuration"
)

func Open(settings config.Database) (*sql.DB, error) {
	parameters := make(map[string]string, len(settings.Parameters)+1)
	for key, value := range settings.Parameters {
		parameters[key] = value
	}
	if _, exists := parameters["charset"]; !exists {
		parameters["charset"] = "utf8mb4"
	}

	driverConfig := mysql.NewConfig()
	driverConfig.User = settings.User
	driverConfig.Passwd = settings.Password
	driverConfig.Net = "tcp"
	driverConfig.Addr = net.JoinHostPort(settings.Host, strconv.Itoa(settings.Port))
	driverConfig.DBName = settings.Name
	driverConfig.Params = parameters
	driverConfig.ParseTime = true
	driverConfig.Timeout = settings.ConnectTimeout.Duration
	driverConfig.ReadTimeout = settings.ConnectTimeout.Duration
	driverConfig.WriteTimeout = settings.ConnectTimeout.Duration

	db, err := sql.Open("mysql", driverConfig.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(settings.MaxOpenConns)
	db.SetMaxIdleConns(settings.MaxIdleConns)
	db.SetConnMaxLifetime(settings.ConnMaxLifetime.Duration)

	ctx, cancel := context.WithTimeout(context.Background(), settings.ConnectTimeout.Duration)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping MySQL at %s: %w", driverConfig.Addr, err)
	}
	return db, nil
}
