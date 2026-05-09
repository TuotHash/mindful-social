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

- **Personal argument graphs** — Visualization of a user's nodes, pins (supports/opposes), and reasoning threads. Shows how their own positions connect to the broader graph.

### Threading & Discussion

- **Threads on View nodes** — Each view node becomes a host for a discussion thread. Users can comment, reply, and build conversations anchored to specific views in the argument graph.

- **Threads on Reasoning nodes** — Similar to views, reasoning nodes can host threaded discussions, allowing users to debate and refine the logic behind specific arguments.

### Voting & Engagement

- **Voting system** — Upvote / downvote mechanism for nodes and edges. Powers the trending section and personalizes graph visualizations with community emphasis.

## Future Considerations

- AI helpers (link suggestion, compaction, thread→graph promotion)
- Graph editor UI (Svelte + svelte-flow)
- Mobile polish
- Advanced permission models (named groups, invites, org-wide graphs)
