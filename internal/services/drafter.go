package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/karan/travel-diary/internal/adapters"
	"github.com/karan/travel-diary/internal/domain"
	"github.com/karan/travel-diary/internal/store"
)

// SectionDrafter generates narrative blocks from experiences and photos.
type SectionDrafter struct {
	store *store.Store
	text  adapters.TextAdapter
	style *domain.StyleProfile
}

// NewSectionDrafter creates a new drafter.
func NewSectionDrafter(s *store.Store, text adapters.TextAdapter, style *domain.StyleProfile) *SectionDrafter {
	return &SectionDrafter{store: s, text: text, style: style}
}

// Generate creates an article from experiences and matched photos.
func (d *SectionDrafter) Generate(ctx context.Context, tripID string) (*domain.ArticleDocument, error) {
	// Get experiences
	experiences, err := d.store.GetExperiences(ctx, tripID)
	if err != nil {
		return nil, fmt.Errorf("get experiences: %w", err)
	}

	// Get photo matches
	matches, err := d.store.GetPhotoMatches(ctx, tripID)
	if err != nil {
		return nil, fmt.Errorf("get matches: %w", err)
	}

	// Get trip for title
	trip, err := d.store.Trips.Get(ctx, tripID)
	if err != nil {
		return nil, fmt.Errorf("get trip: %w", err)
	}

	// Build prompt for section drafting
	prompt := d.buildPrompt(trip, experiences, matches)

	// Generate narrative
	response, err := d.text.Generate(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("generate narrative: %w", err)
	}

	// Parse response into blocks
	blocks := d.parseBlocks(response, matches)

	// Create article document
	doc := &domain.ArticleDocument{
		SchemaVersion:  1,
		ArticleID:      "art_" + tripID,
		Title:          trip.Name,
		Slug:           domain.GenerateSlug(trip.Name),
		Excerpt:        truncate(response, 200),
		FeaturePhotoID: selectFeaturePhoto(matches),
		Tags:           extractTags(experiences),
		Blocks:         blocks,
	}

	// Save article
	if err := d.store.Articles.Save(ctx, tripID, trip.Name); err != nil {
		return nil, fmt.Errorf("save article: %w", err)
	}

	// Save revision
	docJSON, _ := json.Marshal(doc)
	rev := domain.ArticleRevision{
		ID:        "rev_1",
		ArticleID: doc.ArticleID,
		Author:    "system",
		Reason:    "initial generation",
		Document:  string(docJSON),
	}
	if err := d.store.Articles.SaveRevision(ctx, rev); err != nil {
		return nil, fmt.Errorf("save revision: %w", err)
	}

	return doc, nil
}

func (d *SectionDrafter) buildPrompt(trip *domain.Trip, experiences []store.Experience, matches []domain.PhotoMatch) string {
	var sb strings.Builder
	sb.WriteString("Write a first-person travel narrative based on these experiences.\n\n")
	sb.WriteString("Style: warm, medium paragraphs\n\n")
	sb.WriteString("Experiences:\n")
	for i, exp := range experiences {
		// NoteSpans is JSON, extract first span text
		var spans []struct{ Text string }
		json.Unmarshal([]byte(exp.NoteSpans), &spans)
		text := ""
		if len(spans) > 0 {
			text = spans[0].Text
		}
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, text))
		if exp.Location != "" {
			sb.WriteString(fmt.Sprintf("   Location: %s\n", exp.Location))
		}
		if exp.Foods != "" {
			sb.WriteString(fmt.Sprintf("   Food: %s\n", exp.Foods))
		}
	}

	sb.WriteString("\nMatched photos:\n")
	for _, m := range matches {
		sb.WriteString(fmt.Sprintf("- Photo %s (score %.2f): %s\n", m.PhotoID, m.Score, m.Justification))
	}

	sb.WriteString("\nRules:\n")
	sb.WriteString("- Only use facts from the experiences above\n")
	sb.WriteString("- Do not invent anecdotes or opinions not in the notes\n")
	sb.WriteString("- Use photo references where matched\n")
	sb.WriteString("- Keep claims traceable to source evidence\n")

	return sb.String()
}

func (d *SectionDrafter) parseBlocks(response string, matches []domain.PhotoMatch) []domain.ArticleBlock {
	var blocks []domain.ArticleBlock

	// Split by double newline for paragraphs
	paragraphs := strings.Split(response, "\n\n")
	blockID := 1

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		// Check if this looks like a heading
		if len(para) < 100 && !strings.HasSuffix(para, ".") {
			blocks = append(blocks, domain.ArticleBlock{
				ID:    fmt.Sprintf("b%d", blockID),
				Type:  domain.BlockHeading,
				Text:  para,
				Level: 2,
			})
		} else {
			// Find matching photos for this paragraph
			var photoIDs []string
			for _, m := range matches {
				// Simple heuristic: match photos to paragraphs by order
				photoIDs = append(photoIDs, m.PhotoID)
			}

			block := domain.ArticleBlock{
				ID:   fmt.Sprintf("b%d", blockID),
				Type: domain.BlockParagraph,
				Text: para,
			}

			// Add gallery if photos matched
			if len(photoIDs) > 0 {
				blocks = append(blocks, block)
				blocks = append(blocks, domain.ArticleBlock{
					ID:       fmt.Sprintf("b%d_g", blockID),
					Type:     domain.BlockGallery,
					PhotoIDs: photoIDs,
					Caption:  "From the trip",
				})
			} else {
				blocks = append(blocks, block)
			}
		}
		blockID++
	}

	return blocks
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func selectFeaturePhoto(matches []domain.PhotoMatch) string {
	if len(matches) > 0 {
		return matches[0].PhotoID
	}
	return ""
}

func extractTags(experiences []store.Experience) []string {
	var tags []string
	seen := make(map[string]bool)
	for _, exp := range experiences {
		if exp.Location != "" && !seen[exp.Location] {
			tags = append(tags, exp.Location)
			seen[exp.Location] = true
		}
	}
	return tags
}
