-- name: ListTopics :many
SELECT t.slug, t.name, p.slug AS parent_slug, t.description, t.headline
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
SELECT p.slug, p.name, p.description, p.launched,
    COALESCE(
        array_agg(t.slug ORDER BY pt.position, t.slug) FILTER (WHERE t.id IS NOT NULL),
        '{}'
    )::text[] AS topics
FROM professions p
LEFT JOIN profession_topics pt ON pt.profession_id = p.id
LEFT JOIN topics t ON t.id = pt.topic_id
GROUP BY p.id
ORDER BY p.name, p.slug;

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

-- name: ListTopicSuggestions :many
-- Topics to suggest following. Candidates are related to a topic the user follows (topic_relations,
-- either way round) or siblings of a followed child topic. Left out: topics the user follows,
-- directly or through a followed ancestor, or mutes, directly or through a muted ancestor (the most
-- specific choice wins, as in ListTimeline), and topics with no story (tagged with the topic or a
-- child) published since @since. Best first by the affinity of the followed topic a candidate comes
-- from, then recent stories, then slug. Affinity is ListTimeline's: a topic's weighted engaged share
-- of the user's history since affinity_since, smoothed toward their overall rate by affinity_prior
-- pseudo-stories, minus that rate (affinity_weight only scales it, so it isn't needed here).
WITH RECURSIVE followed AS (
    SELECT ut.topic_id AS id FROM user_topics ut WHERE ut.user_id = @user_id
    UNION
    SELECT t.id FROM topics t JOIN followed f ON t.parent_id = f.id
    WHERE NOT EXISTS (SELECT 1 FROM user_topic_mutes um WHERE um.user_id = @user_id AND um.topic_id = t.id)
), muted AS (
    SELECT um.topic_id AS id FROM user_topic_mutes um WHERE um.user_id = @user_id
    UNION
    SELECT t.id FROM topics t JOIN muted m ON t.parent_id = m.id
    WHERE NOT EXISTS (SELECT 1 FROM user_topics ut WHERE ut.user_id = @user_id AND ut.topic_id = t.id)
), source AS (
    SELECT ut.topic_id AS id FROM user_topics ut WHERE ut.user_id = @user_id
), candidate AS (
    SELECT tr.related_id AS id, tr.topic_id AS source_id FROM topic_relations tr JOIN source so ON so.id = tr.topic_id
    UNION
    SELECT tr.topic_id, tr.related_id FROM topic_relations tr JOIN source so ON so.id = tr.related_id
    UNION
    SELECT sib.id, leaf.id FROM source so
    JOIN topics leaf ON leaf.id = so.id AND leaf.parent_id IS NOT NULL
    JOIN topics sib ON sib.parent_id = leaf.parent_id AND sib.id <> leaf.id
    WHERE NOT EXISTS (SELECT 1 FROM topics c WHERE c.parent_id = leaf.id)
), open_candidate AS (
    SELECT c.id, c.source_id FROM candidate c
    WHERE c.id NOT IN (SELECT f.id FROM followed f) AND c.id NOT IN (SELECT m.id FROM muted m)
), recent AS (
    SELECT oc.id, count(DISTINCT st.story_id) AS stories
    FROM (SELECT DISTINCT o.id FROM open_candidate o) oc
    JOIN topics t ON t.id = oc.id OR t.parent_id = oc.id
    JOIN story_topics st ON st.topic_id = t.id
    JOIN stories s ON s.id = st.story_id
    WHERE s.published_at >= @since
    GROUP BY oc.id
), history AS (
    SELECT sv.story_id FROM story_views sv
    WHERE sv.user_id = @user_id AND sv.seen_at >= @affinity_since AND sv.seen_at < sqlc.arg(as_of)::timestamptz
    UNION
    SELECT h.story_id FROM user_story_state h
    WHERE h.user_id = @user_id AND h.read_at >= @affinity_since AND h.read_at < sqlc.arg(as_of)::timestamptz
    UNION
    SELECT h.story_id FROM user_bookmarks h
    WHERE h.user_id = @user_id AND h.created_at >= @affinity_since AND h.created_at < sqlc.arg(as_of)::timestamptz
    UNION
    SELECT h.story_id FROM user_story_dismissals h
    WHERE h.user_id = @user_id AND h.dismissed_at >= @affinity_since AND h.dismissed_at < sqlc.arg(as_of)::timestamptz
    UNION
    SELECT h.story_id FROM user_story_boosts h
    WHERE h.user_id = @user_id AND h.created_at >= @affinity_since AND h.created_at < sqlc.arg(as_of)::timestamptz
), history_state AS (
    -- Joins on the (user_id, story_id) keys rather than ListTimeline's per-story EXISTS subplans:
    -- same result, but the planner hashes them instead of costing ~4 index probes per history row,
    -- which put a heavy user's plan past jit_above_cost.
    SELECT h.story_id,
        (d.story_id IS NOT NULL) AS dismissed,
        (x.story_id IS NOT NULL OR b.story_id IS NOT NULL) AS kept,
        (bo.story_id IS NOT NULL) AS boosted
    FROM history h
    LEFT JOIN user_story_dismissals d
        ON d.user_id = @user_id AND d.story_id = h.story_id AND d.dismissed_at < sqlc.arg(as_of)::timestamptz
    LEFT JOIN user_story_state x
        ON x.user_id = @user_id AND x.story_id = h.story_id AND x.read_at < sqlc.arg(as_of)::timestamptz
    LEFT JOIN user_bookmarks b
        ON b.user_id = @user_id AND b.story_id = h.story_id AND b.created_at < sqlc.arg(as_of)::timestamptz
    LEFT JOIN user_story_boosts bo
        ON bo.user_id = @user_id AND bo.story_id = h.story_id AND bo.created_at < sqlc.arg(as_of)::timestamptz
), engagement AS (
    SELECT hs.story_id,
        CASE WHEN NOT hs.dismissed AND (hs.kept OR hs.boosted) THEN 1 ELSE 0 END AS engaged,
        CASE WHEN hs.boosted AND NOT hs.dismissed THEN sqlc.arg(boost_engagements)::float8 ELSE 1 END AS weight
    FROM history_state hs
), overall AS (
    SELECT coalesce(sum(e.weight * e.engaged) / nullif(sum(e.weight), 0), 0)::float8 AS rate FROM engagement e
), source_affinity AS (
    SELECT st.topic_id,
        (sum(e.weight * e.engaged) + sqlc.arg(affinity_prior)::float8 * o.rate)
            / (sum(e.weight) + sqlc.arg(affinity_prior)::float8)
        - o.rate AS affinity
    FROM engagement e
    JOIN story_topics st ON st.story_id = e.story_id
    CROSS JOIN overall o
    WHERE st.topic_id IN (SELECT so.id FROM source so)
    GROUP BY st.topic_id, o.rate
), best AS (
    SELECT oc.id, max(coalesce(sa.affinity, 0))::float8 AS affinity
    FROM open_candidate oc
    LEFT JOIN source_affinity sa ON sa.topic_id = oc.source_id
    GROUP BY oc.id
)
SELECT t.slug, t.name, p.slug AS parent_slug, t.description, t.headline
FROM best b
JOIN recent r ON r.id = b.id
JOIN topics t ON t.id = b.id
LEFT JOIN topics p ON p.id = t.parent_id
ORDER BY b.affinity DESC, r.stories DESC, t.slug
LIMIT @page_size;
