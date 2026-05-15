-- name: CreateNode :one
INSERT INTO nodes (
    type,
    title,
    body,
    source_url,
    created_by,
    slug,
    visibility,
    visibility_group_id,
    group_id,
    parent_node_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: GetNode :one
SELECT * FROM nodes WHERE id = $1;

-- name: GetNodeBySlug :one
SELECT * FROM nodes WHERE slug = $1;

-- name: CanViewNode :one
SELECT node_visible_to(n.*, sqlc.narg(viewer_id)::uuid)::bool AS visible
FROM nodes n
WHERE n.id = $1;

-- name: ListRecentNodesForViewer :many
-- Home page feed. node_visible_to() handles the per-row visibility check;
-- viewer_id is NULL for logged-out users (only public nodes match).
SELECT n.id, n.slug, n.type, n.title, n.created_at, u.username AS author_username
FROM nodes n
JOIN users u ON u.id = n.created_by
WHERE node_visible_to(n.*, sqlc.narg(viewer_id)::uuid)
ORDER BY n.created_at DESC
LIMIT $1;

-- name: ListNodesByType :many
SELECT * FROM nodes
WHERE type = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: UpdateNode :one
UPDATE nodes
SET title = $2,
    body = $3,
    source_url = $4,
    visibility = $5,
    visibility_group_id = $6,
    group_id = $7,
    edit_policy = $8,
    link_policy = $9,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CanEditNode :one
-- True when `viewer` is permitted to edit `node` under its edit_policy.
-- Implemented in SQL so handlers can call it without re-implementing the
-- mutual-follow logic.
SELECT node_action_allowed(n.edit_policy, n.created_by, sqlc.narg(viewer_id)::uuid)::bool AS allowed
FROM nodes n WHERE n.id = $1;

-- name: CanLinkToNode :one
-- True when `viewer` is permitted to create an edge touching `node` under
-- its link_policy.
SELECT node_action_allowed(n.link_policy, n.created_by, sqlc.narg(viewer_id)::uuid)::bool AS allowed
FROM nodes n WHERE n.id = $1;

-- name: CountNodes :one
SELECT count(*) FROM nodes;

-- name: DeleteNode :exec
DELETE FROM nodes WHERE id = $1;

-- name: CountEdgesForNode :one
-- Total edges (incoming + outgoing) that would cascade-delete if the node
-- were removed. Used on the deletion confirmation page.
SELECT count(*) FROM edges WHERE from_node = $1 OR to_node = $1;

-- name: CountOtherUserPinsForNode :one
-- Pins on this node by users other than the node's author. The author's own
-- pin is excluded — they obviously consent to losing it. Other users' pins
-- are surfaced on the confirmation page so the author knows what cascades.
SELECT count(*) FROM user_node_pins WHERE node_id = $1 AND user_id <> $2;

-- name: ListNodesAuthoredByForViewer :many
-- Nodes a user has authored, most recent first — for the "Authored" section
-- on a profile page. Filtered through node_visible_to() so a visitor only
-- sees nodes they're entitled to.
SELECT * FROM nodes n
WHERE n.created_by = sqlc.arg(author_id)
  AND node_visible_to(n.*, sqlc.narg(viewer_id)::uuid)
ORDER BY n.created_at DESC
LIMIT sqlc.arg(result_limit);

-- name: SearchPostParents :many
-- Parent picker for the post form. type_filter restricts to a single node
-- type ('topic' when creating a view or sub-topic); leave it empty to match
-- any type ('topic', 'view', or 'finding') — used when creating a finding,
-- which can attach to any existing node. Fuzzy-searches titles when a query
-- is given; falls back to recency order when empty so the picker is
-- pre-populated. Respects node_visible_to() so viewers only see candidates
-- they can post under.
SELECT id, type, title
FROM nodes
WHERE (sqlc.arg(type_filter)::text = '' OR type::text = sqlc.arg(type_filter)::text)
  AND node_visible_to(nodes.*, sqlc.narg(viewer_id)::uuid)
  AND (sqlc.arg(query)::text = '' OR title %> sqlc.arg(query)::text)
ORDER BY
    CASE WHEN sqlc.arg(query)::text = '' THEN created_at ELSE NULL END DESC NULLS LAST,
    word_similarity(sqlc.arg(query)::text, title) DESC,
    title ASC
LIMIT 20;

-- name: PickerSearchNodes :many
-- Trigram fuzzy match against the title for the edge-creation picker.
-- Handles prefix ("nuc" → "Nuclear"), infix ("uclear" → "Nuclear") and
-- typo tolerance ("nucear" → "Nuclear") in one mechanism. The %> operator
-- uses the GIN trigram index and respects pg_trgm.word_similarity_threshold,
-- which we set to 0.25 in pgxpool.AfterConnect. Excludes the source node
-- and skips nodes the viewer can't see. Raw user text is safe here —
-- pg_trgm operators take plain text, no query syntax to escape.
SELECT n.id, n.type, n.title
FROM nodes n
WHERE n.id != sqlc.arg(source_id)
  AND n.title %> sqlc.arg(query)::text
  AND node_visible_to(n.*, sqlc.narg(viewer_id)::uuid)
ORDER BY word_similarity(sqlc.arg(query)::text, n.title) DESC, n.title ASC
LIMIT 50;

-- name: SearchNodes :many
-- Hybrid full-text + fuzzy search. tsvector handles stems, stop-words, and
-- phrase quotes via websearch_to_tsquery; pg_trgm word_similarity on the
-- title catches typos and partial words ("nucear" → "Nuclear"). A row
-- matches if EITHER mechanism hits.
--
-- Ranking gives semantic matches a +1.0 head-start so they always sit above
-- pure-fuzzy matches at the same nominal score; within each group we order
-- by relevance score and then creation date as a tiebreaker.
--
-- ts_headline runs over the tsquery only — fuzzy-only matches get an empty
-- excerpt because the body did not contain the searched lexemes; the title
-- carries enough information in that case.
--
-- node_visible_to() filters out anything the viewer isn't entitled to.
SELECT
    n.id, n.slug, n.type, n.title, n.body, n.source_url, n.created_by,
    n.created_at, n.updated_at,
    u.username AS author_username,
    CASE WHEN n.search_tsv @@ websearch_to_tsquery('english', sqlc.arg(query)::text)
         THEN 1.0 + ts_rank(n.search_tsv, websearch_to_tsquery('english', sqlc.arg(query)::text))
         ELSE word_similarity(sqlc.arg(query)::text, n.title)::real
    END AS rank,
    ts_headline('english', n.body, websearch_to_tsquery('english', sqlc.arg(query)::text),
                'StartSel=«HL», StopSel=«/HL», MaxFragments=1, MaxWords=24, MinWords=8')::text AS excerpt
FROM nodes n
JOIN users u ON u.id = n.created_by
WHERE (n.search_tsv @@ websearch_to_tsquery('english', sqlc.arg(query)::text)
       OR n.title %> sqlc.arg(query)::text)
  AND node_visible_to(n.*, sqlc.narg(viewer_id)::uuid)
ORDER BY rank DESC, n.created_at DESC
LIMIT sqlc.arg(result_limit);

-- name: ListViewsForTopic :many
-- Topic pages render their child views as a thread feed. parent_node_id is
-- the canonical hierarchy link; node_visible_to() keeps group/list/private
-- child views hidden from viewers outside their audience.
SELECT
    n.id,
    n.slug,
    n.title,
    n.body,
    n.created_at,
    u.username AS author_username,
    u.profile_image_path AS author_profile_image_path,
    count(c.id) FILTER (WHERE c.deleted_at IS NULL)::bigint AS comment_count
FROM nodes n
JOIN users u ON u.id = n.created_by
LEFT JOIN comments c ON c.node_id = n.id
WHERE n.type = 'view'
  AND n.parent_node_id = sqlc.arg(topic_id)::uuid
  AND node_visible_to(n.*, sqlc.narg(viewer_id)::uuid)
GROUP BY n.id, n.slug, n.title, n.body, n.created_at, u.username, u.profile_image_path
ORDER BY n.created_at DESC
LIMIT sqlc.arg(result_limit);
