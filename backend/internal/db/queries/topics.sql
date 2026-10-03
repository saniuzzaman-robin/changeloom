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

-- name: ListProfessions :many
SELECT p.slug, p.name, p.description,
    COALESCE(
        array_agg(t.slug ORDER BY pt.position, t.slug) FILTER (WHERE t.id IS NOT NULL),
        '{}'
    )::text[] AS topics
FROM professions p
LEFT JOIN profession_topics pt ON pt.profession_id = p.id
LEFT JOIN topics t ON t.id = pt.topic_id
GROUP BY p.id
ORDER BY p.position, p.slug;

-- name: GetProfessionIDsBySlugs :many
SELECT id, slug FROM professions WHERE slug = ANY(@slugs::text[]);

-- name: UpsertProfession :one
INSERT INTO professions (slug, name, description, position)
VALUES (@slug, @name, @description, @position)
ON CONFLICT (slug) DO UPDATE
SET name = EXCLUDED.name, description = EXCLUDED.description, position = EXCLUDED.position, updated_at = now()
RETURNING id;

-- name: DeleteProfessionTopics :exec
DELETE FROM profession_topics WHERE profession_id = @profession_id;

-- name: InsertProfessionTopics :exec
INSERT INTO profession_topics (profession_id, topic_id, position)
SELECT @profession_id, t.id, (u.ord - 1)::smallint
FROM unnest(@topic_slugs::text[]) WITH ORDINALITY AS u(slug, ord)
JOIN topics t ON t.slug = u.slug;
