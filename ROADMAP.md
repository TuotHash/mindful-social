# Feature Roadmap

This document outlines upcoming features beyond the MVP.

## Planned Features

### Social Foundation

- **Followers system** — Users can follow each other. Enables personalized feeds, visibility controls, and community discovery.

- **Visibility controls** — Nodes and threads can be marked as public, followers-only, private (creator only), or other granular permission levels. Visibility set at creation time, changeable later.

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

- **Trust score system** — Each user has a trust score derived from community signals, not just time on the platform. Possible inputs: net votes received on authored nodes/edges, how often your contributions survive without being reverted, explicit endorsements from high-trust users. Score is visible on profiles and gates certain actions (see tiers below). Domain-scoped trust is a stretch goal: high trust in "climate" does not carry over to "economics".

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
