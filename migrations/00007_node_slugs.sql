-- +goose Up
-- +goose StatementBegin

-- URLs are based on a stable slug derived from the node's title at creation
-- time. The slug is set once on insert and never changes afterwards
-- (renaming the title leaves the URL stable, à la GitHub repo names).
-- Uniqueness is enforced by a unique index; conflicts are resolved by
-- appending -2, -3, … in Go on creation.

ALTER TABLE nodes ADD COLUMN slug TEXT;

-- Generate slugs for any existing rows. Rules mirror the Go slugify():
-- lowercase, run-collapse non-alphanumerics to '-', trim '-' from the ends,
-- empty/garbage falls back to literal 'node'. ROW_NUMBER() resolves
-- duplicates by suffixing -2, -3, … in created_at order.
WITH base AS (
    SELECT
        id,
        created_at,
        COALESCE(
            NULLIF(
                trim(both '-' from regexp_replace(lower(title), '[^a-z0-9]+', '-', 'g')),
                ''
            ),
            'node'
        ) AS bs
    FROM nodes
),
ranked AS (
    SELECT id, bs,
        ROW_NUMBER() OVER (PARTITION BY bs ORDER BY created_at, id) AS rn
    FROM base
)
UPDATE nodes n
SET slug = CASE WHEN r.rn = 1 THEN r.bs ELSE r.bs || '-' || r.rn END
FROM ranked r
WHERE n.id = r.id;

ALTER TABLE nodes ALTER COLUMN slug SET NOT NULL;
CREATE UNIQUE INDEX nodes_slug_idx ON nodes(slug);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS nodes_slug_idx;
ALTER TABLE nodes DROP COLUMN slug;

-- +goose StatementEnd
