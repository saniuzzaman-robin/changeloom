-- name: ListTimeline :many
-- Every story in the window (deals only for the user's country, or global ones), ranked by the user's interest: tier 0 is tagged with a followed
-- topic (or a descendant of one), tier 1 (affinity) with a topic of a story the user read or saved
-- between affinity_since and as_of, tier 2 with an area of one of the user's professions (or a
-- descendant), tier 3 with a related topic (a relation neighbour or an ancestor of a followed
-- topic), tier 4 with a headline topic (or a descendant) and of at least @headline_min_importance,
-- tier 5 (explore) with anything else, and only when of at least explore_min_importance or the user
-- follows nothing and has no profession. Unread first, by score: tier, importance and severity points minus
-- one point per decay_hours of age at as_of, minus seen_penalty for a story first seen before
-- seen_before and not saved. Read stories (score 0) follow, newest first. Every input is fixed
-- for a given as_of, so later views or new stories don't move rows across a page boundary.
-- Feedback: dismissed stories are left out. A muted topic covers its descendants down to (not
-- including) a followed one, and following likewise stops at a muted descendant, so the most
-- specific choice wins. Muted topics give no tier, and a story whose every topic is muted is left out.
-- Optional filters: kinds (empty means all) and read_filter (NULL both, true read only, false unread only).
-- Keyset pagination on (is_read, score, published_at, id).
-- Tiers come from one aggregate over the in-tier topics' story_topics rows instead of per-story
-- subqueries, and topic slugs are built only for the page: per-row subplans over the whole window
-- inflated the plan cost past jit_above_cost, and JIT compilation took ~90% of the query time.
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
), ancestors AS (
    SELECT t.parent_id AS id FROM topics t JOIN user_topics ut ON ut.topic_id = t.id
    WHERE ut.user_id = @user_id AND t.parent_id IS NOT NULL
    UNION
    SELECT t.parent_id FROM topics t JOIN ancestors a ON a.id = t.id WHERE t.parent_id IS NOT NULL
), affinity AS (
    SELECT DISTINCT st.topic_id AS id
    FROM story_topics st
    JOIN (
        SELECT r.story_id FROM user_story_state r
        WHERE r.user_id = @user_id AND r.read_at >= sqlc.arg(affinity_since)::timestamptz AND r.read_at < sqlc.arg(as_of)::timestamptz
        UNION
        SELECT b.story_id FROM user_bookmarks b
        WHERE b.user_id = @user_id AND b.created_at >= sqlc.arg(affinity_since)::timestamptz AND b.created_at < sqlc.arg(as_of)::timestamptz
    ) e ON e.story_id = st.story_id
), profession AS (
    SELECT pt.topic_id AS id
    FROM user_professions up JOIN profession_topics pt ON pt.profession_id = up.profession_id
    WHERE up.user_id = @user_id
    UNION
    SELECT t.id FROM topics t JOIN profession p ON t.parent_id = p.id
), headline AS (
    SELECT t.id FROM topics t WHERE t.headline
    UNION
    SELECT t.id FROM topics t JOIN headline h ON t.parent_id = h.id
), topic_tier AS (
    SELECT f.id, 0 AS tier FROM followed f
    UNION ALL
    SELECT af.id, 1 FROM affinity af
    UNION ALL
    SELECT p.id, 2 FROM profession p
    UNION ALL
    SELECT tr.related_id, 3 FROM topic_relations tr JOIN followed f ON f.id = tr.topic_id
    UNION ALL
    SELECT tr.topic_id, 3 FROM topic_relations tr JOIN followed f ON f.id = tr.related_id
    UNION ALL
    SELECT a.id, 3 FROM ancestors a
    UNION ALL
    SELECT h.id, 4 FROM headline h
), story_tier AS (
    SELECT st.story_id, min(tt.tier) AS tier
    FROM story_topics st JOIN topic_tier tt ON tt.id = st.topic_id
    WHERE NOT EXISTS (SELECT 1 FROM muted m WHERE m.id = tt.id)
    GROUP BY st.story_id
), muted_story AS (
    -- Stories with at least one muted topic, kept when every one of their topics is muted.
    SELECT st.story_id
    FROM story_topics st LEFT JOIN muted m ON m.id = st.topic_id
    WHERE st.story_id IN (SELECT ms.story_id FROM story_topics ms JOIN muted mm ON mm.id = ms.topic_id)
    GROUP BY st.story_id
    HAVING count(m.id) = count(*)
), ranked AS (
    SELECT s.id, s.title, s.summary, s.kind, s.severity, s.importance, s.published_at, uss.read_at,
        (uss.read_at IS NOT NULL) AS is_read,
        (ub.story_id IS NOT NULL) AS is_bookmarked,
        CASE WHEN stt.tier = 4 AND s.importance < @headline_min_importance::smallint THEN 5
            ELSE COALESCE(stt.tier, 5) END AS tier,
        sv.seen_at
    FROM stories s
    LEFT JOIN story_tier stt ON stt.story_id = s.id
    LEFT JOIN user_story_state uss ON uss.story_id = s.id AND uss.user_id = @user_id
    LEFT JOIN user_bookmarks ub ON ub.story_id = s.id AND ub.user_id = @user_id
    LEFT JOIN story_views sv ON sv.story_id = s.id AND sv.user_id = @user_id
    LEFT JOIN user_story_dismissals usd ON usd.story_id = s.id AND usd.user_id = @user_id
    LEFT JOIN muted_story ms ON ms.story_id = s.id
    WHERE s.published_at >= @since
    AND usd.story_id IS NULL
    AND ms.story_id IS NULL
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
            -- Affinity sits half a step below followed, so every other tier keeps its points.
            - sqlc.arg(tier_weight)::float8 * CASE r.tier WHEN 0 THEN 0 WHEN 1 THEN 0.5 ELSE r.tier - 1 END
            + sqlc.arg(severity_weight)::float8
                * CASE r.severity WHEN 'critical' THEN 3 WHEN 'high' THEN 2 WHEN 'medium' THEN 1 ELSE 0 END
            - extract(epoch FROM greatest(sqlc.arg(as_of)::timestamptz - r.published_at, interval '0'))
                / 3600 / sqlc.arg(decay_hours)::float8
            - CASE WHEN r.seen_at < sqlc.arg(seen_before)::timestamptz AND NOT r.is_bookmarked
                THEN sqlc.arg(seen_penalty)::float8 ELSE 0 END
        END::float8 AS score
    FROM ranked r
    WHERE r.tier < 5 OR r.importance >= sqlc.arg(explore_min_importance)::smallint
        OR NOT (
            EXISTS (SELECT 1 FROM user_topics ut WHERE ut.user_id = @user_id)
            OR EXISTS (SELECT 1 FROM user_professions up WHERE up.user_id = @user_id)
        )
), page AS (
    SELECT sc.* FROM scored sc
    WHERE sqlc.narg(cursor_id)::bigint IS NULL
        OR sc.is_read > sqlc.narg(cursor_read)::boolean
        OR (
            sc.is_read = sqlc.narg(cursor_read)::boolean
            AND (sc.score, sc.published_at, sc.id) < (
                sqlc.narg(cursor_score)::float8, sqlc.narg(cursor_published_at)::timestamptz, sqlc.narg(cursor_id)::bigint
            )
        )
    ORDER BY sc.is_read ASC, sc.score DESC, sc.published_at DESC, sc.id DESC
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
ORDER BY p.is_read ASC, p.score DESC, p.published_at DESC, p.id DESC;

-- name: GetStory :one
SELECT s.id, s.title, s.summary, s.body_md, s.kind, s.severity, s.importance, s.published_at, uss.read_at,
    (ub.story_id IS NOT NULL)::boolean AS is_bookmarked,
    ARRAY(
        SELECT tp.slug FROM story_topics stp JOIN topics tp ON tp.id = stp.topic_id
        WHERE stp.story_id = s.id ORDER BY tp.slug
    )::text[] AS topics
FROM stories s
LEFT JOIN user_story_state uss ON uss.story_id = s.id AND uss.user_id = @user_id
LEFT JOIN user_bookmarks ub ON ub.story_id = s.id AND ub.user_id = @user_id
WHERE s.id = @id;

-- name: ListStorySources :many
SELECT url, source_name FROM story_sources WHERE story_id = @story_id ORDER BY source_name, url;

-- name: StoryExists :one
SELECT EXISTS (SELECT 1 FROM stories WHERE id = @id);

-- name: MarkStoryRead :exec
INSERT INTO user_story_state (user_id, story_id, read_at)
VALUES (@user_id, @story_id, now())
ON CONFLICT (user_id, story_id) DO NOTHING;

-- name: MarkStoryUnread :exec
DELETE FROM user_story_state WHERE user_id = @user_id AND story_id = @story_id;

-- name: DismissStory :exec
INSERT INTO user_story_dismissals (user_id, story_id)
VALUES (@user_id, @story_id)
ON CONFLICT (user_id, story_id) DO NOTHING;

-- name: UndismissStory :exec
DELETE FROM user_story_dismissals WHERE user_id = @user_id AND story_id = @story_id;

-- name: AddBookmark :exec
INSERT INTO user_bookmarks (user_id, story_id) VALUES (@user_id, @story_id)
ON CONFLICT (user_id, story_id) DO NOTHING;

-- name: RemoveBookmark :exec
DELETE FROM user_bookmarks WHERE user_id = @user_id AND story_id = @story_id;

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

-- name: SearchStories :many
-- Full-text search over all stories visible to the user, newest first; titles also match fuzzily (typos) by trigram
-- word similarity. Keyset pagination on (published_at, id).
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
        OR word_similarity(@query::text, s.title) >= 0.4
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
