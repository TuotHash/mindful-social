# Feature Roadmap

This document outlines upcoming features beyond the MVP. For the current feature set, see [FEATURES.md](FEATURES.md).

---

## Next Up

### Node Hierarchy Polish

- **Fix pin form** — Remove the finding-node picker from the pin/stance form. Pinning is a simple three-way stance: Support, Oppose, or Feature. No nested finding attachment at pin time.

- **Enforced parent connections** — Each non-topic node type must declare a parent at creation time:
  - **View** → must connect to a parent **Topic**.
  - **Finding** → must connect to a parent **View** or another **Finding**.
  - Topics have no required parent; they are root nodes.

- **Featured topic bundles** *(deferred)* — Surface a whole topic together with its key views and findings as a single unit. Individual nodes can still be featured standalone; the bundle is an additive display mode on top.

- **Re-enable finding node creation inside the Connect menu.**

---

## Planned

### Personal Feeds & Discovery

- **Trending section** — A dedicated page showing nodes and threads with the highest engagement (votes, comments, connections) over recent time periods. Community-driven discovery without algorithmic ranking.

### Graphs & Visualisation

- **Central graph engine** — A single interactive graph viewer that renders different slices of the argument graph depending on the active view and filters. All graph modes below are powered by this shared engine.

- **Personal graph view** — Shows a user's own nodes, pins, and finding threads.

- **Friends / social bubble view** — Shows the combined graph of people you follow. Highlights where your positions overlap, conflict, or are unconnected.

- **Trending graph view** — Shows the most-engaged nodes and edges over a recent time window.

- **Graph filters** — Narrow the graph by topic, node type, or specific person.

- **Graph search** — Full-text search across visible nodes and edges directly within the graph view. Matching nodes highlight in place so the user can see them in context.

### Threading & Discussion

#### Mental model

The social layer maps cleanly onto familiar patterns:

| This platform | Reddit analogy |
|---------------|---------------|
| Topic node    | Subreddit / forum board |
| View node     | Post / thread starter |
| Comment       | Comment or reply |
| Finding       | Wikipedia reference (citation, not discussion) |

**Topics** are containers. The topic page lists its view nodes the way a subreddit lists posts — title, author, timestamp, comment count.

**Views** ARE the threads. Clicking a view opens its full page: the view body, its findings listed as references below, and a two-level comment section below that.

**Findings** are structured citations attached to a view — links, images, videos, or other safe file types. They live on the view page as a reference list, not as discussion. Their relationship to the view node (via the existing edge model or a simpler attachment model) is an open design decision.

**Comments** are free-form plain text. They do not create nodes or edges. Posting a comment is low-friction — just writing, like Reddit.

#### Comment threading: two-level

- **Top-level comments** reply directly to the view.
- **Replies** respond to a top-level comment. One level only — you cannot reply to a reply.
- This keeps the thread readable and discourages the fragmented sub-debates that deep nesting produces. The argument graph already handles structured debate; comments are for social reaction and discussion.

#### Schema

```
comments
  id          bigint PK
  node_id     bigint → nodes(id) ON DELETE CASCADE   -- must be a view node
  parent_id   bigint → comments(id) ON DELETE CASCADE -- null = top-level; non-null = reply
  author_id   bigint → users(id)
  body        text NOT NULL
  created_at  timestamptz
  edited_at   timestamptz NULL
  deleted_at  timestamptz NULL   -- soft delete: preserves thread shape, shows "comment removed"
```

Two-level is enforced in the application: a reply's `parent_id` must point to a top-level comment (one whose own `parent_id` is null). The schema does not need to encode this constraint.

#### Visibility

Comments inherit the view node's visibility. If a view is group-only or restricted to a specific audience, its comment thread is too. No separate visibility configuration needed.

#### Authoring rules

- Any user who can see the view can post a comment (consistent with wiki-open editing philosophy for nodes).
- Authors can edit their own comment; `edited_at` is shown to readers.
- Authors can soft-delete their own comment. The slot remains with "comment removed" so replies do not orphan.
- Tier 3 moderators can hard-delete (ties into the trust tier system).
- No markdown in v1 — plain textarea. Can revisit once the UX is validated.

#### Topic page (view listing)

The topic's page shows its connected view nodes as a feed: title, author avatar + username, relative timestamp, comment count, and a short excerpt of the view body. Sorted chronologically by default; by engagement once voting lands.

#### Open design decisions

- **Findings attachment model** — do findings attach to a view via the existing typed-edge graph (a finding node + an edge), or via a lighter `view_findings` join table that skips the graph layer? The edge model is more powerful but adds friction when attaching a quick citation. Decide before implementing.
- **Comment notifications** — notify the view author when someone comments; notify a commenter when someone replies. Requires a notifications table (not yet designed).
- **Voting on comments** — deferred to the Voting & Engagement milestone; design comment voting alongside node voting so the schema is consistent.

### Voting & Engagement

- **Voting system** — Upvote / downvote for nodes and edges. Powers the trending section and personalises graph visualisations with community emphasis.

### Moderation & Trust

- **Version history + rollback** — Every edit to a node body creates a versioned snapshot. Any user can view the edit history and revert to a previous version. Turns wiki-open editing into a self-healing system.

- **Trust score system** — Each user has a trust score derived from community signals:
  - **Net votes on authored nodes/edges** — most direct signal of contribution quality.
  - **Revert rate** — fraction of edits to others' nodes that were rolled back.
  - **Content longevity** — nodes authored that are still standing after 30/90 days.
  - **Endorsements from high-trust users** — explicit and weighted.
  - **Intentionally excluded:** raw post count, follower count, account age alone.
  - Score is visible on profiles and gates actions via trust tiers (see below).

- **Domain-scoped trust** *(stretch goal)* — Trust score scoped per tag: high trust in "climate science" does not carry over to "economics". Deferred until flat trust score is proven.

- **Trust tiers** — Actions gated by tier rather than a single moderator flag:
  - **Tier 0 (new):** can post and pin; cannot edit others' content.
  - **Tier 1 (member):** can edit nodes wiki-open, highlight/unhighlight edges.
  - **Tier 2 (trusted):** can flag content, access the mod queue, hide nodes pending review.
  - **Tier 3 (moderator):** manually assigned by admins; full mod powers — permanent deletion, user suspension, override flags.

- **Flagging + mod queue** — Any Tier 1+ user can flag a node or edge. After a threshold of flags the content is soft-hidden; Tier 2/3 users review and restore or remove.

- **Vouching** — Tier 2+ users can vouch for a newcomer, giving them an accelerated path to Tier 1. Vouches are recorded and factor into the voucher's own trust score.

---

## Future Considerations

- AI helpers: link suggestion, argument compaction, thread-to-graph promotion.
- Graph editor UI (Svelte + svelte-flow).
- Mobile polish.
- Org-wide graphs / multi-tenant deployments.

---

## Completed

Features that have shipped and are documented in [FEATURES.md](FEATURES.md).

- **Argument graph** — topics, views, findings; typed directed edges; parent-topic enforcement; per-node action policies; edge highlighting; slug URLs; wiki-open editing.
- **Content authoring** — Markdown bodies (EasyMDE), image uploads, video uploads with 1080p transcode.
- **Discovery** — full-text search, trigram fuzzy search, HTMX live search, tag system.
- **Social foundation** — follow/connections, audience lists (Trusted + custom), per-node visibility levels, pins with finding attachments, home feed, public profiles; author bylines on node detail, home feed, and search results.
- **Groups & communities** — collaborative spaces, group visibility modes, member roles, invites, group-scoped node visibility.
- **Authentication** — password + OAuth/OIDC (Google, GitHub, any OIDC provider), auto-account linking, CSRF protection.
- **Administration** — role-based access, admin UI, `ADMIN_USERS` bootstrap.
- **Deployment** — self-contained binary, embedded migrations, Nix flake, NixOS module, health check endpoint.
