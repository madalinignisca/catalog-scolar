-- name: GetSourceMapping :one
SELECT * FROM source_mappings
WHERE entity_type = $1 AND entity_id = $2 AND source_system = $3;

-- name: GetSourceMappingByExternalID :one
SELECT * FROM source_mappings
WHERE source_system = $1 AND source_id = $2 AND entity_type = $3;

-- name: ListSourceMappingsByEntity :many
SELECT * FROM source_mappings
WHERE entity_type = $1 AND entity_id = $2
ORDER BY source_system;

-- name: ListSourceMappingsBySystem :many
SELECT * FROM source_mappings
WHERE source_system = $1 AND entity_type = $2
ORDER BY created_at DESC;

-- name: UpsertSourceMapping :one
INSERT INTO source_mappings (
    school_id, entity_type, entity_id, source_system, source_id, source_metadata, last_synced_at
) VALUES (
    current_school_id(), $1, $2, $3, $4, $5, now()
)
ON CONFLICT (school_id, entity_type, entity_id, source_system) DO UPDATE SET
    source_id = EXCLUDED.source_id,
    source_metadata = EXCLUDED.source_metadata,
    last_synced_at = now(),
    updated_at = now()
RETURNING *;

-- name: DeleteSourceMapping :exec
DELETE FROM source_mappings
WHERE entity_type = $1 AND entity_id = $2 AND source_system = $3;

-- name: CountSourceMappingsBySystem :one
SELECT COUNT(*) FROM source_mappings
WHERE source_system = $1 AND entity_type = $2;
