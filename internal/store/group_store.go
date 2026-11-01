package store

import (
	"context"
	"fmt"
)

// SavePhotoGroup stores a photo group and its members.
func (s *Store) SavePhotoGroup(ctx context.Context, group PhotoGroup) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Insert or replace group
	_, err = tx.ExecContext(ctx, `
		INSERT INTO photo_groups (id, trip_id, type)
		VALUES (?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET type = excluded.type
	`, group.ID, group.TripID, group.Type)
	if err != nil {
		return fmt.Errorf("insert group: %w", err)
	}

	// Clear old members
	_, err = tx.ExecContext(ctx, `DELETE FROM photo_group_members WHERE group_id = ?`, group.ID)
	if err != nil {
		return fmt.Errorf("clear members: %w", err)
	}

	// Insert members
	for _, photoID := range group.Photos {
		_, err = tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO photo_group_members (group_id, photo_id)
			VALUES (?, ?)
		`, group.ID, photoID)
		if err != nil {
			return fmt.Errorf("insert member %s: %w", photoID, err)
		}
	}

	return tx.Commit()
}

// PhotoGroup represents a cluster of duplicate or similar photos.
type PhotoGroup struct {
	ID     string   `json:"id"`
	TripID string   `json:"trip_id"`
	Type   string   `json:"type"` // "duplicate" or "similar"
	Photos []string `json:"photo_ids"`
}

// GetPhotoGroups returns all groups for a trip.
func (s *Store) GetPhotoGroups(ctx context.Context, tripID string) ([]PhotoGroup, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT pg.id, pg.trip_id, pg.type, pgm.photo_id
		FROM photo_groups pg
		JOIN photo_group_members pgm ON pg.id = pgm.group_id
		WHERE pg.trip_id = ?
		ORDER BY pg.id, pgm.photo_id
	`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groupMap := make(map[string]*PhotoGroup)
	var groupIDs []string

	for rows.Next() {
		var id, tripID, groupType, photoID string
		if err := rows.Scan(&id, &tripID, &groupType, &photoID); err != nil {
			return nil, err
		}
		g, ok := groupMap[id]
		if !ok {
			g = &PhotoGroup{ID: id, TripID: tripID, Type: groupType}
			groupMap[id] = g
			groupIDs = append(groupIDs, id)
		}
		g.Photos = append(g.Photos, photoID)
	}

	var result []PhotoGroup
	for _, id := range groupIDs {
		result = append(result, *groupMap[id])
	}
	return result, rows.Err()
}
