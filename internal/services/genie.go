package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/karan/travel-diary/internal/adapters"
	"github.com/karan/travel-diary/internal/config"
	"github.com/karan/travel-diary/internal/domain"
	"github.com/karan/travel-diary/internal/store"
)

// Genie runs the analysis pipeline: VLM analysis of photos,
// experience extraction, and initial article generation.
type Genie struct {
	cfg     *config.Config
	store   *store.Store
	vlm     adapters.VLMAdapter
	text    adapters.TextAdapter
	proc    adapters.ImageProcessor
}

// NewGenie creates a new Genie service.
func NewGenie(cfg *config.Config, s *store.Store, vlm adapters.VLMAdapter, text adapters.TextAdapter, proc adapters.ImageProcessor) *Genie {
	return &Genie{cfg: cfg, store: s, vlm: vlm, text: text, proc: proc}
}

// AnalyseTrip runs VLM analysis on all photos in a trip.
func (g *Genie) AnalyseTrip(ctx context.Context, tripID string) error {
	// Update trip status
	if err := g.store.Trips.UpdateStatus(ctx, tripID, domain.StatusAnalysing); err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	// Get photos
	photos, err := g.store.Photos.GetByTrip(ctx, tripID)
	if err != nil {
		return fmt.Errorf("get photos: %w", err)
	}

	// Update job with total items
	jobID := "job_" + tripID + "_analyse"
	g.store.Jobs.Update(ctx, jobID, func(j *domain.Job) {
		j.TotalItems = len(photos)
		j.CurrentStep = "analysing photos"
	})

	for i, photo := range photos {
		// Read preview image
		previewPath := photo.PreviewPath
		if previewPath == "" {
			previewPath = photo.ThumbnailPath
		}
		if previewPath == "" {
			continue
		}

		imageBytes, err := os.ReadFile(previewPath)
		if err != nil {
			continue
		}

		// Run VLM analysis
		result, err := g.vlm.Analyze(ctx, photo.ID, imageBytes)
		if err != nil {
			// Record error but continue with other photos
			g.store.Photos.UpdateSelection(ctx, photo.ID, domain.PhotoStatusReview)
			g.store.Jobs.Update(ctx, jobID, func(j *domain.Job) {
				j.CompletedItems = i + 1
				j.Progress = (i + 1) * 100 / len(photos)
			})
			continue
		}

		// Store result
		resultJSON, _ := json.Marshal(result)
		if err := g.store.Photos.UpdateVLMResult(ctx, photo.ID, string(resultJSON)); err != nil {
			return fmt.Errorf("update VLM result: %w", err)
		}

		// Set initial status based on quality
		if len(result.QualityFlags) == 0 {
			g.store.Photos.UpdateSelection(ctx, photo.ID, domain.PhotoStatusRecommended)
		} else {
			g.store.Photos.UpdateSelection(ctx, photo.ID, domain.PhotoStatusReview)
		}

		// Update progress
		g.store.Jobs.Update(ctx, jobID, func(j *domain.Job) {
			j.CompletedItems = i + 1
			j.Progress = (i + 1) * 100 / len(photos)
		})
	}

	// Update trip status
	if err := g.store.Trips.UpdateStatus(ctx, tripID, domain.StatusPhotoReview); err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	return nil
}

// GenerateArticle creates an initial article from the trip's notes and photos.
func (g *Genie) GenerateArticle(ctx context.Context, tripID string) error {
	// Read notes
	trip, err := g.store.Trips.Get(ctx, tripID)
	if err != nil {
		return fmt.Errorf("get trip: %w", err)
	}

	notesPath := filepath.Join(trip.SourcePath, "story.txt")
	notes, _ := os.ReadFile(notesPath)

	// Generate narrative using text adapter
	prompt := fmt.Sprintf("Write a first-person travel narrative based on these notes:\n%s\n\nKeep it grounded in the facts provided. Use a warm, observant tone.", string(notes))
	narrative, err := g.text.Generate(ctx, prompt)
	if err != nil {
		return fmt.Errorf("generate narrative: %w", err)
	}

	// Create article document
	doc := domain.ArticleDocument{
		SchemaVersion: 1,
		ArticleID:     "art_" + tripID,
		Title:         trip.Name,
		Slug:          domain.GenerateSlug(trip.Name),
		Excerpt:       narrative[:min(200, len(narrative))],
		Blocks: []domain.ArticleBlock{
			{ID: "b1", Type: domain.BlockHeading, Text: trip.Name, Level: 2},
			{ID: "b2", Type: domain.BlockParagraph, Text: narrative},
		},
	}

	// Create article record
	if err := g.store.Articles.Save(ctx, tripID, trip.Name); err != nil {
		return fmt.Errorf("save article: %w", err)
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
	if err := g.store.Articles.SaveRevision(ctx, rev); err != nil {
		return fmt.Errorf("save revision: %w", err)
	}

	// Update trip status
	if err := g.store.Trips.UpdateStatus(ctx, tripID, domain.StatusEditing); err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
