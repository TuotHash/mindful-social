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

## Configuration

The server is configured entirely through environment variables. Inside
`nix develop`, `DATABASE_URL` is set automatically via the shell hook.

### Core

| Variable | Required | Default | Description |
|---|---|---|---|
| `DATABASE_URL` | **yes** | — | Postgres connection string, e.g. `postgres:///mindful_social?host=/path/to/socket` |
| `LISTEN_ADDR` | no | `127.0.0.1:8080` | TCP address the HTTP server binds to |
| `PUBLIC_BASE_URL` | no* | `http://127.0.0.1:8080` | Absolute origin the browser sees. Required when any OAuth provider is configured, because callback URLs are derived from it. Set to your public domain, e.g. `https://mindful.example.org` |

### OAuth / SSO (all optional)

The app starts with no OAuth configured — only password auth is available. Set
any of the blocks below to enable the corresponding provider. Multiple
providers can be active at the same time.

**Google**

```
GOOGLE_CLIENT_ID=<client_id>
GOOGLE_CLIENT_SECRET=<client_secret>
```

Redirect URI to register in the Google Cloud Console:
`<PUBLIC_BASE_URL>/auth/callback/google`

**GitHub**

```
GITHUB_CLIENT_ID=<client_id>
GITHUB_CLIENT_SECRET=<client_secret>
```

Redirect URI: `<PUBLIC_BASE_URL>/auth/callback/github`

**Custom OIDC (Authelia, Authentik, Keycloak, Zitadel, …)**

Register one or more keys in `OIDC_PROVIDERS`, then supply per-key variables:

```
OIDC_PROVIDERS=work,community

OIDC_WORK_ISSUER=https://auth.example.org
OIDC_WORK_CLIENT_ID=<client_id>
OIDC_WORK_CLIENT_SECRET=<client_secret>
OIDC_WORK_LABEL=Work SSO          # optional — button label, defaults to key title-cased

OIDC_COMMUNITY_ISSUER=https://id.community.example
OIDC_COMMUNITY_CLIENT_ID=<client_id>
OIDC_COMMUNITY_CLIENT_SECRET=<client_secret>
```

Keys are case-insensitive (`work` and `WORK` are the same). The redirect URI
for a key named `work` is `<PUBLIC_BASE_URL>/auth/callback/oidc:work`.

Any provider that is missing its required variables is skipped at startup with
a warning; it does not prevent the other providers or the app from starting.

## License

TBD — likely AGPL-3.0 or similar copyleft FOSS license.
