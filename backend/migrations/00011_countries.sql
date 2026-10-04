-- +goose Up
-- ISO 3166-1 alpha-2 country the user picked; null until they do.
ALTER TABLE users ADD COLUMN country text CHECK (country ~ '^[A-Z]{2}$');
-- Countries a story applies to; empty means global. Only deal stories carry countries.
ALTER TABLE stories ADD COLUMN countries text[] NOT NULL DEFAULT '{}'
    CHECK (array_to_string(countries, '') ~ '^([A-Z]{2})*$');
CREATE INDEX stories_countries_idx ON stories USING gin (countries);

-- +goose Down
DROP INDEX stories_countries_idx;
ALTER TABLE stories DROP COLUMN countries;
ALTER TABLE users DROP COLUMN country;
