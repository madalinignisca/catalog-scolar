-- ============================================================
-- Messages (teacher-parent messaging + school announcements)
-- ============================================================
-- Romanian terms: mesaj = message, anunț = announcement

-- name: CreateMessage :one
-- Creates a new message. The school_id is set via RLS context.
INSERT INTO messages (
    school_id, sender_id, subject, body, is_announcement, target_class_id
) VALUES (
    current_school_id(), $1, $2, $3, $4, $5
) RETURNING *;

-- name: CreateMessageRecipient :exec
-- Links a message to a recipient. One row per recipient.
INSERT INTO message_recipients (school_id, message_id, recipient_id)
VALUES (current_school_id(), $1, $2);

-- name: GetMessageByID :one
-- Returns a single message by its ID.
SELECT m.*,
    u.first_name AS sender_first_name,
    u.last_name AS sender_last_name
FROM messages m
JOIN users u ON u.id = m.sender_id
WHERE m.id = $1;

-- name: ListMessagesForUser :many
-- Returns all messages where the user is a recipient, ordered by newest first.
-- Includes sender name and read status.
SELECT m.id, m.sender_id, m.subject, m.body, m.is_announcement,
    m.target_class_id, m.created_at,
    u.first_name AS sender_first_name,
    u.last_name AS sender_last_name,
    mr.read_at
FROM messages m
JOIN message_recipients mr ON mr.message_id = m.id
JOIN users u ON u.id = m.sender_id
WHERE mr.recipient_id = $1
ORDER BY m.created_at DESC
LIMIT 50;

-- name: MarkMessageRead :exec
-- Marks a message as read by the current user.
UPDATE message_recipients
SET read_at = now()
WHERE message_id = $1 AND recipient_id = $2 AND read_at IS NULL;

-- name: ListParentsForClass :many
-- Returns all parent user IDs linked to students in a given class.
-- Used to build recipient lists for class-targeted announcements.
SELECT DISTINCT psl.parent_id
FROM parent_student_links psl
JOIN class_enrollments ce ON ce.student_id = psl.student_id
WHERE ce.class_id = $1 AND ce.withdrawn_at IS NULL;
