package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/karan/travel-diary/internal/domain"
)

// SaveStyleProfile stores a style profile.
func (s *Store) SaveStyleProfile(ctx context.Context, profile *domain.StyleProfile) error {
	dataJSON, _ := json.Marshal(profile)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO style_profiles (id, name, version, data)
		VALUES (?, ?, 1, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, version = excluded.version + 1, data = excluded.data
	`, profile.ID, profile.Name, string(dataJSON))
	return err
}

// GetStyleProfile retrieves a style profile.
func (s *Store) GetStyleProfile(ctx context.Context, id string) (*domain.StyleProfile, error) {
	var dataJSON string
	err := s.db.QueryRowContext(ctx, `
		SELECT data FROM style_profiles WHERE id = ?
	`, id).Scan(&dataJSON)
	if err != nil {
		return nil, err
	}
	var profile domain.StyleProfile
	if err := json.Unmarshal([]byte(dataJSON), &profile); err != nil {
		return nil, err
	}
	return &profile, nil
}

// ensure fmt is used
var _ = fmt.Errorf
