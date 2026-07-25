-- +goose Up
-- +goose StatementBegin
ALTER TABLE connections ADD COLUMN slug VARCHAR(255);

WITH cleaned_slugs AS (
    SELECT 
        id,
        workspace_id,
        NULLIF(
            TRIM(BOTH '-' FROM LOWER(REGEXP_REPLACE(name, '[^a-zA-Z0-9]+', '-', 'g'))),
            ''
        ) AS raw_slug,
        channel,
        created_at
    FROM connections
),
base_slugs AS (
    SELECT 
        id,
        workspace_id,
        COALESCE(raw_slug, LOWER(channel), 'conn') AS base_slug,
        created_at
    FROM cleaned_slugs
),
ranked_slugs AS (
    SELECT 
        id,
        base_slug,
        ROW_NUMBER() OVER (PARTITION BY workspace_id, base_slug ORDER BY created_at, id) AS rn
    FROM base_slugs
)
UPDATE connections c
SET slug = CASE 
    WHEN r.rn = 1 THEN r.base_slug
    ELSE r.base_slug || '-' || r.rn::text
END
FROM ranked_slugs r
WHERE c.id = r.id;

ALTER TABLE connections ALTER COLUMN slug SET NOT NULL;

CREATE UNIQUE INDEX idx_connections_workspace_slug ON connections(workspace_id, slug);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_connections_workspace_slug;
ALTER TABLE connections DROP COLUMN IF EXISTS slug;
-- +goose StatementEnd
