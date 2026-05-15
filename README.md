# Mindful Social

A community platform for respectful, mindful conversations. Combines free-form
discussion with a structured **argument graph**: typed nodes (Topic / View /
Finding) connected by typed edges (supports / opposes / related). Users
commit to views and attach findings that explain their thinking.

> **Status:** v1.0.0-alpha — argument graph, social layer, and groups are live;
> threads, voting, and graph visualisation are on the roadmap.

See [FEATURES.md](FEATURES.md) for the full current feature set and
[ROADMAP.md](ROADMAP.md) for what's coming next.

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

## Tests

Pure-function unit tests run with no setup:

```bash
go test ./...
```

Integration tests hit a real Postgres. They auto-skip if
`TEST_DATABASE_URL` is unset, so the line above stays green. To run them:

```bash
./scripts/db-test-setup.sh        # create + migrate the test DB (once)
go test ./...                     # TEST_DATABASE_URL is set by the dev shell
```

Re-run `db-test-setup.sh` after adding a new migration. Tests truncate the
data tables between cases, so you don't need to reset between runs.

## Project layout

```
cmd/server/         entry point
internal/           private application code
  auth/               session management, OAuth/OIDC providers, identity linking
  config/             env-var loading
  db/                 sqlc-generated query code (run `sqlc generate` to refresh)
  migrate/            migration runner (goose, embedded)
  server/             HTTP server, routes, middleware, handlers
  views/              templ components (compiled to Go, run `templ generate`)
migrations/         goose .sql migrations (numbered)
queries/            SQL files consumed by sqlc to generate Go code
scripts/            dev helpers (db-init, migrate-up, …)
static/             CSS and vendored JS (htmx v2.0.4)
uploads/            user-uploaded files (images, videos) — not committed
flake.nix           reproducible dev shell + Nix package (flake users)
default.nix         channels entrypoint (non-flake `nix-build`)
nix/package.nix     buildGoModule recipe, shared by both
nix/module.nix      NixOS module (systemd service + optional Postgres)
```

## Tech stack

| Layer | Tool |
|-------|------|
| Language | Go |
| Router | `chi` |
| Database | Postgres |
| SQL → Go | `sqlc` (write SQL, get type-safe Go) |
| Migrations | `goose` (embedded, auto-applied on startup) |
| Templates | `templ` (type-safe HTML components compiled to Go) |
| Sessions / auth | `scs` + `bcrypt`; OAuth/OIDC via `coreos/go-oidc` |
| Interactivity | HTMX (vendored at `static/htmx.min.js`, v2.0.4) |
| Markdown | `goldmark` (server-side render) + `bluemonday` (sanitise) |
| Graph view | Svelte component (planned) |
| Dev env | Nix flake |

## Domain model

- **Node** — anything in the graph. `type` is one of `topic`, `view`,
  `finding`. Bodies are written in Markdown. Free-form `tags` add cross-cutting
  grouping without bloating the type system.
- **Edge** — directed, typed link between two nodes. `kind` is one of
  `supports`, `opposes`, `related`. Edges can be highlighted to surface key
  reasoning inline.
- **Pin** — a user committing to a View with a three-way stance (Support /
  Oppose / Feature). Displayed on their public profile.
- **Follow / Connection** — users follow each other one-way; mutual follows
  automatically become *connections* (mutuals).
- **Audience list** — a named set of users the author curates for fine-grained
  visibility. The built-in **Trusted** list plus any number of custom lists.
- **Visibility** — every node has a level: `public`, `connections`, list,
  `group`, or `private`. Child nodes inherit the parent's visibility ceiling.
- **Group** — a collaborative space where members share nodes and discussions.
  Distinct from audience lists: a list is a private recipient set; a group is
  a shared canvas every member can see.

See [`migrations/`](migrations/) for the full schema history.

## Configuration

The server is configured entirely through environment variables. Inside
`nix develop`, `DATABASE_URL` is set automatically via the shell hook.

### Core

| Variable | Required | Default | Description |
|---|---|---|---|
| `DATABASE_URL` | **yes** | — | Postgres connection string, e.g. `postgres:///mindful_social?host=/path/to/socket` |
| `LISTEN_ADDR` | no | `127.0.0.1:8080` | TCP address the HTTP server binds to |
| `PUBLIC_BASE_URL` | no* | `http://127.0.0.1:8080` | Absolute origin the browser sees. Required when any OAuth provider is configured, because callback URLs are derived from it. Set to your public domain, e.g. `https://mindful.example.org` |
| `SIGNUP_ENABLED` | no | `true` | Set to `false` to close password sign-up while keeping OAuth/OIDC account creation open |
| `ADMIN_USERS` | no | — | Comma-separated list of email addresses bootstrapped as admins on every startup (idempotent) |

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
