-- name: ListTimeline :many
-- Stories in the window (deals only for the user's country, or global ones) that match the user's
-- followed topics, and nothing else: tier 0 is tagged with a followed topic (or a descendant of one);
-- professions only suggest topics at onboarding and add no stories. Unread first, by score: tier,
-- importance and severity points, action_weight for an action-required story, plus the topic
-- affinity bonus, minus one point per decay_hours of age at as_of, minus seen_penalty for a story
-- first seen before seen_before and not saved. Age runs from when the story was added (created_at)
-- plus fetch_lag_weight of the time between publishing and adding it, so a story the curator found
-- days late ranks near new ones (1 counts age from published_at alone, 0 from created_at alone). Read
-- stories (score 0) follow, newest first. Every input is fixed for a given as_of, so later views or
-- new stories don't move rows across a page boundary.
-- Affinity: the user's history is every story they saw, read, saved, boosted or dismissed from
-- affinity_since to as_of; a story counts as engaged when it was read, saved or boosted and not
-- dismissed, and a boosted one ("More like this") weighs as boost_engagements engaged stories. A
-- topic's rate is its weighted engaged share, smoothed toward the user's overall rate by affinity_prior pseudo-stories, and
-- its bonus is affinity_weight times its rate minus the overall rate, so a topic without history,
-- or a user without any, adds nothing. A story takes the mean bonus of its in-tier topics.
-- Feedback: dismissed stories are left out. A muted topic covers its descendants down to (not
-- including) a followed one, and following likewise stops at a muted descendant, so the most
-- specific choice wins. Muted topics give no tier, so a story needs a followed topic that isn't muted.
-- Optional filters: kinds (empty means all), read_filter (NULL both, true read only, false unread only)
-- and action_filter (NULL both, true action-required only, false the others).
-- Unread stories of a followed topic (tier 0) come before every other unread story, whatever their score.
-- Keyset pagination on (is_read, is_followed, score, published_at, id).
-- seen: the story was on screen (story_views) before as_of; unseen_count counts the unread, unseen
-- stories of the whole filtered set, not only the page, so the first page can say how many are new.
-- Tiers come from one pass over the in-tier topics' story_topics rows instead of per-story
-- subqueries, and topic slugs are built only for the page: per-row subplans over the whole window
-- inflated the plan cost past jit_above_cost, and JIT compilation took ~90% of the query time.
-- The in-tier topics go in as an array: the recursive CTE's row estimate is ~200x too high, which made
-- the planner seq scan all stories; with the array it reads story_topics_topic_id_idx and then only the
-- matching stories by id. story_views is consulted only for a story the seen penalty could apply to.
WITH RECURSIVE followed AS (
    SELECT ut.topic_id AS id FROM user_topics ut WHERE ut.user_id = @user_id
    UNION
    SELECT t.id FROM topics t JOIN followed f ON t.parent_id = f.id
    WHERE NOT EXISTS (SELECT 1 FROM user_topic_mutes um WHERE um.user_id = @user_id AND um.topic_id = t.id)
), muted AS (
    SELECT um.topic_id AS id FROM user_topic_mutes um WHERE um.user_id = @user_id
    UNION
    SELECT t.id FROM topics t JOIN muted m ON t.parent_id = m.id
    WHERE NOT EXISTS (SELECT 1 FROM user_topics ut WHERE ut.user_id = @user_id AND ut.topic_id = t.id)
), in_tier AS (
    SELECT ARRAY(SELECT f.id FROM followed f EXCEPT SELECT m.id FROM muted m) AS ids
), history AS (
    SELECT sv.story_id FROM story_views sv
    WHERE sv.user_id = @user_id AND sv.seen_at >= @affinity_since AND sv.seen_at < sqlc.arg(as_of)::timestamptz
    UNION
    SELECT h.story_id FROM user_story_state h
    WHERE h.user_id = @user_id AND h.read_at >= @affinity_since AND h.read_at < sqlc.arg(as_of)::timestamptz
    UNION
    SELECT h.story_id FROM user_bookmarks h
    WHERE h.user_id = @user_id AND h.created_at >= @affinity_since AND h.created_at < sqlc.arg(as_of)::timestamptz
    UNION
    SELECT h.story_id FROM user_story_dismissals h
    WHERE h.user_id = @user_id AND h.dismissed_at >= @affinity_since AND h.dismissed_at < sqlc.arg(as_of)::timestamptz
    UNION
    SELECT h.story_id FROM user_story_boosts h
    WHERE h.user_id = @user_id AND h.created_at >= @affinity_since AND h.created_at < sqlc.arg(as_of)::timestamptz
), history_state AS (
    SELECT h.story_id,
        EXISTS (
            SELECT 1 FROM user_story_dismissals d
            WHERE d.user_id = @user_id AND d.story_id = h.story_id AND d.dismissed_at < sqlc.arg(as_of)::timestamptz
        ) AS dismissed,
        EXISTS (
            SELECT 1 FROM user_story_state x
            WHERE x.user_id = @user_id AND x.story_id = h.story_id AND x.read_at < sqlc.arg(as_of)::timestamptz
        ) OR EXISTS (
            SELECT 1 FROM user_bookmarks b
            WHERE b.user_id = @user_id AND b.story_id = h.story_id AND b.created_at < sqlc.arg(as_of)::timestamptz
        ) AS kept,
        EXISTS (
            SELECT 1 FROM user_story_boosts x
            WHERE x.user_id = @user_id AND x.story_id = h.story_id AND x.created_at < sqlc.arg(as_of)::timestamptz
        ) AS boosted
    FROM history h
), engagement AS (
    SELECT hs.story_id,
        CASE WHEN NOT hs.dismissed AND (hs.kept OR hs.boosted) THEN 1 ELSE 0 END AS engaged,
        CASE WHEN hs.boosted AND NOT hs.dismissed THEN sqlc.arg(boost_engagements)::float8 ELSE 1 END AS weight
    FROM history_state hs
), overall AS (
    SELECT coalesce(sum(e.weight * e.engaged) / nullif(sum(e.weight), 0), 0)::float8 AS rate FROM engagement e
), topic_bonus AS (
    SELECT st.topic_id,
        sqlc.arg(affinity_weight)::float8 * (
            (sum(e.weight * e.engaged) + sqlc.arg(affinity_prior)::float8 * o.rate)
                / (sum(e.weight) + sqlc.arg(affinity_prior)::float8)
            - o.rate
        ) AS bonus
    FROM engagement e
    JOIN story_topics st ON st.story_id = e.story_id
    CROSS JOIN overall o
    WHERE st.topic_id = ANY((SELECT it.ids FROM in_tier it)::bigint[])
    GROUP BY st.topic_id, o.rate
), story_tier AS (
    SELECT st.story_id, 0 AS tier, avg(coalesce(tb.bonus, 0))::float8 AS affinity
    FROM story_topics st
    LEFT JOIN topic_bonus tb ON tb.topic_id = st.topic_id
    WHERE st.topic_id = ANY((SELECT it.ids FROM in_tier it)::bigint[])
    GROUP BY st.story_id
), ranked AS (
    SELECT s.id, s.title, s.summary, s.kind, s.severity, s.importance, s.published_at, s.created_at, uss.read_at,
        s.key_points, s.action_required,
        (uss.read_at IS NOT NULL) AS is_read,
        (ub.story_id IS NOT NULL) AS is_bookmarked,
        s.id IN (
            SELECT sv.story_id FROM story_views sv
            WHERE sv.user_id = @user_id AND sv.seen_at < sqlc.arg(as_of)::timestamptz
        ) AS is_seen,
        stt.tier, stt.affinity
    FROM stories s
    JOIN story_tier stt ON stt.story_id = s.id
    LEFT JOIN user_story_state uss ON uss.story_id = s.id AND uss.user_id = @user_id
    LEFT JOIN user_bookmarks ub ON ub.story_id = s.id AND ub.user_id = @user_id
    LEFT JOIN user_story_dismissals usd ON usd.story_id = s.id AND usd.user_id = @user_id
    WHERE s.published_at >= @since
    AND usd.story_id IS NULL
    AND (
        s.kind <> 'deal' OR cardinality(s.countries) = 0
        OR (SELECT u.country FROM users u WHERE u.id = @user_id) = ANY(s.countries)
    )
    AND (cardinality(@kinds::text[]) = 0 OR s.kind = ANY(@kinds::text[]))
    AND (sqlc.narg(read_filter)::boolean IS NULL OR (uss.read_at IS NOT NULL) = sqlc.narg(read_filter)::boolean)
    AND (sqlc.narg(action_filter)::boolean IS NULL OR s.action_required = sqlc.narg(action_filter)::boolean)
), scored AS (
    -- sqlc.arg, not @name: sqlc fails to resolve the ranked alias in arithmetic with @name params.
    SELECT r.*,
        CASE WHEN r.is_read THEN 0 ELSE
            sqlc.arg(importance_weight)::float8 * r.importance
            - sqlc.arg(tier_weight)::float8 * CASE r.tier WHEN 0 THEN 0 ELSE r.tier - 1 END
            + r.affinity
            + sqlc.arg(severity_weight)::float8
                * CASE r.severity WHEN 'critical' THEN 3 WHEN 'high' THEN 2 WHEN 'medium' THEN 1 ELSE 0 END
            + CASE WHEN r.action_required THEN sqlc.arg(action_weight)::float8 ELSE 0 END
            -- Only the total is clamped, so a weight of 1 gives exactly as_of - published_at, even for a
            -- story added after as_of. Interval arithmetic is exact to the microsecond, so equal ages tie.
            - extract(epoch FROM greatest(
                sqlc.arg(as_of)::timestamptz - r.created_at
                    + sqlc.arg(fetch_lag_weight)::float8 * greatest(r.created_at - r.published_at, interval '0'),
                interval '0'
            )) / 3600 / sqlc.arg(decay_hours)::float8
            - CASE WHEN r.is_bookmarked THEN 0
                WHEN r.id IN (
                    SELECT sv.story_id FROM story_views sv
                    WHERE sv.user_id = sqlc.arg(user_id) AND sv.seen_at < sqlc.arg(seen_before)::timestamptz
                ) THEN sqlc.arg(seen_penalty)::float8
                ELSE 0 END
        END::float8 AS score,
        (NOT r.is_read AND r.tier = 0) AS is_followed,
        count(*) FILTER (WHERE NOT r.is_read AND NOT r.is_seen) OVER () AS unseen_count
    FROM ranked r
), page AS (
    SELECT sc.* FROM scored sc
    WHERE sqlc.narg(cursor_id)::bigint IS NULL
        OR sc.is_read > sqlc.narg(cursor_read)::boolean
        OR (
            sc.is_read = sqlc.narg(cursor_read)::boolean
            AND (
                sc.is_followed < sqlc.narg(cursor_followed)::boolean
                OR (
                    sc.is_followed = sqlc.narg(cursor_followed)::boolean
                    AND (sc.score, sc.published_at, sc.id) < (
                        sqlc.narg(cursor_score)::float8, sqlc.narg(cursor_published_at)::timestamptz, sqlc.narg(cursor_id)::bigint
                    )
                )
            )
        )
    ORDER BY sc.is_read ASC, sc.is_followed DESC, sc.score DESC, sc.published_at DESC, sc.id DESC
    LIMIT @page_size
)
SELECT p.id, p.title, p.summary, p.kind, p.severity, p.importance, p.published_at, p.created_at AS added_at, p.read_at,
    p.key_points, p.action_required,
    p.is_bookmarked::boolean AS is_bookmarked,
    p.tier::integer AS tier,
    p.score::float8 AS score,
    p.is_seen::boolean AS seen,
    p.unseen_count::bigint AS unseen_count,
    ARRAY(
        SELECT tp.slug FROM story_topics stp JOIN topics tp ON tp.id = stp.topic_id
        WHERE stp.story_id = p.id ORDER BY tp.slug
    )::text[] AS topics
FROM page p
ORDER BY p.is_read ASC, p.is_followed DESC, p.score DESC, p.published_at DESC, p.id DESC;

-- name: GetStory :one
SELECT s.id, s.title, s.summary, s.body_md, s.kind, s.severity, s.importance, s.published_at, s.created_at AS added_at, uss.read_at,
    s.key_points, s.action_required,
    coalesce(s.dedupe_keys ->> 'version', '')::text AS version,
    (ub.story_id IS NOT NULL)::boolean AS is_bookmarked,
    ARRAY(
        SELECT tp.slug FROM story_topics stp JOIN topics tp ON tp.id = stp.topic_id
        WHERE stp.story_id = s.id ORDER BY tp.slug
    )::text[] AS topics,
    -- Sources as parallel arrays in the same (source_name, url) order; url is unique per story.
    ARRAY(SELECT ss.url FROM story_sources ss WHERE ss.story_id = s.id ORDER BY ss.source_name, ss.url)::text[]
        AS source_urls,
    ARRAY(SELECT ss.source_name FROM story_sources ss WHERE ss.story_id = s.id ORDER BY ss.source_name, ss.url)::text[]
        AS source_names
FROM stories s
LEFT JOIN user_story_state uss ON uss.story_id = s.id AND uss.user_id = @user_id
LEFT JOIN user_bookmarks ub ON ub.story_id = s.id AND ub.user_id = @user_id
WHERE s.id = @id;

-- name: GetStoryThread :many
-- Other stories about the same project (dedupe_keys.project) or sharing a CVE id with story id,
-- published since since, newest first: the project's changelog. Left out are stories the user
-- dismissed, stories whose every topic the user muted (muted as in ListTimeline: a mute stops at a
-- followed descendant), and deals outside the user's country. The planner overestimates `@>`
-- matches ~100x, so with the filters, ORDER BY and LIMIT on stories it walks
-- stories_published_at_idx over the whole window. Instead the materialized thread CTE reads only
-- stories_dedupe_keys_idx matches (needles go in as an array) and filters them per story; only the
-- page joins the rest.
WITH RECURSIVE muted AS (
    SELECT um.topic_id AS id FROM user_topic_mutes um WHERE um.user_id = @user_id
    UNION
    SELECT t.id FROM topics t JOIN muted m ON t.parent_id = m.id
    WHERE NOT EXISTS (SELECT 1 FROM user_topics ut WHERE ut.user_id = @user_id AND ut.topic_id = t.id)
), muted_ids AS (
    SELECT ARRAY(SELECT m.id FROM muted m) AS ids
), needles AS (
    SELECT ARRAY(
        SELECT jsonb_build_object('project', a.dedupe_keys ->> 'project')
        WHERE coalesce(a.dedupe_keys ->> 'project', '') <> ''
        UNION ALL
        SELECT jsonb_build_object('cve_ids', jsonb_build_array(c.id))
        FROM jsonb_array_elements_text(
            CASE WHEN jsonb_typeof(a.dedupe_keys -> 'cve_ids') = 'array' THEN a.dedupe_keys -> 'cve_ids' ELSE '[]' END
        ) AS c (id)
        WHERE c.id <> ''
    ) AS keys
    FROM stories a WHERE a.id = @id
), thread AS MATERIALIZED (
    SELECT c.id, c.published_at FROM stories c
    WHERE c.dedupe_keys @> ANY((SELECT n.keys FROM needles n)::jsonb[])
        AND c.id <> @id
        AND c.published_at >= @since
        AND NOT EXISTS (
            SELECT 1 FROM user_story_dismissals usd WHERE usd.user_id = @user_id AND usd.story_id = c.id
        )
        -- A per-story subplan: an EXISTS here becomes a semi join that hashes every story_topics row.
        AND NOT ARRAY(SELECT st.topic_id FROM story_topics st WHERE st.story_id = c.id)
            <@ (SELECT mi.ids FROM muted_ids mi)::bigint[]
        AND (
            c.kind <> 'deal' OR cardinality(c.countries) = 0
            OR (SELECT u.country FROM users u WHERE u.id = @user_id) = ANY(c.countries)
        )
), page AS (
    SELECT th.id FROM thread th ORDER BY th.published_at DESC, th.id DESC LIMIT @page_size
)
SELECT s.id, s.title, s.summary, s.kind, s.severity, s.importance, s.published_at, uss.read_at,
    s.key_points, s.action_required,
    coalesce(s.dedupe_keys ->> 'version', '')::text AS version,
    (ub.story_id IS NOT NULL)::boolean AS is_bookmarked,
    ARRAY(
        SELECT tp.slug FROM story_topics stp JOIN topics tp ON tp.id = stp.topic_id
        WHERE stp.story_id = s.id ORDER BY tp.slug
    )::text[] AS topics
FROM page p
JOIN stories s ON s.id = p.id
LEFT JOIN user_story_state uss ON uss.story_id = s.id AND uss.user_id = @user_id
LEFT JOIN user_bookmarks ub ON ub.story_id = s.id AND ub.user_id = @user_id
ORDER BY s.published_at DESC, s.id DESC;

-- The single-story writes below return whether the story exists, so the handler can 404 without a
-- separate round trip; they write nothing when it doesn't.

-- name: MarkStoryRead :one
WITH story AS (SELECT id FROM stories WHERE id = @story_id), ins AS (
    INSERT INTO user_story_state (user_id, story_id, read_at)
    SELECT @user_id, story.id, now() FROM story
    ON CONFLICT (user_id, story_id) DO NOTHING
)
SELECT EXISTS (SELECT 1 FROM story);

-- name: MarkStoryUnread :one
WITH del AS (DELETE FROM user_story_state WHERE user_id = @user_id AND story_id = @story_id)
SELECT EXISTS (SELECT 1 FROM stories WHERE id = @story_id);

-- name: DismissStory :one
WITH story AS (SELECT id FROM stories WHERE id = @story_id), ins AS (
    INSERT INTO user_story_dismissals (user_id, story_id)
    SELECT @user_id, story.id FROM story
    ON CONFLICT (user_id, story_id) DO NOTHING
)
SELECT EXISTS (SELECT 1 FROM story);

-- name: UndismissStory :one
WITH del AS (DELETE FROM user_story_dismissals WHERE user_id = @user_id AND story_id = @story_id)
SELECT EXISTS (SELECT 1 FROM stories WHERE id = @story_id);

-- name: BoostStory :one
WITH story AS (SELECT id FROM stories WHERE id = @story_id), ins AS (
    INSERT INTO user_story_boosts (user_id, story_id)
    SELECT @user_id, story.id FROM story
    ON CONFLICT (user_id, story_id) DO NOTHING
)
SELECT EXISTS (SELECT 1 FROM story);

-- name: UnboostStory :one
WITH del AS (DELETE FROM user_story_boosts WHERE user_id = @user_id AND story_id = @story_id)
SELECT EXISTS (SELECT 1 FROM stories WHERE id = @story_id);

-- name: AddBookmark :one
WITH story AS (SELECT id FROM stories WHERE id = @story_id), ins AS (
    INSERT INTO user_bookmarks (user_id, story_id)
    SELECT @user_id, story.id FROM story
    ON CONFLICT (user_id, story_id) DO NOTHING
)
SELECT EXISTS (SELECT 1 FROM story);

-- name: RemoveBookmark :one
WITH del AS (DELETE FROM user_bookmarks WHERE user_id = @user_id AND story_id = @story_id)
SELECT EXISTS (SELECT 1 FROM stories WHERE id = @story_id);

-- name: AddBookmarks :exec
-- Batch form of AddBookmark; ids that are not stories are skipped by the join.
INSERT INTO user_bookmarks (user_id, story_id)
SELECT @user_id, s.id FROM stories s WHERE s.id = ANY(@ids::bigint[])
ON CONFLICT (user_id, story_id) DO NOTHING;

-- name: RemoveBookmarks :exec
DELETE FROM user_bookmarks WHERE user_id = @user_id AND story_id = ANY(@ids::bigint[]);

-- name: ListBookmarks :many
-- The user's bookmarked stories, most recently bookmarked first. Keyset pagination on
-- (bookmarked_at, id).
SELECT s.id, s.title, s.summary, s.kind, s.severity, s.importance, s.published_at, s.created_at AS added_at, uss.read_at,
    s.key_points, s.action_required,
    ub.created_at AS bookmarked_at,
    ARRAY(
        SELECT tp.slug FROM story_topics stp JOIN topics tp ON tp.id = stp.topic_id
        WHERE stp.story_id = s.id ORDER BY tp.slug
    )::text[] AS topics
FROM user_bookmarks ub
JOIN stories s ON s.id = ub.story_id
LEFT JOIN user_story_state uss ON uss.story_id = s.id AND uss.user_id = @user_id
WHERE ub.user_id = @user_id
    AND (
        sqlc.narg(cursor_id)::bigint IS NULL
        OR (ub.created_at, s.id) < (sqlc.narg(cursor_time)::timestamptz, sqlc.narg(cursor_id)::bigint)
    )
ORDER BY ub.created_at DESC, s.id DESC
LIMIT @page_size;

-- name: SetSearchSimilarityThreshold :exec
-- Sets the word similarity SearchStories' `<%` needs, for the current transaction only: a session SET is unsafe behind
-- a transaction pooler.
SELECT set_config('pg_trgm.word_similarity_threshold', '0.4', true);

-- name: SearchStories :many
-- Full-text search over all stories visible to the user, newest first. Only when no visible story matches the words
-- do titles match fuzzily (typos) by trigram word similarity of at least 0.4; otherwise a near word ("click" for
-- "ClickHouse") would bury the real matches. Run it after SetSearchSimilarityThreshold in the same transaction: `<%`
-- (unlike word_similarity() >= 0.4) can use stories_title_trgm_idx. Keyset pagination on (published_at, id).
WITH word_match AS (
    SELECT EXISTS (
        SELECT 1 FROM stories ws
        WHERE ws.search @@ websearch_to_tsquery('english', @query::text)
            AND (
                ws.kind <> 'deal' OR cardinality(ws.countries) = 0
                OR (SELECT u.country FROM users u WHERE u.id = @user_id) = ANY(ws.countries)
            )
    ) AS found
)
SELECT s.id, s.title, s.summary, s.kind, s.severity, s.importance, s.published_at, s.created_at AS added_at, uss.read_at,
    s.key_points, s.action_required,
    (ub.story_id IS NOT NULL)::boolean AS is_bookmarked,
    ARRAY(
        SELECT tp.slug FROM story_topics stp JOIN topics tp ON tp.id = stp.topic_id
        WHERE stp.story_id = s.id ORDER BY tp.slug
    )::text[] AS topics
FROM stories s
LEFT JOIN user_story_state uss ON uss.story_id = s.id AND uss.user_id = @user_id
LEFT JOIN user_bookmarks ub ON ub.story_id = s.id AND ub.user_id = @user_id
CROSS JOIN word_match
WHERE (
        s.search @@ websearch_to_tsquery('english', @query::text)
        OR (NOT word_match.found AND @query::text <% s.title)
    )
    AND (
        s.kind <> 'deal' OR cardinality(s.countries) = 0
        OR (SELECT u.country FROM users u WHERE u.id = @user_id) = ANY(s.countries)
    )
    AND (
        sqlc.narg(cursor_id)::bigint IS NULL
        OR (s.published_at, s.id) < (sqlc.narg(cursor_published_at)::timestamptz, sqlc.narg(cursor_id)::bigint)
    )
ORDER BY s.published_at DESC, s.id DESC
LIMIT @page_size;

-- name: RecordStoryViews :exec
-- Idempotent; ids that are not stories are skipped by the join.
INSERT INTO story_views (user_id, story_id)
SELECT @user_id, s.id FROM stories s WHERE s.id = ANY(@ids::bigint[])
ON CONFLICT DO NOTHING;
