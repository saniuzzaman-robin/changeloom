-- name: ListFetchTopics :many
-- Every topic with what fetch planning needs: its parent, priority (@default_priority when it has
-- none), hints, demand (summed over the hosted envs), the professions its root serves, whether one
-- of them is launched, whether it or its parent is a headline, whether it has children and when a
-- fetch call covering it last succeeded and how many have succeeded since it last got a story.
SELECT t.id,
    t.slug,
    t.name,
    t.description,
    p.slug AS parent_slug,
    COALESCE(tp.priority, @default_priority::integer)::integer AS priority,
    COALESCE((SELECT sum(s.followers) FROM topic_stats s WHERE s.topic_id = t.id), 0)::integer AS followers,
    COALESCE((SELECT sum(s.profession_users) FROM topic_stats s WHERE s.topic_id = t.id), 0)::integer AS profession_users,
    COALESCE((SELECT sum(s.engaged_7d) FROM topic_stats s WHERE s.topic_id = t.id), 0)::integer AS engaged_7d,
    COALESCE((SELECT sum(s.views_7d) FROM topic_stats s WHERE s.topic_id = t.id), 0)::integer AS views_7d,
    EXISTS (SELECT 1 FROM topics c WHERE c.parent_id = t.id) AS has_children,
    COALESCE((SELECT array_agg(h.url ORDER BY h.url) FROM topic_hints h WHERE h.topic_id = t.id), '{}')::text[] AS hints,
    COALESCE((
        SELECT array_agg(pr.name ORDER BY pr.position, pr.name)
        FROM profession_topics pt JOIN professions pr ON pr.id = pt.profession_id
        WHERE pt.topic_id = COALESCE(t.parent_id, t.id)
    ), '{}')::text[] AS professions,
    EXISTS (
        SELECT 1 FROM profession_topics pt JOIN professions pr ON pr.id = pt.profession_id
        WHERE pt.topic_id = COALESCE(t.parent_id, t.id) AND pr.launched
    ) AS launched,
    (t.headline OR COALESCE(p.headline, false))::boolean AS headline,
    -- 'epoch' when no call covering the topic has succeeded yet.
    COALESCE((
        SELECT max(fr.started_at) FROM fetch_runs fr
        WHERE fr.status = 'succeeded' AND fr.country IS NULL AND fr.topic_ids @> ARRAY[t.id]
    ), 'epoch')::timestamptz AS last_fetched_at,
    -- Successful global calls covering the topic since a story last landed in it (all of them when
    -- none has), the signal for backing off topics that keep coming back empty.
    COALESCE((
        SELECT count(*) FROM fetch_runs fr
        WHERE fr.status = 'succeeded' AND fr.country IS NULL AND fr.topic_ids @> ARRAY[t.id]
            AND fr.started_at > COALESCE((
                SELECT max(s.created_at) FROM story_topics st JOIN stories s ON s.id = st.story_id
                WHERE st.topic_id = t.id
            ), 'epoch')
    ), 0)::integer AS empty_fetches
FROM topics t
LEFT JOIN topics p ON p.id = t.parent_id
LEFT JOIN topic_priority tp ON tp.topic_id = t.id
ORDER BY t.slug;

-- name: ListCountryFetches :many
-- When a successful call last covered each topic for each country.
SELECT tid::bigint AS topic_id, fr.country::text AS country, max(fr.started_at)::timestamptz AS last_fetched_at
FROM fetch_runs fr, unnest(fr.topic_ids) AS tid
WHERE fr.status = 'succeeded' AND fr.country IS NOT NULL
GROUP BY tid, fr.country;

-- name: ListActiveCountries :many
-- Countries with users, summed over the hosted envs, the most users first.
SELECT country, sum(users)::integer AS users
FROM country_stats
GROUP BY country
ORDER BY sum(users) DESC, country
LIMIT @max_rows;

-- name: ListTopicRelations :many
-- Both directions of every related pair.
SELECT a.slug AS slug, b.slug AS related_slug
FROM topic_relations r
JOIN topics a ON a.id = r.topic_id
JOIN topics b ON b.id = r.related_id
UNION ALL
SELECT b.slug, a.slug
FROM topic_relations r
JOIN topics a ON a.id = r.topic_id
JOIN topics b ON b.id = r.related_id;

-- name: ListRecentStoryRefs :many
-- Recent stories in the given topics, sent to Claude so it skips what we already have. A
-- non-empty @country keeps only stories for that country.
SELECT s.title
FROM stories s
WHERE s.published_at >= @since
    AND (@country::text = '' OR @country::text = ANY(s.countries))
    AND EXISTS (SELECT 1 FROM story_topics st WHERE st.story_id = s.id AND st.topic_id = ANY(@topic_ids::bigint[]))
ORDER BY s.published_at DESC, s.id DESC
LIMIT @max_rows;

-- name: FindStoryBySourceURL :one
SELECT story_id FROM story_sources
JOIN stories s ON s.id = story_id
WHERE url = ANY(@urls::text[])
    AND NOT story_id = ANY(@exclude_ids::bigint[])
    AND (s.countries && @countries::text[] OR (cardinality(s.countries) = 0 AND cardinality(@countries::text[]) = 0))
ORDER BY story_id DESC
LIMIT 1;

-- name: FindMergeTarget :one
-- A recent story about the same CVE, or the same project and version, for the same countries.
SELECT id FROM stories
WHERE published_at >= @since
    AND (countries && @countries::text[] OR (cardinality(countries) = 0 AND cardinality(@countries::text[]) = 0))
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
INSERT INTO stories (title, summary, body_md, kind, severity, importance, published_at, dedupe_keys, model, prompt_version, countries, key_points, action_required)
VALUES (@title, @summary, @body_md, @kind, @severity, @importance, @published_at, @dedupe_keys, @model, @prompt_version, @countries::text[], @key_points::text[], @action_required)
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
-- A merged item raises the story's importance, makes it action-required when either is, fills in
-- key points when the story has none and marks it for the next sync.
UPDATE stories
SET importance = GREATEST(importance, @importance::smallint),
    action_required = action_required OR @action_required::boolean,
    key_points = CASE WHEN cardinality(key_points) = 0 THEN @key_points::text[] ELSE key_points END,
    updated_at = now()
WHERE id = @id;

-- name: StartFetchRun :one
INSERT INTO fetch_runs (group_slug, topic_ids, country)
VALUES (@group_slug, @topic_ids::bigint[], @country)
RETURNING id;

-- name: FinishFetchRun :exec
UPDATE fetch_runs
SET status = @status,
    finished_at = now(),
    stories_added = @stories_added,
    error = @error,
    cost_usd = @cost_usd
WHERE id = @id;
