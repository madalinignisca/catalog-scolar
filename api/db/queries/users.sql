-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1 AND school_id = current_school_id();

-- name: GetUserByActivationToken :one
-- Note: this query runs WITHOUT RLS (activation is pre-login)
SELECT u.*, s.name as school_name
FROM users u
JOIN schools s ON s.id = u.school_id
WHERE u.activation_token = $1
    AND u.activated_at IS NULL;

-- name: ListUsersBySchool :many
SELECT * FROM users
WHERE is_active = true
ORDER BY last_name, first_name;

-- name: ListPendingActivations :many
SELECT * FROM users
WHERE activated_at IS NULL AND is_active = true
ORDER BY created_at DESC;

-- name: ProvisionUser :one
INSERT INTO users (
    school_id, role, email, phone, first_name, last_name,
    provisioned_by, siiir_student_id, activation_token, activation_sent_at
) VALUES (
    current_school_id(), $1, $2, $3, $4, $5, $6, $7, $8, now()
) RETURNING *;

-- name: ActivateUser :one
UPDATE users SET
    password_hash = $2,
    activation_token = NULL,
    activated_at = now(),
    updated_at = now()
WHERE id = $1 AND activated_at IS NULL
RETURNING *;

-- name: SetGDPRConsent :exec
UPDATE users SET gdpr_consent_at = now(), updated_at = now() WHERE id = $1;

-- name: SetTOTPSecret :exec
UPDATE users SET totp_secret = $2, totp_enabled = true, updated_at = now() WHERE id = $1;

-- name: UpdateLastLogin :exec
UPDATE users SET last_login_at = now() WHERE id = $1;

-- name: ListChildrenForParent :many
SELECT u.* FROM users u
JOIN parent_student_links psl ON psl.student_id = u.id
WHERE psl.parent_id = $1;

-- name: LinkParentStudent :exec
INSERT INTO parent_student_links (school_id, parent_id, student_id, relationship, is_primary)
VALUES (current_school_id(), $1, $2, $3, $4);
