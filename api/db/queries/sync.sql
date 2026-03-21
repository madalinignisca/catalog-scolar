-- name: CreateSyncConflict :one
INSERT INTO sync_conflicts (
    school_id, entity_type, entity_id,
    client_version, server_version, resolution
) VALUES (
    current_school_id(), $1, $2, $3, $4, $5
) RETURNING *;

-- name: InsertAuditLog :exec
INSERT INTO audit_log (
    school_id, user_id, action, entity_type, entity_id,
    old_values, new_values, ip_address, user_agent
) VALUES (
    current_school_id(), $1, $2, $3, $4, $5, $6, $7, $8
);

-- name: ListAuditLogByEntity :many
SELECT * FROM audit_log
WHERE entity_type = $1 AND entity_id = $2
ORDER BY created_at DESC
LIMIT 50;

-- name: ListAuditLogByUser :many
SELECT * FROM audit_log
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT 50;
