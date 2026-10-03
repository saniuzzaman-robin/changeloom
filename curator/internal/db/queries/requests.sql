-- name: ListHostedPendingRequests :many
-- Run against the hosted DB. Only the request itself is read, never the user.
SELECT id, text, created_at
FROM topic_requests
WHERE status = 'pending'
ORDER BY id;

-- name: ListHostedTopicDemand :many
-- Run against the hosted DB. Aggregate demand per topic slug, never per user: followers, users whose
-- profession maps to the topic's root, and distinct viewers of the topic's stories since @since.
-- Topics with no demand are left out.
SELECT d.slug, d.followers, d.profession_users, d.views
FROM (
    SELECT t.slug,
        (SELECT count(*) FROM user_topics ut WHERE ut.topic_id = t.id)::integer AS followers,
        (SELECT count(DISTINCT up.user_id)
            FROM profession_topics pt
            JOIN user_professions up ON up.profession_id = pt.profession_id
            WHERE pt.topic_id = COALESCE(t.parent_id, t.id))::integer AS profession_users,
        (SELECT count(DISTINCT v.user_id)
            FROM story_topics st
            JOIN story_views v ON v.story_id = st.story_id
            WHERE st.topic_id = t.id AND v.seen_at >= @since)::integer AS views
    FROM topics t
) d
WHERE d.followers > 0 OR d.profession_users > 0 OR d.views > 0;

-- name: UpsertInboxRequest :execrows
-- A request already in the inbox keeps its local decision.
INSERT INTO request_inbox (env, remote_id, text, created_at)
VALUES (@env, @remote_id, @text, @created_at)
ON CONFLICT (env, remote_id) DO NOTHING;

-- name: ListPendingInbox :many
SELECT id, env, text, created_at
FROM request_inbox
WHERE status = 'pending'
ORDER BY created_at, id;

-- name: ResolveInboxRequest :execrows
UPDATE request_inbox
SET status = @status, topic_slug = @topic_slug, note = @note, resolved_at = now()
WHERE id = @id AND status = 'pending';

-- name: DeleteTopicStats :exec
DELETE FROM topic_stats WHERE env = @env;

-- name: AddTopicStat :execrows
-- Zero rows when the hosted topic is not in the local catalog.
INSERT INTO topic_stats (topic_id, env, followers, profession_users, views_7d)
SELECT id, @env, @followers, @profession_users, @views_7d FROM topics WHERE slug = @slug;

-- name: PruneHostedStories :execrows
-- Run against the hosted DB. Deletes at most @max_rows stories that nobody saved and that are
-- either older than @max_age_before or older than @grace_before with fewer than @min_viewers
-- distinct viewers. Sources, topics, views and read state cascade.
DELETE FROM stories
WHERE id IN (
    SELECT s.id FROM stories s
    WHERE NOT EXISTS (SELECT 1 FROM user_bookmarks b WHERE b.story_id = s.id)
        AND (
            s.published_at < @max_age_before
            OR (
                s.published_at < @grace_before
                AND (SELECT count(*) FROM story_views v WHERE v.story_id = s.id) < @min_viewers::bigint
            )
        )
    ORDER BY s.id
    LIMIT @max_rows
);
