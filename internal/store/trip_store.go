package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/karan/travel-diary/internal/domain"
)

// TripStore provides CRUD operations for trips and source files.
type TripStore struct {
	db *sql.DB
}

// Save creates or updates a trip.
func (s *TripStore) Save(ctx context.Context, trip domain.Trip) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO trips (id, name, source_path, notes_hash, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			status = excluded.status,
			updated_at = excluded.updated_at
	`, trip.ID, trip.Name, trip.SourcePath, trip.NotesHash, trip.Status, now, now)
	return err
}

// Get retrieves a trip by ID.
func (s *TripStore) Get(ctx context.Context, id string) (*domain.Trip, error) {
	var trip domain.Trip
	var createdAt, updatedAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, source_path, notes_hash, status, created_at, updated_at
		FROM trips WHERE id = ?
	`, id).Scan(&trip.ID, &trip.Name, &trip.SourcePath, &trip.NotesHash, &trip.Status, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	trip.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	trip.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &trip, nil
}

// List returns all trips.
func (s *TripStore) List(ctx context.Context) ([]domain.Trip, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, source_path, notes_hash, status, created_at, updated_at
		FROM trips ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var trips []domain.Trip
	for rows.Next() {
		var trip domain.Trip
		var createdAt, updatedAt string
		if err := rows.Scan(&trip.ID, &trip.Name, &trip.SourcePath, &trip.NotesHash, &trip.Status, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		trip.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		trip.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		trips = append(trips, trip)
	}
	return trips, rows.Err()
}

// UpdateStatus changes a trip's status, validating the transition.
func (s *TripStore) UpdateStatus(ctx context.Context, id string, newStatus domain.TripStatus) error {
	trip, err := s.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("get trip: %w", err)
	}
	if !trip.Status.CanTransition(newStatus) {
		return fmt.Errorf("invalid transition: %s -> %s", trip.Status, newStatus)
	}
	trip.Status = newStatus
	trip.UpdatedAt = time.Now().UTC()
	return s.Save(ctx, *trip)
}

// SaveSourceFile creates or updates a source file record.
func (s *TripStore) SaveSourceFile(ctx context.Context, sf domain.SourceFile) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO source_files (trip_id, relative_path, mime_type, file_size, sha256, capture_time, scan_error)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(trip_id, relative_path) DO UPDATE SET
			mime_type = excluded.mime_type,
			file_size = excluded.file_size,
			sha256 = excluded.sha256,
			scan_error = excluded.scan_error
	`, sf.TripID, sf.RelativePath, sf.MIME, sf.Bytes, sf.SHA256, formatTime(sf.CaptureTime), sf.ScanError)
	return err
}

// ListSourceFiles returns all source files for a trip.
func (s *TripStore) ListSourceFiles(ctx context.Context, tripID string) ([]domain.SourceFile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT trip_id, relative_path, mime_type, file_size, sha256, capture_time, scan_error
		FROM source_files WHERE trip_id = ?
	`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []domain.SourceFile
	for rows.Next() {
		var sf domain.SourceFile
		var captureTime sql.NullString
		if err := rows.Scan(&sf.TripID, &sf.RelativePath, &sf.MIME, &sf.Bytes, &sf.SHA256, &captureTime, &sf.ScanError); err != nil {
			return nil, err
		}
		if captureTime.Valid {
			t, _ := time.Parse(time.RFC3339, captureTime.String)
			sf.CaptureTime = &t
		}
		files = append(files, sf)
	}
	return files, rows.Err()
}

func formatTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}
