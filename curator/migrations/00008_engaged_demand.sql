-- +goose Up
-- Demand pulled from each hosted env: distinct users who opened (read) or saved one of the topic's
-- stories over the last week. Views stay as a weaker signal.
ALTER TABLE topic_stats
    ADD COLUMN engaged_7d integer NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE topic_stats
    DROP COLUMN engaged_7d;
