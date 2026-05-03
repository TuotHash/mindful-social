# Mindful Social

A community platform for respectful, mindful conversations. Combines free-form
discussion with a structured **argument graph**: typed nodes (Topic / View /
Reasoning / Fact) connected by typed edges (supports / opposes / refines /
cites / relates_to). Users commit to views and attach personal reasoning.

> **Status:** early development — MVP in progress.

## Quickstart

This project uses Nix flakes. The dev shell pins all tools (Go, Postgres,
sqlc, goose, templ, air) at known versions so the environment is identical
on macOS, Linux, NixOS, and CI.

```bash
nix develop                      # enter dev shell

# First-time setup:
./scripts/db-init.sh             # initialize Postgres data dir
./scripts/db-start.sh            # start Postgres in foreground (Ctrl-C to stop)
# in another shell, also inside `nix develop`:
./scripts/db-create.sh           # create the mindful_social database
./scripts/migrate-up.sh          # apply migrations

# Run the app:
go run ./cmd/server              # listens on 127.0.0.1:8080
```

The app speaks plain HTTP on `127.0.0.1:8080`. Put a reverse proxy
(HAProxy, Caddy, nginx) in front for TLS — the app never sees a certificate.

## Project layout

```
cmd/server/         entry point
internal/           private application code
  config/             env-var loading
  db/                 sqlc-generated query code (run `sqlc generate` to refresh)
  server/             HTTP server, routes, middleware
migrations/         goose .sql migrations (numbered)
queries/            SQL files consumed by sqlc to generate Go code
scripts/            dev helpers (db-init, migrate-up, …)
flake.nix           reproducible dev shell
```

## Tech stack

| Layer          | Tool                                               |
|----------------|----------------------------------------------------|
| Language       | Go                                                 |
| Router         | `chi`                                              |
| Database       | Postgres                                           |
| SQL → Go       | `sqlc` (write SQL, get type-safe Go)               |
| Migrations     | `goose`                                            |
| Templates      | `templ` (added when we have HTML pages)            |
| Sessions / auth| `scs` + `bcrypt` (added when we add auth)          |
| Interactivity  | HTMX (added when needed)                           |
| Graph view     | Svelte component (deferred)                        |
| Dev env        | Nix flake                                          |

## Domain model

- **Node** — anything in the graph. `type` is one of `topic`, `view`,
  `reasoning`, `fact`. Free-form `tags` add further grouping (domain, nature)
  without bloating the type system.
- **Edge** — directed, typed link between two nodes. `kind` is one of
  `supports`, `opposes`, `refines`, `cites`, `relates_to`.
- **Commitment** — a user pinning themselves to a View, optionally with a
  personal Reasoning node they authored. Shows on their profile as a feed
  entry.

See [`migrations/00001_initial_schema.sql`](migrations/00001_initial_schema.sql)
for the full schema.

## License

TBD — likely AGPL-3.0 or similar copyleft FOSS license.
