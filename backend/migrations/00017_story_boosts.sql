-- +goose Up
-- "More like this": a boosted story counts as several engagements in its topics' affinity, so the
-- user's timeline ranks those topics higher. The story itself stays where it is.
CREATE TABLE user_story_boosts (
    user_id    bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    story_id   bigint NOT NULL REFERENCES stories (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, story_id)
);
CREATE INDEX user_story_boosts_story_id_idx ON user_story_boosts (story_id);

-- +goose Down
DROP TABLE user_story_boosts;
