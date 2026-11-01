package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"strings"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrator applies schema migrations to the database.
type Migrator struct {
	db *sql.DB
}

// NewMigrator creates a migrator for the given database.
func NewMigrator(db *sql.DB) *Migrator {
	return &Migrator{db: db}
}

// MigrateTo runs all migrations up to the given version.
func (m *Migrator) MigrateTo(ctx context.Context, targetVersion int) error {
	// Ensure migration table exists
	_, err := m.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT (datetime('now'))
		)
	`)
	if err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}

	// Get current version
	var currentVersion int
	row := m.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations")
	if err := row.Scan(&currentVersion); err != nil {
		return fmt.Errorf("read current version: %w", err)
	}

	// Apply pending migrations
	for v := currentVersion + 1; v <= targetVersion; v++ {
		sql, err := readMigration(v)
		if err != nil {
			return fmt.Errorf("read migration %d: %w", v, err)
		}
		if err := m.applyMigration(ctx, v, sql); err != nil {
			return fmt.Errorf("apply migration %d: %w", v, err)
		}
	}

	return nil
}

// CurrentVersion returns the latest applied migration version.
func (m *Migrator) CurrentVersion(ctx context.Context) (int, error) {
	var version int
	row := m.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations")
	if err := row.Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

func (m *Migrator) applyMigration(ctx context.Context, version int, sql string) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Split by semicolons and execute each statement
	statements := strings.Split(sql, ";")
	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("statement %q: %w", stmt[:min(80, len(stmt))], err)
		}
	}

	// Record migration
	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES (?)", version); err != nil {
		return err
	}

	return tx.Commit()
}

func readMigration(version int) (string, error) {
	f, err := migrationFiles.Open(fmt.Sprintf("migrations/v%d.sql", version))
	if err != nil {
		return "", err
	}
	defer f.Close()

	buf := make([]byte, 1<<20) // 1MB max
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return "", err
	}
	return string(buf[:n]), nil
}
