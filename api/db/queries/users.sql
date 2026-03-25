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
-- Returns all children linked to the given parent, with their current class info.
-- Joins through class_enrollments and classes to get the class name and education level.
-- If a child is not enrolled in any class, class fields will be NULL.
SELECT u.id, u.first_name, u.last_name, u.email, u.role,
    c.id AS class_id, c.name AS class_name, c.education_level AS class_education_level
FROM users u
JOIN parent_student_links psl ON psl.student_id = u.id
LEFT JOIN class_enrollments ce ON ce.student_id = u.id AND ce.withdrawn_at IS NULL
LEFT JOIN classes c ON c.id = ce.class_id
WHERE psl.parent_id = $1;

-- name: GetUserByEmailForLogin :one
-- Finds a user by email for login purposes. This query does NOT use RLS because
-- at login time we have no school_id context yet — the user hasn't authenticated.
-- We filter by is_active to prevent disabled accounts from logging in.
SELECT * FROM users WHERE email = $1 AND is_active = true;

-- name: UpdateUserProfile :one
-- Updates mutable profile fields for the current user.
-- Only phone and email can be changed — role, school_id, is_active are immutable.
UPDATE users SET
    email = COALESCE($2, email),
    phone = COALESCE($3, phone),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: LinkParentStudent :exec
INSERT INTO parent_student_links (school_id, parent_id, student_id, relationship, is_primary)
VALUES (current_school_id(), $1, $2, $3, $4);

-- name: SoftDeleteUser :exec
-- Soft-deletes a user by setting is_active=false and clearing PII.
-- We anonymize email, phone, first_name, last_name but keep the row
-- for audit/legal purposes (Romanian education law requires keeping student records).
UPDATE users SET
    is_active = false,
    email = NULL,
    phone = NULL,
    first_name = 'DELETED',
    last_name = 'USER',
    password_hash = NULL,
    totp_secret = NULL,
    totp_enabled = false,
    updated_at = now()
WHERE id = $1;

-- name: GetUserDataExport :one
-- Returns profile data for GDPR export. Explicitly lists columns to EXCLUDE
-- sensitive fields (password_hash, totp_secret, activation_token) at the SQL
-- level, preventing them from ever entering application memory.
SELECT id, school_id, role, email, phone, first_name, last_name,
    is_active, gdpr_consent_at, activated_at, last_login_at, created_at
FROM users WHERE id = $1;
