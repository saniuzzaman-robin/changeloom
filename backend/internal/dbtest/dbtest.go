// Package dbtest provides throwaway, fully migrated Postgres databases for tests.
package dbtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/saniuzzaman-robin/changeloom/backend/migrations"
)

// New creates an empty database on the server in TEST_DATABASE_URL (falling back to
// DATABASE_URL), applies all migrations and drops it when the test ends.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := t.Context()

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = os.Getenv("DATABASE_URL")
	}
	if url == "" {
		t.Fatal("dbtest: set TEST_DATABASE_URL or DATABASE_URL (run tests via `make test` after `make db-up`)")
	}

	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("dbtest: connect to %s (is Postgres running? `make db-up`): %v", redact(url), err)
	}
	defer func() { _ = admin.Close(context.Background()) }()

	suffix := make([]byte, 6)
	_, _ = rand.Read(suffix)
	name := "changeloom_test_" + hex.EncodeToString(suffix)
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
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	if err != nil {
		t.Fatalf("dbtest: goose provider: %v", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("dbtest: migrate: %v", err)
	}
	return pool
}

func redact(url string) string {
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return "<unparseable DATABASE_URL>"
	}
	return cfg.Host + "/" + cfg.Database
}
