# GPT Findings

Audit date: 2026-05-13

Scope: static audit of the Go web app, handlers, auth/session logic, SQL queries, and migrations. Generated sqlc/templ outputs were treated as supporting context. No source files were changed.

Verification:

- `nix develop --extra-experimental-features 'nix-command flakes'`
- `go test ./...` passed inside the Nix development shell.

## High Severity

### 1. State-changing POST routes have no CSRF protection

Evidence:

- Routes in `internal/server/server.go:168-227` register many state-changing `POST` endpoints, including logout, account changes, node creation/update/delete, edge changes, pins, follows, list membership, and admin actions.
- Middleware in `internal/server/server.go:138-147` loads sessions and users but does not install CSRF middleware.
- Handlers parse and trust form bodies directly, for example `internal/server/handlers_auth.go:93-98`, `internal/server/handlers_account.go:70-105`, `internal/server/handlers_nodes.go:587-687`, `internal/server/handlers_admin.go:45-84`.
- Session cookies are `SameSite=Lax` in `internal/auth/sessions.go:25-28`. That helps for many cross-site POSTs, but it is not a complete application-level CSRF defense and can be bypassed in some browser/navigation and same-site-subdomain scenarios.

Impact:

An attacker who can cause a logged-in user's browser to submit a form can potentially trigger mutations as that user. The admin endpoints make this especially risky: a targeted admin could be induced to change roles, reset passwords, or modify account data.

Recommendation:

Add a CSRF middleware/token system for all unsafe methods (`POST`, future `PUT/PATCH/DELETE`). Render tokens into all forms, validate them before handlers run, and include htmx-compatible token handling for modal/fragment forms.

## Medium Severity

### 2. Public profiles leak private/list/connections pinned nodes and reasoning titles

Evidence:

- `handleProfile` correctly loads authored nodes through `ListNodesAuthoredByForViewer` with `ViewerID` in `internal/server/handlers_profile.go:37-46`.
- The same handler loads profile pins with `ListPinsByUser` in `internal/server/handlers_profile.go:47`.
- `queries/pins.sql:27-42` joins pinned nodes without `node_visible_to`.
- `pinRows` then loads attached reasonings with `ListReasoningsForPins` in `internal/server/handlers_profile.go:120`.
- `queries/pins.sql:53-61` joins reasoning nodes without `node_visible_to`.

Impact:

Anyone viewing `/users/{username}` can see titles/slugs/types of nodes that should be hidden by `private`, `connections`, or `list` visibility if those nodes are pinned by that profile user. Attached private reasoning titles can also be exposed.

Recommendation:

Make profile pin queries viewer-aware. Add `viewer_id` parameters to pin listing queries and filter both pinned nodes and attached reasonings through `node_visible_to`. Add tests for anonymous, unrelated, connected, and list-member viewers.

### 3. Edge deletion can delete unrelated edges

Evidence:

- `handleEdgeDelete` resolves the page node and checks edit permission for that page node in `internal/server/handlers_nodes.go:516-526`.
- It then deletes by `edgeID` only in `internal/server/handlers_nodes.go:527-532`.
- `DeleteEdge` in `queries/edges.sql` is `DELETE FROM edges WHERE id = $1` and does not require the edge to touch the page node.

Impact:

A user with edit rights on any node can submit `POST /nodes/{editable-node}/edges/{edgeID}/delete` with the UUID of an unrelated edge and delete that unrelated edge. This bypasses the intended endpoint relationship check.

Recommendation:

Change deletion to require both `edge_id` and `pov_node`, with SQL such as `DELETE FROM edges WHERE id = $1 AND $2 IN (from_node, to_node)`. Treat zero affected rows as not found/forbidden. Add a regression test where a user can edit node A but attempts to delete an edge between B and C.

### 4. Edge highlight/unhighlight may silently succeed on unrelated or invalid edges

Evidence:

- `handleEdgeHighlight` and `handleEdgeUnhighlight` check edit permission only on the URL node in `internal/server/handlers_nodes.go:395-440`.
- The SQL update queries in `queries/edges.sql` include endpoint checks, so unrelated edges are not modified.
- The handlers do not inspect affected row count because sqlc generated these as `:exec`, so they redirect success even when no row matched.

Impact:

This is not the same destructive authorization bug as edge deletion, but it creates misleading success behavior and weakens testability. A crafted request against an unrelated edge, nonexistent edge, or non-reasoning highlight target appears successful to the client.

Recommendation:

Return affected row count for highlight/unhighlight and report not found/forbidden when no row changes. Add tests for unrelated edge IDs and non-reasoning highlight targets.

## Low Severity

### 5. OIDC email verification is ignored before account linking

Evidence:

- `oidcProvider.Identify` parses `EmailVerified` in `internal/auth/oauth.go:76-80`.
- It returns `claims.Email` regardless of whether `EmailVerified` is true in `internal/auth/oauth.go:96-100`.
- `FindOrCreateOAuthUser` links a new OAuth identity to an existing local user by email in `internal/auth/identities.go:137-149`.

Impact:

For OIDC providers that can emit unverified email claims, a user could be linked to an existing account based on an email address the provider has not verified. Many major providers verify emails, but the generic OIDC path supports arbitrary issuers, so this should be enforced by the app or made explicit per provider.

Recommendation:

Only use OIDC email for account linking when `email_verified` is true, or add provider configuration that explicitly marks an issuer's email claims as trusted. Otherwise create an unlinked account or require an explicit in-session linking flow.

### 6. Pin reasoning replacement is non-transactional

Evidence:

- `replacePinReasonings` deletes all existing reasonings and then inserts new rows one by one in `internal/server/handlers_pins.go:252-264`.
- The code comment notes that a partial failure can leave inconsistent state.

Impact:

A transient database error or constraint failure after deletion can erase previously attached reasonings while leaving the pin saved. This is a data integrity issue, not a direct access-control issue.

Recommendation:

Run the delete and inserts in a transaction, ideally in the auth/server service layer or as a single SQL function/query pattern.

