-- +goose Up
-- Full-text search over stories.
ALTER TABLE stories ADD COLUMN search tsvector GENERATED ALWAYS AS (
    setweight(to_tsvector('english', title), 'A')
    || setweight(to_tsvector('english', summary), 'B')
    || setweight(to_tsvector('english', body_md), 'C')
) STORED;
CREATE INDEX stories_search_idx ON stories USING gin (search);

-- Push notifications and near-duplicate checks only look at stories created after this
-- migration, so existing stories are marked as already handled.
ALTER TABLE stories
    ADD COLUMN notified_at       timestamptz,
    ADD COLUMN dedupe_checked_at timestamptz;
UPDATE stories SET notified_at = now(), dedupe_checked_at = now();

CREATE TABLE user_bookmarks (
    user_id    bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    story_id   bigint NOT NULL REFERENCES stories (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, story_id)
);
CREATE INDEX user_bookmarks_user_created_idx ON user_bookmarks (user_id, created_at DESC, story_id DESC);

CREATE TABLE device_tokens (
    token      text PRIMARY KEY,
    user_id    bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    platform   text NOT NULL CHECK (platform IN ('android', 'ios')),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX device_tokens_user_id_idx ON device_tokens (user_id);

-- Web discovery agent items are stored under a source of this kind.
ALTER TABLE sources DROP CONSTRAINT sources_kind_check;
ALTER TABLE sources ADD CONSTRAINT sources_kind_check
    CHECK (kind IN ('rss', 'gh_advisory', 'kev', 'hn', 'html', 'discovery'));

-- +goose Down
ALTER TABLE sources DROP CONSTRAINT sources_kind_check;
ALTER TABLE sources ADD CONSTRAINT sources_kind_check
    CHECK (kind IN ('rss', 'gh_advisory', 'kev', 'hn', 'html'));
DROP TABLE device_tokens;
DROP TABLE user_bookmarks;
ALTER TABLE stories DROP COLUMN dedupe_checked_at, DROP COLUMN notified_at;
DROP INDEX stories_search_idx;
ALTER TABLE stories DROP COLUMN search;
