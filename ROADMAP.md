# Feature Roadmap

This document outlines upcoming features beyond the MVP. For the current feature set, see [FEATURES.md](FEATURES.md).

---

## Next Up

### Node Hierarchy Polish

- **Featured topic bundles** *(deferred)* — surface a whole topic together with its key views and findings as a single unit. Individual nodes can still be featured standalone; the bundle is an additive display mode on top.

---

## Planned

### Personal Feeds & Discovery

- **Trending section** — a dedicated page listing nodes and threads with the highest engagement (votes, comments, connections) over recent time windows. Community-driven discovery without algorithmic ranking.

- **Comment notifications** — notify the view's author when someone comments, and notify a commenter when someone replies. Requires a `notifications` table (not yet designed).

### Graphs & Visualisation

- **Expanded graph modes** — the initial graph viewer is live; personal, friends-of, and trending slices should build on the same visual surface.

- **Personal graph view** — a user's own nodes, pins, and finding threads.

- **Friends / social bubble view** — combined graph of people you follow, with positions where you overlap, conflict, or are unconnected highlighted.

- **Trending graph view** — most-engaged nodes and edges over a recent window.

- **3D argument graph** — add an optional `/graph/3d` explorer powered by `3d-force-graph` for spatial graph exploration while keeping the readable 2D graph as the default view.

- **Graph filters** — narrow by topic, node type, or specific person.

- **Graph search** — full-text search inside the graph view; matches highlight in place so the user sees them in context.

### Voting & Engagement

- **Voting** — upvote / downvote for nodes, edges, and comments. Powers the trending feed and personalises graph visualisations with community emphasis. Comment voting ships alongside node voting so the schema stays consistent.

### Moderation & Trust

- **Version history + rollback** — every body edit creates a versioned snapshot. Any user can view the history and revert. Turns wiki-open editing into a self-healing system.

- **Trust score system** — per-user score derived from community signals:
  - **Net votes on authored nodes/edges** — primary signal of contribution quality.
  - **Revert rate** — fraction of edits to others' nodes that were rolled back.
  - **Content longevity** — nodes still standing after 30 / 90 days.
  - **Endorsements from high-trust users** — explicit, weighted.
  - **Intentionally excluded:** raw post count, follower count, account age.
  - Score is visible on profiles and gates actions via trust tiers (below).

- **Domain-scoped trust** *(stretch)* — score scoped per tag: high "climate science" trust does not carry over to "economics". Deferred until the flat score is proven.

- **Trust tiers** — actions gated by tier rather than a single moderator flag:
  - **Tier 0 (new):** can post and pin; cannot edit others' content.
  - **Tier 1 (member):** wiki-open editing, highlight/unhighlight edges.
  - **Tier 2 (trusted):** flag content, access the mod queue, hide nodes pending review.
  - **Tier 3 (moderator):** manually assigned by admins; permanent deletion, user suspension, override flags.

- **Flagging + mod queue** — any Tier 1+ user can flag a node or edge. After a threshold of flags the content is soft-hidden until Tier 2/3 reviews.

- **Vouching** — Tier 2+ users vouch for a newcomer, giving them an accelerated path to Tier 1. Vouches factor into the voucher's own trust score.

- **Comment markdown** — comments ship as plain text in v1 to validate the UX. Revisit markdown support once threading is mature.

---

## Future Considerations

- AI helpers: link suggestion, argument compaction, thread-to-graph promotion.
- Graph editor UI (Svelte + svelte-flow).
- Mobile polish.
- Org-wide graphs / multi-tenant deployments.

---

## Completed

Features that have shipped and are documented in [FEATURES.md](FEATURES.md).

- **Argument graph** — topics, views, findings; typed directed edges; parent enforcement (views need a topic, findings need a view or another finding); inline finding creation from the Connect form; per-node action policies; edge highlighting; slug URLs; wiki-open editing.
- **Content authoring** — Markdown bodies (EasyMDE), image uploads, video uploads with 1080p transcode.
- **Discussion** — view comment threads with one-level replies; author edit and soft-delete; visibility inherits from the parent view.
- **Discovery** — full-text search, trigram fuzzy search, HTMX live search, tag system.
- **Social foundation** — follow/connections, audience lists (Trusted + custom), per-node visibility levels, stance-only pins (Support/Oppose/Feature), home feed, public profiles; author bylines on node detail, home feed, and search results.
- **Groups & communities** — collaborative spaces, group visibility modes, member roles, invites, group-scoped node visibility.
- **Authentication** — password + OAuth/OIDC (Google, GitHub, any OIDC provider), auto-account linking, CSRF protection.
- **Administration & moderation** — role-based access, admin UI, `ADMIN_USERS` bootstrap; staff can delete any node and change visibility on any node.
- **Observability** — structured JSON logs via `slog`; one request line per HTTP request (method, path, status, duration, request id, user id); auth and node-lifecycle audit events; panic recovery routed through slog; config typos surface as warnings on startup.
- **Deployment** — self-contained binary, embedded migrations, Nix flake, NixOS module, health check endpoint.
