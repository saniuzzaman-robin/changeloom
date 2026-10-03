package migrate_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/stdlib"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/dbtest"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/migrate"
)

func TestLocalTracksCuratorMigrationsSeparately(t *testing.T) {
	pool := dbtest.New(t) // already migrated once
	ctx := t.Context()

	sqlDB := stdlib.OpenDBFromPool(pool)
	defer func() { _ = sqlDB.Close() }()
	if err := migrate.Local(ctx, sqlDB, os.DirFS(filepath.Join("..", "..", "..", "backend", "migrations"))); err != nil {
		t.Fatalf("re-running migrations: %v", err)
	}

	var curator, backend int
	err := pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM curator_goose_db_version WHERE version_id > 0),
		       (SELECT count(*) FROM goose_db_version WHERE version_id = 1)`).Scan(&curator, &backend)
	if err != nil {
		t.Fatal(err)
	}
	if curator != 4 || backend != 1 {
		t.Fatalf("curator versions = %d, backend has 00001 = %d; want 4 and 1", curator, backend)
	}
	if _, err := pool.Exec(ctx, `SELECT topic_id, url FROM topic_hints LIMIT 0`); err != nil {
		t.Fatalf("topic_hints missing: %v", err)
	}
}

func TestMissingBackendDir(t *testing.T) {
	pool := dbtest.New(t)
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer func() { _ = sqlDB.Close() }()
	if err := migrate.Remote(t.Context(), sqlDB, os.DirFS(t.TempDir())); err == nil {
		t.Fatal("Remote with an empty migrations dir succeeded, want an error")
	}
}
