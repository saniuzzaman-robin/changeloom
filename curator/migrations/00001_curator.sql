-- +goose Up
-- Curator-only tables. They live in the local DB next to the backend schema and are never synced.

-- Source URLs and feeds Claude should check for a topic.
CREATE TABLE topic_hints (
    topic_id bigint NOT NULL REFERENCES topics (id) ON DELETE CASCADE,
    url      text NOT NULL,
    PRIMARY KEY (topic_id, url)
);

-- Aggregate follower counts pulled from the hosted DB.
CREATE TABLE topic_stats (
    topic_id   bigint PRIMARY KEY REFERENCES topics (id) ON DELETE CASCADE,
    followers  integer NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Pending topic requests pulled from the hosted DB, and the curator's decisions on them.
-- remote_id is topic_requests.id in the hosted DB. No user IDs are copied.
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

-- Stories merged into another story locally; sync moves user state over and deletes them remotely.
CREATE TABLE story_tombstones (
    uid             uuid PRIMARY KEY,
    merged_into_uid uuid NOT NULL,
    deleted_at      timestamptz NOT NULL DEFAULT now(),
    pushed_at       timestamptz
);

-- One row per Claude fetch call.
CREATE TABLE fetch_runs (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    group_slug    text NOT NULL,
    started_at    timestamptz NOT NULL DEFAULT now(),
    finished_at   timestamptz,
    status        text NOT NULL DEFAULT 'running'
        CHECK (status IN ('running', 'succeeded', 'failed')),
    stories_added integer NOT NULL DEFAULT 0,
    error         text,
    cost_usd      double precision
);
CREATE INDEX fetch_runs_group_started_idx ON fetch_runs (group_slug, started_at DESC);

-- Sync watermarks, e.g. key 'stories'.
CREATE TABLE sync_state (
    key   text PRIMARY KEY,
    value timestamptz NOT NULL
);

-- +goose Down
DROP TABLE sync_state;
DROP TABLE fetch_runs;
DROP TABLE story_tombstones;
DROP TABLE request_inbox;
DROP TABLE topic_stats;
DROP TABLE topic_hints;
