-- +goose Up
-- launched: the profession is offered at onboarding; the others stay valid for users who already
-- picked them. headline: the topic (and its descendants) reaches every user's timeline when a story
-- is important enough. Both are set by `curator sync` from the catalog.
ALTER TABLE professions ADD COLUMN launched boolean NOT NULL DEFAULT true;
ALTER TABLE topics ADD COLUMN headline boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE topics DROP COLUMN headline;
ALTER TABLE professions DROP COLUMN launched;
