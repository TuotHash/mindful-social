# Claude Findings — Full Audit

Audit date: 2026-05-17
Reviewer: Claude (Opus 4.7), guided by the project owner.
Scope: handlers, SQL queries, auth/session/CSRF layer, file uploads,
template rendering, and migrations under `/Users/pxct/Documents/Software/mindful-social`.

Methodology:

- Five parallel deep-dives across auth, authorization/visibility, file
  uploads, template/XSS surfaces, and SQL/handler edge cases.
- Critical and High findings were re-verified by reading the cited code
  and, where relevant, vendored library source (e.g. `templ.SafeURL`).
- Medium and Low findings are reported as the audit agents found them;
  spot-check them against the live code before changing behaviour.

This file replaces the 2026-05-13 audit response. The three findings
from that pass are summarised under "Status of prior findings" below.

---

## Status of prior findings (2026-05-13)

| # | Finding | Status |
|---|---|---|
| 1 | No CSRF middleware on state-changing POSTs | Fixed (`gorilla/csrf` wired in `internal/server/server.go:147-161`; htmx bridge in `internal/views/layout.templ:43-51`; `@CSRFField()` present in every POST form sampled). |
| 2 | Profile pin listing leaks titles of private/list/connections nodes | Fixed (`queries/pins.sql:42` filters with `node_visible_to(n.*, sqlc.narg(viewer_id))`; `handlers_profile.go:46` passes `viewerID(r)`). |
| 3 | `DeleteEdge` only checks edge id | Fixed (`queries/edges.sql:113-119` requires `page_node_id IN (from_node, to_node)`; handler 404s on zero rows). |
| 4 | Highlight/unhighlight silently succeeds on unrelated edges | Fixed (both queries are `:execrows`; handlers 404 on zero rows). |
| 5 | OIDC `email_verified` claim not enforced | **Not fixed.** Re-filed below as Critical (#3). |
| 6 | Pin reasoning replacement is non-transactional | Obsolete — the schema was reworked (migration `00016`) and the multi-step write no longer exists. |

---

## Critical

### 1. Stored XSS via node `source_url` (`templ.SafeURL` is a pass-through cast)

**Severity:** Critical
**Verified:** Yes, by reading `~/go/pkg/mod/github.com/a-h/templ@v0.3.1001/url.go:22-23` and the templates.

Files:

- `internal/views/nodes.templ:412` — `<a href={ templ.SafeURL(*node.SourceUrl) } target="_blank" rel="noopener">↗ { *node.SourceUrl }</a>`
- `internal/server/handlers_nodes.go:776` — server only does `strings.TrimSpace(sourceURL)` before persisting.

`templ.SafeURL` is **not** a sanitizer. It is a plain type alias
(`type SafeURL string`). The sanitizing function is `templ.URL`, which
collapses non-allow-listed schemes to
`about:invalid#TemplFailedSanitizationURL`. The generated templ code
calls `JoinURLErrs` (templ url.go:26-31), which short-circuits and
returns the value as-is whenever the input is already typed `SafeURL`.

Attack flow (any logged-in user with permission to create a Finding):

```
POST /nodes
title=Anything
type=finding
parent=<some-topic-uuid>
source_url=javascript:alert(document.domain)
gorilla.csrf.Token=...
```

The browser-side `<input type="url">` constraint is a UI hint and is
bypassed by any non-browser POST (curl, scripts, even DevTools).

Any viewer clicking the source link then executes attacker JavaScript
in your origin with their session cookie attached.

Fix:

- Change `internal/views/nodes.templ:412` to use `templ.URL(...)` so
  unknown schemes collapse to the sentinel URL.
- Also reject in the handler (`internal/server/handlers_nodes.go`):
  parse `sourceURL` with `url.Parse`, require `Scheme` to be `http`
  or `https`, return a form error otherwise.

Both layers together. Server validation is the real defense; the
template fix is defense in depth.

### 2. Missing edit-permission check on edge creation

**Severity:** Critical
**Verified:** Yes, by reading `internal/server/handlers_nodes.go:948-1087`.

`handleEdgeCreate` calls `s.resolveNode` (which only checks
visibility) and then proceeds straight to `CreateEdge` and, in the
`to_mode=new` branch, `CreateNode` (lines 1017-1028). It never calls
`requireEditPermission(fromNode)`. Every other edge mutation does:

- `handleEdgeHighlight` — `internal/server/handlers_nodes.go:499`
- `handleEdgeUnhighlight` — `internal/server/handlers_nodes.go:528`
- `handleEdgeDelete` — `internal/server/handlers_nodes.go:666`

Default `edit_policy` is `'author'`. Today the policy is enforced for
deletes, highlights, and unhighlights but not for creates, so the
policy is half-applied.

Attack flow (Mallory is logged in; Alice has a public node
`/nodes/alice-essay` with `edit_policy='author'`):

```
POST /nodes/alice-essay/edges
gorilla.csrf.Token=...
kind=opposes
to_mode=new
new_finding_title=Alice%20fabricated%20her%20data
```

Result: a new finding node is created (parent = `alice-essay`, so it
inherits her visibility and group), authored by Mallory, plus an edge
`alice-essay --opposes--> "Alice fabricated her data"`. Every viewer
of Alice's essay now sees the smear card in her legend. With
`to_mode=existing` and a different `to_id`, Mallory can also wire
Alice's node up to any target she can see.

Combined with how the new finding inherits `Visibility`,
`VisibilityGroupID`, and `GroupID` from the parent (lines 1024-1028),
Mallory can also plant content inside a group she is not a member of
by attaching it to a group-hosted parent.

Fix:

- After `resolveNode`, add the same guard the other edge handlers use:
  `if !s.requireEditPermission(w, r, fromNode) { return }`.
- If you intentionally want a looser "anyone-who-can-see-can-connect"
  rule (the dropped `link_policy`), then loosen the *other* edge
  handlers to match. The current state is inconsistent.

### 3. OIDC `email_verified` claim is parsed and then discarded

**Severity:** Critical
**Verified:** Yes, by reading `internal/auth/oauth.go:63-101`,
`internal/auth/identities.go:122-138`.

`oauth.go:79` parses `EmailVerified bool \`json:"email_verified"\``
into a local struct. The returned `Identity{}` (oauth.go:36-40) has
no `EmailVerified` field; lines 96-100 build the result from `Sub`,
`Email`, `DisplayName` only. The verified flag is lost.

`FindOrCreateOAuthUser` (`internal/auth/identities.go:122-138`) then
links the OAuth identity to any existing user that matches by email.

Attack flow (against a generic OIDC issuer configured via
`OIDC_PROVIDERS`, e.g. a self-hosted Authelia/Authentik/Keycloak that
the attacker controls a tenant on, or any IdP that accepts
attacker-supplied unverified emails):

1. Attacker registers `mallory` at the IdP and configures her profile
   so the id_token will carry `email: alice@victim.com,
   email_verified: false`.
2. Attacker hits your `/auth/<key>/start`, completes the IdP flow.
3. Your server verifies the id_token signature (correct), parses
   claims (correct), drops `email_verified` on the floor (bug), finds
   Alice's local user by email, inserts an `auth_identities` row
   binding Mallory's IdP subject to Alice's `user_id`.
4. Attacker is now signed in as Alice on every subsequent visit.

GitHub is less exposed because `oauth.go:165-176` already requires
`Primary && Verified` from `/user/emails` — *but only when
`userResp.Email` from `/user` is empty*. If the user has marked any
email public on their GitHub profile, that path is taken without the
verified check (oauth.go:160-176).

Fix:

- Add `EmailVerified bool` to `Identity` (oauth.go:36).
- Populate it in OIDC `Identify` from `claims.EmailVerified`.
- Populate it in GitHub `Identify`: always fetch `/user/emails`, pick
  the primary verified one, and only fall back to `userResp.Email`
  when no verified primary is returned (and treat the fallback as
  unverified).
- In `FindOrCreateOAuthUser`, only enter the email-linking branch
  when `ident.EmailVerified` is true. Otherwise create a fresh user
  with no email, or require an in-session linking step.

---

## High

### 4. Uploaded media served with `Cache-Control: public, max-age=86400` regardless of node visibility

**Severity:** High
**Verified:** Yes, by reading `internal/server/handlers.go:26-36`
and `internal/server/server.go:179-180`.

`/uploads/*` is served by `http.FileServer(noDirectoryListing(...))`
wrapped in `cacheStatic`. `cacheStatic` unconditionally writes
`Cache-Control: public, max-age=86400`. There is no visibility check
between the request and the disk read.

The random 16-byte hex filenames are the only barrier. URLs leak
routinely — referrer headers, screenshots, browser history, future
federation traffic, search-engine archives. Once leaked, the asset
is cached for 24 hours in every intermediary (reverse proxy, CDN,
browser, corporate gateway) and is served to anyone with the URL,
even if the underlying node is `private`, `connections`, or
group-scoped.

Fix options (any one will do):

- Split URL space into `/uploads/public/...` and
  `/uploads/private/...`. Serve only `public` through `cacheStatic`;
  serve `private` through a handler that looks up the owning
  `node_images` / `node_videos` row, calls
  `canViewNode(rootTopicID, viewer)`, and sets `Cache-Control:
  private, no-store` on a hit / 404 on a miss.
- Or: keep one URL space, but always look up the parent node's
  visibility and pick `public, max-age=86400` vs `private, no-store`
  accordingly. The lookup cost is one indexed SELECT per request.

Profile images (`/uploads/profiles/...`) are intentionally public and
can keep the existing cache lifetime.

### 5. GIF passthrough + no `X-Content-Type-Options: nosniff` on `/uploads/*`

**Severity:** High (medium today, high once the allow-list widens)
**Verified:** Yes, by reading
`internal/server/handlers_node_images.go:216-220`.

`image.Decode` is the gate for image uploads. PNG and JPEG inputs
are decoded and re-encoded to JPEG by `compressNodeImage`, which
strips trailing bytes. **GIF inputs are stored verbatim** ("animated
GIFs can't survive a still-frame re-encode"). Polyglot files of the
form `<valid_GIF_header><valid_GIF_trailer><HTML_or_JS_or_PDF>` pass
`image.Decode` (the decoder stops at the trailer and ignores the
suffix), get saved with extension `.gif`, and are served back by
`http.FileServer` with `Content-Type: image/gif` (via
`mime.TypeByExtension`).

A `Content-Type: image/gif` response is not directly script-
executable from `<img>`, but **no `X-Content-Type-Options: nosniff`
is set anywhere on `/uploads/*`**. `cacheStatic` only sets
`Cache-Control`. Older browsers and specific embedding contexts
(plugins, sandboxed iframes, in-page PDF viewers) may still sniff.
The bigger concern is future regressions: any later addition to the
allow-list (`.svg`, `.html`, `.pdf`) — or any other code path that
ever writes attacker bytes into `UploadDir` — becomes a stored XSS
because the MIME is derived from the filename and not from the DB.

A second nearby issue: the video handler writes the raw,
unsanitized input bytes to `<UploadDir>/tmp/in-*.bin` during transcode
(`internal/server/handlers_node_videos.go:129-143`). That directory
lives inside `UploadDir` and is therefore reachable via
`/uploads/tmp/...` for the lifetime of the request (until the
deferred `os.Remove`). Filename guessing is hard, but it should not
be in the served tree at all.

Fix:

- Set on every `/uploads/*` response:
  - `X-Content-Type-Options: nosniff`
  - `Content-Security-Policy: sandbox; default-src 'none'`
  - `Content-Type` from the DB row (the recorded MIME), not from
    `mime.TypeByExtension`.
- Re-encode GIF to a fixed format (mp4/webp) or, at minimum, strip
  bytes after the GIF trailer before storing.
- Move the video `tmp/` directory outside `UploadDir`.

### 6. No security headers anywhere (CSP, X-Frame-Options, Referrer-Policy, nosniff)

**Severity:** High (defense in depth)
**Verified:** Yes — `grep -r Content-Security-Policy internal/`
returns nothing; `cacheStatic` and `csrfMiddleware` are the only
header-touching middlewares.

Effect:

- The XSS in #1 has no CSP backstop, so it gets full script
  privileges (read same-origin, call APIs).
- Logged-in pages can be framed by arbitrary attacker sites
  (clickjacking). CSRF blocks the resulting POSTs, but UI overlays
  remain possible.
- `Referer` headers leak full paths including search queries to
  every outgoing link.

Fix: one middleware in the chain (after `csrfMiddleware`):

```
Content-Security-Policy: default-src 'self';
  script-src 'self' 'nonce-<random-per-request>';
  style-src 'self' 'unsafe-inline';
  img-src 'self' data:;
  font-src 'self';
  frame-ancestors 'none';
  form-action 'self';
  base-uri 'self'
X-Content-Type-Options: nosniff
Referrer-Policy: same-origin
X-Frame-Options: DENY
```

Inline `<script>` blocks currently in `internal/views/layout.templ`
(theme bootstrap, htmx CSRF bridge, theme toggle, user-menu close)
will need either nonces (cleanest with templ — propagate via
`ctx`) or hashes.

### 7. OAuth username squat on first sign-in

**Severity:** High
**Verified:** Yes, by reading `internal/auth/identities.go:141-223`.

`suggestIdentity` derives the new user's username from the IdP's
`DisplayName` (Google `name` / OIDC `preferred_username` / GitHub
`name` or `login`). `sanitizeUsername` normalises it; `uniqueUsername`
appends a numeric suffix only when a row already exists.

Attack flow:

1. Mallory sets her Google display name to `alice`.
2. Mallory signs in via Google before the real Alice has a chance to.
3. Mallory's new account lands at `/users/alice`. The real Alice can
   no longer use that handle.

Result is reputational, not full takeover, but it is irreversible
without admin intervention. Worse, repeated logins with different
display names each create *new* accounts because the `(provider,
subject)` index only matches on exact prior logins, so a single IdP
subject can burn through many usernames.

Fix: force a one-time "pick your username" step on first OAuth sign-
in. Never let attacker-controlled IdP strings land verbatim as a
username. Server-side: maybe seed a placeholder like
`g_<short-hash-of-subject>` and let the user rename once.

### 8. Decompression bomb on image upload

**Severity:** High
**Verified:** Yes, by reading
`internal/server/handlers_node_images.go:193-198` and
`internal/server/handlers_account.go:216-221`.

`http.MaxBytesReader` caps the *compressed* upload at 8 MiB (node
images) and 12 MiB (profile). `image.Decode` allocates the entire
pixel buffer (`width * height * bytes-per-pixel`) before any
dimension check happens. An 8 MiB PNG can legitimately decompress to
>5 GiB raw RGBA (e.g. a 40000×40000 single-colour PNG compresses to
tens of KB). A single attacker upload OOMs the process.

Fix: call `image.DecodeConfig(bytes.NewReader(data))` first — it
reads only the header and returns width/height. Reject anything
above a sensible budget (e.g. `cfg.Width * cfg.Height > 50_000_000`,
i.e. 50 megapixels), then do the full `image.Decode`. Apply to both
node and profile image paths.

---

## Medium

### 9. Login allows email enumeration via timing

**Severity:** Medium
**Verified:** Yes, by reading
`internal/auth/identities.go:84-100`.

`AuthenticatePassword` short-circuits on `pgx.ErrNoRows` *before*
calling bcrypt. Response times:

- Unknown email: <1 ms (one indexed SELECT).
- Wrong password: ~80 ms (bcrypt at default cost).

The error message is the same, but timing leaks the difference.
There is also no rate-limit anywhere on `/login`, `/signup`, or
`/auth/<key>/start`, so credential stuffing is free.

Fix:

- On `ErrNoRows`, call `bcrypt.CompareHashAndPassword` against a
  fixed valid hash before returning, to equalise timing.
- Add an `httprate`-style middleware on `/login` keyed on remote IP
  + form email. Maybe also per-user lockout after N consecutive
  failures.

### 10. Sessions are not revoked on password or email change

**Severity:** Medium
**Verified:** Yes — `internal/auth/identities.go` `ChangePassword`
and `AdminUpdateEmail` update the row but never iterate scs sessions.

A stolen cookie remains valid after the user changes their password
or after an admin changes their email. The user has no way to log
out their other devices.

Fix: after a password change or email change, iterate sessions with
`sm.Iterate(ctx, func(s *scs.Session) error { ... })` and destroy
every one whose stored `user_id` matches.

### 11. Group `AddGroupMember` can silently downgrade admins/editors

**Severity:** Medium
**Verified:** Yes, by reading `queries/groups.sql:12-19` and
`internal/server/handlers_groups.go:200`.

```sql
INSERT INTO group_memberships (group_id, user_id, role)
VALUES ($1, $2, $3)
ON CONFLICT (group_id, user_id) DO UPDATE
SET role = CASE
  WHEN group_memberships.role = 'owner' THEN group_memberships.role
  ELSE EXCLUDED.role
END;
```

The handler always passes `role = db.GroupMemberRoleMember`
(`handlers_groups.go:203`). An admin re-adding an existing admin or
editor by username silently downgrades them to `member`. Two admins
can demote each other through this endpoint with no warning.

There is also no invite/accept flow, despite the `group_invites`
table existing (migration 00017, queries at `groups.sql:200-228`).
Group admins can force-add anyone to their group without consent.

Fix:

- Change the ON CONFLICT branch to `DO NOTHING` (so existing
  membership rows are not touched), or restrict the update so it
  cannot lower an existing role.
- Replace the direct-add UI with the existing `group_invites` flow.

### 12. Member-visibility setting silently defaults to the most permissive value

**Severity:** Medium
**Verified:** By the audit; spot-check before fixing.
Files: `internal/server/handlers_groups.go:270-368`.

`parseMemberVisibility` returns `member` (the most open option) on
empty or unknown input. A settings POST that omits the field — for
example, a partial form submission, a future template change that
drops a hidden input, a re-render that forgets the value — overwrites
the existing setting to `member`. This can expose a member list to
everyone in the group when the admin meant `editor` or `admin`.

Same anti-pattern in `parseGroupVisibility`, which silently defaults
to `private` (safe direction, but still silent).

Fix: return `(value, ok bool)` from both parsers. Skip the write
when `!ok`, render a "Pick a valid visibility option" flash, and
preserve the current value.

### 13. Tag set replacement is non-atomic

**Severity:** Medium
**Verified:** By the audit.
Files: `internal/server/handlers_tags.go:72-87`.

`setTagsForNode` deletes all existing `node_tags` rows for the node
then re-attaches one by one *outside* any transaction. A timeout
mid-loop, a network drop, or two concurrent saves by two editors
leaves a partial tag set.

Fix: wrap the delete + per-tag attach in `s.tx(ctx, func(q
*db.Queries) error { ... })`, mirroring the pattern already used in
`internal/auth/identities.go:158`.

### 14. Slug-generation race on concurrent inserts

**Severity:** Medium
**Verified:** By the audit; the SQL has a unique index, so the race
is a UX bug rather than a data-integrity one.
Files: `internal/server/slug.go:36-49`.

`uniqueSlug` checks `GetNodeBySlug` (free) then `CreateNode` inserts.
Two concurrent submissions of the same title both see the slug free;
one wins, the other hits the unique-violation `23505`. The handler
catches the generic error and renders "Could not create post.
Please try again." — losing the form contents on retry.

Fix: on `pgconn.PgError.Code == "23505"` from `CreateNode`, retry
with the next-numbered suffix transparently. Or restructure to
`INSERT ... ON CONFLICT (slug) DO NOTHING RETURNING ...` and bump
the suffix server-side.

### 15. Node-revision number race + swallowed snapshot errors

**Severity:** Medium
**Verified:** By the audit.
Files: `queries/node_revisions.sql:1-17`,
`internal/server/handlers_node_revisions.go` (`snapshotNodeRevision`).

`CreateNodeRevision` computes `COALESCE(max(revision), 0) + 1` in a
single statement, but at `READ COMMITTED` two concurrent edits can
both compute the same value. The unique `(node_id, revision)`
constraint catches the second; `snapshotNodeRevision` only logs the
error and returns. The live `nodes` row gets updated either way, so
one of the two edits ships without a history snapshot.

Fix: `SELECT ... FOR UPDATE` on the node row inside a transaction
that holds both the snapshot and the UPDATE; on unique-violation,
re-compute `max(revision)` and retry. Or use an advisory lock keyed
on `node_id`.

### 16. No "last admin" guard

**Severity:** Medium
**Verified:** By the audit.
Files: `internal/server/handlers_admin.go:49-93`.

Self-demotion is blocked but two admins can demote each other
through the API, leaving zero admins. The instance becomes
unmanageable through the UI until an operator either sets
`ADMIN_USERS` (which re-promotes on every startup) or hand-edits the
DB.

Fix: in `handleAdminSetRole`, before committing a demotion, `SELECT
COUNT(*) FROM users WHERE role = 'admin'` and refuse if the demotion
would drop the count to zero.

### 17. Pin delete silently succeeds on a non-existent pin

**Severity:** Medium (cosmetic today; structural risk if reused).
Verified by reading `queries/pins.sql:13` and
`internal/server/handlers_pins.go:77-92`.

`DeletePin` is `:exec`. The handler does not check rowcount. POST
`/nodes/{id}/unpin` for a node the user never pinned returns 303
success with no flash.

Fix: switch `DeletePin` to `:execrows`, return 404 on zero rows. Same
pattern is already used for edge delete / highlight / unhighlight.

---

## Low / improvements

### L1. `pg_trgm.word_similarity_threshold = 0.25` is set per-connection

`internal/server/server.go:52-55` lowers the trigram threshold on
every new pool connection. The setting is then live for every `%>`
query on that connection: `SearchUsers`, `SearchGroups`, `SearchTags
ForViewer`, `SearchPostParents`, the picker, every suggest
endpoint. Any future query that wants the strict default (0.6)
silently gets 0.25 instead.

Fix: scope to the specific query with `SET LOCAL` inside an explicit
transaction, or pass the threshold as a query argument.

### L2. No length cap on search query strings

`handleSearch`, `handleSearchSuggest`, picker, suggest endpoints —
none cap `q`. A 64 KB query runs `websearch_to_tsquery`, `%>`, and
`word_similarity` per row.

Fix: enforce `len(q) <= 256` (or so) in the handler; reject longer
with 400.

### L3. `HighlightEdge` `MAX(position) + 1` race

Two concurrent highlight clicks on the same side compute the same
`MAX + 1`. No unique constraint prevents duplicate positions, so
both succeed and the UI displays them in arbitrary order.

Fix: add `UNIQUE (from_node, position) WHERE position IS NOT NULL`
and a mirror for `to_position`. Retry on conflict.

### L4. 30-day session lifetime, 7-day idle, no step-up auth

`internal/auth/sessions.go:23-30` is generous. A stolen long-lived
cookie has full powers. Admin role changes, password resets,
identity disconnect, and email changes are gated by the steady-
state session only.

Fix: drop the idle timeout to 24-72 hours. Add a "re-enter your
password" step before admin-only actions, identity disconnect, and
password / email changes.

### L5. `handleNodeImageUpload` permission and quota

`internal/server/handlers_node_images.go:75-122` lets anyone who can
*view* a root-topic descendant upload images. No per-user disk
quota.

Fix: require `requireEditPermission(rootNode)` (or the upload
target node) to upload. Add a quota table summed over `uploaded_by`.

### L6. Title length cap is in bytes, not runes

`internal/server/handlers_nodes.go:222` does `len(title) > 200`. 200
bytes of UTF-8 ≈ 50 CJK or emoji characters. Comment validation uses
runes (good), so the two paths are inconsistent.

Fix: `utf8.RuneCountInString(title) > 200`.

### L7. `parseRevisionParam` silently truncates large integers

`internal/server/handlers_node_revisions.go` uses `strconv.Atoi` and
then `int32(n)`. On 64-bit, `strconv.Atoi("9999999999")` succeeds;
the cast then narrows to a wrap-around small int, and the handler
silently views the wrong revision.

Fix: `strconv.ParseInt(raw, 10, 32)` and 400 on `ErrRange`.

### L8. Comments allow replies to soft-deleted parents

`queries/comments.sql:9-25` validates that the parent is a root and
on the same node, but does not check `c.deleted_at IS NULL`. A user
can reply to a tombstone.

Fix: add `AND c.deleted_at IS NULL` to the parent CTE.

### L9. `bootstrapAdmins` re-promotes from `ADMIN_USERS` on every restart

`internal/server/server.go:122-138` will undo a deliberate UI
demotion if the env var still lists the user. Operators need to
clear the env var or the demotion sticks only until the next deploy.

Fix: document the behaviour prominently. Optionally, only promote
during a "first run" detection, not on every boot.

### L10. Several suggest/htmx endpoints return 200 with empty body on DB error

`handleSearchSuggest`, `handleGraphNodesSuggest`,
`handleUsersSuggest`, `handleTagsSuggest`, `handleGroupsSuggest`
log the error but still write a 200 with no body. The htmx dropdown
disappears with no signal — indistinguishable from "no matches".

Fix: render at least an empty dropdown component on error, or return
500 so htmx surfaces a generic failure.

### L11. `handleArgumentGraphData` swallows JSON encode errors

`_ = json.NewEncoder(w).Encode(data)` — if a future field has a
non-marshalable type, the response body is silently empty.

Fix: log the error.

### L12. Inconsistent symmetric uniqueness on `relates_to`

The `(from, to, kind)` unique constraint catches `(A, B, related)`
but not `(B, A, related)`. The verb is symmetric in user intent, so
both rows can coexist.

Fix: either tighten with a normalised pair (sort UUIDs at insert
time) or document the duplicate is intentional.

### L13. `validateCommentBody` accepts whitespace-only bodies

Non-breaking spaces, lone newlines, and other "visible-empty" inputs
pass the trim and rune-cap check.

Fix: collapse Unicode whitespace before length validation, or
explicitly reject when the trimmed result is whitespace-only.

### L14. Graph `<script type="application/json">` payload is safe today

Verified safe via `json.Marshal` default escaping (`<`, `>`, `&` →
`<`, `>`, `&`). If anyone ever swaps to a
`json.Encoder` with `SetEscapeHTML(false)`, the `<script>` becomes
escapable. Worth a comment in the helper to prevent the regression.

### L15. Search `ts_headline` excerpt rendering is safe today

`internal/server/handlers_search.go:229-254` splits on the unicode
sentinels `«HL»` / `«/HL»`; `internal/views/search.templ:157-166`
renders each `ExcerptPart.Text` via `{ p.Text }` (auto-escaped).
Confirmed safe. Flagging only because the technique relies on those
sentinels never appearing in user input — which, given they're non-
ASCII and the body is tokenised first, holds today but is fragile.

### L16. Markdown rendering is well-defended

Verified: goldmark + bluemonday allow-list, schemes restricted to
`http`/`https`/`mailto`/relative, `javascript:` and `data:` both
blocked, no `script`/`iframe`/`style`/`svg`/`object`/`embed`/event-
handler attributes. The markdown_test.go cases cover the key payloads.

### L17. Migration locks on large tables

Migrations 00017, 00024, 00025 each `ALTER TYPE ... RENAME` enums or
rebuild constraints in ways that rewrite every dependent row. This
will hold long locks on a populated `nodes` table at deploy time.

Fix: when the table grows, switch to the `ADD ... CHECK NOT VALID` /
`VALIDATE CONSTRAINT` pattern, or do swap-table migrations.

### L18. `parentGroupID` inheritance lets non-members host content in a group

`internal/server/handlers_nodes.go:264-295` copies the parent's
`group_id` or `visibility_group_id` into the new node when the
creator does not specify one. If the parent is visible to the creator
through some other branch (e.g. `connections`), the creator can plant
content in a group they do not belong to.

Fix: verify the creator is a member of `parentGroupID` before
inheriting; otherwise create the child without a `group_id`.

### L19. GitHub `userResp.Email` used even when unverified

Tightening to "always prefer the verified primary" is straightforward
and pairs naturally with Critical #3's fix. Treat the `/user` email
as unverified unless re-confirmed via `/user/emails`.

### L20. `removePreviousProfileImage` uses substring `..` check

`internal/server/handlers_account.go:268-282` rejects `/`, `..`, and
empty strings. Currently safe because the stored path is always
server-generated. Replace the `strings.Contains(name, "..")` check
with `filepath.Clean` equality, to avoid relying on a defensive
heuristic.

### L21. Profile-image filename uses only a 16-bit hash suffix

`internal/server/handlers_account.go:240-241` uses
`sha256(processed)[0:4hex]`. 16 bits collide at trivial rate when
the same user uploads many images. Not a security issue — collisions
just mean the new image overwrites the old one — but worth widening
to 8 hex chars (32 bits) so the path acts as a cache-bust.

### L22. ffmpeg / ffprobe shell-out: safe, but worth sandboxing

`internal/server/handlers_node_videos.go:212-334` calls ffmpeg and
ffprobe via `exec.CommandContext` with a fixed argument list and a
server-generated temp path. No argument injection. ffmpeg's
demuxers, however, parse attacker bytes and historically ship CVEs.
Production deployments should run the binary under additional
sandboxing (separate UID, `systemd` `PrivateTmp=`, seccomp).

---

## Recommended order of fixes

The first three are real exploits accessible to a logged-in user.
Everything else needs either an attacker-controlled IdP, a misconfigured
deploy, or specific timing.

1. **#1 source_url XSS** — one-line template change plus a scheme
   check in two handlers.
2. **#2 edge create permission** — one-line guard in
   `handleEdgeCreate`.
3. **#3 OIDC `email_verified` + L19 GitHub email + #7 username squat**
   — all live in `internal/auth/`; bundle as one PR.
4. **#4 + #5 + #6 — uploads and security headers** — one PR adding a
   header middleware plus the private-uploads visibility gate.
5. **#8 decompression bomb**, **#9 login timing**, **#10 session
   revoke**, **#11 + #12 group settings**.
6. **#13 tag transaction**, **#14 slug race**, **#15 revision race**,
   **#16 last-admin guard**, **#17 pin delete rowcount** — data-
   integrity polish.
7. Low-severity items as time allows.

Items marked "Verified: Yes" were re-checked against the live code at
audit time; the rest were reported by the audit agents and should be
spot-checked before changing behaviour.
