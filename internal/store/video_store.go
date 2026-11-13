package store

import (
	"context"
	"database/sql"

	"github.com/karan/travel-diary/internal/domain"
)

type VideoStore struct{ db *sql.DB }

const videoColumns = `id,trip_id,source_file_id,file_name,container,codec,frame_rate,source_path,source_bytes,path,poster_path,output_bytes,duration,width,height,has_audio,profile,status,error,selected,source_container,source_codec,source_duration,source_width,source_height,source_frame_rate,source_has_audio,rotation,poster_bytes,start_time,end_time,source_created_at`

func (s *VideoStore) Upsert(ctx context.Context, v domain.Video) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO videos (`+videoColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET container=excluded.container,codec=excluded.codec,frame_rate=excluded.frame_rate,source_path=excluded.source_path,source_bytes=excluded.source_bytes,path=excluded.path,poster_path=excluded.poster_path,output_bytes=excluded.output_bytes,duration=excluded.duration,width=excluded.width,height=excluded.height,has_audio=excluded.has_audio,profile=excluded.profile,status=excluded.status,error=excluded.error,selected=excluded.selected,source_container=excluded.source_container,source_codec=excluded.source_codec,source_duration=excluded.source_duration,source_width=excluded.source_width,source_height=excluded.source_height,source_frame_rate=excluded.source_frame_rate,source_has_audio=excluded.source_has_audio,rotation=excluded.rotation,poster_bytes=excluded.poster_bytes,start_time=excluded.start_time,end_time=excluded.end_time,source_created_at=excluded.source_created_at`,
		v.ID, v.TripID, v.SourceFileID, v.FileName, v.Container, v.Codec, v.FrameRate, v.SourcePath, v.SourceBytes, v.Path, v.PosterPath, v.OutputBytes, v.Duration, v.Width, v.Height, v.HasAudio, v.Profile, v.Status, v.Error, v.Selected, v.SourceContainer, v.SourceCodec, v.SourceDuration, v.SourceWidth, v.SourceHeight, v.SourceFrameRate, v.SourceHasAudio, v.Rotation, v.PosterBytes, v.StartTime, v.EndTime, v.SourceCreatedAt)
	return err
}

func (s *VideoStore) GetByTrip(ctx context.Context, tripID string) ([]domain.Video, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+videoColumns+` FROM videos WHERE trip_id=? ORDER BY rowid`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.Video
	for rows.Next() {
		v, err := scanVideo(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *v)
	}
	return result, rows.Err()
}

func (s *VideoStore) UpdateSelection(ctx context.Context, tripID, id string, selected bool) error {
	result, err := s.db.ExecContext(ctx, `UPDATE videos SET selected=? WHERE trip_id=? AND id=?`, selected, tripID, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *VideoStore) Get(ctx context.Context, id string) (*domain.Video, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+videoColumns+` FROM videos WHERE id=?`, id)
	return scanVideo(row)
}

type rowScanner interface{ Scan(...any) error }

func scanVideo(row rowScanner) (*domain.Video, error) {
	var v domain.Video
	var audio, selected int
	var sourceAudio int
	err := row.Scan(&v.ID, &v.TripID, &v.SourceFileID, &v.FileName, &v.Container, &v.Codec, &v.FrameRate, &v.SourcePath, &v.SourceBytes, &v.Path, &v.PosterPath, &v.OutputBytes, &v.Duration, &v.Width, &v.Height, &audio, &v.Profile, &v.Status, &v.Error, &selected, &v.SourceContainer, &v.SourceCodec, &v.SourceDuration, &v.SourceWidth, &v.SourceHeight, &v.SourceFrameRate, &sourceAudio, &v.Rotation, &v.PosterBytes, &v.StartTime, &v.EndTime, &v.SourceCreatedAt)
	if err != nil {
		return nil, err
	}
	v.HasAudio = audio != 0
	v.SourceHasAudio = sourceAudio != 0
	v.Selected = selected != 0
	return &v, nil
}
