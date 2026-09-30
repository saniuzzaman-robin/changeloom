-- name: GetUserByFirebaseUID :one
SELECT id, firebase_uid, email FROM users WHERE firebase_uid = @firebase_uid;

-- name: UpsertUser :one
INSERT INTO users (firebase_uid, email)
VALUES (@firebase_uid, @email)
ON CONFLICT (firebase_uid) DO UPDATE SET email = COALESCE(EXCLUDED.email, users.email)
RETURNING id, firebase_uid, email;

-- name: ListUserTopicSlugs :many
SELECT t.slug
FROM user_topics ut
JOIN topics t ON t.id = ut.topic_id
WHERE ut.user_id = @user_id
ORDER BY t.slug;

-- name: DeleteUserTopics :exec
DELETE FROM user_topics WHERE user_id = @user_id;

-- name: InsertUserTopics :exec
INSERT INTO user_topics (user_id, topic_id)
SELECT @user_id, unnest(@topic_ids::bigint[]);
