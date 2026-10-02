-- +goose Up
-- Symmetric topic relations; each pair is stored once with the smaller id first.
CREATE TABLE topic_relations (
    topic_id   bigint NOT NULL REFERENCES topics (id) ON DELETE CASCADE,
    related_id bigint NOT NULL REFERENCES topics (id) ON DELETE CASCADE,
    PRIMARY KEY (topic_id, related_id),
    CHECK (topic_id < related_id)
);
CREATE INDEX topic_relations_related_id_idx ON topic_relations (related_id);

ALTER TABLE topics ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

-- uid is the key the curator syncs stories by; updated_at drives incremental sync.
ALTER TABLE stories
    ADD COLUMN uid        uuid NOT NULL UNIQUE DEFAULT gen_random_uuid(),
    ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();
CREATE INDEX stories_updated_at_idx ON stories (updated_at);

CREATE TABLE topic_requests (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    text        text NOT NULL,
    status      text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'accepted', 'merged', 'rejected')),
    topic_id    bigint REFERENCES topics (id) ON DELETE SET NULL,
    note        text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz
);
CREATE UNIQUE INDEX topic_requests_pending_text_idx ON topic_requests (user_id, lower(text)) WHERE status = 'pending';
CREATE INDEX topic_requests_user_created_idx ON topic_requests (user_id, created_at DESC, id DESC);

-- +goose Down
DROP TABLE topic_requests;
DROP INDEX stories_updated_at_idx;
ALTER TABLE stories DROP COLUMN updated_at, DROP COLUMN uid;
ALTER TABLE topics DROP COLUMN updated_at;
DROP TABLE topic_relations;
