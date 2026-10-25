# Go website starter

A small production-oriented base for Go websites. HTML, static files, and the
database table definitions are embedded in one binary. Runtime configuration is
kept in `release/config.json`, so the entire `release/` directory can be copied
to a server and started there.

Requires Go 1.24+ and MySQL 5.7+ or MariaDB 10.6+.

Release automation, supported database CI, module-proxy requirements, tagged
release steps, and package upgrade instructions are documented in
[`docs/RELEASING.md`](docs/RELEASING.md).

## Build and run

```sh
make test
make check
make build
$EDITOR release/config.json
./release/website
```

`make build` creates `release/config.json` from `config.example.json` only when
it does not already exist. Rebuilding therefore does not overwrite server
credentials. You can use another binary name with `make build APP_NAME=my-site`.

The application version comes from the root `version.txt`. Update that file
before building a release. It is embedded into the binary and is available in
Go code, templates through `.Version`, startup logs, and the health responses.
The binary also reports it directly with `./release/website --version`.

Preview database changes before applying them:

```sh
./release/website --schema-plan
```

This prints the SQL without executing it or updating the schema marker.

Create a timestamped, permission-restricted compressed backup using the database
credentials and uploads path from `release/config.json`:

```sh
make backup
```

Backups are written to `release/backups/` as `.tar.gz` archives. Each archive
contains `database.sql`, `manifest.txt`, and the complete `uploads/` tree. The
database dump includes stored routines, triggers, and events. Upload symlinks
are skipped so they cannot pull unrelated server files into an archive. Set
`storage.uploads_directory` to an absolute path or a path relative to
`config.json`. Copy backups off the application server and test restores
regularly.

`backup.retention_count` controls how many archives are retained. Cleanup runs
only after a new archive completes successfully; set it to `0` to disable it.
Verify an archive without changing live data:

```sh
make verify-backup BACKUP=release/backups/website-YYYYMMDD-HHMMSS.tar.gz
```

Verification checks gzip/tar integrity, safe paths, the format manifest, a
non-empty SQL dump, and the uploads tree. To restore, stop the service and take
a fresh backup first, then use the explicitly guarded command:

```sh
sudo systemctl stop website
make restore BACKUP=release/backups/website-YYYYMMDD-HHMMSS.tar.gz CONFIRM=YES
sudo systemctl start website
curl --fail https://example.com/health/ready
```

Restore imports `database.sql`, preserves current uploads in a timestamped
`.before-restore-*` directory, and activates the archived uploads. Test this
process against a non-production database before depending on it.

The platform media service stores validated uploads locally or in built-in
S3-compatible storage (including MinIO), records the owning backend, ownership,
and application attachments in the database, and provides a
permission-protected admin media library. Storage can be changed live from
Admin → Settings without hiding existing objects. Applications access it
through `runtime.Media`; configuration and examples are in
[`docs/PLATFORM.md`](docs/PLATFORM.md#upload-and-attach-media).

Daily scheduling examples are in `deploy/website-backup.service` and
`deploy/website-backup.timer`. Adjust their user and `/opt/website` paths, copy
them to `/etc/systemd/system/`, then enable `website-backup.timer`.

To build and run the website locally using `release/config.json`:

```sh
make run
```

This rebuilds the executable and starts it from inside `release/`. No root-level
configuration is created. Edit `release/config.json` with your local database
settings before running; subsequent builds do not overwrite it.

To create clean GitHub-ready Linux ZIP packages without copying the live
configuration or uploads, run `make release`. The validated AMD64 and ARM64
archives and their `SHA256SUMS` file are written to `dist/`; see
[`docs/RELEASING.md`](docs/RELEASING.md) for tagging and publishing.

The configuration path can also be explicit:

```sh
./release/website -config /etc/my-site/config.json
```

## Reliability and deployment checks

Every HTTP request receives an `X-Request-ID`. A valid incoming ID is preserved;
otherwise the application generates one. Structured request logs include method,
path, status, duration, response bytes, client IP, and authenticated user ID.
Under systemd these logs are available through `journalctl`.

Panics are recovered per request, logged with the request ID and stack trace,
and rendered as a safe `500` page without stopping the server. Public pages have
dedicated `403`, `404`, and `500` templates. Admin errors use the independent
admin layout.

Before deploying, run:

```sh
make check
./release/website --check
```

`make check` runs race-enabled tests and `go vet`. The binary preflight validates
configuration, database connectivity, embedded schemas, templates, registered
permissions, service settings, and release-directory writability without opening
the HTTP listener. Because normal startup owns schema synchronization, `--check`
also applies pending schema changes when needed.

Database-backed workflow tests are opt-in so normal test runs never touch a real
database. They cover stores, browser-style login/admin/logout behavior, and a
real dump/verify/restore cycle including uploads. The restore test replaces the
configured database, so point it only at a disposable schema and explicitly
confirm that use:

```sh
WEBSITE_INTEGRATION_DSN='user:password@tcp(127.0.0.1:3306)/website_test?parseTime=true' \
WEBSITE_INTEGRATION_ALLOW_RESTORE=YES make integration-test
```

The suite runs packages sequentially, creates uniquely named records, validates
session and authorization behavior, verifies login throttling and expiry under
concurrent attempts, restores the dump, and removes its records.

## Linux service

On a Linux server using systemd, configure the `service` section in
`release/config.json`, then register and enable the release binary:

```sh
sudo ./release/website --register
sudo systemctl start website
sudo systemctl status website
```

Registration writes `/etc/systemd/system/website.service`, reloads systemd, and
enables the service for startup. It intentionally does not start the service.
Set `service.user` and `service.group` to a dedicated account when desired; that
account needs read access to the release and write access for `tables.hash`.
The generated unit also restricts filesystem writes, Linux capabilities,
kernel access, devices, address families, and privilege escalation.

To stop, disable, and delete the generated unit file:

```sh
sudo ./release/website --unregister
```

Both commands use `config.json` beside the executable unless `-config` is given.
They refuse to overwrite or delete a unit file not created by this application.

### Nginx and production HTTPS

An HTTPS reverse-proxy example is provided at `deploy/nginx.conf`. Replace its
domain and certificate paths, then set the release configuration to production:

```json
"server": {
  "address": "127.0.0.1:9090",
  "public_url": "https://example.com",
  "production": true,
  "trusted_proxies": ["127.0.0.1", "::1"]
},
"authentication": {
  "secure_cookies": true
}
```

Only addresses listed in `trusted_proxies` may supply `X-Forwarded-For` or
`X-Real-IP`. Direct clients cannot spoof the address used by request logs,
login throttling, and the audit log. Production mode refuses to start unless
`public_url` uses HTTPS and secure cookies are enabled.

The MySQL database and user must already exist. The configured user needs the
DDL permissions required to create or alter the application's tables.

The complete server checklist, least-privilege database and firewall guidance,
log-retention setup, rollout steps, and rollback procedure are in
[`docs/DEPLOYMENT.md`](docs/DEPLOYMENT.md).

## Database tables

This project imports [`github.com/script-php/tables`](https://github.com/script-php/tables)
as a normal Go module. At startup, after connecting to MySQL and before the HTTP
listener starts, the application applies the embedded schema.

Platform-required tables live in `platform/database`. Keep project tables and
small merge patches directly inside the root `database/`, for example:

```text
database/
  users.json       optional patch for the platform users table
  articles.json
```

Choose `SchemaAddOnly`, `SchemaMergeExisting`, or `SchemaReplaceExisting` when
constructing the platform. Merge mode preserves unspecified platform fields and
combines named columns, indexes, and foreign keys. Rebuild after every schema
edit because these files are embedded in the executable. On a
successful check, the tables package creates `release/tables.hash`; deploy that
file with the release when it exists. Delete it to force a full schema comparison.

Critical authentication and authorization columns cannot be changed or dropped
by an application patch. Full replacement of protected platform tables is also
rejected. `AllowUnsafePlatformChanges` is an explicit escape hatch for a
carefully reviewed migration; normal projects should leave it false.

Schema application is serialized per database with a MySQL/MariaDB advisory
lock. This prevents two newly started instances from applying the same change
concurrently. `database.migration_timeout` also bounds how long an instance
waits to obtain that lock.

### Migration recovery

Schema synchronization runs before the HTTP listener opens. If it fails, the
process exits without serving traffic. Preserve the failure log and database
backup, correct the schema JSON or database permissions, rebuild, and run
`website --check` before restarting. Do not edit `tables.hash` merely to hide a
failure. For destructive changes, deploy in two stages: add and backfill the new
schema while old code remains compatible, then remove obsolete columns in a
later release. Roll back code only when its embedded schema remains compatible
with the already-applied database structure.

## Project layout

```text
.
├── main.go                 application startup and graceful shutdown
├── platform/               self-contained extension API, defaults, and schemas
├── app/web/                project-owned templates and assets
├── database/               project tables and platform-table patches
├── internal/
│   ├── app/                project page and admin registration
│   ├── auth/               optional authentication and authorization helpers
│   ├── database/           MySQL connection setup
│   └── server/
│       ├── admin/          extensible dashboard and management modules
│       ├── catalog/        shared public/admin page registry
│       ├── routes.go       built-in platform URL mappings
│       ├── middleware.go   shared HTTP middleware
│       ├── health.go       operational health endpoint
│       ├── pages/
│       │   ├── home.go     one handler file per page or feature
│       │   └── about.go
│       └── render/
│           └── render.go   shared template rendering
├── config.example.json     safe configuration template
├── version.txt             release version embedded into the binary
├── Makefile
└── release/                deployable binary, config, and tables.hash
```

The platform embeds its files from `platform/web` and required tables from
`platform/database`. Projects should normally change only `internal/app`, `app/web`, project database
definitions, and the composition options in `main.go`. See
[`docs/PLATFORM.md`](docs/PLATFORM.md) for extension and override examples and
[`UPGRADING.md`](UPGRADING.md) for the upstream upgrade workflow.

The starter exposes `/`, `/about`, and three operational health endpoints:

- `/health` returns the combined process and database state.
- `/health/live` confirms that the Go HTTP process is responding without
  querying the database.
- `/health/ready` checks database connectivity and returns `503` while the
  application cannot serve normal traffic, including as soon as graceful
  shutdown begins.


## Add a page

For a new `/contact` page, put its handler and registration under `internal/app`,
create `app/web/templates/pages/contact.html`, and register it through
`app.RegisterPages`. The application filesystem overrides the platform by
relative path, while missing templates and assets fall back to `platform/web/`.

The platform also supports custom admin routes, dashboard widgets, user-form
sections, feature flags, template/asset overrides, and complete handler
replacement through stable page IDs. Prefer extending a built-in page over
copying it so future platform improvements continue to apply.

## Authentication helpers

The public `platform` package provides the authentication helpers available to
importing applications:

- bcrypt password hashing with cost 12
- Random 256-bit session tokens; only SHA-256 hashes are stored
- Database-backed session resolution with roles and merged permissions
- Secure, HTTP-only, SameSite session cookies
- CSRF tokens for every state-changing form
- `Authenticate` resolves the session once and stores its user in the request context.
- `CurrentUser` reads that user in handlers and templates.
- `RequireLogin` redirects anonymous visitors to the login page.
- `RequireRole` returns `401` or `403` for role-protected handlers.
- `SafeRedirectPath` safely handles a login form's `next` destination.

`platform.New` constructs the default database-backed authentication service
and installs its middleware:

```go
runtime, err := platform.New(platform.Options{
    DB: db, Config: cfg, Version: version,
    Resources: platform.Resources{FS: applicationWeb, Root: "app/web"},
    Schemas: platform.Schemas{
        FS: applicationSchemas, Root: "database",
        Existing: platform.SchemaMergeExisting,
    },
    RegisterPages: app.RegisterPages,
    RegisterAdmin: app.RegisterAdmin,
})
```

Call `runtime.Run(ctx)` to start the mail worker and HTTP server and to shut
both down gracefully when the context is cancelled.

Application handlers can use the authentication helpers when they need custom
protection:

```go
mux.Handle(
    "GET /account",
    platform.RequireLogin("/login", http.HandlerFunc(pageHandlers.Account)),
)

mux.Handle(
    "GET /admin",
    platform.RequireLogin("/login",
        platform.RequireRole("admin", http.HandlerFunc(pageHandlers.Admin)),
    ),
)
```

The application exposes `GET /login`, `POST /login`, `POST /logout`, and the
protected `GET /account`. Disabled users cannot log in. Changing a password
revokes that user's existing sessions.

Configure authentication in `config.json`:

```json
"authentication": {
  "session_lifetime": "24h",
  "secure_cookies": false,
  "login_max_attempts": 5,
  "login_lockout": "15m"
}
```

Use `false` only for local HTTP development. Production HTTPS deployments must
set `secure_cookies` to `true`.

Failed logins are throttled per email and client address. Counters live in the
database, so every running website instance observes the same lockout. The
defaults block the fifth consecutive failure for 15 minutes; both values are
configurable. Applications can also register database-shared HTTP policies with
`platform.Options.RateLimits`; see `docs/PLATFORM.md`.

### SMTP email and account security

Outgoing mail uses SMTP. Put deployment defaults in `config.json`; the admin
Settings page can override them immediately at runtime:

```json
"email": {
  "enabled": true,
  "host": "smtp.example.com",
  "port": 587,
  "username": "smtp-user",
  "password": "",
  "password_env": "WEBSITE_SMTP_PASSWORD",
  "password_file": "",
  "from_name": "My website",
  "from_address": "noreply@example.com",
  "encryption": "starttls",
  "timeout": "10s"
}
```

Database and SMTP passwords can be provided inline with `password`, through an
environment variable named by `password_env`, or through a file named by
`password_file`. Configure at most one external source for each password. An
external source overrides the inline value; relative file paths are resolved
from the directory containing `config.json`. Environment variables must be
present and secret files must be non-empty, otherwise startup fails.

Encryption accepts `starttls`, `tls` (implicit TLS), or `none`; use an encrypted
mode in production. The admin form never displays a saved SMTP password. Leaving
the password field blank preserves the current database override or config
value. Because runtime settings live in the database, include them in backups.

The built-in account flows provide single-use, expiring password-reset and
email-verification links. Users can enable email-based two-factor authentication
from `/account` after verifying their address. Sign-in codes expire after ten
minutes and are invalidated after five failed attempts. Admin settings can
disable new two-factor enrollment and enable security messages for new sign-ins
and password changes. Password-reset requests deliberately return the same
response for known and unknown email addresses.

Application messages are written to the `email_queue` table before returning
from a request. A background worker delivers them through SMTP and retries
temporary failures after 1 minute, 5 minutes, 15 minutes, and 1 hour, stopping
after five attempts. Interrupted `sending` records are recovered on startup.
The Admin → Email page shows queued, sent, and failed deliveries and allows
failed messages to be retried manually without displaying message bodies. Admin
→ Settings also provides a queued test-email action. Sent message bodies are
cleared after delivery, and completed delivery records are retained for 90 days
by default; change `mail.retention_days` in the admin settings when needed.

### Create the first administrator

After configuring the database, build and run the interactive bootstrap command:

```sh
make build
./release/website --create-admin admin@example.com
```

It securely prompts twice for a password, creates or resets the user, creates
the Administrator role, grants all currently registered permissions, assigns
the role, and revokes older sessions. Passwords are never accepted as command
arguments or written to shell history.

## Universal admin area

The reusable admin foundation lives in `internal/server/admin` and is mounted at
`/admin/`. It includes:

- Dashboard counts for users, roles, and registered pages
- User create/edit/delete, activation, and role assignment
- Role create/edit/delete and permission assignment
- Automatic public/admin page catalog
- Application, SMTP email, and account-security settings
- Permission-filtered navigation
- Same-origin protection on modifying requests
- Process RAM/CPU, HTTP traffic, request count, and uptime metrics
- Shared rate-limit visibility and emergency unblock controls

The required tables are embedded from `platform/database/`: `roles`,
`permissions`, `user_roles`, `role_permissions`, and `settings`. The schema is
applied before serving. Registered admin permissions are synchronized into the
`permissions` table at startup.

Admin handlers require permissions such as `admin.users.view` and
`admin.users.manage`. Anonymous requests are redirected to `/login`; logged-in
users without the required permission receive `403`.

Public templates receive saved application settings as `.Settings`; for example,
`{{index .Settings "app.name"}}`. The shared layout already uses `app.name` for
the title and brand.

The dashboard resource panel reports the website process rather than the entire
server. Bandwidth in/out counts HTTP body bytes read and written since process
startup; it intentionally excludes TCP/TLS and HTTP-header overhead. Rates are
the average since startup. CPU can exceed 100% when work spans multiple cores.

Create the first administrator with `--create-admin` before opening the admin
area.

### Add an admin feature

Feature-specific admin areas implement `admin.Module`, keeping new features out
of the universal admin package:

```go
type ProductsAdmin struct {
    handlers *ProductHandlers
}

func (module ProductsAdmin) Register(area *admin.Admin) error {
    return area.Handle(
        "GET /admin/products",
        catalog.Page{
            ID:          "products",
            Label:       "Products",
            Path:        "/admin/products",
            Description: "Manage the product catalog",
            Permission:  "admin.products.view",
            Navigation:  true,
            Order:       60,
        },
        http.HandlerFunc(module.handlers.List),
    )
}
```

Register it from `app.RegisterAdmin`. Put its template in
`app/web/templates/admin/pages/` and call `area.Render` from its handler. The page
then appears automatically in admin navigation and the Pages screen, and its
permission becomes available to roles.

Public pages registered through `app.RegisterPages` use the same catalog.
Their navigation and the admin Pages screen update from that metadata, so page
lists do not need separate maintenance.
