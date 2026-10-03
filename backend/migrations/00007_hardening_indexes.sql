-- +goose Up
-- Push fan-out and the curator's follower counts look up user_topics by topic.
CREATE INDEX user_topics_topic_id_idx ON user_topics (topic_id);

-- Only stories that may still be pushed, so the notifier never scans the whole table.
CREATE INDEX stories_notify_pending_idx ON stories (created_at)
    WHERE notified_at IS NULL AND kind = 'security' AND severity IN ('high', 'critical');

-- +goose Down
DROP INDEX stories_notify_pending_idx;
DROP INDEX user_topics_topic_id_idx;
