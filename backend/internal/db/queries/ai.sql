-- name: ClaimPendingRawItems :many
-- Newest first, so a tight budget is spent on fresh items. Locks the rows until commit.
SELECT ri.id, ri.url, ri.title, ri.content, ri.published_at, s.name AS source_name
FROM raw_items ri
JOIN sources s ON s.id = ri.source_id
WHERE ri.status = 'pending'
ORDER BY ri.published_at DESC NULLS LAST, ri.id DESC
LIMIT @max_items
FOR UPDATE OF ri SKIP LOCKED;

-- name: GetRawItem :one
SELECT ri.id, ri.url, ri.title, ri.content, ri.published_at, ri.fetched_at, ri.status, s.name AS source_name
FROM raw_items ri
JOIN sources s ON s.id = ri.source_id
WHERE ri.id = @id;

-- name: ListSubmittedRawItems :many
SELECT ri.id, ri.url, ri.published_at, ri.fetched_at, s.name AS source_name
FROM raw_items ri
JOIN sources s ON s.id = ri.source_id
WHERE ri.ai_batch_id = @batch_id AND ri.status = 'submitted';

-- name: MarkRawItemsSubmitted :exec
UPDATE raw_items SET status = 'submitted', ai_batch_id = @batch_id WHERE id = ANY(@ids::bigint[]);

-- name: SetRawItemStatus :exec
UPDATE raw_items SET status = @status, error = @error, ai_batch_id = NULL WHERE id = @id;

-- name: RetryRawItem :one
-- Counts a failed attempt: back to pending, or failed once max_attempts is reached.
UPDATE raw_items
SET attempts = attempts + 1,
    error = @error,
    ai_batch_id = NULL,
    status = CASE WHEN attempts + 1 >= @max_attempts::smallint THEN 'failed' ELSE 'pending' END
WHERE id = @id
RETURNING status;

-- name: InsertAIBatch :one
INSERT INTO ai_batches (provider_batch_id, status, item_count, estimated_tokens)
VALUES (@provider_batch_id, @status, @item_count, @estimated_tokens)
RETURNING id;

-- name: ListOpenAIBatches :many
SELECT id, provider_batch_id FROM ai_batches WHERE ended_at IS NULL ORDER BY id;

-- name: UpdateAIBatchStatus :exec
UPDATE ai_batches SET status = @status WHERE id = @id;

-- name: FinishAIBatch :exec
UPDATE ai_batches
SET status = @status,
    input_tokens = @input_tokens,
    output_tokens = @output_tokens,
    cache_read_tokens = @cache_read_tokens,
    cache_creation_tokens = @cache_creation_tokens,
    ended_at = now()
WHERE id = @id;

-- name: TokensUsedSince :one
-- Actual usage for finished batches, the submit-time estimate for batches still running.
SELECT COALESCE(SUM(
    CASE WHEN ended_at IS NULL THEN estimated_tokens
         ELSE input_tokens + output_tokens + cache_read_tokens + cache_creation_tokens END
), 0)::bigint
FROM ai_batches
WHERE submitted_at >= @since;

-- name: FindMergeTarget :one
-- A recent story about the same CVE, or the same project and version.
SELECT id FROM stories
WHERE published_at >= @since
    AND (
        EXISTS (
            SELECT 1 FROM jsonb_array_elements_text(dedupe_keys -> 'cve_ids') c
            WHERE c = ANY(@cve_ids::text[])
        )
        OR (
            @project::text <> '' AND @version::text <> ''
            AND dedupe_keys ->> 'project' = @project::text
            AND dedupe_keys ->> 'version' = @version::text
        )
    )
ORDER BY published_at DESC, id DESC
LIMIT 1;

-- name: InsertStory :one
INSERT INTO stories (title, summary, body_md, kind, severity, importance, published_at, dedupe_keys, model, prompt_version)
VALUES (@title, @summary, @body_md, @kind, @severity, @importance, @published_at, @dedupe_keys, @model, @prompt_version)
RETURNING id;

-- name: AddStorySource :exec
INSERT INTO story_sources (story_id, raw_item_id, url, source_name)
VALUES (@story_id, @raw_item_id, @url, @source_name)
ON CONFLICT (story_id, url) DO NOTHING;

-- name: AddStoryTopics :exec
INSERT INTO story_topics (story_id, topic_id)
SELECT @story_id, unnest(@topic_ids::bigint[])
ON CONFLICT DO NOTHING;

-- name: RaiseStoryImportance :exec
UPDATE stories SET importance = GREATEST(importance, @importance::smallint) WHERE id = @id;
