# Production deployment checklist

Use this checklist for a new server and for every release. Commands assume the
application is installed in `/opt/website` and Nginx runs on the same host.

## Before the first deployment

- Create a dedicated, non-login service account and database account.
- Allow the service account to read the release directory and write only its
  release directory, including `uploads/` and `tables.hash`.
- Give the database account access only to the application database. Schema
  synchronization needs `SELECT`, `INSERT`, `UPDATE`, `DELETE`, `CREATE`,
  `ALTER`, `INDEX`, and `REFERENCES`; avoid global privileges and `GRANT OPTION`.
- Bind MySQL to localhost when it shares the web server. For a remote database,
  restrict port 3306 to the application server's private IP and require TLS.
- Permit inbound TCP 22, 80, and 443 only. Never expose the application's 9090
  listener or MySQL publicly.
- Put secrets only in `release/config.json`, owned by the service user with mode
  `0600`. Do not commit the production file.
- Configure HTTPS, `server.production: true`, a correct `public_url`, trusted
  proxy addresses, and secure cookies.
- Configure SMTP and test password recovery before launch.

Example UFW policy:

```sh
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw allow OpenSSH
sudo ufw allow 'Nginx Full'
sudo ufw enable
```

## Deploy a release

1. Create and verify a current backup, including uploads.
2. Copy the complete release directory to a versioned staging location.
3. Preserve the production `config.json`, uploads, and schema marker.
4. Run `./website --version` and `./website --schema-plan` as the service user.
5. Review every planned SQL statement. Test destructive changes on staging.
6. Run `./website --check` against the production configuration.
7. Install or refresh the hardened unit with `sudo ./website --register`.
8. Restart with `sudo systemctl restart website`.
9. Verify `/health/live`, `/health/ready`, login, admin access, and one public
   page through HTTPS.
10. Inspect `journalctl -u website --since '-10 minutes'` and Nginx errors.

Run `systemd-analyze security website.service` after registration. Some findings
are expected for a network service, but investigate regressions when changing
the unit.

## Logs and retention

Application logs go to the systemd journal; the service does not create a log
file. Configure limits in `/etc/systemd/journald.conf` or a drop-in such as
`/etc/systemd/journald.conf.d/website.conf`:

```ini
[Journal]
SystemMaxUse=1G
SystemKeepFree=2G
MaxRetentionSec=30day
Compress=yes
```

Restart journald after changing it and confirm usage with `journalctl
--disk-usage`. Nginx file logs can use `deploy/logrotate-nginx.conf`; copy it to
`/etc/logrotate.d/website-nginx` after checking the configured paths.

## Database outages

If the database becomes unavailable after startup, public and admin requests
receive a cache-disabled `503` maintenance page with `Retry-After: 30`.
`/health/live` remains `200`, while `/health` and `/health/ready` return `503`.
Nginx and load balancers can therefore distinguish a running process from one
that is ready for traffic. The application automatically resumes normal
requests after connectivity returns; no restart is required.

## Rollback

Keep the previous binary until health and browser checks pass. Code rollback is
safe only when the previous release supports the database schema already
applied. Otherwise restore the verified backup during a maintenance window.
Never reverse a schema migration by editing `tables.hash`.
