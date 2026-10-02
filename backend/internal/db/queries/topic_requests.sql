-- name: CreateTopicRequest :one
INSERT INTO topic_requests (user_id, text) VALUES (@user_id, @text)
RETURNING id, text, status, note, created_at, resolved_at;

-- name: CountPendingTopicRequests :one
SELECT count(*) FROM topic_requests WHERE user_id = @user_id AND status = 'pending';

-- name: ListMyTopicRequests :many
-- The user's topic requests, newest first, with the slug of the topic each was resolved to.
SELECT tr.id, tr.text, tr.status, t.slug AS topic_slug, tr.note, tr.created_at, tr.resolved_at
FROM topic_requests tr
LEFT JOIN topics t ON t.id = tr.topic_id
WHERE tr.user_id = @user_id
ORDER BY tr.created_at DESC, tr.id DESC
LIMIT @max_rows;
