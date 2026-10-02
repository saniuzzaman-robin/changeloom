// Package dbtest provides throwaway, fully migrated local curator databases for tests.
package dbtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/migrate"
)

// New creates an empty database on the server in TEST_DATABASE_URL (falling back to
// LOCAL_DATABASE_URL), applies the backend and curator migrations and drops it when the test ends.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := t.Context()

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = os.Getenv("LOCAL_DATABASE_URL")
	}
	if url == "" {
		t.Fatal("dbtest: set TEST_DATABASE_URL or LOCAL_DATABASE_URL (run tests via `make curator-test` after `make db-up`)")
	}

	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("dbtest: connect to %s (is Postgres running? `make db-up`): %v", redact(url), err)
	}
	defer func() { _ = admin.Close(context.Background()) }()

	suffix := make([]byte, 6)
	_, _ = rand.Read(suffix)
	name := "changeloom_curator_test_" + hex.EncodeToString(suffix)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("dbtest: create database: %v", err)
	}
	t.Cleanup(func() {
		conn, err := pgx.Connect(context.Background(), url)
		if err != nil {
			t.Errorf("dbtest: connect to drop %s: %v", name, err)
			return
		}
		defer func() { _ = conn.Close(context.Background()) }()
		if _, err := conn.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Errorf("dbtest: drop database %s: %v", name, err)
		}
	})

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatalf("dbtest: parse database url: %v", err)
	}
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("dbtest: open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	sqlDB := stdlib.OpenDBFromPool(pool)
	defer func() { _ = sqlDB.Close() }()
	if err := migrate.Local(ctx, sqlDB, os.DirFS(backendMigrationsDir(t))); err != nil {
		t.Fatalf("dbtest: migrate: %v", err)
	}
	return pool
}

// backendMigrationsDir locates backend/migrations relative to this source file, so tests work
// from any package directory.
func backendMigrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("dbtest: cannot locate source file")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "backend", "migrations")
}

func redact(url string) string {
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return "<unparseable database url>"
	}
	return cfg.Host + "/" + cfg.Database
}
