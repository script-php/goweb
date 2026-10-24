# Upgrading a project from the starter

Keep the reusable starter as the `upstream` Git remote and the project as
`origin`:

```sh
git remote rename origin upstream
git remote add origin <project-repository>
git push -u origin main
```

Update on a dedicated branch:

```sh
git switch -c upgrade-platform-0.22
git fetch upstream
git merge upstream/main
make check
make build
./release/website --check
```

Before testing an upgrade against a production database, create and verify a
backup. Test schema synchronization and critical browser flows on a staging or
disposable database first.

## Conflict policy

Project work should normally be limited to:

- `internal/app/`
- `app/web/`
- project database table definitions
- the composition options in `main.go`

Avoid editing platform implementation under `internal/auth`, `internal/mail`,
and `internal/server`. If these directories remain unmodified, upstream merges
are usually straightforward.

For each upgrade:

1. Read this file and the release notes between the two versions.
2. Search the project for overridden templates and stable page IDs.
3. Compare every overridden template with its new platform default.
4. Compile and run race-enabled tests with `make check`.
5. Run `website --check` against the target configuration and staging database.
6. Exercise login, two-factor authentication, password recovery, admin access,
   email delivery, backup, and health endpoints.
7. Deploy the entire `release/` directory and verify `/health/ready`.

## Ownership of overrides

An application handler override intentionally stops inheriting behavior from
the replaced platform handler. A template override intentionally stops
inheriting edits to that platform template. Prefer widgets, form sections, and
new pages when possible; reserve full overrides for genuinely different
workflows.

## Current compatibility baseline

Version 0.22 introduces `platform.New`, layered application files, stable page
IDs, feature flags, public/admin registration callbacks, dashboard widgets,
user-form sections, and handler overrides. Earlier projects should move their
custom page registration to `internal/app` and project templates/assets to
`app/web` before taking later platform updates.

Version 0.23 makes package mode self-contained. Platform web resources and
required schemas moved under `platform/`; project schemas now use the explicit
add-only, merge, or replacement policy. Existing clone-mode projects should
leave platform tables under `platform/database` and keep only project tables or
patches under the root `database` directory.

Version 0.24 stabilizes package consumption: configuration lives in the public
`platform/configuration` package, extension callbacks use platform-owned
wrapper types, and `Runtime.Run(ctx)` owns the standard lifecycle. It also adds
non-mutating `--schema-plan` output and protects critical platform schema fields
unless `AllowUnsafePlatformChanges` is explicitly enabled. External projects
should stop importing any `internal/` package and use the public configuration,
database, registration, and lifecycle APIs.
