# Contributing to StarOcean

Thanks for your interest! StarOcean is a lightweight, single-binary ERP for SMEs
(Go + templ + htmx, Turso/SQLite by default). We keep the process boring on purpose.

## Development setup (zero dependencies)

```bash
go version          # Go 1.26+
make dev            # templ + Tailwind + run on :8080 (Turso file DB, seeded)
# login: admin / 3dQAKbZHqP6P (demo only, see SECURITY.md)
```

Useful targets: `make build`, `make test` (full suite, temp Turso file DB),
`make lint` (golangci-lint, must be clean), `make hooks` (lefthook install).

## Ground rules

1. **Core stays lean.** No new runtime dependencies without discussion.
   Industry-specific logic goes through the extension conventions in
   `docs/PLUGINS.md` (events + `properties`), not into core code.
2. **Server-rendered first.** New pages are templ + htmx (forms POST+303,
   lists swap `#list`). No frontend framework, no new JS build steps.
3. **Turso-native SQL.** Application SQL must be valid Turso/SQLite
   (`strftime`, `date('now',…)`, `LIKE`, `json_patch`, `CAST(… AS …)`,
   `julianday`, `$N` placeholders). Do **not** write PostgreSQL dialect
   (`NOW()`, `ILIKE`, `::type`, `DATE_TRUNC`, `INTERVAL`, `FOR UPDATE`,
   `gen_random_uuid`, `CURRENT_DATE - date`). Migration `*.up.sql` files
   remain historical PG-shaped DDL and are translated only at migrate time
   (`internal/db/turso_migrate.go` + golden tests).
4. **Money and stock are sacred.** Amount parsing must reject (never silently
   zero); stock/payment read-modify-write only inside transactions
   (`BeginTx` → `BEGIN IMMEDIATE`). Add/update tests in `main_test.go` for
   any order/stock/ledger change.

## Pull requests

- Small, focused PRs with a clear description (what + why + how verified).
- `make build && make test && make lint` must pass locally.
- Update `docs/DESIGN.md` §0 if you change architecture, CLI, or deployment.
- Generated files (`*_templ.go`, `public/css/output.css`) are committed;
  run `make templ css` before pushing (lefthook checks consistency).

## Reporting bugs

Use the bug report template. Include: version/commit, `DATABASE_URL`
(`sqlite:` / `turso:`), steps to reproduce, expected vs actual, and relevant logs.
Security issues: **do not** open a public issue — see `SECURITY.md`.
