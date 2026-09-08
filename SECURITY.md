# Security Policy

## Supported versions

| Version | Supported          |
| ------- | ------------------ |
| main    | :white_check_mark: |
| older   | :x: (please upgrade) |

## Reporting a vulnerability

**Do not open a public issue.** Email the maintainers with:

- affected version/commit and deployment mode (SQLite/PG, Docker/systemd)
- steps to reproduce or proof of concept
- impact assessment, if possible

We aim to acknowledge within 72 hours and will coordinate disclosure with you.
Credit is given with your permission.

## Security-relevant notes for operators

- `SECRET_KEY` is **required** and must be random (≥32 chars). The server
  refuses to start with a weak key in `serve` mode.
- The seeded demo account (`admin` / `3dQAKbZHqP6P`) exists **only for local
  evaluation**. Set `ADMIN_PASSWORD` when seeding any shared/production
  database, and delete or rotate the account afterwards.
- Old git history contains development-only credentials (`dev-secret-...`,
  demo passwords). They were never production secrets; still, never reuse
  them anywhere.
- Session cookies are `HttpOnly` + `SameSite=Lax`, with `Secure` following
  the request scheme — terminate TLS at your reverse proxy in production.
- The project is single-tenant with minimal RBAC (`admin` = read/write,
  `viewer` = read-only, enforced on all write endpoints). Anyone with an
  `admin` login has full access. Do not expose it to untrusted users without
  an access gate (VPN, Cloudflare Access, etc.).
