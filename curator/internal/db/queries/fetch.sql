-- name: ListFetchTopics :many
-- Every topic with what fetch planning needs: its parent, hints, follower count, whether it has
-- children and when a fetch call covering it last succeeded.
SELECT t.id,
    t.slug,
    t.name,
    t.description,
    p.slug AS parent_slug,
    COALESCE((SELECT sum(s.followers) FROM topic_stats s WHERE s.topic_id = t.id), 0)::integer AS followers,
    EXISTS (SELECT 1 FROM topics c WHERE c.parent_id = t.id) AS has_children,
    COALESCE((SELECT array_agg(h.url ORDER BY h.url) FROM topic_hints h WHERE h.topic_id = t.id), '{}')::text[] AS hints,
    -- 'epoch' when no call covering the topic has succeeded yet.
    COALESCE((
        SELECT max(fr.started_at) FROM fetch_runs fr
        WHERE fr.status = 'succeeded' AND fr.topic_ids @> ARRAY[t.id]
    ), 'epoch')::timestamptz AS last_fetched_at
FROM topics t
LEFT JOIN topics p ON p.id = t.parent_id
ORDER BY t.slug;

-- name: ListRecentStoryRefs :many
-- Recent stories in the given topics, sent to Claude so it skips what we already have.
SELECT s.title, COALESCE(MIN(ss.url), '')::text AS url
FROM stories s
LEFT JOIN story_sources ss ON ss.story_id = s.id
WHERE s.published_at >= @since
    AND EXISTS (SELECT 1 FROM story_topics st WHERE st.story_id = s.id AND st.topic_id = ANY(@topic_ids::bigint[]))
GROUP BY s.id
ORDER BY s.published_at DESC, s.id DESC
LIMIT @max_rows;

-- name: FindStoryBySourceURL :one
SELECT story_id FROM story_sources
WHERE url = ANY(@urls::text[])
    AND NOT story_id = ANY(@exclude_ids::bigint[])
ORDER BY story_id DESC
LIMIT 1;

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
INSERT INTO story_sources (story_id, url, source_name)
VALUES (@story_id, @url, @source_name)
ON CONFLICT (story_id, url) DO NOTHING;

-- name: AddStoryTopics :exec
INSERT INTO story_topics (story_id, topic_id)
SELECT @story_id, unnest(@topic_ids::bigint[])
ON CONFLICT DO NOTHING;

-- name: TouchMergedStory :exec
-- A merged item raises the story's importance and marks it for the next sync.
UPDATE stories
SET importance = GREATEST(importance, @importance::smallint),
    updated_at = now()
WHERE id = @id;

-- name: StartFetchRun :one
INSERT INTO fetch_runs (group_slug, topic_ids)
VALUES (@group_slug, @topic_ids::bigint[])
RETURNING id;

-- name: FinishFetchRun :exec
UPDATE fetch_runs
SET status = @status,
    finished_at = now(),
    stories_added = @stories_added,
    error = @error,
    cost_usd = @cost_usd
WHERE id = @id;
