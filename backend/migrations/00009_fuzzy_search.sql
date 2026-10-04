-- +goose Up
-- Typo-tolerant story search: trigram similarity on titles alongside the full-text match.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- +goose Down
DROP EXTENSION IF EXISTS pg_trgm;
