# Changelog

This project follows semantic versioning for the exported `platform` API.

## 1.0.0-rc.1

- Moved login throttling into atomic database-backed counters shared by multiple website instances.
- Added a protected rate-limit schema and cross-instance integration coverage.
- Added public request rate-limit policies with IP/user/custom keys, HTTP 429 responses, and scheduled stale-counter cleanup.
- Added a responsive rate-limit administration page with search, active-block visibility, permission-protected clearing, and audit records.
- Exposed supported authentication helpers through the public platform package.
- Expanded the external reference application and release-candidate compatibility checks.
- Added an explicit external API contract gate and private security-reporting policy.
- Updated Go supplementary modules to their newest reviewed Go 1.24-compatible releases.
- Added vulnerability scanning as a CI and release gate plus dependency manifests in release archives.
- Added environment-variable and file-backed database and SMTP secrets.
- Serialized schema application with a per-database advisory migration lock.
- Made readiness fail immediately when graceful shutdown begins.
- Added renewable database leases to prevent cross-instance background-job overlap and recover work after crashes.
- Added built-in S3-compatible/MinIO media storage with live admin configuration, secure secret sources, bucket checks, and per-object backend routing.
- Added a validated `make release` workflow that creates GitHub-ready Linux AMD64 and ARM64 ZIP packages with checksums.

## 0.32.1

- Fixed release validation incorrectly treating branch names as version tags.
- Fixed MariaDB CI startup checks to use the health command bundled with its official image.
- Made database backups disable MySQL client column statistics when dumping MariaDB.
- Fixed CSRF validation for multipart admin forms, including user edits and avatar uploads.
- Added multipart CSRF regression coverage.

## 0.32.0

- Added persistent, configurable background jobs with manual runs and admin status.
- Moved media and email maintenance onto the shared scheduler and exposed application job registration.
- Added public avatar delivery and the `AvatarURL` helper.
- Added media pagination, bulk deletion, and optional aged-unused-media cleanup.
- Added protected admin backup creation, download, and verification.
- Added Prometheus process, HTTP, database, media-storage, and job-failure metrics.
- Expanded the external reference application with media references and background jobs.

## 0.31.0

- Added nullable user avatar media references and automatic cleanup registration.
- Added square avatar selection during user creation and preview/change/remove controls when editing users.
- Exposed avatar media IDs to authenticated users and admin user-form extensions.

## 0.30.0

- Added bidirectional media cleanup for orphan files and missing-file rows.
- Added safe, purpose-independent application reference rules that clear stale media IDs.
- Added cleanup preview, manual execution, periodic scheduling, overlap protection, and admin controls.
- Added optional object listing support for custom media backends.

## 0.29.0

- Organized new media objects into safe purpose and upload-date directories.
- Kept legacy flat storage keys readable and included nested media in cleanup.
- Added admin media filtering and editing for purpose and record attachments.

## 0.28.1

- Added optional, searchable media purpose labels such as `avatar` and `post`.
- Added `Media.SetPurpose` and documented application-table media references.

## 0.28.0

- Added validated media uploads, randomized local storage, ownership and record
  attachments, automatic orphan cleanup, and a replaceable storage backend API.
- Added a permission-protected admin media library with search, previews,
  downloads, deletion, auditing, and responsive presentation.
- Added media lifecycle, unsafe-path, MIME-type, size-limit, and cleanup tests.

## 0.27.0

- Added Go, MySQL, and MariaDB CI with full disposable-database integration tests.
- Added validated tag-driven Linux releases, checksums, and release artifacts.
- Documented canonical module-proxy resolution, publishing, installation, and upgrades.

## 0.26.1

- Added database-backed login-rate-limit integration coverage for account/IP
  isolation, reset, expiry, HTTP 429 responses, and concurrent attempts.
- Made the HTTP `Retry-After` value follow the configured lockout duration.

## 0.26.0

- Added reusable admin notifications and empty-state components.
- Added browser-session, responsive-layout, route-authorization, and complete
  database/upload backup-restore test coverage.

## 0.25.0

- Hardened generated systemd services and added production deployment guidance.
- Added an automatic maintenance response for runtime database outages.

## 0.24.0

- Stabilized external package use with public configuration and extension types.
- Added `Runtime.Run(ctx)` as the standard server and worker lifecycle.
- Added `--schema-plan` and the `PlanSchemas` API for non-mutating previews.
- Protected critical platform table columns from accidental application patches.
- Added a separate-module compilation test for the supported import boundary.

## 0.23.0

- Made platform templates, assets, and required schemas self-contained.
- Added configurable application resource directories and schema merge policies.

## 0.22.0

- Introduced `platform.New`, stable page IDs, feature flags, extension callbacks,
  dashboard widgets, user-form sections, and handler overrides.
