-- +goose NO TRANSACTION
-- +goose Up
-- Lets GetStoryThread find stories about the same project or CVE (`dedupe_keys @> …`) through an
-- index. The curator stores dedupe_keys as {"project": "", "version": "", "cve_ids": []}.
CREATE INDEX CONCURRENTLY IF NOT EXISTS stories_dedupe_keys_idx ON stories USING gin (dedupe_keys jsonb_path_ops);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS stories_dedupe_keys_idx;
