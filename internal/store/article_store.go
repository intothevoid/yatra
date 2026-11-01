package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/karan/travel-diary/internal/domain"
)

// ArticleStore provides CRUD operations for articles and revisions.
type ArticleStore struct {
	db *sql.DB
}

// Save creates an article record.
func (s *ArticleStore) Save(ctx context.Context, tripID, title string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO articles (id, trip_id, status, title)
		VALUES (?, ?, 'EDITING', ?)
	`, "art_"+tripID, tripID, title)
	return err
}

// GetLatest returns the latest revision of an article.
func (s *ArticleStore) GetLatest(ctx context.Context, articleID string) (*domain.ArticleDocument, error) {
	var docJSON string
	err := s.db.QueryRowContext(ctx, `
		SELECT document FROM article_revisions
		WHERE article_id = ?
		ORDER BY created_at DESC LIMIT 1
	`, articleID).Scan(&docJSON)
	if err != nil {
		return nil, err
	}
	var doc domain.ArticleDocument
	if err := json.Unmarshal([]byte(docJSON), &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// SaveRevision stores an immutable article revision.
// rev.Document is already canonical JSON (a string).
func (s *ArticleStore) SaveRevision(ctx context.Context, rev domain.ArticleRevision) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO article_revisions (id, article_id, author, reason, document, provenance, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, rev.ID, rev.ArticleID, rev.Author, rev.Reason, rev.Document, rev.Provenance, now)
	if err != nil {
		return err
	}
	// Update article's current revision pointer
	_, err = s.db.ExecContext(ctx, `
		UPDATE articles SET current_revision = ? WHERE id = ?
	`, rev.ID, rev.ArticleID)
	return err
}

// GetRevision retrieves a specific revision.
func (s *ArticleStore) GetRevision(ctx context.Context, revisionID string) (*domain.ArticleRevision, error) {
	var rev domain.ArticleRevision
	var docJSON, createdAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, article_id, author, reason, document, provenance, created_at
		FROM article_revisions WHERE id = ?
	`, revisionID).Scan(&rev.ID, &rev.ArticleID, &rev.Author, &rev.Reason, &docJSON, &rev.Provenance, &createdAt)
	if err != nil {
		return nil, err
	}
	rev.Document = docJSON
	rev.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &rev, nil
}

// ListRevisions returns all revisions for an article.
func (s *ArticleStore) ListRevisions(ctx context.Context, articleID string) ([]domain.ArticleRevision, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, article_id, author, reason, document, provenance, created_at
		FROM article_revisions WHERE article_id = ?
		ORDER BY created_at DESC
	`, articleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var revisions []domain.ArticleRevision
	for rows.Next() {
		var rev domain.ArticleRevision
		var docJSON, createdAt string
		if err := rows.Scan(&rev.ID, &rev.ArticleID, &rev.Author, &rev.Reason, &docJSON, &rev.Provenance, &createdAt); err != nil {
			return nil, err
		}
		rev.Document = docJSON
		rev.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		revisions = append(revisions, rev)
	}
	return revisions, rows.Err()
}

// ApproveRevision marks a revision as the approved version for export.
func (s *ArticleStore) ApproveRevision(ctx context.Context, articleID, revisionID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE articles SET approved_revision = ?, status = 'ARTICLE_APPROVED'
		WHERE id = ?
	`, revisionID, articleID)
	return err
}
