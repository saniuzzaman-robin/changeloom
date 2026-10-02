-- +goose Up
-- The RSS/AI ingestion pipeline is replaced by the curator, which writes stories directly.
ALTER TABLE story_sources DROP COLUMN raw_item_id;
ALTER TABLE stories DROP COLUMN dedupe_checked_at;
DROP TABLE raw_items;
DROP TABLE ai_batches;
DROP TABLE sources;

-- +goose Down
CREATE TABLE sources (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name              text NOT NULL UNIQUE,
    kind              text NOT NULL
        CONSTRAINT sources_kind_check CHECK (kind IN ('rss', 'gh_advisory', 'kev', 'hn', 'html', 'discovery')),
    config            jsonb NOT NULL DEFAULT '{}',
    default_topic_ids bigint[] NOT NULL DEFAULT '{}',
    poll_interval     interval NOT NULL,
    etag              text,
    last_modified     text,
    last_polled_at    timestamptz,
    enabled           boolean NOT NULL DEFAULT true
);

CREATE TABLE ai_batches (
    id                    bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    provider_batch_id     text NOT NULL UNIQUE,
    status                text NOT NULL,
    item_count            integer NOT NULL,
    input_tokens          bigint NOT NULL DEFAULT 0,
    output_tokens         bigint NOT NULL DEFAULT 0,
    cache_read_tokens     bigint NOT NULL DEFAULT 0,
    cache_creation_tokens bigint NOT NULL DEFAULT 0,
    submitted_at          timestamptz NOT NULL DEFAULT now(),
    ended_at              timestamptz,
    estimated_tokens      bigint NOT NULL DEFAULT 0
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
    fetched_at   timestamptz NOT NULL DEFAULT now(),
    attempts     smallint NOT NULL DEFAULT 0,
    ai_batch_id  bigint REFERENCES ai_batches (id) ON DELETE SET NULL
);
CREATE INDEX raw_items_status_idx ON raw_items (status);
CREATE INDEX raw_items_ai_batch_id_idx ON raw_items (ai_batch_id);

-- Existing stories count as already checked, as in 00003.
ALTER TABLE stories ADD COLUMN dedupe_checked_at timestamptz;
UPDATE stories SET dedupe_checked_at = now();
ALTER TABLE story_sources ADD COLUMN raw_item_id bigint REFERENCES raw_items (id) ON DELETE SET NULL;
