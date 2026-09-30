-- name: ListTimeline :many
-- Stories tagged with any followed topic (or a descendant of one), unread first,
-- newest first within each group. Keyset pagination on (is_read, published_at, id).
WITH RECURSIVE followed AS (
    SELECT ut.topic_id AS id FROM user_topics ut WHERE ut.user_id = @user_id
    UNION
    SELECT t.id FROM topics t JOIN followed f ON t.parent_id = f.id
)
SELECT s.id, s.title, s.summary, s.kind, s.severity, s.importance, s.published_at, uss.read_at,
    (ub.story_id IS NOT NULL)::boolean AS is_bookmarked,
    ARRAY(
        SELECT tp.slug FROM story_topics stp JOIN topics tp ON tp.id = stp.topic_id
        WHERE stp.story_id = s.id ORDER BY tp.slug
    )::text[] AS topics
FROM stories s
LEFT JOIN user_story_state uss ON uss.story_id = s.id AND uss.user_id = @user_id
LEFT JOIN user_bookmarks ub ON ub.story_id = s.id AND ub.user_id = @user_id
WHERE s.published_at >= @since
    AND EXISTS (
        SELECT 1 FROM story_topics st JOIN followed f ON f.id = st.topic_id WHERE st.story_id = s.id
    )
    AND (
        sqlc.narg(cursor_id)::bigint IS NULL
        OR (uss.read_at IS NOT NULL) > sqlc.narg(cursor_read)::boolean
        OR (
            (uss.read_at IS NOT NULL) = sqlc.narg(cursor_read)::boolean
            AND (s.published_at, s.id) < (sqlc.narg(cursor_published_at)::timestamptz, sqlc.narg(cursor_id)::bigint)
        )
    )
ORDER BY (uss.read_at IS NOT NULL) ASC, s.published_at DESC, s.id DESC
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

-- name: ListStoriesToDedupe :many
-- Recently created stories not yet checked for near-duplicates.
SELECT id, title, summary, kind, importance FROM stories
WHERE dedupe_checked_at IS NULL AND created_at >= @since
ORDER BY id
LIMIT @max_rows;

-- name: ListDedupeCandidates :many
-- Stories that share a topic with the story and could describe the same event.
SELECT DISTINCT s.id, s.title, s.summary, s.kind, s.published_at FROM stories s
JOIN story_topics st ON st.story_id = s.id
WHERE s.id <> @story_id
    AND s.published_at >= @since
    AND st.topic_id IN (SELECT own.topic_id FROM story_topics own WHERE own.story_id = @story_id)
ORDER BY s.published_at DESC, s.id DESC
LIMIT @max_rows;

-- name: MarkStoriesDedupeChecked :exec
UPDATE stories SET dedupe_checked_at = now() WHERE id = ANY(@ids::bigint[]);

-- name: MoveStorySources :exec
INSERT INTO story_sources (story_id, raw_item_id, url, source_name)
SELECT @into_id::bigint, ss.raw_item_id, ss.url, ss.source_name FROM story_sources ss WHERE ss.story_id = @from_id
ON CONFLICT (story_id, url) DO NOTHING;

-- name: MoveStoryTopics :exec
INSERT INTO story_topics (story_id, topic_id)
SELECT @into_id::bigint, stp.topic_id FROM story_topics stp WHERE stp.story_id = @from_id
ON CONFLICT (story_id, topic_id) DO NOTHING;

-- name: MoveStoryBookmarks :exec
INSERT INTO user_bookmarks (user_id, story_id, created_at)
SELECT ub.user_id, @into_id::bigint, ub.created_at FROM user_bookmarks ub WHERE ub.story_id = @from_id
ON CONFLICT (user_id, story_id) DO NOTHING;

-- name: DeleteStory :exec
DELETE FROM stories WHERE id = @id;
