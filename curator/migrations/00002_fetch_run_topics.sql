-- +goose Up
-- The topics a fetch call covered, so planning can find each topic's last successful fetch.
ALTER TABLE fetch_runs ADD COLUMN topic_ids bigint[] NOT NULL DEFAULT '{}';
CREATE INDEX fetch_runs_topic_ids_idx ON fetch_runs USING gin (topic_ids);

-- +goose Down
DROP INDEX fetch_runs_topic_ids_idx;
ALTER TABLE fetch_runs DROP COLUMN topic_ids;
