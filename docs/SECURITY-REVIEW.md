# 1.0 release-candidate security review

Reviewed on 2026-10-25. This review is a release gate, not a claim that the
software can never contain a vulnerability.

## Verified controls

- Passwords use bcrypt and session/token secrets are stored only as hashes.
- Authentication, role, permission, and force-password-change checks run before
  protected handlers. Every built-in admin route has an authorization test.
- State-changing browser requests require same-origin CSRF tokens. Multipart
  avatar and media forms have regression coverage.
- Login and application rate limits use atomic shared-database counters. Trusted
  proxy configuration controls which forwarded client addresses are accepted.
- Uploads enforce size and MIME allowlists, randomized storage keys, safe path
  handling, and explicit media-reference cleanup rules.
- Backup extraction rejects absolute paths, traversal, links, and unsupported
  archive entries. Restore requires an explicit confirmation flag.
- Production configuration requires HTTPS public URLs and secure cookies.
- Database passwords and SMTP credentials are not logged. Release archives use
  `config.example.json`, never the local runtime configuration or backups.
- Panic responses, database-maintenance responses, and authorization failures do
  not expose internal errors to browsers.

## Deployment responsibilities

- Terminate TLS at a maintained reverse proxy and configure only its addresses
  under `server.trusted_proxies`.
- Give the database user access only to the application database and protect
  `config.json`, backups, and uploads with operating-system permissions.
- Review schema plans and backups before upgrades, particularly application
  schemas that opt into unsafe platform changes.
- Restrict `/metrics` at the reverse proxy if operational metadata must not be
  public.
- Keep Go, the operating system, database, and reverse proxy patched.

## Release-candidate result

No known release-blocking issue remains. Automated race, authorization,
responsive-browser, archive-safety, schema-upgrade, backup/restore, MySQL, and
MariaDB tests remain mandatory release gates.
