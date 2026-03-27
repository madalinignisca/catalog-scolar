-- ============================================================
-- Push subscriptions and notification queries
-- ============================================================

-- name: CreatePushSubscription :one
-- Registers a Web Push subscription for a user's device/browser.
INSERT INTO push_subscriptions (
    school_id, user_id, endpoint, p256dh_key, auth_key, user_agent
) VALUES (
    current_school_id(), $1, $2, $3, $4, $5
)
ON CONFLICT (user_id, endpoint) DO UPDATE SET
    p256dh_key = EXCLUDED.p256dh_key,
    auth_key = EXCLUDED.auth_key,
    user_agent = EXCLUDED.user_agent
RETURNING *;

-- name: DeletePushSubscription :exec
-- Removes a push subscription (user unsubscribes or changes device).
DELETE FROM push_subscriptions
WHERE user_id = $1 AND endpoint = $2;

-- name: ListPushSubscriptionsForUser :many
-- Returns all push subscriptions for a user (one per device/browser).
SELECT * FROM push_subscriptions
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: ListPushSubscriptionsForUsers :many
-- Returns all push subscriptions for a list of user IDs.
-- Used by the dispatcher to send notifications to multiple recipients.
SELECT * FROM push_subscriptions
WHERE user_id = ANY($1::uuid[])
ORDER BY user_id, created_at DESC;
