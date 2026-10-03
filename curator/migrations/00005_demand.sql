-- +goose Up
-- Demand pulled from each hosted env: users whose profession maps to the topic's root, and
-- distinct viewers of the topic's stories over the last week.
ALTER TABLE topic_stats
    ADD COLUMN profession_users integer NOT NULL DEFAULT 0,
    ADD COLUMN views_7d         integer NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE topic_stats
    DROP COLUMN views_7d,
    DROP COLUMN profession_users;
