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

-- name: DeleteUserTopicsByIDs :exec
DELETE FROM user_topics WHERE user_id = @user_id AND topic_id = ANY(@topic_ids::bigint[]);

-- name: ListUserMutedTopicSlugs :many
SELECT t.slug
FROM user_topic_mutes um
JOIN topics t ON t.id = um.topic_id
WHERE um.user_id = @user_id
ORDER BY t.slug;

-- name: DeleteUserTopicMutes :exec
DELETE FROM user_topic_mutes WHERE user_id = @user_id;

-- name: InsertUserTopicMutes :exec
INSERT INTO user_topic_mutes (user_id, topic_id)
SELECT @user_id, unnest(@topic_ids::bigint[]);

-- name: DeleteUserTopicMutesByIDs :exec
DELETE FROM user_topic_mutes WHERE user_id = @user_id AND topic_id = ANY(@topic_ids::bigint[]);

-- name: GetUserStats :one
SELECT
    (SELECT count(*) FROM user_bookmarks ub WHERE ub.user_id = @user_id)::bigint AS saved,
    (SELECT count(*) FROM user_story_state uss WHERE uss.user_id = @user_id)::bigint AS read;

-- name: DeleteUser :exec
-- Deleting the user cascades to their follows, mutes, read and dismissed state, bookmarks, devices and topic
-- requests.
DELETE FROM users WHERE id = @id;

-- name: ListUserProfessionSlugs :many
SELECT p.slug
FROM user_professions up
JOIN professions p ON p.id = up.profession_id
WHERE up.user_id = @user_id
ORDER BY p.position, p.slug;

-- name: DeleteUserProfessions :exec
DELETE FROM user_professions WHERE user_id = @user_id;

-- name: InsertUserProfessions :exec
INSERT INTO user_professions (user_id, profession_id)
SELECT @user_id, unnest(@profession_ids::bigint[]);

-- name: GetUserCountry :one
SELECT country FROM users WHERE id = @user_id;

-- name: SetUserCountry :exec
UPDATE users SET country = sqlc.narg(country) WHERE id = @user_id;
