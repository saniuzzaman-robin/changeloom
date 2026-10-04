-- name: ListTimeline :many
-- Every story in the window (deals only for the user's country, or global ones), ranked by the user's interest: tier 0 is tagged with a followed
-- topic (or a descendant of one), tier 1 with an area of one of the user's professions (or a
-- descendant), tier 2 with a related topic (a relation neighbour or an ancestor of a followed
-- topic), tier 3 with anything else. Unread first, then by tier,
-- then newest first. Keyset pagination on (is_read, tier, published_at, id).
-- Tiers come from one aggregate over the in-tier topics' story_topics rows instead of per-story
-- subqueries, and topic slugs are built only for the page: per-row subplans over the whole window
-- inflated the plan cost past jit_above_cost, and JIT compilation took ~90% of the query time.
WITH RECURSIVE followed AS (
    SELECT ut.topic_id AS id FROM user_topics ut WHERE ut.user_id = @user_id
    UNION
    SELECT t.id FROM topics t JOIN followed f ON t.parent_id = f.id
), ancestors AS (
    SELECT t.parent_id AS id FROM topics t JOIN user_topics ut ON ut.topic_id = t.id
    WHERE ut.user_id = @user_id AND t.parent_id IS NOT NULL
    UNION
    SELECT t.parent_id FROM topics t JOIN ancestors a ON a.id = t.id WHERE t.parent_id IS NOT NULL
), profession AS (
    SELECT pt.topic_id AS id
    FROM user_professions up JOIN profession_topics pt ON pt.profession_id = up.profession_id
    WHERE up.user_id = @user_id
    UNION
    SELECT t.id FROM topics t JOIN profession p ON t.parent_id = p.id
), topic_tier AS (
    SELECT f.id, 0 AS tier FROM followed f
    UNION ALL
    SELECT p.id, 1 FROM profession p
    UNION ALL
    SELECT tr.related_id, 2 FROM topic_relations tr JOIN followed f ON f.id = tr.topic_id
    UNION ALL
    SELECT tr.topic_id, 2 FROM topic_relations tr JOIN followed f ON f.id = tr.related_id
    UNION ALL
    SELECT a.id, 2 FROM ancestors a
), story_tier AS (
    SELECT st.story_id, min(tt.tier) AS tier
    FROM story_topics st JOIN topic_tier tt ON tt.id = st.topic_id
    GROUP BY st.story_id
), ranked AS (
    SELECT s.id, s.title, s.summary, s.kind, s.severity, s.importance, s.published_at, uss.read_at,
        (uss.read_at IS NOT NULL) AS is_read,
        (ub.story_id IS NOT NULL) AS is_bookmarked,
        COALESCE(stt.tier, 3) AS tier
    FROM stories s
    LEFT JOIN story_tier stt ON stt.story_id = s.id
    LEFT JOIN user_story_state uss ON uss.story_id = s.id AND uss.user_id = @user_id
    LEFT JOIN user_bookmarks ub ON ub.story_id = s.id AND ub.user_id = @user_id
    WHERE s.published_at >= @since
    AND (
        s.kind <> 'deal' OR cardinality(s.countries) = 0
        OR (SELECT u.country FROM users u WHERE u.id = @user_id) = ANY(s.countries)
    )
), page AS (
    SELECT r.* FROM ranked r
    WHERE sqlc.narg(cursor_id)::bigint IS NULL
        OR r.is_read > sqlc.narg(cursor_read)::boolean
        OR (r.is_read = sqlc.narg(cursor_read)::boolean AND r.tier > sqlc.narg(cursor_tier)::integer)
        OR (
            r.is_read = sqlc.narg(cursor_read)::boolean AND r.tier = sqlc.narg(cursor_tier)::integer
            AND (r.published_at, r.id) < (sqlc.narg(cursor_published_at)::timestamptz, sqlc.narg(cursor_id)::bigint)
        )
    ORDER BY r.is_read ASC, r.tier ASC, r.published_at DESC, r.id DESC
    LIMIT @page_size
)
SELECT p.id, p.title, p.summary, p.kind, p.severity, p.importance, p.published_at, p.read_at,
    p.is_bookmarked::boolean AS is_bookmarked,
    p.tier::integer AS tier,
    ARRAY(
        SELECT tp.slug FROM story_topics stp JOIN topics tp ON tp.id = stp.topic_id
        WHERE stp.story_id = p.id ORDER BY tp.slug
    )::text[] AS topics
FROM page p
ORDER BY p.is_read ASC, p.tier ASC, p.published_at DESC, p.id DESC;

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
