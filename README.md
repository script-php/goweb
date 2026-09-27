# Go website starter

A small production-oriented base for Go websites. HTML, static files, and the
database table definitions are embedded in one binary. Runtime configuration is
kept in `release/config.json`, so the entire `release/` directory can be copied
to a server and started there.

Requires Go 1.24+ and MySQL 5.7+ or MariaDB 10.6+.

## Build and run

```sh
make test
make build VERSION=1.0.0
$EDITOR release/config.json
./release/website
```

`make build` creates `release/config.json` from `config.example.json` only when
it does not already exist. Rebuilding therefore does not overwrite server
credentials. You can use another binary name with `make build APP_NAME=my-site`.

For development, copy the example and run the module directly:

```sh
cp config.example.json config.json
go run .
```

The configuration path can also be explicit:

```sh
./release/website -config /etc/my-site/config.json
```

The MySQL database and user must already exist. The configured user needs the
DDL permissions required to create or alter the application's tables.

## Database tables

This project imports [`github.com/script-php/tables`](https://github.com/script-php/tables)
as a normal Go module. At startup, after connecting to MySQL and before the HTTP
listener starts, the application applies the embedded schema.

Keep one table per file directly inside `database/`, for example:

```text
database/
  users.json
  sessions.json
  articles.json
```

The included `database/users.json` is an example and can be replaced. Rebuild
after every schema edit because these files are embedded in the executable. On a
successful check, the tables package creates `release/tables.hash`; deploy that
file with the release when it exists. Delete it to force a full schema comparison.

## Project layout

```text
.
├── main.go                 application startup and graceful shutdown
├── database/               one JSON schema file per table
├── internal/
│   ├── config/             strict config.json loading and validation
│   ├── database/           MySQL connection setup
│   └── server/             routes and HTTP middleware
├── web/
│   ├── static/             CSS, JavaScript, images
│   └── templates/          server-rendered HTML
├── config.example.json     safe configuration template
├── Makefile
└── release/                deployable binary, config, and tables.hash
```

The starter exposes `/` and a database-aware `/healthz` endpoint.

