-- name: UpsertSource :one
INSERT INTO sources (name, kind, config, default_topic_ids, poll_interval, enabled)
VALUES (@name, @kind, @config, @default_topic_ids, make_interval(secs => @poll_seconds::bigint), @enabled)
ON CONFLICT (name) DO UPDATE
SET kind = EXCLUDED.kind,
    config = EXCLUDED.config,
    default_topic_ids = EXCLUDED.default_topic_ids,
    poll_interval = EXCLUDED.poll_interval,
    enabled = EXCLUDED.enabled
RETURNING id;

-- name: GetSource :one
SELECT id, name, kind, config, etag, last_modified, enabled
FROM sources
WHERE id = @id;

-- name: GetSourceByName :one
SELECT id, name, kind, config, etag, last_modified, enabled
FROM sources
WHERE name = @name;

-- name: ListDueSourceIDs :many
SELECT id
FROM sources
WHERE enabled AND (last_polled_at IS NULL OR last_polled_at + poll_interval <= now())
ORDER BY last_polled_at NULLS FIRST, id;

-- name: RecordSourcePoll :exec
UPDATE sources
SET etag = @etag, last_modified = @last_modified, last_polled_at = now()
WHERE id = @id;

-- name: MarkSourcePolled :exec
UPDATE sources SET last_polled_at = now() WHERE id = @id;

-- name: ExistingURLHashes :many
SELECT url_hash FROM raw_items WHERE url_hash = ANY(@hashes::bytea[]);

-- name: InsertRawItem :execrows
INSERT INTO raw_items (source_id, external_id, url, url_hash, title, content, published_at)
VALUES (@source_id, @external_id, @url, @url_hash, @title, @content, @published_at)
ON CONFLICT (url_hash) DO NOTHING;

-- name: GetSourceLastPolledAt :one
SELECT last_polled_at FROM sources WHERE id = @id;
