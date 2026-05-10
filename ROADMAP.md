# Feature Roadmap

This document outlines upcoming features beyond the MVP.

## Planned Features

### Social Foundation

- **Follow system** — One-directional follow with one button. When two users follow each other, they automatically become **connections** (mutuals). No friend requests, no private accounts.

- **Audience lists** — Beyond connections, users can curate named lists of specific people to enable fine-grained sharing:
  - **Trusted** — a built-in list for people you explicitly trust, independent of mutual-follow status
  - **Custom lists** — user-created named lists (e.g. "Colleagues", "Research group") for any grouping that makes sense to the author

- **Visibility controls** — Every node and thread is assigned a visibility level at creation time, changeable later. Levels in order from most to least open:
  - `public` — visible to anyone, including logged-out users
  - `connections` — visible to mutual followers only; provides real protection since you control who you follow back
  - `[list name]` — visible to members of a specific audience list (Trusted or any custom list)
  - `private` — visible only to the author (drafts, personal notes)

### Personal Feeds & Discovery

- **Personal feed** — Chronological or engagement-sorted feed showing content from followed users, own posts, and trending items. Acts as home page for logged-in users.

- **Trending section** — Dedicated page showing nodes and threads with highest engagement (votes, comments, connections) over recent time periods. Community-driven discovery without algorithmic ranking.

### Graphs & Visualization

- **Central graph engine** — A single interactive graph viewer that renders different slices of the argument graph depending on the active view and filters. All graph modes below are powered by this shared engine.

- **Personal graph view** — Shows a user's own nodes, pins (supports/opposes), and reasoning threads. Starting point for understanding one's own argument landscape.

- **Friends / social bubble view** — Shows the combined graph of people you follow. Highlights where your positions overlap, conflict, or are unconnected with the people in your network.

- **Trending graph view** — Shows the most-engaged nodes and edges over a recent time window (driven by votes and connections), surfacing community-wide debates without algorithmic sorting.

- **Graph filters** — Once a view is selected, users can narrow the graph further by:
  - **Topic** — filter to nodes tagged under a specific subject area
  - **Node type** — show only View nodes, Claim nodes, Reasoning nodes, etc.
  - **Person** — overlay or isolate the nodes of a specific user

- **Graph search** — Full-text search across visible nodes and edges directly within the graph view. Matching nodes are highlighted in place so the user can see them in context rather than jumping to a list.

### Threading & Discussion

- **Threads on View nodes** — Each view node becomes a host for a discussion thread. Users can comment, reply, and build conversations anchored to specific views in the argument graph.

- **Threads on Reasoning nodes** — Similar to views, reasoning nodes can host threaded discussions, allowing users to debate and refine the logic behind specific arguments.

### Voting & Engagement

- **Voting system** — Upvote / downvote mechanism for nodes and edges. Powers the trending section and personalizes graph visualizations with community emphasis.

### Moderation & Trust

- **Version history + rollback** — Every edit to a node body creates a versioned snapshot. Any user can view the edit history and revert to a previous version. Turns the wiki-open model into a self-healing system: edit wars get rolled back by the community rather than escalated to admins.

- **Trust score system** — Each user has a trust score derived from community signals, not just time on the platform. Inputs:
  - **Net votes on authored nodes/edges** — the most direct signal of contribution quality
  - **Revert rate** — fraction of your edits to others' nodes that were rolled back; low revert rate means you edit constructively
  - **Content longevity** — nodes you authored that are still standing (not deleted/hidden) after 30/90 days
  - **Endorsements from high-trust users** — explicit and weighted; a Tier 2 endorsement counts more than a Tier 1 one
  - **What is intentionally excluded:** raw post count (gameable by spamming), follower count (popularity ≠ trustworthiness), account age alone
  - Score is visible on profiles and gates actions via trust tiers (see below).

- **Domain-scoped trust (stretch goal)** — Trust score is scoped per tag: high trust in "climate science" does not carry over to "economics". Lets subject-matter experts moderate their corner of the graph without having power outside it. Maps naturally onto the existing tag system. Deferred until the flat trust score is proven; makes the score harder to explain to users.

- **Trust tiers** — Actions are gated by tier rather than a single moderator flag:
  - **Tier 0 (new):** can post and pin; cannot edit others' content
  - **Tier 1 (member):** unlocked after basic participation; can edit nodes wiki-open, feature/unfeature edges
  - **Tier 2 (trusted):** requires meaningful trust score; can flag content, access the mod queue, hide nodes pending review
  - **Tier 3 (moderator):** manually assigned by admins; full mod powers — permanent deletion, user suspension, override flags

- **Flagging + mod queue** — Any Tier 1+ user can flag a node or edge. After a threshold of flags (or a single Tier 2 flag) the content is soft-hidden: invisible to other users but accessible to the creator and visible in the mod queue. Tier 2/3 users review the queue and restore or permanently remove flagged content.

- **Vouching** — Tier 2+ users can vouch for a newcomer, giving them an accelerated path to Tier 1. Vouches are recorded and factor into the voucher's own trust score to discourage abuse.

## Future Considerations

- AI helpers (link suggestion, compaction, thread→graph promotion)
- Graph editor UI (Svelte + svelte-flow)
- Mobile polish
- Advanced permission models (named groups, invites, org-wide graphs)
