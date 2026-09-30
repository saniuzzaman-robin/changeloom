-- name: ListTopics :many
SELECT t.slug, t.name, p.slug AS parent_slug, t.description
FROM topics t
LEFT JOIN topics p ON p.id = t.parent_id
ORDER BY t.slug;

-- name: UpsertTopic :one
INSERT INTO topics (slug, name, parent_id, description)
VALUES (@slug, @name, @parent_id, @description)
ON CONFLICT (slug) DO UPDATE
SET name = EXCLUDED.name, parent_id = EXCLUDED.parent_id, description = EXCLUDED.description
RETURNING id;

-- name: GetTopicIDsBySlugs :many
SELECT id, slug FROM topics WHERE slug = ANY(@slugs::text[]);

-- name: ListFollowedTopics :many
-- Topics followed by at least one user.
SELECT t.slug, t.name, t.description
FROM topics t
WHERE EXISTS (SELECT 1 FROM user_topics ut WHERE ut.topic_id = t.id)
ORDER BY t.slug;
