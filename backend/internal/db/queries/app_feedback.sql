-- name: CreateAppFeedback :one
INSERT INTO app_feedback (user_id, category, rating, message, app_version)
VALUES (@user_id, @category, sqlc.narg('rating'), @message, sqlc.narg('app_version'))
RETURNING id, created_at;

-- name: CountRecentAppFeedback :one
SELECT count(*) FROM app_feedback WHERE user_id = @user_id AND created_at > @since;

-- name: LockUserAppFeedback :exec
-- Serializes feedback creation per user until the transaction ends, so the daily cap holds.
SELECT pg_advisory_xact_lock(hashtextextended('app_feedback:' || @user_id::bigint, 0));
