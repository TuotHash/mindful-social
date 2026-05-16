# Federation Plan

This document plans how Mindful Social instances will talk to each other
over a small custom HTTP/JSON protocol — the **Mindful Federation Protocol
(MFP)**. Federation is not yet implemented; this file is the design
before the code.

A separate appendix at the end sketches an optional, read-only
[ActivityPub](#glossary) gateway so that Mastodon and other fediverse
apps can follow Mindful Social users and see public nodes as ordinary
posts. The gateway is a translation layer on top of MFP, not a parallel
protocol.

> **Status:** design draft. Nothing here has shipped.

---

## Why federate

A single-server community has one operator, one moderation policy, and
one backup strategy. People who disagree with any of them have nowhere
to go but "leave the platform". Federation lets independent servers
run their own rules while still letting users on one server follow,
comment on, and connect with users on another.

For Mindful Social specifically:

- Every server stays self-hostable — the project's core promise.
- Users keep one account but see content from across the network.
- The **argument graph** — typed nodes and typed edges — can span
  servers. An author on `mindful.example` can write a `finding` that
  *supports* a `view` on `respectful.example`, and both servers render
  that edge.
- The wire format is a faithful image of the local data model, so the
  graph layer survives federation at 100% fidelity.

## Why a custom protocol, not ActivityPub

A side-by-side comparison was done before this rewrite. The short
version of why the design landed on a custom protocol:

- **What ActivityPub gives for free — `Note`, `Like`, `Follow`,
  `Create`, `Update`, `Delete` — is the part Mindful Social cares
  least about.** The standard ActivityPub vocabulary has no concept of
  a typed edge between two typed nodes, of a three-way pin, or of a
  finding that supports a view. Every interesting thing would have to
  ride on a custom JSON-LD extension *anyway*.
- **The implementation cost of ActivityPub is high.** JSON-LD
  contexts, content negotiation, addressing (`to`/`cc`/`bto`/`bcc`/
  `audience`), the magic public collection, draft-cavage HTTP
  signatures (drifting toward RFC 9421), WebFinger, NodeInfo, actor
  vs object types, paged collections — months of work before the first
  real cross-instance edge renders.
- **MFP is shaped like the local model.** Edges, pins, comments,
  follows: one event type each. JSON shapes match the SQL rows. No
  schema translation.
- **Spam and moderation are bounded.** MFP federates only with peers
  that opt in (or, more weakly, with peers that have not been
  defederated). The default is closed; the operator's risk is bounded
  by the peer list.
- **Discoverability through Mastodon is recoverable.** It does not
  need to be in v1, and when it is needed, the optional AP gateway
  appendix below covers it without rewriting MFP.

The trade-off is real: until the gateway lands, a Mastodon user who
pastes a Mindful Social URL into their search box will see nothing.
This document accepts that cost for v1.

---

## Glossary

A short list because several of these words look similar but mean
different things.

- **Instance** — one running Mindful Social server, identified by its
  domain (e.g. `mindful.example`). Synonym: *server*.
- **Federation** — instances exchanging events (a new node, a new
  edge, a follow, a delete, …) over HTTP so a user on one instance
  can interact with content on another.
- **MFP** — *Mindful Federation Protocol*. The custom HTTP/JSON
  protocol defined in this document.
- **Fediverse** — the network of all federated servers that speak
  ActivityPub: Mastodon, Lemmy, PeerTube, and many others. Mindful
  Social joins this network later, through the optional AP gateway.
- **ActivityPub** — a W3C standard for federated social networks.
  Used by the fediverse. Mindful Social does not speak it natively.
- **Actor** — a thing that can publish or receive events: a user, a
  group, or the instance itself. Has a URL, a public key, and an
  inbox URL.
- **Inbox** — an HTTPS endpoint at which a peer instance receives
  POSTed events addressed to one of its actors.
- **Outbox** — an HTTPS endpoint that lists events an actor has
  published, paged and cursorable, so peers can backfill or recover
  from downtime.
- **Subscription** — a record on instance A that "we are interested
  in events from actor X on instance B". The mechanism behind a
  follow, a node watch, and instance-level peering.
- **HTTP Message Signatures (RFC 9421)** — modern HTTP-level
  signature scheme: signs the request method, URL, host, date, and a
  hash of the body. Verified using the sender's published public key.
  Replaces the older draft-cavage scheme used in the fediverse.
- **WebFinger (RFC 7033)** — a discovery protocol, not specific to
  ActivityPub. Given the address `acct:alice@mindful.example`,
  WebFinger returns Alice's actor URL. Mindful Social uses it because
  it is the only well-established way to turn `@user@host` into a
  URL.
- **Mirror / cache** — a local copy of a remote object (a node, a
  comment) stored on this instance so we can render it without
  re-fetching. Updated when the home instance sends an `Update` event.
- **Home instance** of an object — the server where it was first
  created. The home instance is the single source of truth for that
  object's content and lifecycle.
- **Defederation** — refusing to send to or accept from a specific
  remote instance. Per-instance block.

---

## Goals

1. A user on `mindful.example` can follow a user on `respectful.example`
   and see their public nodes, comments, and pins.
2. Two Mindful Social instances exchange the **full** graph layer —
   typed nodes, typed edges, typed pins — at 100% fidelity.
3. Each instance keeps full control over its own moderation,
   defederation list, and storage budget.
4. Identity stays one-server-only. No identity migration in v1 — a
   user account exists on exactly one home instance.
5. The protocol is small enough that a single maintainer can hold the
   whole specification in their head.
6. A future ActivityPub gateway can be added without changing MFP.

## Non-goals (v1)

These are explicit "we are not doing this yet" decisions. Each is
worth revisiting later; adding any of them would balloon the design.

- **Private content does not federate.** Only nodes with
  `visibility = public` are exposed. `connections`, `list`, `group`,
  and `private` stay local.
- **No cross-instance wiki editing.** Only a node's home instance can
  edit it. Remote instances render mirrors as read-only.
- **No cross-instance edge editing.** Once an edge is created, only
  its home instance can delete it.
- **Eventual consistency only.** No transactional cross-server writes.
  An `Update` may arrive seconds or hours late.
- **No federation of trust scores, audience lists, or admin roles.**
  These are local concepts.
- **No identity migration / account move.** A user is tied to one home
  instance.
- **No multi-master editing.** A node always has one canonical owner.
- **No native ActivityPub speak.** Phase 5's optional gateway covers
  read-only AP interop. Inbound AP requests are out of scope until
  then.

---

## Protocol overview

MFP is plain JSON over HTTPS. Every cross-instance interaction is one
of:

- **A push:** instance A `POST`s an event to instance B's inbox.
- **A pull:** instance B `GET`s an outbox, an actor document, or a
  single object from instance A.

There is no streaming, no long-poll, no websocket, no JSON-LD, no
content negotiation. Browsers see HTML at the existing URLs; peers
see JSON at `/mfp/...` URLs. The two surfaces are kept on different
paths to avoid any negotiation confusion.

### Wire format

Every event is a JSON object with a uniform envelope:

```jsonc
{
  "mfp":         "1",                                   // protocol version
  "id":          "https://mindful.example/mfp/events/01HXYZ...",
  "type":        "edge.created",                        // see catalogue below
  "actor":       "https://mindful.example/users/alice",  // who emitted this
  "published":   "2026-05-16T12:00:00Z",
  "object":      { ... },                                // type-specific body
  "audience": {
    "to":        ["https://respectful.example/users/bob"],
    "cc":        ["https://mindful.example/users/alice/followers"]
  }
}
```

Object bodies mirror the local data model directly. An edge body:

```jsonc
{
  "id":          "https://mindful.example/mfp/edges/d1c4...",
  "kind":        "supports",                             // supports | opposes | related
  "from":        "https://mindful.example/nodes/pwr-emissions",
  "to":          "https://respectful.example/nodes/nuclear-expansion",
  "highlighted": false,
  "author":      "https://mindful.example/users/alice",
  "created_at":  "2026-05-16T12:00:00Z"
}
```

A node body:

```jsonc
{
  "id":          "https://mindful.example/nodes/pwr-emissions",
  "type":        "finding",                              // topic | view | finding
  "title":       "PWR reactor lifecycle emissions are low",
  "slug":        "pwr-emissions",
  "body":        "Pressurised water reactors ...",
  "source_url":  "https://example.org/study.pdf",
  "tags":        ["climate", "nuclear-energy"],
  "parent":      "https://mindful.example/nodes/nuclear-expansion",
  "author":      "https://mindful.example/users/alice",
  "created_at":  "2026-05-13T09:11:00Z",
  "updated_at":  "2026-05-16T12:00:00Z",
  "visibility":  "public"
}
```

### Event catalogue

| Type                     | Object                | Notes |
|--------------------------|-----------------------|-------|
| `node.created`           | full node             | Public nodes only. |
| `node.updated`           | full node             | Replaces title/body/tags on mirror. |
| `node.deleted`           | `{id}`                | Cascades to local mirror. |
| `edge.created`           | full edge             | Source-home instance emits. |
| `edge.updated`           | full edge             | Currently only `highlighted` toggle. |
| `edge.deleted`           | `{id}`                | Source-home instance emits. |
| `pin.set`                | full pin              | Pinner's home instance emits. |
| `pin.unset`              | `{id}`                |       |
| `comment.created`        | full comment          | On a public node. |
| `comment.updated`        | full comment          | Edited body. |
| `comment.deleted`        | `{id}`                |       |
| `follow.requested`       | `{actor, target}`     | Sent by follower's home instance. |
| `follow.accepted`        | `{actor, target}`     | Sent by target's home instance. |
| `follow.cancelled`       | `{actor, target}`     |       |
| `user.updated`           | partial user          | Profile / avatar / display-name changes. |
| `group.updated`          | partial group         | Description / visibility changes (Phase 5). |
| `instance.peer_request`  | `{instance}`          | Optional in allowlist mode. |

Unknown event types are ignored on receive. New types are added
without bumping the protocol version, as long as the envelope is
unchanged.

### Versioning

- The envelope's `mfp` field carries the major protocol version
  (`"1"`).
- Unknown fields inside bodies are ignored. New optional fields are
  additive and do not change the major version.
- Backward-incompatible changes bump the major version. An instance
  advertises supported major versions in its NodeInfo document.

---

## Identity and discovery

Every federated thing needs a URL that one server can dereference on
another. Mindful Social's existing slug URLs already work as web URLs;
MFP exposes the same content as JSON under `/mfp/`.

### Users

- HTML profile: `https://mindful.example/users/alice` (as today).
- MFP actor: `https://mindful.example/mfp/users/alice` — JSON,
  including the public key, inbox URL, outbox URL, and follower/
  following collection URLs.
- WebFinger handle: `acct:alice@mindful.example`. Resolved by
  `GET /.well-known/webfinger?resource=acct:alice@mindful.example`,
  which returns the MFP actor URL.
- UI shorthand for remote users: `@alice@mindful.example`. Local
  shorthand `@alice` works only for local users.

### Nodes

- HTML: `https://mindful.example/nodes/pwr-emissions`.
- MFP: `https://mindful.example/mfp/nodes/pwr-emissions`.
- Remote nodes are stored locally with an `origin_uri` pointing at
  the home MFP URL, and shown to local users at a stable mirror path
  `https://mindful.example/n/respectful.example/nuclear-expansion`.
  Mirror URLs never collide with local slugs.

### Edges, pins, comments, events

Each gets a stable URL on its home instance under `/mfp/`:

- `https://mindful.example/mfp/edges/{id}`
- `https://mindful.example/mfp/pins/{id}`
- `https://mindful.example/mfp/nodes/{slug}/comments/{id}`
- `https://mindful.example/mfp/events/{ulid}`

### Instance metadata

- `GET /.well-known/nodeinfo` → pointer to NodeInfo document.
- `GET /nodeinfo/2.1` → software name, version, user count, and an
  MFP-specific section advertising supported MFP major versions, the
  shared inbox URL, and whether peering is open or allowlist-only.

### Username collisions

Local usernames remain unique within an instance. Remote `alice` and
local `alice` are distinct rows with distinct URLs. The unique
constraint on `users.username` becomes:

- `UNIQUE (username) WHERE instance_id IS NULL` for local users.
- `UNIQUE (instance_id, username) WHERE instance_id IS NOT NULL`
  for remote mirrors.

---

## HTTP surface

New endpoints, all under `/mfp/` except for the standard well-known
paths:

| Path | Method | Purpose |
|------|--------|---------|
| `/.well-known/webfinger` | GET | Resolve `acct:` to actor URL |
| `/.well-known/nodeinfo` | GET | Pointer to NodeInfo document |
| `/nodeinfo/2.1` | GET | NodeInfo + MFP metadata |
| `/mfp/users/{username}` | GET | Actor document (public key, inbox, …) |
| `/mfp/users/{username}/inbox` | POST | Per-user inbox |
| `/mfp/users/{username}/outbox` | GET | Paged event stream |
| `/mfp/users/{username}/followers` | GET | Followers collection |
| `/mfp/users/{username}/following` | GET | Following collection |
| `/mfp/inbox` | POST | Shared inbox (preferred for batch delivery) |
| `/mfp/nodes/{slug}` | GET | Node JSON |
| `/mfp/nodes/{slug}/outbox` | GET | Events scoped to this node (edges, comments, pins) |
| `/mfp/edges/{id}` | GET | Edge JSON |
| `/mfp/pins/{id}` | GET | Pin JSON |
| `/mfp/groups/{slug}` | GET | Group JSON (Phase 5) |
| `/mfp/events/{ulid}` | GET | Single event by id |

Existing routes (`/nodes/{slug}`, `/users/{username}`, …) keep
returning HTML for browsers. There is no content negotiation between
HTML and MFP — different paths, different surfaces.

---

## Authentication: HTTP Message Signatures

Every MFP request between instances is signed using **HTTP Message
Signatures (RFC 9421)**. The receiver verifies using the sender's
public key, fetched once from the sender's actor document and cached
locally.

- Every local actor (user, group) gets a freshly generated RSA-2048
  key pair on signup. Existing users get keys on first outbound
  delivery via a backfill job.
- Outbound: every POST and every authenticated GET signs
  `@method`, `@target-uri`, `host`, `date`, and `content-digest`
  (sha-256 of the body).
- Inbound: verify the signature against the cached public key. If
  the key id refers to an unknown actor, fetch the actor document
  first, then verify. Refresh the cached key on signature failure
  (the sender may have rotated).
- The `keyid` parameter in the signature is the actor's URL plus
  `#main-key`, matching the `publicKey.id` field in the actor
  document.
- Private keys live in the database. Storing them encrypted at rest
  is a follow-up; the threat model there is "attacker with read
  access to Postgres", which already implies password-hash
  compromise.

We deliberately do not implement draft-cavage signatures (the older
fediverse standard). MFP starts fresh, and RFC 9421 is the modern,
well-specified successor.

---

## Delivery model

Push primary, pull for backfill.

### Push

When a local actor publishes an event:

1. Compute the set of recipient inboxes:
   - The author's followers' inboxes (deduplicated by shared inbox).
   - Explicit `to` recipients on the event.
   - For events tied to a remote object (edge, pin, comment), also
     the home instance of that object.
2. Enqueue one row in `mfp_deliveries` per unique inbox URL.
3. A background worker drains the queue: signed POST, retry with
   exponential backoff on 5xx / network errors, mark delivered_at
   on 2xx, give up after a hard cap (default 7 days).

Each event has a stable URL (`/mfp/events/{ulid}`). A retry uses the
same URL so receivers can de-duplicate via `processed_events`.

### Pull (backfill)

For new subscribers and post-downtime catch-up, the home instance
exposes paged outboxes:

- `GET /mfp/users/alice/outbox?cursor=<ulid>` — paged event list,
  newest-first by default, oldest-first when paginating with `after`.
- `GET /mfp/nodes/{slug}/outbox?cursor=<ulid>` — events scoped to a
  single node (edges, pins, comments).

ULID-based cursors make ordering total and resumable. Pagination is
plain `?after=<ulid>` / `?before=<ulid>` with a `Link` header for
next/prev — no AP-style `OrderedCollectionPage` ceremony.

### Idempotency

- Every event id is stored on receive in `processed_events`.
- Receiving an event whose id is already in `processed_events` is a
  no-op success.
- Receivers must tolerate out-of-order delivery: an `Update` may
  arrive before the matching `Create` (because retries reorder); the
  receiver fetches the missing object via pull before applying the
  update.

---

## Subscriptions

Three layers, in order of granularity:

1. **User follow** — `follow.requested` from A's home to B's home;
   B's home sends `follow.accepted`; A's home subscribes A to B's
   outbox. Future events from B addressed to followers go to A's
   inbox. This is the everyday case.

2. **Node watch** — when a remote node becomes relevant locally
   (because a local user created an edge to it, pinned it, or
   commented on it), the local instance auto-subscribes to that
   node's outbox. Used to keep the cross-instance legend fresh
   without each user explicitly following every author.

3. **Instance peering** — two instances optionally agree to exchange
   `instance.peer_request` events; this enables shared-inbox bulk
   delivery and (optionally) federated discovery beyond the immediate
   follow graph. Off by default.

Subscriptions are revocable: `follow.cancelled` for users, a quiet
unwatch for nodes once the last local edge / pin / comment is
removed.

---

## What federates, entity by entity

| Entity        | Federates? | MFP shape                          |
|---------------|------------|------------------------------------|
| User          | yes        | Actor (with public key)            |
| Group         | yes (Phase 5) | Actor (with member role enum)   |
| Node: topic   | yes (public only) | `node.created/updated/deleted` with `type: "topic"` |
| Node: view    | yes (public only) | `type: "view"` |
| Node: finding | yes (public only) | `type: "finding"` (`source_url` optional) |
| Edge          | yes        | `edge.created/updated/deleted` carrying `kind` |
| Pin           | yes        | `pin.set` / `pin.unset` carrying `kind` |
| Comment       | yes (public only) | `comment.created/updated/deleted` |
| Follow        | yes        | `follow.requested/accepted/cancelled` |
| Tag           | implicit   | Embedded in node bodies as a string array |
| Audience list | no         | Local concept only                 |
| Trust score   | no         | Local concept only                 |
| Revision      | no         | We send `node.updated` for the latest text |
| Admin role    | no         | Local concept only                 |
| Visibility ≠ public | no   | Filtered out before serialisation  |

---

## Schema sketch

This is a sketch, not the final SQL. Numbers will follow the existing
migration sequence (next slot is `00027`).

```sql
-- One row per remote instance we have seen.
CREATE TABLE instances (
  id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  domain             TEXT NOT NULL UNIQUE,
  software           TEXT,                       -- "mindful-social", "mastodon", …
  software_version   TEXT,
  mfp_major_versions INT[] NOT NULL DEFAULT '{}',
  shared_inbox_url   TEXT,
  blocked            BOOLEAN NOT NULL DEFAULT FALSE,
  blocked_reason     TEXT,
  first_seen_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_fetched_at    TIMESTAMPTZ
);

-- Users gain a home instance and an MFP identity.
ALTER TABLE users
  ADD COLUMN instance_id       UUID REFERENCES instances(id) ON DELETE RESTRICT,
  ADD COLUMN actor_uri         TEXT UNIQUE,
  ADD COLUMN inbox_url         TEXT,
  ADD COLUMN shared_inbox_url  TEXT,
  ADD COLUMN public_key_pem    TEXT,
  ADD COLUMN private_key_pem   TEXT;   -- NULL for remote actors

-- The global UNIQUE on username becomes per-instance.
ALTER TABLE users DROP CONSTRAINT users_username_key;
CREATE UNIQUE INDEX users_local_username_idx
  ON users(username) WHERE instance_id IS NULL;
CREATE UNIQUE INDEX users_remote_username_idx
  ON users(instance_id, username) WHERE instance_id IS NOT NULL;

-- Federated content tracks its home instance and canonical URL.
ALTER TABLE nodes
  ADD COLUMN origin_uri          TEXT UNIQUE,
  ADD COLUMN origin_instance_id  UUID REFERENCES instances(id) ON DELETE RESTRICT,
  ADD COLUMN last_fetched_at     TIMESTAMPTZ;

ALTER TABLE edges
  ADD COLUMN origin_uri          TEXT UNIQUE,
  ADD COLUMN origin_instance_id  UUID REFERENCES instances(id) ON DELETE RESTRICT;

ALTER TABLE comments
  ADD COLUMN origin_uri          TEXT UNIQUE,
  ADD COLUMN origin_instance_id  UUID REFERENCES instances(id) ON DELETE RESTRICT;

ALTER TABLE user_node_pins
  ADD COLUMN origin_uri          TEXT UNIQUE,
  ADD COLUMN origin_instance_id  UUID REFERENCES instances(id) ON DELETE RESTRICT;

ALTER TABLE follows
  ADD COLUMN origin_uri          TEXT UNIQUE,
  ADD COLUMN status              TEXT NOT NULL DEFAULT 'accepted';  -- pending|accepted|rejected

-- Outbound queue: one row per (event, inbox) pair. A worker drains the
-- queue with exponential backoff, marks delivered_at on success, and
-- gives up after a hard cap (default 7 days).
CREATE TABLE mfp_deliveries (
  id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  event_uri        TEXT NOT NULL,
  inbox_url        TEXT NOT NULL,
  payload          JSONB NOT NULL,
  attempt_count    INT NOT NULL DEFAULT 0,
  next_attempt_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  delivered_at     TIMESTAMPTZ,
  last_error       TEXT,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX mfp_deliveries_due_idx
  ON mfp_deliveries(next_attempt_at)
  WHERE delivered_at IS NULL;

-- Idempotency: drop inbound events we have already processed.
CREATE TABLE processed_events (
  event_uri    TEXT PRIMARY KEY,
  received_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Cache of remote actor public keys for signature verification.
CREATE TABLE remote_keys (
  key_id          TEXT PRIMARY KEY,        -- e.g. https://host/mfp/users/alice#main-key
  public_key_pem  TEXT NOT NULL,
  owner_actor_uri TEXT NOT NULL,
  fetched_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Node watches (per Subscriptions §): keep track of remote nodes we
-- have auto-subscribed to because a local user touched them.
CREATE TABLE node_watches (
  node_id      UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  watched_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (node_id)
);
```

### Migration of the visibility predicate

`node_visible_to()` already filters on `created_by` and visibility
columns. After federation, the predicate must also refuse to expose
non-public nodes through any `/mfp/...` endpoint, regardless of
caller. The cleanest place to enforce that is in the MFP handler
(one extra check before serialising), not in SQL.

---

## Background worker

A new `internal/federation/` package owns:

- **Outbound delivery** — drains `mfp_deliveries`, signs each request,
  POSTs to the inbox, retries on 5xx / network errors, marks
  `delivered_at` on 2xx.
- **Inbound dispatch** — receives inbox POSTs, verifies the signature,
  fetches and caches the sender's public key on first contact, records
  the event in `processed_events`, dispatches by `type`.
- **Actor fetching** — given a URL, fetches the actor document and
  stores or refreshes the local mirror row.
- **Object fetching** — given an MFP URL, fetches a node / edge / pin
  / comment on demand (for picker URL paste, for resolving out-of-order
  events, …).
- **NodeInfo fetching** — periodically refreshes
  `instances.software_version` and `instances.mfp_major_versions`.

The worker runs as a goroutine inside the same binary, controlled by
a `FEDERATION_ENABLED` config flag. Federation is off by default in
v1 so existing single-instance deployments are not affected.

---

## Cross-instance argument graph

This is the part that justifies the whole exercise. A worked example:

```
mindful.example:
   finding "PWR reactor lifecycle emissions are low"
     -- supports -->
                                       respectful.example:
                                          view "Nuclear should be expanded"
```

1. Alice (`mindful.example`) authors the finding locally.
2. Alice opens the Connect form on her finding, searches for a view.
   The picker first matches local nodes. If she types a remote URL
   or a `@user@host` ref, the picker fetches that node's MFP JSON and
   offers it as a target candidate.
3. On submit, `mindful.example` creates the edge row with `from_node`
   = the local finding, `to_node` = a mirrored row for the remote
   view (created on the fly if not already mirrored), and
   `kind = supports`.
4. `mindful.example` enqueues `edge.created` to
   `respectful.example`'s shared inbox, addressed to the followers of
   the target view and to its author.
5. `respectful.example` stores the edge as a mirror with `origin_uri`
   pointing at `mindful.example`'s `/mfp/edges/{id}`. The view's
   legend now shows "supported by alice@mindful.example".
6. `respectful.example` adds the target view to its `node_watches`
   on `mindful.example`'s side (so further events on the *finding*
   reach the view's home). Conversely, `mindful.example` watches the
   target view to pick up its later updates.
7. Edge deletion sends `edge.deleted`.

**Edge ownership rule:** the source node's home instance owns the
edge. Only that instance can delete it. This makes the cycle
predictable and avoids two-sided write conflicts.

Pins follow the same pattern. Alice pinning a remote view sends a
`pin.set` event to the view's home server, which records a pin
attributed to her remote actor URL.

---

## Visibility, in detail

The hard rule: anything that is not `visibility = public` is filtered
out of every MFP handler and every outbound event.

- Outbound: `node.created` / `node.updated` / `node.deleted` for a
  node only fires when the node is public. Going from public to
  private sends a `node.deleted` to everyone who saw the public
  version.
- Inbox: if an incoming node payload claims `visibility != public`,
  drop it. We do not store private content that arrived federated.
- Comments on a node inherit the node's visibility ceiling.
  Comments on non-public nodes never federate.
- Audience addressing on outbound events:
  - `to`: the followers collection of the author (for personal posts),
    or the followers of the target (for replies/pins/edges).
  - `cc`: omitted in v1. There is no "public timeline" in MFP — peers
    receive only what they subscribe to.

---

## Wiki-open editing, in detail

Edit rights stay attached to the home instance.

- Local users can edit local nodes (subject to existing action
  policies) and never edit remote nodes.
- When a local user edits a local node, the server sends a
  `node.updated` event. Remote instances that mirror the node replace
  the title / body / tags with the new content, append a single mirror
  revision ("synced from mindful.example, …"), and re-render.
- Local revision history (`node_revisions`) stays local. A remote
  server's mirror table accumulates "snapshot at fetch time" entries
  in its own `node_revisions`; these capture what *we saw arrive*,
  not the authoritative history.
- Reverts: a revert on the home instance is just another edit; it
  sends `node.updated` like any other.

This is much simpler than peer-to-peer wiki sync and matches how
Wikipedia federation has consistently been "do not bother".

---

## Comments

Comments are first-class MFP events (`comment.created/updated/deleted`)
on a public node. Threading is at most one level deep, matching the
local model — there is no need to flatten anything on receive because
all MFP peers agree on the threading depth.

A `comment.created` carries the comment id, the node URL it replies
to, the parent comment URL (or null for top-level), the author URL,
the body, and timestamps.

---

## Pins

`pin.set` / `pin.unset` carry the pinner URL, the target node URL,
and the kind (`support` / `oppose` / `feature`). Topics can only be
featured; the receiver re-applies the same validation as on local
pins and drops invalid combinations with a logged warning.

---

## Groups (Phase 5)

Federate as group actors with their own keys, inbox, and outbox.

- Public groups have a discoverable actor at `/mfp/groups/{slug}`.
- Group nodes do not federate (visibility = group implies local-only).
- Membership stays local in Phase 5. Remote users cannot join a local
  group from their own instance.
- A future v2 step would let groups accept `follow.requested` from
  remote actors to subscribe to public group posts.

---

## Moderation and trust

- **Per-instance defederation.** Admin UI shows the list of known
  instances and a one-click block. Blocked instances are dropped from
  inbox and outbox.
- **Per-user remote block.** A user can block any actor, local or
  remote. Effects: hide their nodes / comments / pins from this
  user's views, refuse to deliver to their inbox.
- **Allowlist mode.** A `FEDERATION_ALLOWED_PEERS` env var, when set,
  restricts MFP to a fixed peer list. Useful for closed networks
  (research group, intra-org).
- **Reports.** Forwarding reports to the offending actor's home
  instance is a stretch goal (event type `flag.created`, not in
  Phase 1–4).
- **Trust score never federates.** Each instance computes its own.
- **No shared blocklist.** Each instance maintains its own.

---

## Phased rollout

Each phase is independently shippable.

### Phase 0 — Identity surface (no events yet)

- New tables: `instances`, `processed_events`, `remote_keys`,
  identity columns on `users` (kept NULL for now).
- WebFinger endpoint.
- NodeInfo endpoint, advertising MFP version `1`.
- Actor JSON at `/mfp/users/{username}`.
- Key pair generation on user signup; backfill for existing users.
- HTTP Message Signature middleware on `/mfp/...` (no events to sign
  yet, but the verification pipeline is wired and tested).

No real federation happens; peers can *discover* but not yet
interact. Useful in isolation because it forces the URL scheme and
key-management code to land cleanly.

### Phase 1 — Public read-only outbound

- JSON node, edge, pin, comment endpoints under `/mfp/...`.
- User outbox endpoint listing the user's public-node events.
- Followers / following collections (empty for now).
- Node outbox endpoint listing events scoped to a public node.

A peer can pull a node and recursively explore its outbox to mirror
the relevant slice of graph, even without a single follow.

### Phase 2 — Follows and the inbox

- Accept inbound `follow.requested` / `follow.cancelled`.
- Send outbound `follow.requested` / `follow.cancelled` when local
  users follow remote actors via `@user@host` syntax.
- Per-user inbox accepts POSTs, verifies signatures, dispatches.
- `follow.accepted` sent automatically.
- Outbound delivery worker live; `mfp_deliveries` queue active.

At this point two Mindful Social instances can follow each other and
exchange follow state. Nothing renders yet beyond the follower list.

### Phase 3 — Nodes, comments, and pins

- `node.created` / `node.updated` / `node.deleted` outbound on
  publish/edit/delete (public nodes only).
- Inbound: mirror remote nodes, render them under `/n/{host}/{slug}`.
- `comment.created` / `comment.updated` / `comment.deleted` outbound
  on local comments addressed to the target node's home.
- Inbound: store remote comments on local nodes; show them in the
  thread alongside local comments.
- `pin.set` / `pin.unset` outbound and inbound; render remote pinners
  on local view pages.

After this phase, two Mindful Social instances behave like a federated
forum — without the graph layer yet.

### Phase 4 — The argument graph

- `edge.created` / `edge.updated` / `edge.deleted` outbound when an
  edge touches a remote node on either end.
- Inbound: mirror remote edges; render them in the legend with a
  "from `host`" badge.
- Picker fetches a node by URL paste or `@user@host` ref to attach
  edges across instances.
- Auto node-watch on first cross-instance touch of a remote node, so
  later edges / pins / comments on that node propagate without an
  explicit follow.
- `/graph` ingests remote mirrors from followed-instance hosts and
  watched nodes.

This is the phase that makes Mindful Social federation feel different
from any AP-based fediverse app. After this lands, two instances
exchange the full graph at 100% fidelity.

### Phase 5 — Groups, admin UX, optional AP gateway

- `Group` actors with their own MFP URL.
- Remote `follow.requested` against a group actor (subscribe to
  public group posts).
- `/admin/federation` page: instance list, defederation, delivery
  queue health, key rotation tool, peer allowlist editor.
- Cap on stored remote mirror bytes per instance; oldest unviewed
  remote nodes evicted on overflow.
- **Optional ActivityPub gateway** (see appendix), enabled by an
  env var. Read-only outbound translation so Mastodon and other
  AP apps can follow Mindful Social users.

---

## Configuration sketch

Adds to the existing env var surface, all optional:

| Variable | Default | Description |
|----------|---------|-------------|
| `FEDERATION_ENABLED` | `false` | Master switch. When false, no MFP endpoints are mounted and no outbound delivery runs. |
| `FEDERATION_USER_AGENT` | `Mindful Social/<version>` | UA header on outbound HTTP. |
| `FEDERATION_FETCH_TIMEOUT_SECONDS` | `15` | Per-request timeout for fetching remote actors / objects. |
| `FEDERATION_DELIVERY_WORKERS` | `4` | Outbound delivery worker concurrency. |
| `FEDERATION_MIRROR_CAP_GB` | `5` | Soft cap on mirrored remote content; oldest evicted first. |
| `FEDERATION_ALLOWED_PEERS` | (empty) | Comma-separated allowlist of instance domains; empty = federate with anyone non-blocked. |
| `FEDERATION_AP_GATEWAY_ENABLED` | `false` | Mount the ActivityPub gateway (appendix). |

`PUBLIC_BASE_URL` (already required for OAuth) defines the URL that
appears in actor URLs. Changing it after federation is live is a
breaking event for every follower; the admin UX should warn loudly.

---

## Risks and open questions

- **Adoption depends on other Mindful Social instances existing.**
  Until a second instance is running, MFP is theoretical. Mitigation:
  ship a turnkey NixOS module and a one-command Docker recipe so
  spinning up a peer is half an hour, not half a day.
- **Storage cost.** Mirroring remote nodes is unbounded by default.
  The cap config covers the common case; spam-driven churn would
  still be a problem. Eviction policy needs tuning.
- **Spam.** With allowlist mode off, anyone can POST events to the
  shared inbox. Mitigations: per-instance rate buckets, automatic
  defederation on flood, optional invite-only federation via
  `FEDERATION_ALLOWED_PEERS`.
- **Edge-edit conflicts across instances.** The "edge home = source
  home" rule avoids this for create/delete, but if we ever federate
  highlight toggles, two instances could disagree. Keep highlight
  on the source-home side only.
- **Tag-name fragmentation.** The same human concept may carry
  different tag spellings on different instances. v1: keep tags local;
  remote nodes are tagged with whatever their home instance sent,
  and our search treats them as opaque strings.
- **Identity migration.** Out of scope. A user changing instances
  re-creates their account.
- **Schema versioning of node / edge / pin kinds.** Adding a fourth
  node type or a fourth edge kind means every other instance needs
  to understand the new value. Receivers must tolerate unknown enum
  values (render as "unknown kind", do not drop the row).
- **Signature spec drift.** RFC 9421 is the modern standard, but
  some early implementations may not match the final RFC's nuances.
  Plan: be liberal in what we accept, strict in what we send.
- **License compatibility.** AGPL-3.0 is fine for MFP; the protocol
  is open and unencumbered.
- **What about non-public groups?** Federation v1 leaves them local.
  A future "private group across two instances" feature is large —
  needs end-to-end addressing, group key distribution, and a story
  for an admin on the remote instance who can see the encrypted
  payloads. Defer until there is real demand.

---

## What this plan does *not* lock in

- The specific shape of every event body. The phase-by-phase work
  will refine those as it goes, within the constraint that
  additions are backward-compatible.
- The exact admin UX. The `/admin/federation` page lands in Phase 5
  and can iterate.
- Whether to ship a separate relay binary for servers that want to
  peer with many small instances. Decide after Phase 4.
- Whether the AP gateway grows beyond read-only outbound. Decide
  after measuring demand.

The fixed parts of this plan are:

- **Protocol:** custom HTTP/JSON (MFP), versioned.
- **Authentication:** HTTP Message Signatures (RFC 9421).
- **Visibility:** public-only federates in v1.
- **Authority:** one home instance per object; no multi-master.
- **Wiki:** local edit only.
- **Phasing:** identity → public-read → follows → nodes/comments/pins
  → graph extensions → groups & optional AP gateway.

---

## Appendix: optional ActivityPub gateway (Phase 5)

A small read-only translation layer that lets Mastodon and other
ActivityPub apps see Mindful Social public content. Enabled by
`FEDERATION_AP_GATEWAY_ENABLED=true`.

### Scope

- **Outbound:** publish each local user as an AP `Person` actor at
  `/ap/users/{username}`. Translate each public node into an AP
  `Note` exposed at `/ap/nodes/{slug}`, with title in `name`, body in
  `content`, author in `attributedTo`, tags in `as:tag`. Translate
  the user outbox into an AP `OrderedCollection`.
- **Inbound:** accept exactly one activity type: `Create Note` with
  `inReplyTo` pointing at a local public node. The gateway records
  the note as a comment authored by a "remote AP actor" mirror user.
  Inbound `Follow` is also accepted; outbound `Accept` is sent.
- **Not supported:** `Like` (no clean Mindful Social mapping), `Announce`
  (no local concept), `Update` / `Delete` on inbound (the AP side is
  not a write surface for content). Edges and pins do not exist in
  AP and are not translated.

### Why a gateway, not native AP

The gateway is bounded: a small fixed translation surface, not a
full AP implementation. The MFP code stays unaware of AP. If AP
adoption stalls or the spec drifts, the gateway can be disabled or
rewritten without touching MFP.

### Identity mapping

- A Mindful Social actor's AP id is `https://host/ap/users/{name}` —
  a separate URL from the MFP actor.
- The actor document advertises a separate AP-specific public key
  (we keep MFP and AP keys distinct so rotating one does not
  invalidate the other).
- WebFinger returns *both* actor URLs (MFP and AP) under different
  rel values. Mastodon picks the AP one by content type.

### Signatures

The gateway uses draft-cavage HTTP signatures on the AP side, to match
the current fediverse de-facto standard. The MFP side continues using
RFC 9421. Two libraries, two key types, kept separate.

### Risks specific to the gateway

- Mastodon users may be confused by the partial fidelity (no edges,
  no typed pins). Mitigation: the AP `Note` includes a footer link
  back to the rich Mindful Social URL — "see argument graph context".
- Defederation lists shared in the Mastodon community may include the
  gateway domain. Mitigation: clearly identify the gateway in
  NodeInfo so admins can choose to peer or not.
- AP brings spam volume. Mitigation: the gateway is opt-in per
  instance, and it inherits all MFP rate limits and defederation
  rules.

The gateway lands only if and when there is real demand. Until
then, MFP carries the whole federation story.
