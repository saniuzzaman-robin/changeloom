-- +goose Up
-- Explicit feedback. A dismissed story ("Not interested") leaves the user's timeline. A muted topic
-- ("Less about") and its descendants stop ranking the user's stories, and a story whose every
-- topic is muted leaves the timeline; a followed descendant of a muted topic stays followed.
CREATE TABLE user_story_dismissals (
    user_id      bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    story_id     bigint NOT NULL REFERENCES stories (id) ON DELETE CASCADE,
    dismissed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, story_id)
);
CREATE INDEX user_story_dismissals_story_id_idx ON user_story_dismissals (story_id);

CREATE TABLE user_topic_mutes (
    user_id  bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    topic_id bigint NOT NULL REFERENCES topics (id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, topic_id)
);
-- The curator's demand counts look mutes up by topic.
CREATE INDEX user_topic_mutes_topic_id_idx ON user_topic_mutes (topic_id);

-- +goose Down
DROP TABLE user_topic_mutes;
DROP TABLE user_story_dismissals;
