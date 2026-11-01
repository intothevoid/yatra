package store

import (
	"context"
	"fmt"
)

// GhostExport records a Ghost export.
type GhostExport struct {
	ArticleID    string `json:"article_id"`
	RevisionID   string `json:"revision_id"`
	RemotePostID string `json:"remote_post_id"`
}

// SaveGhostExport stores a Ghost export record.
func (s *Store) SaveGhostExport(ctx context.Context, export GhostExport) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO ghost_exports (id, article_id, revision_id, remote_post_id)
		VALUES (?, ?, ?, ?)
	`, "ge_"+export.ArticleID, export.ArticleID, export.RevisionID, export.RemotePostID)
	return err
}

// ensure fmt is used
var _ = fmt.Errorf
