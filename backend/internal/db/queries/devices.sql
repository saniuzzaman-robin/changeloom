-- name: UpsertDeviceToken :exec
-- A token belongs to one user: signing in as someone else on the same device moves it.
INSERT INTO device_tokens (token, user_id, platform)
VALUES (@token, @user_id, @platform)
ON CONFLICT (token) DO UPDATE SET user_id = EXCLUDED.user_id, platform = EXCLUDED.platform, updated_at = now();

-- name: DeleteDeviceToken :exec
DELETE FROM device_tokens WHERE token = @token AND user_id = @user_id;

-- name: DeleteDeviceTokens :exec
DELETE FROM device_tokens WHERE token = ANY(@tokens::text[]);

-- name: ClaimStoryToNotify :one
-- Locks the oldest recent high-severity security story that has not been pushed yet, skipping
-- stories another call holds and the ones in skip_ids. The conditions match stories_notify_pending_idx.
SELECT id, title, summary FROM stories
WHERE notified_at IS NULL
    AND kind = 'security' AND severity IN ('high', 'critical')
    AND created_at >= @since
    AND NOT (id = ANY(@skip_ids::bigint[]))
ORDER BY id
LIMIT 1
FOR UPDATE SKIP LOCKED;

-- name: CountStoriesToNotify :one
SELECT count(*) FROM stories
WHERE notified_at IS NULL
    AND kind = 'security' AND severity IN ('high', 'critical')
    AND created_at >= @since;

-- name: ListDeviceTokensForStory :many
-- Device tokens of users who follow a topic of the story or an ancestor of one.
WITH RECURSIVE story_scope AS (
    SELECT st.topic_id AS id FROM story_topics st WHERE st.story_id = @story_id
    UNION
    SELECT t.parent_id FROM topics t JOIN story_scope s ON t.id = s.id WHERE t.parent_id IS NOT NULL
)
SELECT DISTINCT dt.token
FROM device_tokens dt
JOIN user_topics ut ON ut.user_id = dt.user_id
WHERE ut.topic_id IN (SELECT id FROM story_scope);

-- name: MarkStoryNotified :exec
UPDATE stories SET notified_at = now() WHERE id = @id;
