-- +goose Up
ALTER TABLE stories DROP CONSTRAINT stories_kind_check;
ALTER TABLE stories ADD CONSTRAINT stories_kind_check
    CHECK (kind IN ('release', 'breaking', 'security', 'deprecation', 'announcement', 'article', 'research', 'policy', 'deal'));

-- +goose Down
DELETE FROM stories WHERE kind = 'deal';
ALTER TABLE stories DROP CONSTRAINT stories_kind_check;
ALTER TABLE stories ADD CONSTRAINT stories_kind_check
    CHECK (kind IN ('release', 'breaking', 'security', 'deprecation', 'announcement', 'article', 'research', 'policy'));
