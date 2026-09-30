-- +goose Up
CREATE TABLE topics (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    slug        text NOT NULL UNIQUE,
    name        text NOT NULL,
    parent_id   bigint REFERENCES topics (id) ON DELETE RESTRICT,
    description text NOT NULL DEFAULT ''
);
CREATE INDEX topics_parent_id_idx ON topics (parent_id);

CREATE TABLE sources (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name              text NOT NULL UNIQUE,
    kind              text NOT NULL CHECK (kind IN ('rss', 'gh_advisory', 'kev', 'hn', 'html')),
    config            jsonb NOT NULL DEFAULT '{}',
    default_topic_ids bigint[] NOT NULL DEFAULT '{}',
    poll_interval     interval NOT NULL,
    etag              text,
    last_modified     text,
    last_polled_at    timestamptz,
    enabled           boolean NOT NULL DEFAULT true
);

CREATE TABLE raw_items (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    source_id    bigint NOT NULL REFERENCES sources (id) ON DELETE CASCADE,
    external_id  text,
    url          text NOT NULL,
    url_hash     bytea NOT NULL UNIQUE,
    title        text NOT NULL,
    content      text,
    published_at timestamptz,
    status       text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'submitted', 'processed', 'skipped', 'failed', 'needs_review')),
    error        text,
    fetched_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX raw_items_status_idx ON raw_items (status);

CREATE TABLE stories (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    title          text NOT NULL,
    summary        text NOT NULL,
    body_md        text NOT NULL,
    kind           text NOT NULL
        CHECK (kind IN ('release', 'breaking', 'security', 'deprecation', 'announcement', 'article')),
    severity       text CHECK (severity IN ('low', 'medium', 'high', 'critical')),
    importance     smallint NOT NULL CHECK (importance BETWEEN 1 AND 5),
    published_at   timestamptz NOT NULL,
    dedupe_keys    jsonb NOT NULL DEFAULT '{}',
    model          text NOT NULL,
    prompt_version text NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX stories_published_at_idx ON stories (published_at DESC, id DESC);

CREATE TABLE story_sources (
    story_id    bigint NOT NULL REFERENCES stories (id) ON DELETE CASCADE,
    raw_item_id bigint REFERENCES raw_items (id) ON DELETE SET NULL,
    url         text NOT NULL,
    source_name text NOT NULL,
    PRIMARY KEY (story_id, url)
);

CREATE TABLE story_topics (
    story_id bigint NOT NULL REFERENCES stories (id) ON DELETE CASCADE,
    topic_id bigint NOT NULL REFERENCES topics (id) ON DELETE CASCADE,
    PRIMARY KEY (story_id, topic_id)
);
CREATE INDEX story_topics_topic_id_idx ON story_topics (topic_id, story_id);

CREATE TABLE users (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    firebase_uid text NOT NULL UNIQUE,
    email        text,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE user_topics (
    user_id  bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    topic_id bigint NOT NULL REFERENCES topics (id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, topic_id)
);

CREATE TABLE user_story_state (
    user_id  bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    story_id bigint NOT NULL REFERENCES stories (id) ON DELETE CASCADE,
    read_at  timestamptz NOT NULL,
    PRIMARY KEY (user_id, story_id)
);

CREATE TABLE ai_batches (
    id                    bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    anthropic_batch_id    text NOT NULL UNIQUE,
    status                text NOT NULL,
    item_count            integer NOT NULL,
    input_tokens          bigint NOT NULL DEFAULT 0,
    output_tokens         bigint NOT NULL DEFAULT 0,
    cache_read_tokens     bigint NOT NULL DEFAULT 0,
    cache_creation_tokens bigint NOT NULL DEFAULT 0,
    submitted_at          timestamptz NOT NULL DEFAULT now(),
    ended_at              timestamptz
);

-- +goose Down
DROP TABLE ai_batches;
DROP TABLE user_story_state;
DROP TABLE user_topics;
DROP TABLE users;
DROP TABLE story_topics;
DROP TABLE story_sources;
DROP TABLE stories;
DROP TABLE raw_items;
DROP TABLE sources;
DROP TABLE topics;
