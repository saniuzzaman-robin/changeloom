-- name: ListHostedPendingRequests :many
-- Run against the hosted DB. Only the request itself is read, never the user.
SELECT id, text, created_at
FROM topic_requests
WHERE status = 'pending'
ORDER BY id;

-- name: ListHostedTopicDemand :many
-- Run against the hosted DB. Aggregate demand per topic slug, never per user: followers, users whose
-- profession maps to the topic's root, distinct users who opened (read) or saved one of the topic's
-- stories since @since, and distinct viewers of its stories since @since. Users who muted the topic
-- or its root don't count. Topics with no demand are left out.
SELECT d.slug, d.followers, d.profession_users, d.engaged, d.views
FROM (
    SELECT t.slug,
        (SELECT count(*) FROM user_topics ut WHERE ut.topic_id = t.id)::integer AS followers,
        (SELECT count(DISTINCT up.user_id)
            FROM profession_topics pt
            JOIN user_professions up ON up.profession_id = pt.profession_id
            WHERE pt.topic_id = COALESCE(t.parent_id, t.id)
                AND NOT EXISTS (
                    SELECT 1 FROM user_topic_mutes um
                    WHERE um.user_id = up.user_id AND um.topic_id IN (t.id, t.parent_id)
                ))::integer AS profession_users,
        (SELECT count(DISTINCT e.user_id)
            FROM story_topics st
            JOIN (
                SELECT r.user_id, r.story_id FROM user_story_state r WHERE r.read_at >= @since
                UNION ALL
                SELECT b.user_id, b.story_id FROM user_bookmarks b WHERE b.created_at >= @since
            ) e ON e.story_id = st.story_id
            WHERE st.topic_id = t.id
                AND NOT EXISTS (
                    SELECT 1 FROM user_topic_mutes um
                    WHERE um.user_id = e.user_id AND um.topic_id IN (t.id, t.parent_id)
                ))::integer AS engaged,
        (SELECT count(DISTINCT v.user_id)
            FROM story_topics st
            JOIN story_views v ON v.story_id = st.story_id
            WHERE st.topic_id = t.id AND v.seen_at >= @since
                AND NOT EXISTS (
                    SELECT 1 FROM user_topic_mutes um
                    WHERE um.user_id = v.user_id AND um.topic_id IN (t.id, t.parent_id)
                ))::integer AS views
    FROM topics t
) d
WHERE d.followers > 0 OR d.profession_users > 0 OR d.engaged > 0 OR d.views > 0;

-- name: ListHostedCountryDemand :many
-- Run against the hosted DB. Users per chosen country: aggregate only, never per user.
SELECT country::text AS country, count(*)::integer AS users
FROM users
WHERE country IS NOT NULL
GROUP BY country;

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

-- name: DeleteCountryStats :exec
DELETE FROM country_stats WHERE env = @env;

-- name: AddCountryStat :exec
INSERT INTO country_stats (env, country, users) VALUES (@env, @country, @users);

-- name: AddTopicStat :execrows
-- Zero rows when the hosted topic is not in the local catalog.
INSERT INTO topic_stats (topic_id, env, followers, profession_users, engaged_7d, views_7d)
SELECT id, @env, @followers, @profession_users, @engaged_7d, @views_7d FROM topics WHERE slug = @slug;

-- name: PruneHostedStories :execrows
-- Run against the hosted DB. Deletes at most @max_rows stories that nobody saved and that were
-- published before @max_age_before. Sources, topics, views and read state cascade.
DELETE FROM stories
WHERE id IN (
    SELECT s.id FROM stories s
    WHERE NOT EXISTS (SELECT 1 FROM user_bookmarks b WHERE b.story_id = s.id)
        AND s.published_at < @max_age_before
    ORDER BY s.id
    LIMIT @max_rows
);
