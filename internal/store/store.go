package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Store holds all database connections and sub-stores.
type Store struct {
	db        *sql.DB
	Trips     *TripStore
	Photos    *PhotoStore
	Articles  *ArticleStore
	Jobs      *JobStore
}

// Open creates a new store in the given directory.
// It creates the directory if needed, opens SQLite with WAL mode,
// and runs migrations up to the latest version.
func Open(ctx context.Context, dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	dbPath := filepath.Join(dataDir, "travel-diary.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Run migrations
	migrator := NewMigrator(db)
	if err := migrator.MigrateTo(ctx, 2); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return &Store{
		db:     db,
		Trips:  &TripStore{db: db},
		Photos: &PhotoStore{db: db},
		Articles: &ArticleStore{db: db},
		Jobs:   &JobStore{db: db},
	}, nil
}

// Close releases the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}
