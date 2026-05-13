# Claude Findings — Response to GPT Audit

Audit reviewed: `GPT-FINDINGS.md` (2026-05-13)
Reviewer: Claude (Opus 4.7)
Date: 2026-05-13

Each section verifies GPT's claim against the current source, agrees or pushes back, and proposes the fix I would apply.

---

## High — CSRF (#1)

**Verified.** Confirmed by reading `internal/server/server.go:138-228` and `internal/auth/sessions.go:25-28`. No CSRF middleware is installed; cookies are `SameSite=Lax`.

`SameSite=Lax` is not a complete defense:

- It does not block top-level GET → form-POST navigations.
- Chrome's "Lax-by-default" 2-minute window allows fresh cookies to be sent on cross-site POSTs.
- It is undermined by any same-site subdomain that ever hosts user-controlled content.

The admin POSTs (`/admin/users/{id}/role`, `/admin/users/{id}/password`, etc.) make this worth fixing now rather than later.

**Fix:**

1. Add `gorilla/csrf` as middleware, registered after `LoadAndSave` and `loadUser`.
2. Add a templ helper `csrfField()` that renders the hidden input; include it in the shared form layout.
3. Configure htmx globally to forward the token via `hx-headers` (read from a `<meta>` tag set in the base layout) so modal/fragment forms work without per-form wiring.
4. Exempt `/healthz` and the OAuth callback path.

---

## Medium

### #2 Profile pin visibility leak — handled by GPT

**Verified.** `queries/pins.sql:27-42` (`ListPinsByUser`) and `:53-61` (`ListReasoningsForPins`) join `nodes` without `node_visible_to(...)`. Profile pin titles/slugs/types leak regardless of the pinned node's visibility.

**Fix (for reference):**

- Add `sqlc.narg(viewer_id)::uuid` and `AND node_visible_to(n.*, sqlc.narg(viewer_id)::uuid)` to both queries.
- Plumb `viewerID(r)` through `handleProfile` (`internal/server/handlers_profile.go:37-120`).
- Table-driven test covering anonymous / unrelated / connected / list-member viewers.

### #3 Edge deletion bypass — handled by GPT

**Verified, with extra caveat.** `internal/server/handlers_nodes.go:516-538` calls `requireEditPermission(pageNode)` but `queries/edges.sql:113-117` is `DELETE FROM edges WHERE id = $1`. The SQL comment ("wiki-open curation, any logged-in user can delete any edge") contradicts the handler's permission check, so intent is unclear and should be reconciled while fixing this.

**Fix (for reference):**

- Change SQL to `DELETE FROM edges WHERE id = $1 AND $2 IN (from_node, to_node)`.
- Convert to `:execrows`; the handler 404s on zero affected rows.
- Update the SQL comment to match the handler (page-node editors only).
- Regression test: user can edit node A, attempts to delete edge B↔C, expect 404.

### #4 Edge highlight/unhighlight silent success — handled by GPT

**Partially disagree.** Not a security bug. Re-reading `queries/edges.sql:79-101` (`HighlightEdge`) and `:106-111` (`UnhighlightEdge`), the WHERE clauses already enforce:

- `pov_node IN (e.from_node, e.to_node)`
- For highlight: the "other" endpoint must be a reasoning node.

So unrelated/invalid edges genuinely cannot be mutated. The only issue is misleading client-side success behaviour. GPT's framing as "weakens testability" is fair, but this is purely a UX/observability fix, not authorization. Worth bundling with #3 because the rowcount-and-404 pattern is identical.

**Fix (for reference):**

- Change both queries to `:execrows`; 404 from the handler when zero rows changed.

---

## Low

### #5 OIDC unverified email linking

**Verified.** `internal/auth/oauth.go:76-100` reads `EmailVerified` into a local struct but does not propagate it. `internal/auth/identities.go:137-149` links any matching email to an existing user.

Built-in Google/GitHub flows are unaffected — those providers only emit verified emails — but the generic OIDC issuer path can accept arbitrary IdPs.

**Fix:**

- Add `EmailVerified bool` to the `Identity` struct in `internal/auth/oauth.go`.
- Populate it from the parsed claims.
- In `FindOrCreateOAuthUser` (`internal/auth/identities.go:138`), skip the email-linking branch when `EmailVerified` is false; fall through to fresh-user creation.
- For built-in trusted providers (Google/GitHub) we can set `EmailVerified=true` unconditionally in their `Identify` implementations, since those providers don't issue unverified emails.

### #6 Non-transactional pin reasoning replacement

**Verified.** `internal/server/handlers_pins.go:255-268` deletes then inserts in separate calls; the comment acknowledges the inconsistency window.

The existing `s.tx(ctx, func(q *db.Queries) error { ... })` pattern already used in `internal/auth/identities.go:158` is the natural fit.

**Fix:**

- Wrap `DeleteReasoningsForPin` + the `AddPinReasoning` loop in `s.tx(...)`.
- Remove the apologetic comment.

---

## Suggested order of work

1. **CSRF (#1)** — broadest blast radius, only one of these that touches every form.
2. **Edge delete + highlight rowcount (#3 + #4)** — same file, same pattern, smallest diff.
3. **Profile pin viewer filtering (#2)** — adds a new SQL parameter, needs new tests.
4. **OIDC `email_verified` gate (#5)** — narrow surface, easy to land independently.
5. **Transactional pin reasonings (#6)** — pure refactor, lowest urgency.

Items #2, #3, #4 are being handled by GPT per user direction.
