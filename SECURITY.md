# Security policy

## Supported versions

Security fixes are provided for the latest published release candidate or
stable release. Upgrade to the newest patch before reporting a problem that may
already have been corrected.

## Reporting a vulnerability

Do not open a public issue for an undisclosed vulnerability. Use GitHub's
private vulnerability-reporting feature for `script-php/goweb`, and include:

- the affected version or commit;
- the required configuration and attacker access;
- reproducible steps or a minimal proof of concept;
- the security impact; and
- any suggested mitigation.

Do not include real credentials, personal information, or production data.
Allow maintainers time to reproduce and prepare a coordinated fix before public
disclosure.

## Deployment scope

GoWeb provides application-level controls, but operators remain responsible for
TLS termination, host security, database permissions, secret storage, network
access, backups, and timely dependency updates. See
`docs/SECURITY-REVIEW.md` and `docs/DEPLOYMENT.md` for the release review and
production checklist.
