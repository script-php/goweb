# Website starter roadmap

## Next

- [x] Make admin tables mobile-friendly cards instead of horizontally scrolling tables.
- [x] Add trusted reverse-proxy configuration for correct client IP detection.
- [x] Add an Nginx reverse-proxy configuration example.
- [x] Add production validation for HTTPS and secure cookies.
- [x] Add database-backed integration tests for users, roles, and permissions.

## User security

- [x] Add a **Log out all sessions** action to user management.
- [x] Add a **Force password change on next login** option.
- [x] Add optional two-factor authentication.
- [x] Record logout and manual session revocation in the audit log.

## Later — email infrastructure

- [x] Add configurable SMTP email delivery with config and admin overrides.
- [x] Add password recovery by email.
- [x] Add email-address verification.
- [x] Add optional security emails for password changes and new sign-ins.
- [x] Add queued delivery, retry handling, and email-delivery audit records.

## Roles and permissions

- [x] Add human-readable permission descriptions.
- [x] Add **Select all** and **Clear all** controls per permission group.
- [x] Add tests protecting the final active administrator.
- [x] Show how many users are assigned to each role.

## Audit log

- [x] Add search by actor, action, target, and IP address.
- [x] Add date and action filters.
- [x] Add pagination.
- [x] Add a configurable retention and cleanup policy.
- [x] Add audit-log export.

## Admin experience

- [x] Add breadcrumbs.
- [x] Replace browser delete confirmations with an accessible confirmation dialog.
- [x] Link dashboard statistics to their relevant admin pages.
- [x] Add reusable success, warning, and error notifications.
- [x] Add empty-state components for lists without records.

## Database operations

- [x] Compress generated backups, including the configured uploads directory.
- [x] Add configurable backup retention.
- [x] Add a backup verification command.
- [x] Document database restoration and test archive extraction.
- [x] Add a scheduled-backup systemd timer example.
- [x] Document migration recovery and rollback procedures.

## Deployment and operations

- [x] Split health endpoints into `/health`, `/health/live`, and `/health/ready`.
- [x] Add an Nginx HTTPS deployment example.
- [x] Add systemd hardening options.
- [x] Add log rotation guidance.
- [x] Add a production deployment checklist.
- [x] Document firewall and database-access recommendations.
- [x] Provide a graceful maintenance page for database outages.

## Testing

- [x] Add browser tests for login, logout, and admin workflows.
- [x] Add responsive-layout tests for common mobile widths.
- [x] Add login-rate-limit integration tests.
- [x] Test database backup and restore workflows.
- [x] Add authorization tests for every registered admin route.

## Next phase — platform 1.0

- [ ] Push and publish the prepared `v1.0.0-rc.1` release candidate, then verify it through the Go module proxy.
- [x] Add automated tagged releases and module-resolution documentation.
- [x] Add CI for supported Go, MySQL, and MariaDB versions.
- [x] Add a production-ready file upload and media-management API.
- [x] Add bidirectional media cleanup with registered reference rules, previews, and scheduling.
- [x] Add scheduled/background job registration for application modules.
- [x] Export Prometheus metrics. Optional distributed tracing remains a post-1.0 extension.
- [x] Support shared rate-limit storage for multi-instance deployments.
- [x] Build a reference application entirely through the public platform API.
- [x] Define the platform 1.0 API freeze and deprecation policy.
- [x] Prevent duplicate background-job execution across multiple instances with renewable database leases.

## Completed foundation

- [x] Root `main.go` and self-contained release directory.
- [x] Embedded templates, static assets, and database schemas.
- [x] `make build`, `make run`, `make check`, and `make backup`.
- [x] Runtime configuration loaded from `release/config.json`.
- [x] Platform/application split with stable extension and override APIs.
- [x] Public configuration API and platform-owned extension types.
- [x] External-module compilation contract test.
- [x] Unified runtime lifecycle for serving, workers, and graceful shutdown.
- [x] Non-mutating schema plan and protected platform schema fields.
- [x] Layered project templates and assets with platform fallbacks.
- [x] Documented upstream upgrade workflow and compatibility contract.
- [x] Embedded application version from `version.txt`.
- [x] Systemd `--register` and `--unregister` commands.
- [x] Automatic database schema synchronization.
- [x] Authentication, sessions, CSRF protection, roles, and permissions.
- [x] Extensible admin dashboard and navigation registry.
- [x] User search, pagination, role assignment, and password reset.
- [x] Protection against deleting or disabling your own account.
- [x] Protection for the built-in Administrator role.
- [x] Automatic Administrator access to newly registered permissions.
- [x] Audit logging for authentication and administrative changes.
- [x] Configurable login throttling.
- [x] Responsive admin navigation and compact mobile dashboard.
- [x] Request logging, panic recovery, error pages, and graceful shutdown.
