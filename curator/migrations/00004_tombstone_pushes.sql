-- +goose Up
-- Tombstones are pushed to each hosted env (staging, prod) separately, so the push state moves
-- from a column to one row per (uid, env).
ALTER TABLE story_tombstones DROP COLUMN pushed_at;

CREATE TABLE tombstone_pushes (
    uid       uuid NOT NULL REFERENCES story_tombstones (uid) ON DELETE CASCADE,
    env       text NOT NULL,
    pushed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (uid, env)
);

-- +goose Down
DROP TABLE tombstone_pushes;
ALTER TABLE story_tombstones ADD COLUMN pushed_at timestamptz;
