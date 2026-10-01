-- +goose Up
ALTER TABLE ai_batches RENAME COLUMN anthropic_batch_id TO provider_batch_id;

-- +goose Down
ALTER TABLE ai_batches RENAME COLUMN provider_batch_id TO anthropic_batch_id;
