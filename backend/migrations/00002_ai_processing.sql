-- +goose Up
ALTER TABLE ai_batches ADD COLUMN estimated_tokens bigint NOT NULL DEFAULT 0;

ALTER TABLE raw_items
    ADD COLUMN attempts    smallint NOT NULL DEFAULT 0,
    ADD COLUMN ai_batch_id bigint REFERENCES ai_batches (id) ON DELETE SET NULL;
CREATE INDEX raw_items_ai_batch_id_idx ON raw_items (ai_batch_id);

-- +goose Down
DROP INDEX raw_items_ai_batch_id_idx;
ALTER TABLE raw_items DROP COLUMN ai_batch_id, DROP COLUMN attempts;
ALTER TABLE ai_batches DROP COLUMN estimated_tokens;
