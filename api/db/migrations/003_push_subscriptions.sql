-- +goose Up
-- Push notification subscriptions for Web Push API (VAPID).
-- Each user can have multiple subscriptions (one per device/browser).
CREATE TABLE push_subscriptions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id   UUID NOT NULL REFERENCES schools(id),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint    TEXT NOT NULL,
    p256dh_key  TEXT NOT NULL,
    auth_key    TEXT NOT NULL,
    user_agent  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(user_id, endpoint)
);

CREATE INDEX idx_push_sub_user ON push_subscriptions(user_id);

ALTER TABLE push_subscriptions ENABLE ROW LEVEL SECURITY;
CREATE POLICY push_sub_tenant ON push_subscriptions USING (school_id = current_school_id());

GRANT SELECT, INSERT, UPDATE, DELETE ON push_subscriptions TO catalogro_app;

-- +goose Down
DROP TABLE IF EXISTS push_subscriptions CASCADE;
