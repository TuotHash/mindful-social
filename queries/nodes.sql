-- name: CreateNode :one
INSERT INTO nodes (type, title, body, source_url, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetNode :one
SELECT * FROM nodes WHERE id = $1;

-- name: ListRecentNodes :many
SELECT * FROM nodes
ORDER BY created_at DESC
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
    updated_at = now()
WHERE id = $1
RETURNING *;

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

-- name: ListNodesAuthoredBy :many
-- Nodes a user has authored, most recent first — for the "Authored" section
-- on a profile page.
SELECT * FROM nodes
WHERE created_by = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: PickerSearchNodes :many
-- Trigram fuzzy match against the title for the edge-creation picker.
-- Handles prefix ("nuc" → "Nuclear"), infix ("uclear" → "Nuclear") and
-- typo tolerance ("nucear" → "Nuclear") in one mechanism. The %> operator
-- uses the GIN trigram index and respects pg_trgm.word_similarity_threshold,
-- which we set to 0.25 in pgxpool.AfterConnect. Excludes the source node
-- ($2). Raw user text is safe here — pg_trgm operators take plain text, no
-- query syntax to escape.
SELECT n.id, n.type, n.title
FROM nodes n
WHERE n.id != sqlc.arg(source_id) AND n.title %> sqlc.arg(query)::text
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
SELECT
    n.id, n.type, n.title, n.body, n.source_url, n.created_by,
    n.created_at, n.updated_at,
    CASE WHEN n.search_tsv @@ websearch_to_tsquery('english', sqlc.arg(query)::text)
         THEN 1.0 + ts_rank(n.search_tsv, websearch_to_tsquery('english', sqlc.arg(query)::text))
         ELSE word_similarity(sqlc.arg(query)::text, n.title)::real
    END AS rank,
    ts_headline('english', n.body, websearch_to_tsquery('english', sqlc.arg(query)::text),
                'StartSel=«HL», StopSel=«/HL», MaxFragments=1, MaxWords=24, MinWords=8')::text AS excerpt
FROM nodes n
WHERE n.search_tsv @@ websearch_to_tsquery('english', sqlc.arg(query)::text)
   OR n.title %> sqlc.arg(query)::text
ORDER BY rank DESC, n.created_at DESC
LIMIT sqlc.arg(result_limit);
