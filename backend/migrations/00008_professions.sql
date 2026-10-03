-- +goose Up
-- A profession maps many-to-many onto root topics; users pick up to three.
CREATE TABLE professions (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    slug        text NOT NULL UNIQUE,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    position    smallint NOT NULL DEFAULT 0,
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE profession_topics (
    profession_id bigint NOT NULL REFERENCES professions (id) ON DELETE CASCADE,
    topic_id      bigint NOT NULL REFERENCES topics (id) ON DELETE CASCADE,
    position      smallint NOT NULL DEFAULT 0,
    PRIMARY KEY (profession_id, topic_id)
);
CREATE INDEX profession_topics_topic_id_idx ON profession_topics (topic_id);

CREATE TABLE user_professions (
    user_id       bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    profession_id bigint NOT NULL REFERENCES professions (id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, profession_id)
);
CREATE INDEX user_professions_profession_id_idx ON user_professions (profession_id);

-- One row per user and story once the story was visible in their feed.
CREATE TABLE story_views (
    user_id  bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    story_id bigint NOT NULL REFERENCES stories (id) ON DELETE CASCADE,
    seen_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, story_id)
);
CREATE INDEX story_views_story_id_idx ON story_views (story_id);
CREATE INDEX story_views_seen_at_idx ON story_views (seen_at);

ALTER TABLE stories DROP CONSTRAINT stories_kind_check;
ALTER TABLE stories ADD CONSTRAINT stories_kind_check
    CHECK (kind IN ('release', 'breaking', 'security', 'deprecation', 'announcement', 'article', 'research', 'policy'));

-- +goose Down
DELETE FROM stories WHERE kind IN ('research', 'policy');
ALTER TABLE stories DROP CONSTRAINT stories_kind_check;
ALTER TABLE stories ADD CONSTRAINT stories_kind_check
    CHECK (kind IN ('release', 'breaking', 'security', 'deprecation', 'announcement', 'article'));
DROP TABLE story_views;
DROP TABLE user_professions;
DROP TABLE profession_topics;
DROP TABLE professions;
