-- name: ListTopicTree :many
-- Every topic with its parent's slug, local or hosted.
SELECT t.slug, p.slug AS parent_slug
FROM topics t
LEFT JOIN topics p ON p.id = t.parent_id
ORDER BY t.slug;

-- name: ListAllProfessionSlugs :many
SELECT slug FROM professions ORDER BY slug;

-- name: ListHostedTopicsInUse :many
-- Run against a hosted DB: topics a user follows or muted, or that a topic request resolved to.
SELECT t.slug
FROM topics t
WHERE EXISTS (SELECT 1 FROM user_topics ut WHERE ut.topic_id = t.id)
    OR EXISTS (SELECT 1 FROM user_topic_mutes um WHERE um.topic_id = t.id)
    OR EXISTS (SELECT 1 FROM topic_requests r WHERE r.topic_id = t.id)
ORDER BY t.slug;

-- name: ListHostedProfessionsInUse :many
-- Run against a hosted DB: professions a user picked.
SELECT p.slug
FROM professions p
WHERE EXISTS (SELECT 1 FROM user_professions up WHERE up.profession_id = p.id)
ORDER BY p.slug;

-- name: ListRequestedTopicSlugs :many
-- Topics a request in the local inbox resolved to: created for (or merged into) a user's request.
SELECT DISTINCT topic_slug::text AS slug FROM request_inbox WHERE topic_slug IS NOT NULL ORDER BY 1;

-- name: CountStoriesOnlyIn :one
-- Stories every one of whose topics is in @slugs and that nobody saved.
SELECT count(*)::bigint
FROM stories s
WHERE EXISTS (
        SELECT 1 FROM story_topics st JOIN topics t ON t.id = st.topic_id
        WHERE st.story_id = s.id AND t.slug = ANY(@slugs::text[])
    )
    AND NOT EXISTS (
        SELECT 1 FROM story_topics st JOIN topics t ON t.id = st.topic_id
        WHERE st.story_id = s.id AND NOT (t.slug = ANY(@slugs::text[]))
    )
    AND NOT EXISTS (SELECT 1 FROM user_bookmarks b WHERE b.story_id = s.id);

-- name: DeleteStoriesOnlyIn :execrows
-- Deletes at most @max_rows of the stories CountStoriesOnlyIn counts. Sources, topics, views and
-- read state cascade.
DELETE FROM stories
WHERE id IN (
    SELECT s.id FROM stories s
    WHERE EXISTS (
            SELECT 1 FROM story_topics st JOIN topics t ON t.id = st.topic_id
            WHERE st.story_id = s.id AND t.slug = ANY(@slugs::text[])
        )
        AND NOT EXISTS (
            SELECT 1 FROM story_topics st JOIN topics t ON t.id = st.topic_id
            WHERE st.story_id = s.id AND NOT (t.slug = ANY(@slugs::text[]))
        )
        AND NOT EXISTS (SELECT 1 FROM user_bookmarks b WHERE b.story_id = s.id)
    ORDER BY s.id
    LIMIT @max_rows
);

-- name: DeleteChildTopicsBySlug :execrows
DELETE FROM topics WHERE slug = ANY(@slugs::text[]) AND parent_id IS NOT NULL;

-- name: DeleteRootTopicsBySlug :execrows
DELETE FROM topics WHERE slug = ANY(@slugs::text[]) AND parent_id IS NULL;

-- name: DeleteProfessionsBySlug :execrows
DELETE FROM professions WHERE slug = ANY(@slugs::text[]);
