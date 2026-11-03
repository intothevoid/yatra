package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/karan/travel-diary/internal/domain"
)

// SaveExperience stores an experience record.
// Accepts any struct that can be marshaled to JSON.
func (s *Store) SaveExperience(ctx context.Context, exp any) error {
	data, err := json.Marshal(exp)
	if err != nil {
		return fmt.Errorf("marshal experience: %w", err)
	}

	// Parse into a flat structure for storage
	var flat struct {
		ID          string          `json:"id"`
		TripID      string          `json:"trip_id"`
		NoteSpans   json.RawMessage `json:"note_spans"`
		Location    string          `json:"location"`
		Foods       json.RawMessage `json:"foods"`
		Sentiment   string          `json:"sentiment"`
		Uncertainty json.RawMessage `json:"uncertainty"`
	}
	if err := json.Unmarshal(data, &flat); err != nil {
		return fmt.Errorf("unmarshal experience: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO experiences (id, trip_id, note_spans, location, foods, sentiment, uncertainty)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			trip_id = excluded.trip_id,
			note_spans = excluded.note_spans,
			location = excluded.location,
			foods = excluded.foods,
			sentiment = excluded.sentiment,
			uncertainty = excluded.uncertainty
	`, flat.ID, flat.TripID, string(flat.NoteSpans), flat.Location,
		string(flat.Foods), flat.Sentiment, string(flat.Uncertainty))
	return err
}

// GetExperiences returns all experiences for a trip.
func (s *Store) GetExperiences(ctx context.Context, tripID string) ([]Experience, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, trip_id, note_spans, location, foods, sentiment, uncertainty
		FROM experiences WHERE trip_id = ?
	`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var experiences []Experience
	for rows.Next() {
		var exp Experience
		if err := rows.Scan(&exp.ID, &exp.TripID, &exp.NoteSpans, &exp.Location, &exp.Foods, &exp.Sentiment, &exp.Uncertainty); err != nil {
			return nil, err
		}
		experiences = append(experiences, exp)
	}
	return experiences, rows.Err()
}

// Experience is the store-level experience type.
type Experience struct {
	ID          string `json:"id"`
	TripID      string `json:"trip_id"`
	NoteSpans   string `json:"note_spans"`
	Location    string `json:"location"`
	Foods       string `json:"foods"`
	Sentiment   string `json:"sentiment"`
	Uncertainty string `json:"uncertainty"`
}

// SavePhotoMatch stores a photo-experience match.
func (s *Store) SavePhotoMatch(ctx context.Context, match domain.PhotoMatch) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO photo_matches (experience_id, photo_id, score, justification)
		VALUES (?, ?, ?, ?)
	`, match.ExperienceID, match.PhotoID, match.Score, match.Justification)
	return err
}

// GetPhotoMatches returns all matches for a trip.
func (s *Store) GetPhotoMatches(ctx context.Context, tripID string) ([]domain.PhotoMatch, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT pm.experience_id, pm.photo_id, pm.score, pm.justification
		FROM photo_matches pm
		JOIN experiences e ON pm.experience_id = e.id
		WHERE e.trip_id = ?
		ORDER BY pm.score DESC
	`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var matches []domain.PhotoMatch
	for rows.Next() {
		var m domain.PhotoMatch
		if err := rows.Scan(&m.ExperienceID, &m.PhotoID, &m.Score, &m.Justification); err != nil {
			return nil, err
		}
		matches = append(matches, m)
	}
	return matches, rows.Err()
}
