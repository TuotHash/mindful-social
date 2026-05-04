-- +goose Up
-- +goose StatementBegin

-- Full-text search index over title + body. We use a STORED generated
-- column so the tsvector is always in sync with the row content — no
-- triggers, no manual reindex on edit. Title is intentionally NOT
-- weighted higher here; ts_rank gives reasonable ordering for our small
-- corpus, and we can promote title matches later via setweight() if it
-- becomes worth it.
ALTER TABLE nodes ADD COLUMN search_tsv tsvector
    GENERATED ALWAYS AS (to_tsvector('english', title || ' ' || body)) STORED;

CREATE INDEX nodes_search_tsv_idx ON nodes USING GIN (search_tsv);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS nodes_search_tsv_idx;
ALTER TABLE nodes DROP COLUMN search_tsv;

-- +goose StatementEnd
