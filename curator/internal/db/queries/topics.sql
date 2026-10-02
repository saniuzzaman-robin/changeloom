-- name: UpsertTopic :one
-- updated_at only moves when a field actually changes.
INSERT INTO topics (slug, name, parent_id, description)
VALUES (@slug, @name, @parent_id, @description)
ON CONFLICT (slug) DO UPDATE
SET name        = EXCLUDED.name,
    parent_id   = EXCLUDED.parent_id,
    description = EXCLUDED.description,
    updated_at  = CASE
        WHEN (topics.name, topics.parent_id, topics.description)
            IS DISTINCT FROM (EXCLUDED.name, EXCLUDED.parent_id, EXCLUDED.description)
        THEN now()
        ELSE topics.updated_at
    END
RETURNING id;

-- name: AddTopicRelation :execrows
-- Relations are symmetric and stored once, smaller id first.
INSERT INTO topic_relations (topic_id, related_id)
VALUES (LEAST(@a::bigint, @b::bigint), GREATEST(@a::bigint, @b::bigint))
ON CONFLICT DO NOTHING;

-- name: AddTopicHint :execrows
INSERT INTO topic_hints (topic_id, url)
VALUES (@topic_id, @url)
ON CONFLICT DO NOTHING;
