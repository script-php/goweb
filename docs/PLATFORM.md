# Platform extension guide

The repository is split into two ownership layers:

```text
platform/                 stable API, embedded defaults, and schemas
internal/server, auth…    platform implementation; projects should not edit it
internal/app/             project route and admin registration
app/web/                  project templates and assets
platform/web/             embedded platform templates and assets
database/                 project tables and platform table patches
platform/database/        embedded platform-owned table definitions
```

`main.go` is the composition root:

```go
runtime, err := platform.New(platform.Options{
    DB:               db,
    Config:           cfg,
    Version:          version,
    Resources: platform.Resources{
        FS: applicationFiles,
        Root: "app/web",
    },
    Schemas: platform.Schemas{
        FS: applicationSchemas,
        Root: "database",
        Existing: platform.SchemaMergeExisting,
    },
    Features:         platform.DefaultFeatures(),
    RegisterPages:    app.RegisterPages,
    RegisterAdmin:    app.RegisterAdmin,
})
```

The same API supports direct-clone projects and external projects that import
`script-php/goweb/platform`. A standalone importing example is provided under
`examples/package-app`.

An importing application normally declares:

```go
require script-php/goweb v1.0.0-rc.1
```

The short module path requires a Go module proxy that maps
`script-php/goweb` to its source repository. See `docs/RELEASING.md` for the
required proxy endpoints, release process, and upgrade procedure. Until that
distribution is configured, use a local replacement while developing:

```go
replace script-php/goweb => ../goweb
```

Do not change the platform's module path inside an application fork; imports
and upgrade compatibility are based on the canonical path above.

## Authentication in application handlers

Importing projects use the supported helpers in `platform`; they must not
import implementation packages under `internal/`:

```go
handler := platform.RequireLogin("/login",
    platform.RequirePermission("projects.view", http.HandlerFunc(showProject)),
)

func showProject(w http.ResponseWriter, r *http.Request) {
    user := platform.CurrentUser(r.Context())
    // user contains the signed-in account, roles, and merged permissions.
}
```

`RequireRole`, `SafeRedirectPath`, and the `AuthenticatedUser` type are also
part of the public extension API.

## Resource directories

Applications are not required to reproduce this repository's layout. Embed any
folder and map its template and static directories:

```go
Resources: platform.Resources{
    FS:           applicationFiles,
    Root:         "resources",
    TemplatesDir: "views",
    StaticDir:    "public",
}
```

`TemplatesDir` and `StaticDir` default to `templates` and `static`. Application
files resolve before embedded platform files, so a matching relative path is an
override and every missing file falls back to the platform.

## Application database schemas

The platform embeds its required authentication, authorization, settings,
audit, token, session, and mail-queue tables. Applications provide only their
own definitions and intentional modifications:

```go
Schemas: platform.Schemas{
    FS:       applicationSchemas,
    Root:     "tables",
    Existing: platform.SchemaMergeExisting,
}
```

Three conflict policies are available:

- `SchemaAddOnly` allows new tables and rejects any platform table name.
- `SchemaMergeExisting` adds or modifies named columns, indexes, and foreign
  keys while retaining unspecified platform definitions.
- `SchemaReplaceExisting` replaces the complete platform definition and should
  be reserved for projects that intentionally own that table thereafter.

Required identity, session, role, permission, and settings columns are
protected. Merge patches may add application fields and alter non-critical
fields, but changing or dropping a protected column—and replacing a protected
table—fails by default. Only a reviewed migration should set
`AllowUnsafePlatformChanges: true`.

A merge patch for the built-in `users` table can be small:

```json
{
  "name": "users",
  "columns": [
    {"name": "company", "type": "VARCHAR(150)", "nullable": true},
    {"name": "display_name", "type": "VARCHAR(200)", "nullable": true}
  ],
  "indexes": [
    {"name": "idx_users_company", "columns": ["company"]}
  ]
}
```

Items with new names are appended; items matching a platform name override only
the supplied properties. Use `drop_columns`, `drop_indexes`, and
`drop_foreign_keys` for explicit removals. Drops and replacements can break
platform behavior, so test them on a disposable database and follow the backup
workflow. The final merged schema is applied before any HTTP request is served.
Use `website --schema-plan` to print the SQL that would run without changing the
database or schema marker.

## Customization ladder

Choose the least invasive option that solves the requirement:

1. Configure a platform feature.
2. Register an application page or admin page.
3. Add a dashboard widget or user-form section.
4. Override a platform template or asset.
5. Replace a built-in handler by stable page ID.

Options 1–3 retain the most future platform improvements. A copied template or
replacement handler becomes application-owned and must be reviewed during
platform upgrades.

## Add public pages

Register project pages in `internal/app/app.go`:

```go
func RegisterPages(pages *platform.Pages) error {
    return pages.Template(
        "GET /contact",
        platform.Page{
            ID:          "contact",
            Label:       "Contact",
            Path:        "/contact",
            Description: "Contact the team",
            Navigation:  true,
            Order:       30,
        },
        "contact",
        "Contact",
        nil,
    )
}
```

Put the template at `app/web/templates/pages/contact.html`. For a handler that
loads data or processes a form, use `pages.Handle(pattern, page, handler)`.
Registration metadata automatically feeds navigation and the admin page list.

## Upload and attach media

Every runtime exposes `runtime.Media`. Uploads are content-sniffed, size-limited,
stored under random names, and recorded in the platform `media` table. Local
files are organized as `uploads/<purpose>/DD-MM-YYYY/<random-name>.<ext>`; an
empty purpose uses `general`. Purpose directory names are normalized to safe,
lowercase slugs. Never trust the browser's filename or `Content-Type` header:

```go
upload, header, err := r.FormFile("image")
if err != nil {
    return err
}
defer upload.Close()

file, err := runtime.Media.Save(r.Context(), header.Filename, upload,
    platform.MediaOptions{
        OwnerUserID: currentUserID,
        Purpose:     "avatar",
        EntityType:  "user",
        EntityID:    strconv.FormatUint(currentUserID, 10),
    })
```

Applications can attach an existing file later with `Media.Attach`, query it
with `Media.List`, stream it with `Media.Open`, and remove it with
`Media.Delete`. `Media.SetPurpose` changes or clears the optional purpose label,
while `Media.UpdateMetadata` updates purpose and record attachment together.
Use `Purpose` for the role of a file (`avatar`, `post`, `featured-image`) and
use `EntityType` plus `EntityID` for the exact record currently using it.

Application tables should store only the media ID. The platform users table
already includes nullable `avatar_media_id`, its foreign key, and its cleanup
rule. The admin create/edit user screen manages this reference and records the
media row with `purpose=avatar`, `entity_type=user`, and the user ID. Add the
same nullable-ID pattern to application tables and register those locations in
`MediaReferences`. Foreign keys are authoritative; media labels make searching
and management understandable.

A download handler should perform its own record-level access
check before calling `Open`; the platform deliberately does not make uploaded
files public by default.

Configure limits in `config.json`:

```json
"storage": {
  "backend": "s3",
  "uploads_directory": "uploads",
  "max_upload_bytes": 10485760,
  "allowed_mime_types": ["image/jpeg", "image/png", "application/pdf"],
  "s3": {
    "endpoint": "minio.example.com:9000",
    "bucket": "website-media",
    "region": "us-east-1",
    "access_key": "website",
    "secret_key": "",
    "secret_key_env": "WEBSITE_S3_SECRET_KEY",
    "use_tls": true,
    "path_style": true,
    "create_bucket": false,
    "prefix": "production"
  }
}
```

Local storage is the default. Set `backend` to `s3` for MinIO, AWS S3, or a
compatible service. The endpoint must not contain a path; a scheme is accepted
but TLS is controlled by `use_tls`. Path-style lookup is normally required for
MinIO. The bucket must already exist unless `create_bucket` is enabled for a
credential allowed to create it. The secret supports the same inline,
environment, and file sources as database and SMTP passwords.

Admin → Settings can test and apply S3 configuration without restarting. Each
media row records its owning backend, so switching affects new uploads while
existing local or S3 objects remain readable and deletable. Do not remove old
backend credentials until its media has been migrated or deleted.

Applications can still provide a custom adapter through
`platform.Options.MediaBackend`; implement `Put`, `Open`, and `Delete`.
Implement the optional `platform.StorageObjectLister` interface to enable
bidirectional cleanup for a custom backend.

Declare every nullable application column that may store a media ID. Rules are
global and purpose-independent:

```go
runtime, err := platform.New(platform.Options{
    DB: db,
    Config: cfg,
    MediaReferences: []platform.MediaReferenceRule{
        {ID: "post_featured", Label: "Post featured image", Table: "posts", Column: "featured_media_id"},
    },
})
```

The platform registers `users.avatar_media_id` automatically. Applications
only register their additional media-reference columns.

Only code-registered identifiers can be enabled in the admin interface, which
prevents arbitrary SQL identifiers from being submitted by a browser. The
cleanup screen supports non-mutating previews, manual runs, rule selection,
an orphan safety age, and periodic execution. It reconciles storage to the
`media` table and the `media` table to storage; before deleting a row for a
missing file, it sets matching IDs to `NULL` in every enabled rule.

The admin media library provides upload, search, image preview, download, and
purpose/date filters, metadata editing, audited deletion, and storage-path
visibility with the `admin.media.view` and `admin.media.manage`
permissions.

Public templates can render a user's avatar with `platform.AvatarURL(userID)`.
The URL returns 404 when the user is inactive or has no avatar, allowing the
application to render its own fallback.

## Register background jobs

Applications can register periodic work without creating goroutines:

```go
BackgroundJobs: []platform.BackgroundJob{{
    ID: "app.remove_expired_drafts",
    Label: "Remove expired drafts",
    Interval: 24 * time.Hour,
    Enabled: true,
    Run: func(ctx context.Context) error { return removeExpiredDrafts(ctx, db) },
}},
```

The Jobs admin page controls enablement and intervals, runs jobs manually, and
shows duration, next run, errors, and success/failure totals. A database-backed
lease prevents the same job from overlapping across goroutines, processes, or
servers sharing the database. Active jobs renew their leases automatically;
after a process crash, another instance can recover the job when its lease
expires.

## Register shared rate limits

Applications can protect APIs and expensive actions with fixed-window limits
stored in the shared database. Every website instance behind a load balancer
observes the same counter:

```go
RateLimits: []platform.RateLimitPolicy{{
    ID:     "contact-submit",
    Limit:  5,
    Window: time.Minute,
    Match: func(r *http.Request) bool {
        return r.Method == http.MethodPost && r.URL.Path == "/contact"
    },
    Key: platform.RateLimitByIP,
}},
```

`RateLimitByIP` uses the trusted-proxy-aware client address. Use
`RateLimitByUser` to group authenticated requests by user ID while falling back
to the client address for anonymous requests, or provide a custom key function.
An empty custom key skips that policy. Rejected requests receive `429 Too Many
Requests` with a `Retry-After` header. Expired counters are removed by the
platform's daily rate-limit cleanup job.

Prometheus-compatible operational metrics are exposed at `/metrics`, including
process, HTTP traffic, database-pool, media-storage, and job-failure metrics.

## Add admin pages

`platform.Admin` provides the protected admin registry:

```go
func RegisterAdmin(admin *platform.Admin) error {
    return admin.Handle(
        "GET /admin/orders",
        platform.Page{
            ID:          "orders",
            Label:       "Orders",
            Path:        "/admin/orders",
            Description: "Manage orders",
            Permission:  "admin.orders.view",
            Navigation:  true,
            Order:       70,
        },
        http.HandlerFunc(showOrders),
    )
}
```

Place its template at `app/web/templates/admin/pages/orders.html` and render it
with `admin.Render`. The permission is created automatically and navigation is
shown only to authorized users.

## Extend built-in admin pages

Dashboard widgets avoid replacing the dashboard:

```go
err := admin.AddDashboardWidget(platform.DashboardWidget{
    ID:    "orders",
    Title: "Orders",
    Render: func(r *http.Request) (template.HTML, error) {
        return platform.HTML(`<p>42 orders awaiting review</p>`), nil
    },
})
```

User-form sections add project fields to both create and edit forms:

```go
err := admin.AddUserFormSection(platform.UserFormSection{
    ID:    "customer-profile",
    Title: "Customer profile",
    Render: func(r *http.Request, user platform.User) (template.HTML, error) {
        return platform.HTML(`<label>Company<input name="company"></label>`), nil
    },
    Save: func(r *http.Request, userID uint64) error {
        return saveCompany(r.Context(), userID, r.FormValue("company"))
    },
})
```

HTML returned by these compact extension points is trusted. Never insert
unescaped user input. For substantial interfaces, register a normal page and
use `html/template` instead.

## Override templates and assets

Application files are resolved before platform files. To customize a built-in
template, create the same relative path under `app/web`:

```text
platform default: platform/web/templates/admin/pages/dashboard.html
project override: app/web/templates/admin/pages/dashboard.html
```

The project file wins. Files not supplied by the project continue falling back
to the platform layer. The same rule applies to static assets. Project CSS
normally belongs in `app/web/static/css/application.css`, which is loaded after
the platform stylesheet.

An overridden template does not automatically receive later changes made to
the platform version of that template. Record intentional overrides and compare
them when upgrading.

## Replace handlers

Built-in routes have stable IDs. Replace behavior during composition:

```go
runtime, err := platform.New(platform.Options{
    // ...
    PublicOverrides: map[string]http.Handler{
        platform.PageLogin: http.HandlerFunc(appLogin),
    },
    AdminOverrides: map[string]http.Handler{
        platform.AdminUserEdit: http.HandlerFunc(appEditUser),
    },
})
```

The platform retains route registration, metadata, middleware, authentication,
permissions, and navigation. Only the selected handler changes. Use constants
from `platform` instead of hard-coded IDs where available.

## Disable built-in admin features

Start from defaults, then turn off only what the project does not need:

```go
features := platform.DefaultFeatures()
features.Email = false
features.Roles = false
```

Pass `features` through `platform.Options`. Disabled features do not register
their routes, navigation entries, or new permissions. Permission rows created
by an earlier deployment may remain in the database but no longer protect a
registered route. Authentication remains a core platform service because
sessions and admin protection depend on it.

## Extension compatibility rules

- Stable page IDs and exported `platform` types are the supported contract.
- Packages under `internal/` are implementation details.
- Project code belongs under `internal/app` and `app/web`.
- Minor platform releases preserve existing extension points.
- Breaking API or page-ID changes require a major release and instructions in
  `UPGRADING.md`.
- Schema additions are applied at startup, but production upgrades must still
  follow the documented backup and preflight process.

`Runtime.Run(ctx)` is the supported lifecycle entry point. It owns background
workers, the HTTP listener, and graceful shutdown. `Runtime.Handler` remains
available for tests or projects that intentionally provide their own server.
