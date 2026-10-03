-- name: ListHostedPendingRequests :many
-- Run against the hosted DB. Only the request itself is read, never the user.
SELECT id, text, created_at
FROM topic_requests
WHERE status = 'pending'
ORDER BY id;

-- name: ListHostedFollowerCounts :many
-- Run against the hosted DB. Aggregate follower counts per topic slug.
SELECT t.slug, count(*)::integer AS followers
FROM user_topics ut
JOIN topics t ON t.id = ut.topic_id
GROUP BY t.slug;

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
INSERT INTO topic_stats (topic_id, env, followers)
SELECT id, @env, @followers FROM topics WHERE slug = @slug;
