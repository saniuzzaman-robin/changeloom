-- +goose Up
-- How much each topic is wanted when fetching, 1 (fetched first and most often) to 5. Set by
-- `curator seed` from the catalog and by `curator requests` for new topics.
CREATE TABLE topic_priority (
    topic_id bigint PRIMARY KEY REFERENCES topics (id) ON DELETE CASCADE,
    priority smallint NOT NULL CHECK (priority BETWEEN 1 AND 5)
);

-- +goose Down
DROP TABLE topic_priority;
