-- +goose Up
-- Quick-take takeaways (0–3 short lines) and whether readers must upgrade, migrate or patch. Both
-- come from the curator; a constant default makes adding them a catalog-only change.
ALTER TABLE stories
    ADD COLUMN key_points text[] NOT NULL DEFAULT '{}',
    ADD COLUMN action_required boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE stories
    DROP COLUMN action_required,
    DROP COLUMN key_points;
