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
		INSERT INTO trips (id, name, source_path, notes_hash, status, created_at, updated_at, location, notes, feature_photo_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			source_path = excluded.source_path,
			notes_hash = excluded.notes_hash,
			location = excluded.location,
			notes = excluded.notes,
			feature_photo_id = excluded.feature_photo_id,
			status = excluded.status,
			updated_at = excluded.updated_at
	`, trip.ID, trip.Name, trip.SourcePath, trip.NotesHash, trip.Status, now, now, trip.Location, trip.Notes, trip.FeaturePhotoID)
	return err
}

// Get retrieves a trip by ID.
func (s *TripStore) Get(ctx context.Context, id string) (*domain.Trip, error) {
	var trip domain.Trip
	var createdAt, updatedAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, source_path, notes_hash, status, created_at, updated_at, location, notes, feature_photo_id
		FROM trips WHERE id = ?
	`, id).Scan(&trip.ID, &trip.Name, &trip.SourcePath, &trip.NotesHash, &trip.Status, &createdAt, &updatedAt, &trip.Location, &trip.Notes, &trip.FeaturePhotoID)
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
		SELECT id, name, source_path, notes_hash, status, created_at, updated_at, location, notes, feature_photo_id
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
		if err := rows.Scan(&trip.ID, &trip.Name, &trip.SourcePath, &trip.NotesHash, &trip.Status, &createdAt, &updatedAt, &trip.Location, &trip.Notes, &trip.FeaturePhotoID); err != nil {
			return nil, err
		}
		trip.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		trip.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		trips = append(trips, trip)
	}
	return trips, rows.Err()
}

// SetFeaturePhoto stores the selected cover image and ensures it remains part
// of the trip's selected photos.
func (s *TripStore) SetFeaturePhoto(ctx context.Context, tripID, photoID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if photoID != "" {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM source_files WHERE trip_id = ? AND 'p_' || sha256 = ?`, tripID, photoID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return fmt.Errorf("photo does not belong to trip")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE photos SET status = CASE WHEN status IN ('not_selected', 'unknown') THEN 'recommended' ELSE status END WHERE id = ?`, photoID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE trips SET feature_photo_id = ?, updated_at = ? WHERE id = ?`, photoID, time.Now().UTC().Format(time.RFC3339), tripID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *TripStore) ClearFeaturePhotoIf(ctx context.Context, tripID, photoID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE trips SET feature_photo_id = '', updated_at = ? WHERE id = ? AND feature_photo_id = ?`, time.Now().UTC().Format(time.RFC3339), tripID, photoID)
	return err
}

// Delete removes a trip and its dependent records. It refuses to delete while
// work for the trip is queued or running.
func (s *TripStore) Delete(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE target_id = ? AND state IN ('queued', 'running', 'retry_wait')`, id).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return fmt.Errorf("trip has an active job")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM trips WHERE id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM jobs WHERE target_id = ?`, id); err != nil {
		return err
	}
	// Photos are content-addressed and may be shared by multiple trips. Keep a
	// photo record while any trip still references its source hash.
	if _, err := tx.ExecContext(ctx, `DELETE FROM photos WHERE NOT EXISTS (SELECT 1 FROM source_files sf WHERE photos.id = 'p_' || sf.sha256)`); err != nil {
		return err
	}
	return tx.Commit()
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
