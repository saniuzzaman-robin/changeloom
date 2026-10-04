-- +goose Up
-- Users per country pulled from each hosted env (aggregate only), so deal topics are fetched for
-- the countries people chose.
CREATE TABLE country_stats (
    env     text NOT NULL,
    country text NOT NULL CHECK (country ~ '^[A-Z]{2}$'),
    users   integer NOT NULL,
    PRIMARY KEY (env, country)
);

-- The country a fetch call was made for (deal topics); NULL for global calls.
ALTER TABLE fetch_runs ADD COLUMN country text;
CREATE INDEX fetch_runs_country_idx ON fetch_runs (country) WHERE country IS NOT NULL;

-- +goose Down
DROP INDEX fetch_runs_country_idx;
ALTER TABLE fetch_runs DROP COLUMN country;
DROP TABLE country_stats;
