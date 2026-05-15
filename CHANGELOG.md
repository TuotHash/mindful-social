# Changelog

All notable changes to Mindful Social are documented in this file.

The format roughly follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- **Node types simplified** — `reasoning` and `evidence` merged into a
  single `finding` type. The split forced contributors to pick a label
  before the content was written and rarely mattered for downstream
  queries. `source_url` stays optional on findings: set it when the
  content cites an external source, leave it blank when the finding
  stands on its argument. Migration `00016` rewrites the enum, folds
  existing rows into `finding`, and renames `pin_reasonings` →
  `pin_findings` (column `reasoning_id` → `finding_id`).

## [1.0.0-alpha] — 2026-05-11

First public release. The structured-argument layer and the social
foundation are in place. Threads, voting, trust scores, and graph
visualisation are on the roadmap and will land in later alphas.

### Argument graph

- Typed nodes: `topic`, `view`, `reasoning`, `evidence`.
- Typed directed edges: `supports`, `opposes`, `refines`, `cites`, `relates_to`.
- Parent-topic enforcement: every non-topic node attaches to a topic
  at creation, so the graph stays rooted instead of fragmenting.
- Per-node action policies for edit and link, three levels each.
- Edge highlighting surfaces key reasoning inline on the node page.
- Human-readable slug URLs (`/nodes/nuclear-energy`).
- Wiki-open editing model with configurable per-node policies.
- Soft-delete-free model: nodes and edges delete cleanly with explicit
  confirmation, surfacing what cascades before commit.

### Discovery

- Full-text search via Postgres `tsvector` with stemming and excerpts.
- Trigram fuzzy search on titles for typo and partial-match tolerance.
- HTMX live-search in the edge picker.
- Tag system with a public index page and per-tag listings.

### Social

- Follow / unfollow; mutual follows promote to "connections".
- Audience lists — a built-in **Trusted** list plus user-created
  custom lists for finer-grained sharing.
- Per-node visibility levels: `public`, `connections`, list, `private`.
- "Resonate" stance pins (support / oppose / featured) with optional
  reasoning-node attachments. Multiple reasonings per pin supported.
- Personal home feed at `/home` for logged-in users.
- Public user profiles showing authored nodes and active pins.

### Auth and administration

- Password authentication with bcrypt.
- OAuth/OIDC: Google, GitHub, and any configured OIDC provider
  (Authelia, Authentik, Keycloak, Zitadel, …). Multiple providers can
  run simultaneously; signups auto-link to existing accounts by
  verified email.
- Account page: change password, manage linked sign-in methods.
- Configurable signup via `SIGNUP_ENABLED`; OAuth/SSO accounts continue
  to be created even when the password signup form is closed.
- Admin and moderator roles with an admin UI for assignments.
- `ADMIN_USERS` environment variable bootstraps the initial admins
  idempotently on every startup.

### Packaging and deployment

- Self-contained Go binary: static assets and SQL migrations are
  embedded with `go:embed` so the deploy host needs no on-disk
  asset tree and no external `goose` CLI.
- Migrations apply automatically on startup. Idempotent — only pending
  migrations run.
- `Cache-Control: public, max-age=86400` on `/static/*` so reverse
  proxies, browsers, and CDNs can cache assets aggressively.
- `/healthz` reports `ok <version>` and 503s when the database is
  unreachable.
- Nix flake exposes `packages.${system}.default` (and `.mindful-social`)
  for `nix build`.
- NixOS module at `nixosModules.default`. A typical config is just
  `enable = true` plus `publicBaseURL`; local Postgres is provisioned
  by default. Secrets (OAuth client secrets, external `DATABASE_URL`)
  flow through `environmentFile` and never touch the Nix store. The
  systemd unit is sandboxed (`ProtectSystem=strict`, `DynamicUser` in
  external-DB mode, restricted syscall filter, no capabilities).

### Tests

- 58 pure-function unit subtests covering parsing, normalisation, and
  rendering helpers.
- Integration suite (auth, nodes, edges, pins) running against a real
  Postgres via `TEST_DATABASE_URL`; auto-skips when unset so
  `go test ./...` stays green without a database.

### Known limitations

- No threading or discussion on nodes yet.
- No voting system yet.
- Graph visualisation is an initial recent-node slice; personal,
  friends-of, and trending graph modes are still future work.
- No trust score, flagging, or moderator queue yet — admins have full
  manual powers in the meantime.
- No version history or rollback for wiki edits yet.
- Mobile layout is responsive but unpolished.

[1.0.0-alpha]: https://github.com/TuotHash/mindful-social/releases/tag/v1.0.0-alpha
