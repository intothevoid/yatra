package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/karan/travel-diary/internal/adapters"
	"github.com/karan/travel-diary/internal/config"
	"github.com/karan/travel-diary/internal/domain"
	"github.com/karan/travel-diary/internal/store"
)

// SectionDrafter generates narrative blocks from experiences and photos.
type SectionDrafter struct {
	store *store.Store
	text  adapters.TextAdapter
	style *domain.StyleProfile
	cfg   *config.Config
}

func (d *SectionDrafter) SetConfig(cfg *config.Config) { d.cfg = cfg }

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
	photos, err := d.store.Photos.GetByTrip(ctx, tripID)
	if err != nil {
		return nil, fmt.Errorf("get photos: %w", err)
	}

	// Get trip for title
	trip, err := d.store.Trips.Get(ctx, tripID)
	if err != nil {
		return nil, fmt.Errorf("get trip: %w", err)
	}

	// Research the destination first. The lookup is model-only unless explicitly
	// enabled in server configuration.
	research, err := ResearchLocation(ctx, d.cfg, d.text, trip.Location)
	if err != nil {
		return nil, err
	}

	// Build prompt for section drafting
	prompt := d.buildPrompt(trip, experiences, matches, photos, research.Summary)

	// Generate narrative
	response, err := d.text.Generate(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("generate narrative: %w", err)
	}

	// Parse response into blocks
	blocks := d.parseBlocks(response, matches)
	selectedPhotoIDs := make([]string, 0, len(photos))
	for _, photo := range photos {
		if photo.Status != domain.PhotoStatusNotSelected {
			selectedPhotoIDs = append(selectedPhotoIDs, photo.ID)
		}
	}
	if len(selectedPhotoIDs) > 0 {
		blocks = interleavePhotos(blocks, photos)
	}

	// Create article document
	doc := &domain.ArticleDocument{
		SchemaVersion:   1,
		ArticleID:       "art_" + tripID,
		Title:           trip.Name,
		Slug:            domain.GenerateSlug(trip.Name),
		Excerpt:         truncate(response, 200),
		ResearchSummary: research.Summary,
		ResearchSources: research.Sources,
		FeaturePhotoID:  selectFeaturePhoto(matches),
		Tags:            extractTags(experiences),
		Blocks:          blocks,
	}
	if doc.FeaturePhotoID == "" && len(selectedPhotoIDs) > 0 {
		doc.FeaturePhotoID = selectedPhotoIDs[0]
	}

	// Save article. Generation can be retried after a partial failure, so the
	// deterministic article ID must be safe to encounter more than once.
	if err := d.store.Articles.Save(ctx, tripID, trip.Name); err != nil {
		return nil, fmt.Errorf("save article: %w", err)
	}

	// Save revision
	docJSON, _ := json.Marshal(doc)
	rev := domain.ArticleRevision{
		ID:        fmt.Sprintf("rev_%d", time.Now().UTC().UnixNano()),
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

// interleavePhotos places the lead photo before the introduction, then attaches
// each remaining photo to the most relevant paragraph instead of collecting
// every image in one gallery.
func interleavePhotos(blocks []domain.ArticleBlock, photos []domain.Photo) []domain.ArticleBlock {
	var selected []domain.Photo
	for _, photo := range photos {
		if photo.Status != domain.PhotoStatusNotSelected {
			selected = append(selected, photo)
		}
	}
	if len(selected) == 0 {
		return blocks
	}

	lead := selected[0]
	result := []domain.ArticleBlock{{ID: "trip_lead_photo", Type: domain.BlockImage, PhotoIDs: []string{lead.ID}, AltText: photoDescription(lead), Caption: photoDescription(lead)}}
	if len(selected) == 1 {
		return append(result, blocks...)
	}

	paragraphIndexes := make([]int, 0)
	for i, block := range blocks {
		if block.Type == domain.BlockParagraph {
			paragraphIndexes = append(paragraphIndexes, i)
		}
	}
	if len(paragraphIndexes) == 0 {
		paragraphIndexes = []int{len(blocks) - 1}
	}
	assigned := make(map[int][]domain.Photo)
	for photoIndex, photo := range selected[1:] {
		description := photoDescription(photo)
		bestIndex, bestScore := paragraphIndexes[photoIndex%len(paragraphIndexes)], -1.0
		for _, blockIndex := range paragraphIndexes {
			text := blocks[blockIndex].Text
			if blockIndex > 0 && blocks[blockIndex-1].Type == domain.BlockHeading {
				text = blocks[blockIndex-1].Text + " " + text
			}
			score := keywordOverlap(description, text) - float64(len(assigned[blockIndex]))*0.2
			if score > bestScore {
				bestScore, bestIndex = score, blockIndex
			}
		}
		assigned[bestIndex] = append(assigned[bestIndex], photo)
	}

	imageID := 1
	for i, block := range blocks {
		result = append(result, block)
		for _, photo := range assigned[i] {
			description := photoDescription(photo)
			result = append(result, domain.ArticleBlock{ID: fmt.Sprintf("trip_photo_%d", imageID), Type: domain.BlockImage, PhotoIDs: []string{photo.ID}, AltText: description, Caption: description})
			imageID++
		}
	}
	if len(blocks) == 0 {
		for i, photo := range selected[1:] {
			description := photoDescription(photo)
			result = append(result, domain.ArticleBlock{ID: fmt.Sprintf("trip_photo_%d", i+1), Type: domain.BlockImage, PhotoIDs: []string{photo.ID}, AltText: description, Caption: description})
		}
	}
	return result
}

func photoDescription(photo domain.Photo) string {
	var result domain.VisionResult
	if photo.VLMResult != "" && json.Unmarshal([]byte(photo.VLMResult), &result) == nil {
		return strings.TrimSpace(result.Observed.Description)
	}
	return ""
}

func keywordOverlap(a, b string) float64 {
	stop := map[string]bool{"the": true, "and": true, "with": true, "from": true, "that": true, "this": true, "there": true, "their": true, "about": true, "while": true, "were": true, "was": true, "are": true, "for": true, "you": true, "your": true, "photo": true, "image": true, "people": true}
	words := func(value string) map[string]bool {
		out := make(map[string]bool)
		for _, word := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool { return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) }) {
			if len(word) > 2 && !stop[word] {
				out[word] = true
			}
		}
		return out
	}
	left, right := words(a), words(b)
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	overlap := 0
	for word := range left {
		if right[word] {
			overlap++
		}
	}
	return float64(overlap) / float64(len(left))
}

func (d *SectionDrafter) buildPrompt(trip *domain.Trip, experiences []store.Experience, matches []domain.PhotoMatch, photos []domain.Photo, research string) string {
	var sb strings.Builder
	sb.WriteString("Write a first-person travel blog post using the supplied trip details, notes, and photo descriptions.\n\n")
	sb.WriteString("Post title: " + trip.Name + "\n")
	sb.WriteString("Writing style: informal, conversational, straightforward, and personal. Start with a short introduction naming the destination and trip context. Use simple section headings for themes such as food, places, activities, or people. Include concrete details from the user's notes, describe what stood out, and end with a brief personal reflection. Keep the language natural; don't sound like a travel brochure.\n\n")
	if trip.Location != "" {
		sb.WriteString("Destination: " + trip.Location + "\n")
	}
	if trip.Notes != "" {
		sb.WriteString("User's trip notes (personal experience; preserve meaning):\n" + trip.Notes + "\n\n")
	}
	if research != "" {
		sb.WriteString("Background information about the destination. Use a small amount in the introduction or relevant sections. Keep these facts separate from first-person memories, and omit anything uncertain:\n" + research + "\n\n")
	}
	sb.WriteString("What can be seen in the selected photos:\n")
	for _, photo := range photos {
		if photo.Status == domain.PhotoStatusNotSelected || photo.VLMResult == "" {
			continue
		}
		var result domain.VisionResult
		if json.Unmarshal([]byte(photo.VLMResult), &result) == nil && result.Observed.Description != "" {
			sb.WriteString(fmt.Sprintf("- %s\n", result.Observed.Description))
		}
	}
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
	sb.WriteString("- Never invent personal anecdotes, opinions, events, dates, prices, or details about the user's trip\n")
	sb.WriteString("- Use only the provided destination research for general place facts\n")
	sb.WriteString("- Use photo descriptions to add accurate visual context, but do not claim the user did or felt something based only on an image\n")
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
			block := domain.ArticleBlock{
				ID:   fmt.Sprintf("b%d", blockID),
				Type: domain.BlockParagraph,
				Text: para,
			}

			blocks = append(blocks, block)
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
