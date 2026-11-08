-- +goose NO TRANSACTION
-- +goose Up
-- Lets the fuzzy title match in SearchStories (`<%`) use an index instead of scoring every story.
CREATE INDEX CONCURRENTLY IF NOT EXISTS stories_title_trgm_idx ON stories USING gin (title gin_trgm_ops);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS stories_title_trgm_idx;
