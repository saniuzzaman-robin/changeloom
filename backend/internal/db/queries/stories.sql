-- name: ListTimeline :many
-- Every story in the window, ranked by the user's interest: tier 0 is tagged with a followed
-- topic (or a descendant of one), tier 1 with a related topic (a relation neighbour or an
-- ancestor of a followed topic), tier 2 with anything else. Unread first, then by tier,
-- then newest first. Keyset pagination on (is_read, tier, published_at, id).
WITH RECURSIVE followed AS (
    SELECT ut.topic_id AS id FROM user_topics ut WHERE ut.user_id = @user_id
    UNION
    SELECT t.id FROM topics t JOIN followed f ON t.parent_id = f.id
), ancestors AS (
    SELECT t.parent_id AS id FROM topics t JOIN user_topics ut ON ut.topic_id = t.id
    WHERE ut.user_id = @user_id AND t.parent_id IS NOT NULL
    UNION
    SELECT t.parent_id FROM topics t JOIN ancestors a ON a.id = t.id WHERE t.parent_id IS NOT NULL
), related AS (
    SELECT tr.related_id AS id FROM topic_relations tr JOIN followed f ON f.id = tr.topic_id
    UNION
    SELECT tr.topic_id FROM topic_relations tr JOIN followed f ON f.id = tr.related_id
    UNION
    SELECT a.id FROM ancestors a
), ranked AS (
    SELECT s.id, s.title, s.summary, s.kind, s.severity, s.importance, s.published_at, uss.read_at,
        (uss.read_at IS NOT NULL) AS is_read,
        (ub.story_id IS NOT NULL) AS is_bookmarked,
        (CASE
            WHEN EXISTS (SELECT 1 FROM story_topics st JOIN followed f ON f.id = st.topic_id WHERE st.story_id = s.id) THEN 0
            WHEN EXISTS (SELECT 1 FROM story_topics st JOIN related r ON r.id = st.topic_id WHERE st.story_id = s.id) THEN 1
            ELSE 2
        END) AS tier
    FROM stories s
    LEFT JOIN user_story_state uss ON uss.story_id = s.id AND uss.user_id = @user_id
    LEFT JOIN user_bookmarks ub ON ub.story_id = s.id AND ub.user_id = @user_id
    WHERE s.published_at >= @since
)
SELECT r.id, r.title, r.summary, r.kind, r.severity, r.importance, r.published_at, r.read_at,
    r.is_bookmarked::boolean AS is_bookmarked,
    r.tier::integer AS tier,
    ARRAY(
        SELECT tp.slug FROM story_topics stp JOIN topics tp ON tp.id = stp.topic_id
        WHERE stp.story_id = r.id ORDER BY tp.slug
    )::text[] AS topics
FROM ranked r
WHERE sqlc.narg(cursor_id)::bigint IS NULL
    OR r.is_read > sqlc.narg(cursor_read)::boolean
    OR (r.is_read = sqlc.narg(cursor_read)::boolean AND r.tier > sqlc.narg(cursor_tier)::integer)
    OR (
        r.is_read = sqlc.narg(cursor_read)::boolean AND r.tier = sqlc.narg(cursor_tier)::integer
        AND (r.published_at, r.id) < (sqlc.narg(cursor_published_at)::timestamptz, sqlc.narg(cursor_id)::bigint)
    )
ORDER BY r.is_read ASC, r.tier ASC, r.published_at DESC, r.id DESC
LIMIT @page_size;

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
-- Full-text search over all stories, newest first. Keyset pagination on (published_at, id).
SELECT s.id, s.title, s.summary, s.kind, s.severity, s.importance, s.published_at, uss.read_at,
    (ub.story_id IS NOT NULL)::boolean AS is_bookmarked,
    ARRAY(
        SELECT tp.slug FROM story_topics stp JOIN topics tp ON tp.id = stp.topic_id
        WHERE stp.story_id = s.id ORDER BY tp.slug
    )::text[] AS topics
FROM stories s
LEFT JOIN user_story_state uss ON uss.story_id = s.id AND uss.user_id = @user_id
LEFT JOIN user_bookmarks ub ON ub.story_id = s.id AND ub.user_id = @user_id
WHERE s.search @@ websearch_to_tsquery('english', @query::text)
    AND (
        sqlc.narg(cursor_id)::bigint IS NULL
        OR (s.published_at, s.id) < (sqlc.narg(cursor_published_at)::timestamptz, sqlc.narg(cursor_id)::bigint)
    )
ORDER BY s.published_at DESC, s.id DESC
LIMIT @page_size;
