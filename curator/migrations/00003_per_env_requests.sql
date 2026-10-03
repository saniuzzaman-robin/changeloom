-- +goose Up
-- Topic requests and follower counts come from each hosted env (staging, prod). Both tables only
-- hold data pulled by `curator requests pull`, so they are rebuilt rather than altered.
DROP TABLE request_inbox;
DROP TABLE topic_stats;

-- Follower counts per env; fetch planning sums them.
CREATE TABLE topic_stats (
    topic_id   bigint NOT NULL REFERENCES topics (id) ON DELETE CASCADE,
    env        text NOT NULL,
    followers  integer NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (topic_id, env)
);

-- remote_id is topic_requests.id in env's hosted DB. No user IDs are copied.
CREATE TABLE request_inbox (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    env         text NOT NULL,
    remote_id   bigint NOT NULL,
    text        text NOT NULL,
    created_at  timestamptz NOT NULL,
    status      text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'accepted', 'merged', 'rejected')),
    topic_slug  text,
    note        text,
    resolved_at timestamptz,
    pushed_at   timestamptz,
    UNIQUE (env, remote_id)
);
CREATE INDEX request_inbox_status_idx ON request_inbox (status);

-- +goose Down
DROP TABLE request_inbox;
DROP TABLE topic_stats;

CREATE TABLE topic_stats (
    topic_id   bigint PRIMARY KEY REFERENCES topics (id) ON DELETE CASCADE,
    followers  integer NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE request_inbox (
    remote_id   bigint PRIMARY KEY,
    text        text NOT NULL,
    created_at  timestamptz NOT NULL,
    status      text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'accepted', 'merged', 'rejected')),
    topic_slug  text,
    note        text,
    resolved_at timestamptz,
    pushed_at   timestamptz
);
CREATE INDEX request_inbox_status_idx ON request_inbox (status);
