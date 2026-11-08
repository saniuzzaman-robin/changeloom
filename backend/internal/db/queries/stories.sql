-- name: ListTimeline :many
-- Stories in the window (deals only for the user's country, or global ones) that match the user's
-- followed topics, and nothing else: tier 0 is tagged with a followed topic (or a descendant of one);
-- professions only suggest topics at onboarding and add no stories. Unread first, by score: tier,
-- importance and severity points minus one point per decay_hours of age at as_of, minus seen_penalty
-- for a story first seen before seen_before and not saved (never for a followed topic). Read stories
-- (score 0) follow, newest first. Every input is fixed for a given as_of, so later views or new stories
-- don't move rows across a page boundary.
-- Feedback: dismissed stories are left out. A muted topic covers its descendants down to (not
-- including) a followed one, and following likewise stops at a muted descendant, so the most
-- specific choice wins. Muted topics give no tier, so a story needs a followed topic that isn't muted.
-- Optional filters: kinds (empty means all) and read_filter (NULL both, true read only, false unread only).
-- Unread stories of a followed topic (tier 0) come before every other unread story, whatever their score.
-- Keyset pagination on (is_read, is_followed, score, published_at, id).
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
), story_tier AS (
    SELECT DISTINCT st.story_id, 0 AS tier
    FROM story_topics st
    WHERE st.topic_id = ANY(ARRAY(SELECT f.id FROM followed f EXCEPT SELECT m.id FROM muted m))
), ranked AS (
    SELECT s.id, s.title, s.summary, s.kind, s.severity, s.importance, s.published_at, uss.read_at,
        (uss.read_at IS NOT NULL) AS is_read,
        (ub.story_id IS NOT NULL) AS is_bookmarked,
        stt.tier
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
), scored AS (
    -- sqlc.arg, not @name: sqlc fails to resolve the ranked alias in arithmetic with @name params.
    SELECT r.*,
        CASE WHEN r.is_read THEN 0 ELSE
            sqlc.arg(importance_weight)::float8 * r.importance
            - sqlc.arg(tier_weight)::float8 * CASE r.tier WHEN 0 THEN 0 ELSE r.tier - 1 END
            + sqlc.arg(severity_weight)::float8
                * CASE r.severity WHEN 'critical' THEN 3 WHEN 'high' THEN 2 WHEN 'medium' THEN 1 ELSE 0 END
            - extract(epoch FROM greatest(sqlc.arg(as_of)::timestamptz - r.published_at, interval '0'))
                / 3600 / sqlc.arg(decay_hours)::float8
            - CASE WHEN r.tier = 0 OR r.is_bookmarked THEN 0
                WHEN r.id IN (
                    SELECT sv.story_id FROM story_views sv
                    WHERE sv.user_id = sqlc.arg(user_id) AND sv.seen_at < sqlc.arg(seen_before)::timestamptz
                ) THEN sqlc.arg(seen_penalty)::float8
                ELSE 0 END
        END::float8 AS score,
        (NOT r.is_read AND r.tier = 0) AS is_followed
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
SELECT p.id, p.title, p.summary, p.kind, p.severity, p.importance, p.published_at, p.read_at,
    p.is_bookmarked::boolean AS is_bookmarked,
    p.tier::integer AS tier,
    p.score::float8 AS score,
    ARRAY(
        SELECT tp.slug FROM story_topics stp JOIN topics tp ON tp.id = stp.topic_id
        WHERE stp.story_id = p.id ORDER BY tp.slug
    )::text[] AS topics
FROM page p
ORDER BY p.is_read ASC, p.is_followed DESC, p.score DESC, p.published_at DESC, p.id DESC;

-- name: GetStory :one
SELECT s.id, s.title, s.summary, s.body_md, s.kind, s.severity, s.importance, s.published_at, uss.read_at,
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
SELECT s.id, s.title, s.summary, s.kind, s.severity, s.importance, s.published_at, uss.read_at,
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
-- Full-text search over all stories visible to the user, newest first; titles also match fuzzily (typos) by trigram
-- word similarity of at least 0.4. Run it after SetSearchSimilarityThreshold in the same transaction: `<%` (unlike
-- word_similarity() >= 0.4) can use stories_title_trgm_idx. Keyset pagination on (published_at, id).
SELECT s.id, s.title, s.summary, s.kind, s.severity, s.importance, s.published_at, uss.read_at,
    (ub.story_id IS NOT NULL)::boolean AS is_bookmarked,
    ARRAY(
        SELECT tp.slug FROM story_topics stp JOIN topics tp ON tp.id = stp.topic_id
        WHERE stp.story_id = s.id ORDER BY tp.slug
    )::text[] AS topics
FROM stories s
LEFT JOIN user_story_state uss ON uss.story_id = s.id AND uss.user_id = @user_id
LEFT JOIN user_bookmarks ub ON ub.story_id = s.id AND ub.user_id = @user_id
WHERE (
        s.search @@ websearch_to_tsquery('english', @query::text)
        OR @query::text <% s.title
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
