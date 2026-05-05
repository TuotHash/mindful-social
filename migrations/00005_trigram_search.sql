-- +goose Up
-- +goose StatementBegin

-- Trigram index on title to support fuzzy/typo-tolerant matching alongside
-- the existing tsvector index. The picker uses this exclusively (so "nuc"
-- finds "Nuclear" and "nucear" matches "Nuclear" via shared trigrams). The
-- public /search uses a hybrid query: tsvector for stem/phrase matches
-- (preferred), trigram for typo-tolerant fallback on the title.
--
-- Body is intentionally not indexed for trigrams — bodies can be long, and
-- fuzzy body matching gives noisy results. tsvector still covers
-- title || body for semantic body search. A user typing a fuzzy query will
-- match nodes whose *title* is similar; the body excerpt comes from
-- ts_headline only when the tsvector matches.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX nodes_title_trgm_idx ON nodes USING GIN (title gin_trgm_ops);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS nodes_title_trgm_idx;
-- Leave the extension installed; other tables/queries may rely on it.

-- +goose StatementEnd
