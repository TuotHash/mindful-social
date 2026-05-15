# Features

Current feature set as of v1.0.0-alpha (May 2026).

---

## Argument Graph

### Node Types

- **Topic** — root node; the subject around which views and findings are organised.
- **View** — a stance or position inside a topic's discussion.
- **Finding** — a claim, argument, or cited source that supports, opposes, or is related to a view or another finding.

### Edges

Directed, typed connections between nodes:

| Type | Meaning |
|------|---------|
| `supports` | source argues in favour of target |
| `opposes` | source argues against target |
| `related` | loose link, citation, or clarification — everything that isn't a stance |

Edge highlighting marks key reasoning inline on the node page.

### Node Properties

- Human-readable slug URLs (`/nodes/nuclear-energy`).
- Optional `source_url` on findings for citing external sources.
- Per-node **action policies** — who may edit or link this node: `author`, `connections`, or `public`.
- Free-form **tags** for cross-cutting grouping.
- Optional **body** written in Markdown (rendered server-side, sanitised before display).
- Attached **images** inserted via Markdown syntax; stored separately from profile images.
- Attached **videos** transcoded to H.264/AAC mp4 at up to 1080p.
- **Author byline** — linked avatar + username shown below the node title on the detail page.

### Editing Model

- Wiki-open: any user with the required action-policy level can edit a node.
- Clean deletes — no soft-delete. A confirmation step shows what cascades before committing.
- Parent enforcement: views attach to a topic, findings attach to a view or another finding. Constraints are enforced at the schema level.
- Findings are created inline from the **Connect** form on any view or finding — the source node becomes the parent and the chosen edge kind makes the link.

---

## Discovery

- **Full-text search** — Postgres `tsvector` with stemming; returns results with highlighted excerpts and an author byline beside each hit.
- **Argument graph view** — `/graph` visualises visible topics, views, findings, and typed edges with node search and type filters.
- **Fuzzy title search** — trigram similarity on node titles for typo and partial-match tolerance.
- **Live search** — HTMX-powered picker in edge and topic forms; results update as you type.
- **Tags** — public tag index and per-tag listings.

---

## Social

### Follow & Connections

- One-directional follow with a single button.
- Two users who follow each other automatically become **connections** (mutuals).
- No friend requests; no private accounts.

### Audience Lists

- Built-in **Trusted** list — explicit trust independent of mutual-follow status.
- **Custom lists** — user-created named groups (e.g. "Colleagues", "Research group").
- Lists are a recipient set you control privately; members are not notified.

### Visibility

Every node carries a visibility level set at creation time:

| Level | Who can see it |
|-------|---------------|
| `public` | Anyone, including logged-out visitors |
| `connections` | Mutual followers only |
| `list` | Members of a specific audience list |
| `group` | Members of the owning group |
| `private` | Author only (drafts, personal notes) |

Child nodes inherit the parent's restrictions — a child cannot be more open than its parent.

### Pins (Resonance)

- Users pin themselves to a view with a three-way stance: **Support**, **Oppose**, or **Feature**.
- Topics can only be Featured (support/oppose don't apply to grouping nodes).
- Pins record stance only — reasoning lives in the typed-edge graph: use **Connect** to add a finding off a view.
- Pins are displayed on public user profiles.

### Home Feed

- `/home` shows recent nodes from users you follow plus your own posts, in reverse-chronological order.
- Each row shows an **author byline** (avatar + linked username) alongside the node title.

---

## Groups

- **Groups** are collaborative spaces for friends, study groups, or research teams.
- Group visibility: `public` (anyone can browse and join), `invite` (listed but requires invite), `closed` (hidden from non-members).
- Member roles: `owner`, `admin`, `member`.
- Nodes posted into a group default to group-scope visibility.
- Group invites with an accept workflow.
- Nodes can optionally belong to a group; group becomes a first-class visibility predicate.

---

## Authentication

- **Password sign-up** — bcrypt hashing; sign-up can be disabled via `SIGNUP_ENABLED`.
- **OAuth / OIDC** — Google, GitHub, and any OIDC-compatible provider (Authelia, Authentik, Keycloak, Zitadel, …). Multiple providers run simultaneously.
- Auto-linking of OAuth accounts to existing accounts by verified email.
- **Account page** — change password, manage linked sign-in methods, set profile image.
- **CSRF protection** — double-submit cookie + token on all state-changing requests.

---

## Administration

- Role-based access: `user` (default), `moderator`, `admin`.
- `ADMIN_USERS` environment variable bootstraps initial admins on every startup (idempotent).
- Admin UI at `/admin`: view and edit usernames, emails, passwords, and roles.
- All admin routes return 404 to non-admins.

---

## Deployment

- **Self-contained binary** — static assets and SQL migrations embedded with `go:embed`; no on-disk asset tree needed at runtime.
- **Auto-migrations** — pending migrations apply on startup; safe to restart repeatedly.
- **Static asset caching** — `Cache-Control: public, max-age=86400` on `/static/*`.
- **Health check** — `GET /healthz` returns `ok <version>` or 503 when the database is unreachable.
- **Nix flake** — `nix build` produces the binary; `nixosModules.default` for NixOS deployments.
- **NixOS module** — minimal config (`enable = true` + `publicBaseURL`); local Postgres provisioned by default; secrets via `environmentFile`; systemd unit is sandboxed.

---

## Testing

- 58 unit subtests covering parsing, normalisation, and rendering helpers.
- Integration suite (auth, nodes, edges, pins, groups) against real Postgres via `TEST_DATABASE_URL`; auto-skips when unset.
