# Feature Roadmap

This document outlines upcoming features beyond the MVP. For the current feature set, see [FEATURES.md](FEATURES.md).

---

## Next Up

### Node Hierarchy Polish

- [ ] **Featured topic bundles** *(deferred)* — surface a whole topic together with its key views and findings as a single unit. Individual nodes can still be featured standalone; the bundle is an additive display mode on top.

---

## Planned

### Personal Feeds & Discovery

- [ ] **Trending section** — a dedicated page listing nodes and threads with the highest engagement (votes, comments, connections) over recent time windows. Community-driven discovery without algorithmic ranking.

- [ ] **Comment notifications** — notify the view's author when someone comments, and notify a commenter when someone replies. Requires a `notifications` table (not yet designed).

### Graphs & Visualisation

- [ ] **Expanded graph modes** — the initial graph viewer is live; personal, friends-of, and trending slices should build on the same visual surface.

- [ ] **Personal graph view** — a user's own nodes, pins, and finding threads.

- [ ] **Friends / social bubble view** — combined graph of people you follow, with positions where you overlap, conflict, or are unconnected highlighted.

- [ ] **Trending graph view** — most-engaged nodes and edges over a recent window.

- [ ] **2D force-directed layout** — replace the static three-column placeholder in `static/app.js` with a physics simulation: a charge force (every node repels every other node, so things don't pile up), a link force (connected nodes attract along a target distance, so related stuff clusters), and a gentle centering force. Optional per-edge-kind tweaks: `supports` pulls harder than `relates_to`; `opposes` could repel a bit, though that gets visually noisy. Likely uses `d3-force` (~30KB, no rendering dep) vendored alongside `htmx.min.js`; a hand-rolled Verlet sim is the fallback. Same data shape carries over to the 3D entry below, so the tuning learned here informs that work.

- [ ] **3D argument graph** — add an optional `/graph/3d` explorer powered by `3d-force-graph` for spatial graph exploration while keeping the readable 2D graph as the default view.

### Voting & Engagement

- [ ] **Voting** — upvote / downvote for nodes, edges, and comments. Powers the trending feed and personalises graph visualisations with community emphasis. Comment voting ships alongside node voting so the schema stays consistent.

### Moderation & Trust

- [ ] **Trust score system** — per-user score derived from community signals:
  - **Net votes on authored nodes/edges** — primary signal of contribution quality.
  - **Revert rate** — fraction of edits to others' nodes that were rolled back.
  - **Content longevity** — nodes still standing after 30 / 90 days.
  - **Endorsements from high-trust users** — explicit, weighted.
  - **Intentionally excluded:** raw post count, follower count, account age.
  - Score is visible on profiles and gates actions via trust tiers (below).

- [ ] **Domain-scoped trust** *(stretch)* — score scoped per tag: high "climate science" trust does not carry over to "economics". Deferred until the flat score is proven.

- [ ] **Trust tiers** — actions gated by tier rather than a single moderator flag:
  - **Tier 0 (new):** can post and pin; cannot edit others' content.
  - **Tier 1 (member):** wiki-open editing, highlight/unhighlight edges.
  - **Tier 2 (trusted):** flag content, access the mod queue, hide nodes pending review.
  - **Tier 3 (moderator):** manually assigned by admins; permanent deletion, user suspension, override flags.

- [ ] **Flagging + mod queue** — any Tier 1+ user can flag a node or edge. After a threshold of flags the content is soft-hidden until Tier 2/3 reviews.

- [ ] **Vouching** — Tier 2+ users vouch for a newcomer, giving them an accelerated path to Tier 1. Vouches factor into the voucher's own trust score.

- [ ] **Comment markdown** — comments ship as plain text in v1 to validate the UX. Revisit markdown support once threading is mature.

---

## Future Considerations

- [ ] AI helpers: link suggestion, argument compaction, thread-to-graph promotion.
- [ ] Graph editor UI (Svelte + svelte-flow).
- [ ] Mobile polish.
- [ ] Org-wide graphs / multi-tenant deployments.

---

## Completed

Features that have shipped and are documented in [FEATURES.md](FEATURES.md).

- [x] **Argument graph** — topics, views, findings; typed directed edges; parent enforcement (views need a topic, findings need a view or another finding); inline finding creation from the Connect form; per-node action policies; edge highlighting; slug URLs; wiki-open editing.
- [x] **Content authoring** — Markdown bodies (EasyMDE), image uploads, video uploads with 1080p transcode.
- [x] **Discussion** — view comment threads with one-level replies; author edit and soft-delete; visibility inherits from the parent view.
- [x] **Discovery** — full-text search, trigram fuzzy search, HTMX live search, tag system; graph filters (author, group, tags, date window, sourced, visibility, edge kind); in-graph search that expands around each match so hits land in context.
- [x] **Version history + rollback** — every edit snapshots title, body, and tags into `node_revisions`. Any user with edit rights can view past revisions and revert in one click. Reverts are themselves recorded as new revisions, so they're also reversible.
- [x] **Social foundation** — follow/connections, audience lists (Trusted + custom), per-node visibility levels, stance-only pins (Support/Oppose/Feature), home feed, public profiles; author bylines on node detail, home feed, and search results.
- [x] **Groups & communities** — collaborative spaces, group visibility modes, member roles, invites, group-scoped node visibility.
- [x] **Authentication** — password + OAuth/OIDC (Google, GitHub, any OIDC provider), auto-account linking, CSRF protection, Secure cookies when `PUBLIC_BASE_URL` uses HTTPS.
- [x] **Administration & moderation** — role-based access, admin UI, `ADMIN_USERS` bootstrap; staff can delete any node and change visibility on any node.
- [x] **Authorization-aware media serving** — uploads served through a visibility-checked route: profile images public, node attachments gated by the owning node's visibility, drafts gated by being logged in. Every response carries a sandbox CSP and `nosniff` so a polyglot byte stream can't execute script. Directory listings stay disabled.
- [x] **Observability** — structured JSON logs via `slog`; one request line per HTTP request (method, path, status, duration, request id, user id); auth and node-lifecycle audit events; panic recovery routed through slog; config typos surface as warnings on startup.
- [x] **Deployment** — self-contained binary, embedded migrations, Nix flake, NixOS module, health check endpoint.
