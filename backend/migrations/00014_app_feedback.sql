-- +goose Up
-- Free-text feedback and reviews users send from the app, kept for product decisions. Rows go with the account.
CREATE TABLE app_feedback (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    category    text NOT NULL CHECK (category IN ('bug', 'idea', 'praise', 'other')),
    rating      smallint CHECK (rating BETWEEN 1 AND 5),
    message     text NOT NULL,
    app_version text,
    created_at  timestamptz NOT NULL DEFAULT now()
);
-- The per-user daily cap counts a user's recent rows.
CREATE INDEX app_feedback_user_created_idx ON app_feedback (user_id, created_at DESC);
-- Reviewing the newest feedback across users.
CREATE INDEX app_feedback_created_idx ON app_feedback (created_at DESC, id DESC);

-- +goose Down
DROP TABLE app_feedback;
