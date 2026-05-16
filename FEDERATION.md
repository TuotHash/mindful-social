# Federation Plan

This document plans how Mindful Social instances will talk to each other,
and to the wider [fediverse](#glossary), over [ActivityPub](#glossary).
Federation is not yet implemented; this file is the design before the code.

> **Status:** design draft. Nothing here has shipped.

---

## Why federate

A single-server community has one operator, one moderation policy, and one
backup strategy. People who disagree with any of them have nowhere to go but
"leave the platform". Federation lets independent servers run their own rules
while still letting users on one server follow, comment on, and connect with
users on another. This is the model behind Mastodon (microblogging), Lemmy
(forums), PeerTube (video), Pixelfed (photos), and several others.

For Mindful Social specifically:

- Every server stays self-hostable — the project's core promise.
- Users keep one account but see content from across the network.
- The **argument graph** — typed nodes and typed edges — can span servers.
  An author on `mindful.example` can write a `finding` that *supports* a
  `view` on `respectful.example`, and both servers render that edge.
- A standard fediverse account (Mastodon, etc.) can follow a Mindful Social
  user and see public nodes as ordinary posts, and reply to them as
  ordinary comments. No graph features, but no isolation either.

---

## Glossary

A short list because several of these words look similar but mean different
things.

- **Instance** — one running Mindful Social server, identified by its domain
  (e.g. `mindful.example`). Synonym: *server*.
- **Federation** — instances exchanging activities (follow, post, reply,
  delete, …) over HTTP so a user on one instance can interact with content
  on another.
- **Fediverse** — the network of all federated servers that speak
  ActivityPub: Mastodon, Lemmy, PeerTube, and many others.
- **ActivityPub** — a W3C standard for federated social networks. Defines
  a JSON document format ([JSON-LD](#glossary)) for *activities* (Create,
  Follow, Like, Delete, …) and *actors* (Person, Group, …), and an
  HTTP delivery model (inbox / outbox).
- **JSON-LD** — JSON extended with a `@context` field that names a vocabulary,
  so two systems can agree on what each property means. ActivityPub uses
  the `https://www.w3.org/ns/activitystreams` vocabulary by default.
- **Actor** — anything that can send or receive activities: a user (`Person`),
  a group (`Group`), an application (`Application`). Has an `id` URI,
  an `inbox`, and an `outbox`.
- **Inbox** — an HTTPS endpoint that accepts `POST`ed activities addressed
  to an actor. The server processes them (saving a follow, recording a
  reply, …).
- **Outbox** — an HTTPS endpoint that lists activities the actor has
  published, like a public timeline.
- **WebFinger** — a discovery protocol. Given the address
  `acct:alice@mindful.example`, WebFinger returns Alice's actor URI so a
  remote server can fetch her profile.
- **HTTP signatures** — every federation request is signed with the
  sender's private key so the receiver can verify "this really came from
  Alice at mindful.example". The draft-cavage spec is the de-facto
  standard the fediverse uses.
- **Follow** — an actor subscribing to another actor's outbox. The
  recipient's server replies with `Accept` (or `Reject`); afterwards new
  activities are pushed to the follower's inbox.
- **Mirror / cache** — a local copy of a remote object (a node, a
  comment) stored on this instance so we can render it without re-fetching.
  Updated when the home instance sends an `Update` activity.
- **Home instance** of an object — the server where it was first
  created. The home instance is the single source of truth for that
  object's content and lifecycle.
- **Defederation** — refusing to send to or accept from a specific
  remote instance. Per-instance block.

---

## Goals

1. A user on `mindful.example` can follow a user on `respectful.example`
   and see their public nodes, comments, and pins.
2. A Mastodon (or other ActivityPub) user can follow a Mindful Social
   user and see public view/finding nodes as regular posts, and can
   reply to them.
3. Two Mindful Social instances can share the **full** graph layer
   — typed edges and pins — so the unique value of the project survives
   federation.
4. Each instance keeps full control over its own moderation, defederation
   list, and storage budget.
5. Identity stays one-server-only. No identity migration in v1 — a user
   account exists on exactly one home instance.

## Non-goals (v1)

These are explicit "we are not doing this yet" decisions. Each is worth
revisiting later but adding any of them would balloon the design.

- **Private content does not federate.** Only nodes with `visibility =
  public` are exposed at all. `connections`, `list`, `group`, and
  `private` stay local. Mapping those to ActivityPub addressing is
  possible but error-prone, and a leak would be hard to undo.
- **No cross-instance wiki editing.** Only a node's home instance can
  edit it. Remote instances render mirrors as read-only.
- **No cross-instance edge editing.** Once an edge is created, only its
  home instance can delete it.
- **Eventual consistency only.** No transactional cross-server writes.
  An `Update` may arrive seconds or hours late.
- **No federation of trust scores, audience lists, or admin roles.**
  These are local concepts.
- **No identity migration / account move.** A user is tied to one home
  instance.
- **No multi-master editing.** A node always has one canonical owner.

---

## Protocol choice

**ActivityPub with Mindful Social extensions.**

The W3C standard gives us interoperability with the existing fediverse
for the parts that map cleanly (profiles, posts, follows, comments).
Anything specific to the argument-graph layer — typed edges, typed
pins, node types beyond "post" — is a custom JSON-LD extension that
other Mindful Social instances understand and other software ignores.

This is the same pattern Lemmy uses: standard `Note` semantics where
they apply, custom `Group`/`Page` extensions where they do not.

### Why not a custom protocol?

A custom protocol is shorter to design but isolates us from every
existing fediverse user. The whole reason to federate is to lower the
cost of joining; cutting off Mastodon would defeat that.

### Why not "ActivityPub only", no extensions?

Edges and pins are the project's reason to exist. Squeezing them into
`Like` and `inReplyTo` would lose all type information. Better to add
custom types that other Mindful Social instances render in full and
other software safely ignores.

---

## What federates, entity by entity

| Entity        | Federates? | ActivityPub mapping                                          |
|---------------|------------|--------------------------------------------------------------|
| User          | yes        | `Person` actor                                               |
| Group         | yes        | `Group` actor (membership stays local in v1)                 |
| Node: topic   | yes (public only) | Custom `mindful:Topic`, also exposes `as:Note` summary |
| Node: view    | yes (public only) | Custom `mindful:View`, also exposes `as:Note` summary  |
| Node: finding | yes (public only) | Custom `mindful:Finding`, also exposes `as:Note`       |
| Edge          | yes        | Custom `mindful:Connect` activity (with `connectKind`)       |
| Pin           | yes        | Custom `mindful:Pin` activity (with `pinKind`)               |
| Comment       | yes (public only) | Standard `Note` with `inReplyTo`                       |
| Follow        | yes        | Standard `Follow` / `Accept` / `Undo`                        |
| Tag           | implicit   | Travels as `as:Hashtag` on nodes                             |
| Audience list | no         | Local concept only                                           |
| Trust score   | no         | Local concept only                                           |
| Revision      | no         | Local concept only; we send `Update` for the latest text     |
| Admin role    | no         | Local concept only                                           |
| Visibility ≠ public | no   | Filtered out before serialisation                            |

### Custom JSON-LD context

We publish a vocabulary at `https://mindful.example/ns#` defining:

```jsonc
{
  "@context": [
    "https://www.w3.org/ns/activitystreams",
    {
      "mindful":      "https://schema.mindful-social.org/ns#",
      "Topic":        "mindful:Topic",
      "View":         "mindful:View",
      "Finding":      "mindful:Finding",
      "Connect":      "mindful:Connect",
      "Disconnect":   "mindful:Disconnect",
      "Pin":          "mindful:Pin",
      "Unpin":        "mindful:Unpin",
      "connectKind":  "mindful:connectKind",  // "supports" | "opposes" | "related"
      "connectFrom":  "mindful:connectFrom",
      "connectTo":    "mindful:connectTo",
      "pinKind":      "mindful:pinKind",      // "support" | "oppose" | "feature"
      "pinTarget":    "mindful:pinTarget",
      "highlighted":  "mindful:highlighted",  // boolean on edges
      "parentNode":   "mindful:parentNode"
    }
  ]
}
```

A second Mindful Social instance reads these terms in full. A Mastodon
server sees only the `as:` properties (title as `name`, body as
`content`, author as `attributedTo`) and renders the node as a post.

---

## Identity and addressing

Every federated thing needs a URI that one server can dereference on
another. Mindful Social's existing slug URLs already work as web URLs;
we add an ActivityPub-flavoured representation at the same path via
content negotiation.

### Users

- Local: `https://mindful.example/users/alice` is the actor URI.
- WebFinger handle: `acct:alice@mindful.example`.
- Browsers visiting the URL get the HTML profile page (as today).
- Servers sending `Accept: application/activity+json` get JSON-LD.

Remote users are shown in the UI as `@alice@mindful.example`. The local
`@alice` shorthand still works for local users only.

### Nodes

- Local: `https://mindful.example/nodes/nuclear-energy` for HTML;
  same URL with the JSON-LD accept header for the actor-style document.
- Remote: stored with `origin_uri = https://respectful.example/nodes/…`.
  Rendered locally at `https://mindful.example/n/respectful.example/nuclear-energy`
  (a stable mirror path) so URLs do not leak between instances.

### Edges, pins, comments

Each gets a stable URI on its home instance:

- `https://mindful.example/ap/edges/{id}`
- `https://mindful.example/ap/pins/{id}`
- `https://mindful.example/nodes/{slug}/comments/{id}`

### Username collisions

Local usernames remain unique within an instance. Remote `alice` and local
`alice` are distinct rows with distinct URIs. The unique constraint on
`users.username` becomes `UNIQUE (instance_id, username)` once federation
lands; `instance_id IS NULL` means local.

---

## HTTP surface

New endpoints:

| Path | Method | Purpose |
|------|--------|---------|
| `/.well-known/webfinger` | GET | Resolve `acct:` to actor URI |
| `/.well-known/nodeinfo` | GET | Pointer to NodeInfo document |
| `/nodeinfo/2.1` | GET | NodeInfo — software, version, user count |
| `/ap/users/{username}` | GET | Person actor JSON-LD |
| `/ap/users/{username}/inbox` | POST | Per-user inbox |
| `/ap/users/{username}/outbox` | GET | Paged activity collection |
| `/ap/users/{username}/followers` | GET | Followers collection |
| `/ap/users/{username}/following` | GET | Following collection |
| `/ap/inbox` | POST | Shared inbox (preferred for batch delivery) |
| `/ap/nodes/{slug}` | GET | Node JSON-LD (also via `/nodes/{slug}` with AP `Accept`) |
| `/ap/edges/{id}` | GET | Edge JSON-LD |
| `/ap/pins/{id}` | GET | Pin JSON-LD |
| `/ap/groups/{slug}` | GET | Group actor JSON-LD |

Existing routes (`/nodes/{slug}`, `/users/{username}`, …) keep returning
HTML for browsers. Content negotiation switches to JSON-LD when the
`Accept` header is `application/activity+json` or
`application/ld+json; profile="https://www.w3.org/ns/activitystreams"`.

---

## Cryptography

- Every local actor (user, group) gets a freshly generated RSA-2048
  key pair the first time federation is enabled (or on signup once
  federation is live). Existing users get keys on first outbound
  delivery via a backfill job.
- Outbound requests carry an HTTP signature (`(request-target) host
  date digest` headers, RSA-SHA256). draft-cavage-http-signatures-12.
- Inbound requests are verified: fetch the sender's public key from
  their actor document (cached locally), verify the signature, reject
  on mismatch.
- Private keys live in the database in `users.private_key_pem`.
  Storing them encrypted at rest is a follow-up; the threat model
  there is "attacker with read access to Postgres", which already
  implies password hash compromise.

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
  shared_inbox_url   TEXT,
  blocked            BOOLEAN NOT NULL DEFAULT FALSE,
  blocked_reason     TEXT,
  first_seen_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_fetched_at    TIMESTAMPTZ
);

-- Users gain a home instance and ActivityPub identity.
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

-- Federated content tracks its home instance and canonical URI.
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

-- Outbound queue: every activity to deliver is a row. A worker drains
-- the queue with exponential backoff, marks delivered_at on success,
-- and gives up after a hard cap (e.g. 7 days).
CREATE TABLE activity_deliveries (
  id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  activity_uri     TEXT NOT NULL,
  inbox_url        TEXT NOT NULL,
  payload          JSONB NOT NULL,
  attempt_count    INT NOT NULL DEFAULT 0,
  next_attempt_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  delivered_at     TIMESTAMPTZ,
  last_error       TEXT,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX activity_deliveries_due_idx
  ON activity_deliveries(next_attempt_at)
  WHERE delivered_at IS NULL;

-- Idempotency: drop inbound activities we have already processed.
CREATE TABLE processed_activities (
  activity_uri  TEXT PRIMARY KEY,
  received_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### Migration of the visibility predicate

`node_visible_to()` already filters on `created_by` and visibility
columns. After federation, the predicate must also refuse to expose
non-public nodes to anyone fetching with the ActivityPub accept header,
regardless of whether they are logged in. The cleanest place to enforce
that is in the AP handler (one extra check before serialising), not in
SQL.

---

## Background worker

A new `internal/federation/` package owns:

- **Outbound delivery** — drains `activity_deliveries`, sends each
  payload with an HTTP signature, retries on 5xx / network errors,
  marks `delivered_at` on 2xx.
- **Inbound dispatch** — receives inbox POSTs, verifies signature,
  fetches and caches the sender's public key on first contact,
  records the activity in `processed_activities` (idempotency), and
  dispatches by `type`.
- **Actor fetching** — given a URI, fetches the JSON-LD actor
  document, stores or refreshes the local mirror row.
- **NodeInfo fetching** — periodically refreshes
  `instances.software_version`.

The worker runs as a goroutine inside the same binary, controlled by
a `FEDERATION_ENABLED` config flag. Federation off by default in v1
so existing single-instance deployments are not affected.

---

## Cross-instance argument graph

This is the part that makes Mindful Social federation different from
Mastodon/Lemmy federation. A worked example:

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
   or a known `@user@host` ref, the picker fetches that node's
   JSON-LD and offers it as a target candidate.
3. On submit, `mindful.example` creates the edge row with `from_node`
   = the local finding, `to_node` = a mirrored row for the remote view
   (created on the fly if not already mirrored), and `kind = supports`.
4. `mindful.example` sends a `Connect` activity to `respectful.example`'s
   inbox, addressed to the followers of the target view and to its
   author.
5. `respectful.example` stores the edge as a mirror with `origin_uri`
   pointing at `mindful.example`'s `/ap/edges/{id}`. The view's
   legend now shows "supported by alice@mindful.example".
6. Edge deletion sends a `Disconnect` activity.

**Edge ownership rule:** the source node's home instance owns the
edge. Only that instance can delete it. This makes the cycle
predictable and avoids two-sided write conflicts.

Pins follow the same pattern. Alice pinning a remote view sends a
`Pin` activity to the view's home server, which records a pin
attributed to her remote actor URI.

---

## Visibility, in detail

The hard rule: anything that is not `visibility = public` is filtered
out of every federation handler.

- Outbound: `Create`/`Update`/`Delete` for a node only fires when
  the node is public. Going from public to private sends a `Delete`
  to everyone who saw the public version.
- Inbox: if an incoming node payload claims a non-public address, drop
  it. We do not store private content that arrived federated.
- Federated comments live on a node and inherit its visibility ceiling.
  Comments on non-public nodes never federate.
- Addressing on outbound activities:
  - `to`: the followers collection of the author for personal posts,
    or the followers of the target for replies/pins/edges.
  - `cc`: `https://www.w3.org/ns/activitystreams#Public` for any node
    that should appear in remote public-timeline browsers.
- We never use `Public` in `to`. That's a deliberate choice to keep
  Mindful Social content out of generic public firehoses unless a
  remote server explicitly subscribes via a follow.

---

## Wiki-open editing, in detail

Edit rights stay attached to the home instance.

- Local users can edit local nodes (subject to existing action
  policies) and never edit remote nodes.
- When a local user edits a local node, the server sends an `Update`
  activity. Remote instances that mirror the node replace the title /
  body / tags with the new content, append a single mirror revision
  ("synced from mindful.example, …"), and re-render.
- Local revision history (`node_revisions`) stays local. A remote
  server's mirror table accumulates "snapshot at fetch time" entries
  in its own `node_revisions`; these capture what *we saw arrive*, not
  the authoritative history.
- Reverts: a revert on the home instance is just another edit; it
  sends `Update` like any other.

This is much simpler than peer-to-peer wiki sync and matches how
Wikipedia federation has consistently been "do not bother".

---

## Comments

Comments map onto the standard ActivityPub `Note` with `inReplyTo`,
which is the same shape Mastodon uses. A Mastodon user replying to a
Mindful Social view sends us a `Note`; we record it as a comment on
the view. Threading is at most one level deep on the Mindful Social
side, so we collapse deep Mastodon reply trees into a flat reply list
(one level under the original).

Editing and deleting comments use `Update` and `Delete` activities, as
in Mastodon.

---

## Groups

Federate as `Group` actors. In v1:

- Public groups have a discoverable actor at `/ap/groups/{slug}`.
- Group nodes do not federate (visibility = group implies local-only).
- Membership stays local. Remote users cannot join a local group from
  their own instance in v1.
- A future v2 step would let groups accept `Follow` from remote actors
  to subscribe to public group posts, similar to how Lemmy communities
  work.

---

## Moderation and trust

- **Per-instance defederation.** Admin UI shows the list of known
  instances and a one-click block. Blocked instances are dropped from
  inbox and outbox.
- **Per-user remote block.** A user can block any actor, local or
  remote. Effects: hide their nodes / comments / pins from this
  user's views, refuse to deliver to their inbox.
- **Reports.** Forwarding reports to the offending actor's home
  instance is a stretch goal (Mastodon supports it via `Flag`).
- **Trust score never federates.** Each instance computes its own.
- **No shared blocklist.** Each instance maintains its own. Importing
  from community lists (FediBlock, etc.) is a manual admin task.

---

## Phased rollout

Each phase is independently shippable.

### Phase 0 — Identity surface (no activities yet)

- New tables: `instances`, key columns on `users` (kept NULL for now).
- WebFinger endpoint.
- NodeInfo endpoint.
- Actor JSON-LD endpoint for local users.
- Key pair generation on user signup; backfill for existing users.

No real federation happens; remote servers can *discover* but not yet
interact. Useful in isolation because it forces the URI scheme and
content negotiation to land cleanly.

### Phase 1 — Public read-only outbound

- JSON-LD representation of public nodes via content negotiation.
- User outbox endpoint listing public node creations.
- Followers / following collections (empty for now).
- HTTP signatures on outbound requests (no outbound to send yet, but
  the signing pipeline is wired and tested).

A Mastodon admin can paste a Mindful Social user URL into their search
box and see the profile + recent posts. They cannot yet follow.

### Phase 2 — Follows and the standard inbox

- Accept inbound `Follow` / `Undo Follow`.
- Send outbound `Follow` / `Undo` when local users follow remote
  actors via `@user@host` syntax.
- Per-user inbox accepts POSTs, verifies signatures, dispatches.
- `Accept` replies sent automatically.
- Outbound delivery worker live; `activity_deliveries` queue active.

At this point we have basic Mastodon-style follow interop.

### Phase 3 — Standard ActivityPub interop for posts and comments

- `Create Note` outbound when a public node is created, addressed to
  followers.
- `Update Note` / `Delete Note` on edit / delete.
- Inbound `Create Note` with `inReplyTo` pointing at a local node
  → recorded as a comment.
- Inbound `Like` → recorded as a pin with `kind = feature` (the
  closest one-to-one map).
- Inbound `Announce` (boost) → no local concept; ignored.

Now a Mastodon user can follow, see public posts, comment, and "like"
a view. Mindful Social users can follow Mastodon accounts and see
their posts in `/home`.

### Phase 4 — The argument graph

- Custom `Topic` / `View` / `Finding` types published alongside the
  `Note` summary, with the `mindful:` context.
- `Connect` / `Disconnect` custom activities for edges, with
  `connectKind`.
- `Pin` / `Unpin` custom activities with `pinKind`.
- Remote nodes appear in the local picker via URL paste / fetch.
- Legend on a view shows remote findings linking in.
- Graph view (`/graph`) ingests remote mirrors of public nodes from
  followed-instance hosts.

This is the phase that makes Mindful Social federation feel different
from Mastodon. After this lands, two instances exchange the full graph.

### Phase 5 — Groups, admin UX, polish

- `Group` actor endpoints.
- Remote `Follow` of a group actor (subscribe to public group posts).
- `/admin/federation` page: instance list, defederation, delivery
  queue health, key rotation tool.
- Cap on stored remote mirror bytes per instance; oldest unviewed
  remote nodes evicted on overflow.
- Optional: per-instance defederation reasons published in NodeInfo
  for transparency.

---

## Configuration sketch

Adds to the existing env var surface, all optional:

| Variable | Default | Description |
|----------|---------|-------------|
| `FEDERATION_ENABLED` | `false` | Master switch. When false, no AP endpoints are mounted and no outbound delivery runs. |
| `FEDERATION_USER_AGENT` | `Mindful Social/<version>` | UA header on outbound HTTP. |
| `FEDERATION_FETCH_TIMEOUT_SECONDS` | `15` | Per-request timeout for fetching remote actors / objects. |
| `FEDERATION_DELIVERY_WORKERS` | `4` | Outbound delivery worker concurrency. |
| `FEDERATION_MIRROR_CAP_GB` | `5` | Soft cap on mirrored remote content; oldest evicted first. |
| `FEDERATION_ALLOWED_PEERS` | (empty) | Comma-separated allowlist of instance domains; empty = federate with anyone non-blocked. |

`PUBLIC_BASE_URL` (already required for OAuth) defines the URL that
appears in actor URIs. Changing it after federation is live is a
breaking event for every follower; the admin UX should warn loudly.

---

## Risks and open questions

- **Storage cost.** Mirroring remote nodes is unbounded by default.
  The cap config covers the common case; spam-driven churn would
  still be a problem. Eviction policy needs tuning.
- **Spam.** ActivityPub has no built-in rate limit. Mitigations:
  per-instance rate buckets on inbox, automatic defederation on flood,
  optional invite-only federation via `FEDERATION_ALLOWED_PEERS`.
- **Edge-edit conflicts across instances.** The "edge home = source
  home" rule avoids this for create/delete, but if we ever federate
  highlight toggles, two instances could disagree. Keep highlight
  local in v1.
- **Tag-name fragmentation.** The same human concept may carry
  different tag spellings on different instances (`climate-science`
  vs `climatescience`). v1: keep tags local; remote nodes are tagged
  with whatever their home instance sent, and our search treats
  them as opaque strings.
- **Identity migration.** Mastodon supports `Move`. We do not, because
  shared identity across instances would require either OAuth-based
  cross-instance auth or DID-style identifiers, both substantial. A
  user changing instances has to re-create their account.
- **Schema versioning of the custom context.** `Topic`/`View`/`Finding`
  are project-specific terms. If the local model adds a fourth node
  type, every other instance needs the new vocabulary. Plan: version
  the context URL (`/ns/v1`, `/ns/v2`) and keep older URLs resolvable.
- **HTTP signature spec drift.** The draft-cavage standard the
  fediverse uses is being superseded by RFC 9421 (HTTP Message
  Signatures). Plan to support both, prefer cavage on outbound until
  the fediverse moves.
- **License compatibility.** AGPL-3.0 is fine for ActivityPub; the
  protocol is open. Federating with proprietary servers is allowed
  but they cannot embed our binary.
- **Domain blocklists.** Should there be an opinionated default block
  list shipped with the binary? Lemmy ships none; Mastodon ships
  none. Stay aligned: no defaults, admin opts in.
- **What about non-public groups?** Federation v1 leaves them local.
  A future "private group across two instances" feature is large —
  needs end-to-end addressing, group key distribution, and a story
  for an admin on the remote instance who can see the encrypted
  payloads. Defer until there is real demand.

---

## What this plan does *not* lock in

- The specific shape of every JSON-LD payload. The phase-by-phase
  work will refine those as it goes.
- The exact admin UX. The `/admin/federation` page lands in phase 5
  and can iterate.
- Whether to ship a separate `mindful-social-relay` binary for
  servers that want to peer with many small instances. Decide after
  phase 4.

The fixed parts of this plan are:

- **Protocol:** ActivityPub + Mindful Social JSON-LD extensions.
- **Visibility:** public-only federates in v1.
- **Authority:** one home instance per object; no multi-master.
- **Wiki:** local edit only.
- **Phasing:** identity → public-read → follows → standard interop →
  graph extensions → groups & admin.
