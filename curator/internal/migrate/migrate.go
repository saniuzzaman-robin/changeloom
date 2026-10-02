// Package migrate applies the backend schema, plus the curator-only schema on the local DB.
package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/pressly/goose/v3"

	"github.com/saniuzzaman-robin/changeloom/curator/migrations"
)

// CuratorTable tracks the curator-only migrations, apart from the backend's goose_db_version.
const CuratorTable = "curator_goose_db_version"

// Local applies the backend migrations, then the curator-only migrations, so the local schema
// matches the hosted one plus the curator's own tables.
func Local(ctx context.Context, db *sql.DB, backend fs.FS) error {
	if err := Remote(ctx, db, backend); err != nil {
		return err
	}
	return up(ctx, "curator", db, migrations.FS, goose.WithTableName(CuratorTable))
}

// Remote applies only the backend migrations; the hosted DB never gets the curator tables.
func Remote(ctx context.Context, db *sql.DB, backend fs.FS) error {
	return up(ctx, "backend", db, backend)
}

func up(ctx context.Context, name string, db *sql.DB, fsys fs.FS, opts ...goose.ProviderOption) error {
	provider, err := goose.NewProvider(goose.DialectPostgres, db, fsys, opts...)
	if errors.Is(err, goose.ErrNoMigrations) {
		return fmt.Errorf("no %s migrations found (check BACKEND_MIGRATIONS_DIR; the default assumes you run from curator/): %w", name, err)
	}
	if err != nil {
		return fmt.Errorf("%s migrations: %w", name, err)
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply %s migrations: %w", name, err)
	}
	for _, r := range results {
		slog.InfoContext(ctx, "migration applied", "set", name, "file", r.Source.Path, "duration", r.Duration)
	}
	slog.InfoContext(ctx, "migrations up to date", "set", name, "applied", len(results))
	return nil
}
