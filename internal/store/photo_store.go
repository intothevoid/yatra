package store

import (
	"context"
	"database/sql"

	"github.com/karan/travel-diary/internal/domain"
)

// PhotoStore provides CRUD operations for photos.
type PhotoStore struct {
	db *sql.DB
}

// Upsert creates or updates a photo record.
func (s *PhotoStore) Upsert(ctx context.Context, photo domain.Photo) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO photos (id, source_file_id, exif, thumbnail_path, preview_path, export_path,
			vlm_result, vlm_schema_version, status, ranking_reasons, user_override)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
		ON CONFLICT(id) DO UPDATE SET
			exif = excluded.exif,
			thumbnail_path = excluded.thumbnail_path,
			preview_path = excluded.preview_path,
			export_path = excluded.export_path,
			vlm_result = excluded.vlm_result,
			vlm_schema_version = excluded.vlm_schema_version,
			status = excluded.status,
			ranking_reasons = excluded.ranking_reasons
	`, photo.ID, photo.SourceFileID, photo.EXIF, photo.ThumbnailPath, photo.PreviewPath,
		photo.ExportPath, photo.VLMResult, 1, photo.Status, photo.RankingReasons)
	return err
}

// Get retrieves a photo by ID.
func (s *PhotoStore) Get(ctx context.Context, id string) (*domain.Photo, error) {
	var photo domain.Photo
	var exif, thumbnail, preview, export, vlmResult, ranking sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT id, source_file_id, exif, thumbnail_path, preview_path, export_path,
			vlm_result, status, ranking_reasons
		FROM photos WHERE id = ?
	`, id).Scan(&photo.ID, &photo.SourceFileID, &exif, &thumbnail, &preview, &export,
		&vlmResult, &photo.Status, &ranking)
	if err != nil {
		return nil, err
	}
	if exif.Valid {
		photo.EXIF = exif.String
	}
	if thumbnail.Valid {
		photo.ThumbnailPath = thumbnail.String
	}
	if preview.Valid {
		photo.PreviewPath = preview.String
	}
	if export.Valid {
		photo.ExportPath = export.String
	}
	if vlmResult.Valid {
		photo.VLMResult = vlmResult.String
	}
	if ranking.Valid {
		photo.RankingReasons = ranking.String
	}
	return &photo, nil
}

// GetByTrip returns all photos for a trip.
func (s *PhotoStore) GetByTrip(ctx context.Context, tripID string) ([]domain.Photo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.source_file_id, p.exif, p.thumbnail_path, p.preview_path, p.export_path,
			p.vlm_result, p.status, p.ranking_reasons
		FROM photos p
		JOIN source_files sf ON p.source_file_id = sf.trip_id || '_' || sf.relative_path
		WHERE sf.trip_id = ?
	`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var photos []domain.Photo
	for rows.Next() {
		var photo domain.Photo
		var exif, thumbnail, preview, export, vlmResult, ranking sql.NullString
		if err := rows.Scan(&photo.ID, &photo.SourceFileID, &exif, &thumbnail, &preview, &export,
			&vlmResult, &photo.Status, &ranking); err != nil {
			return nil, err
		}
		if exif.Valid {
			photo.EXIF = exif.String
		}
		if thumbnail.Valid {
			photo.ThumbnailPath = thumbnail.String
		}
		if preview.Valid {
			photo.PreviewPath = preview.String
		}
		if export.Valid {
			photo.ExportPath = export.String
		}
		if vlmResult.Valid {
			photo.VLMResult = vlmResult.String
		}
		if ranking.Valid {
			photo.RankingReasons = ranking.String
		}
		photos = append(photos, photo)
	}
	return photos, rows.Err()
}

// UpdateSelection changes a photo's selection status.
func (s *PhotoStore) UpdateSelection(ctx context.Context, photoID string, status domain.PhotoStatus) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE photos SET status = ? WHERE id = ?
	`, status, photoID)
	return err
}

// UpdateVLMResult stores the VLM analysis result for a photo.
func (s *PhotoStore) UpdateVLMResult(ctx context.Context, photoID string, result string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE photos SET vlm_result = ?, vlm_schema_version = 1 WHERE id = ?
	`, result, photoID)
	return err
}
