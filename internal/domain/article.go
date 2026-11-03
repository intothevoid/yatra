package domain

import (
	"strings"
	"time"
)

// ArticleDocument is the canonical representation of a travel article.
// Per spec §8: JSON with schema_version, stable block IDs, typed variants.
type ArticleDocument struct {
	SchemaVersion   int            `json:"schema_version"`
	ArticleID       string         `json:"article_id"`
	Title           string         `json:"title"`
	Slug            string         `json:"slug"`
	Excerpt         string         `json:"excerpt"`
	Lexical         string         `json:"lexical,omitempty"`
	ResearchSummary string         `json:"research_summary,omitempty"`
	ResearchSources []string       `json:"research_sources,omitempty"`
	FeaturePhotoID  string         `json:"feature_photo_id,omitempty"`
	Tags            []string       `json:"tags"`
	Blocks          []ArticleBlock `json:"blocks"`
	Approved        bool           `json:"approved"`
}

// ArticleBlock is a typed content block in the article.
type ArticleBlock struct {
	ID            string   `json:"id"`
	Type          string   `json:"type"` // "heading" | "paragraph" | "image" | "gallery" | "quote"
	Text          string   `json:"text,omitempty"`
	Level         int      `json:"level,omitempty"`     // for heading
	PhotoIDs      []string `json:"photo_ids,omitempty"` // for gallery
	CardWidth     string   `json:"card_width,omitempty"`
	PhotoCaptions []string `json:"photo_captions,omitempty"`
	Caption       string   `json:"caption,omitempty"`      // for gallery
	AltText       string   `json:"alt_text,omitempty"`     // for image
	EvidenceIDs   []string `json:"evidence_ids,omitempty"` // for paragraph
}

// Block types from spec §8.
const (
	BlockHeading   = "heading"
	BlockParagraph = "paragraph"
	BlockImage     = "image"
	BlockGallery   = "gallery"
	BlockQuote     = "quote"
)

// ArticleRevision is an immutable snapshot of an article document.
type ArticleRevision struct {
	ID         string    `json:"id"`
	ArticleID  string    `json:"article_id"`
	Author     string    `json:"author"` // "system" or "user"
	Reason     string    `json:"reason"`
	Document   string    `json:"document"` // canonical JSON
	CreatedAt  time.Time `json:"created_at"`
	Provenance string    `json:"provenance,omitempty"`
}

// GenerateSlug creates a URL-safe slug from the title.
func GenerateSlug(title string) string {
	s := strings.ToLower(title)
	s = strings.ReplaceAll(s, " ", "-")
	// Remove non-alphanumeric except hyphens
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Validate checks the document for spec §8 requirements.
func (d *ArticleDocument) Validate() []string {
	var errs []string
	if d.SchemaVersion != 1 {
		errs = append(errs, "unsupported schema_version")
	}
	if d.ArticleID == "" {
		errs = append(errs, "missing article_id")
	}
	if d.Title == "" {
		errs = append(errs, "missing title")
	}
	if d.Slug == "" {
		d.Slug = GenerateSlug(d.Title)
	}
	for _, block := range d.Blocks {
		if block.ID == "" {
			errs = append(errs, "block missing id")
		}
		if block.Type == "" {
			errs = append(errs, "block missing type")
		}
	}
	return errs
}
