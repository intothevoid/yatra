package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/karan/travel-diary/internal/adapters"
	"github.com/karan/travel-diary/internal/config"
	"github.com/karan/travel-diary/internal/domain"
	"github.com/karan/travel-diary/internal/store"
)

// Genie runs the analysis pipeline: VLM analysis of photos,
// experience extraction, photo matching, and article generation.
type Genie struct {
	cfg       *config.Config
	store     *store.Store
	vlm       adapters.VLMAdapter
	text      adapters.TextAdapter
	proc      adapters.ImageProcessor
	extractor *EvidenceExtractor
	matcher   *PhotoMatcher
	drafter   *SectionDrafter
	auditor   *GroundingAuditor
}

// NewGenie creates a new Genie service.
func NewGenie(cfg *config.Config, s *store.Store, vlm adapters.VLMAdapter, text adapters.TextAdapter, proc adapters.ImageProcessor) *Genie {
	extractor := NewEvidenceExtractor(s, text)
	matcher := NewPhotoMatcher(s)
	auditor := NewGroundingAuditor(s)
	drafter := NewSectionDrafter(s, text, nil)
	drafter.SetConfig(cfg)

	return &Genie{
		cfg:       cfg,
		store:     s,
		vlm:       vlm,
		text:      text,
		proc:      proc,
		extractor: extractor,
		matcher:   matcher,
		drafter:   drafter,
		auditor:   auditor,
	}
}

// AnalyseTrip runs VLM analysis on all photos in a trip.
func (g *Genie) AnalyseTrip(ctx context.Context, tripID string) error {
	return g.AnalyseTripJob(ctx, tripID, "job_"+tripID+"_analyse")
}

func (g *Genie) AnalyseTripJob(ctx context.Context, tripID, jobID string) error {
	trip, err := g.store.Trips.Get(ctx, tripID)
	if err != nil {
		return fmt.Errorf("get trip: %w", err)
	}
	returnStatus := domain.StatusPhotoReview
	if trip.Status == domain.StatusPhotosApproved {
		returnStatus = domain.StatusPhotosApproved
	}
	// Update trip status while analysis is running.
	if err := g.store.Trips.UpdateStatus(ctx, tripID, domain.StatusAnalysing); err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	// Get photos
	photos, err := g.store.Photos.GetByTrip(ctx, tripID)
	if err != nil {
		return fmt.Errorf("get photos: %w", err)
	}

	// Update job with total items
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
	if err := g.store.Trips.UpdateStatus(ctx, tripID, returnStatus); err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	return nil
}

// GenerateArticle creates an initial article from the trip's notes and photos.
func (g *Genie) GenerateArticle(ctx context.Context, tripID string) (retErr error) {
	trip, err := g.store.Trips.Get(ctx, tripID)
	if err != nil {
		return fmt.Errorf("get trip: %w", err)
	}
	rollbackStatus := domain.StatusPhotosApproved
	if trip.Status == domain.StatusEditing {
		rollbackStatus = domain.StatusEditing
	}
	if trip.Status == domain.StatusImported {
		if err := g.store.Trips.UpdateStatus(ctx, tripID, domain.StatusAnalysing); err != nil {
			return fmt.Errorf("prepare trip for generation: %w", err)
		}
		trip.Status = domain.StatusAnalysing
	}
	if trip.Status == domain.StatusAnalysing {
		if err := g.store.Trips.UpdateStatus(ctx, tripID, domain.StatusPhotoReview); err != nil {
			return fmt.Errorf("prepare photo review: %w", err)
		}
		trip.Status = domain.StatusPhotoReview
	}
	if trip.Status == domain.StatusPhotoReview {
		if err := g.store.Trips.UpdateStatus(ctx, tripID, domain.StatusPhotosApproved); err != nil {
			return fmt.Errorf("approve photos: %w", err)
		}
	}
	trip, err = g.store.Trips.Get(ctx, tripID)
	if err != nil {
		return fmt.Errorf("reload trip: %w", err)
	}
	if trip.Status != domain.StatusPhotosApproved && trip.Status != domain.StatusEditing {
		return fmt.Errorf("trip must be in photo review or editing before generation (status %s)", trip.Status)
	}
	if err := g.store.Trips.UpdateStatus(ctx, tripID, domain.StatusGenerating); err != nil {
		return fmt.Errorf("begin generation: %w", err)
	}
	defer func() {
		if retErr != nil {
			_ = g.store.Trips.UpdateStatus(ctx, tripID, rollbackStatus)
		}
	}()

	// Extract experiences from notes
	if err := g.extractor.Extract(ctx, tripID); err != nil {
		return fmt.Errorf("extract experiences: %w", err)
	}

	// Match photos to experiences
	if err := g.matcher.Match(ctx, tripID); err != nil {
		return fmt.Errorf("match photos: %w", err)
	}

	// Generate article
	doc, err := g.drafter.Generate(ctx, tripID)
	if err != nil {
		return fmt.Errorf("generate article: %w", err)
	}

	// Audit for grounding issues
	issues := g.auditor.Audit(ctx, tripID, doc)
	if len(issues) > 0 {
		// Log issues but don't fail - user can resolve them
		for _, issue := range issues {
			fmt.Println("WARNING: grounding issue:", issue.Detail)
		}
	}

	// Update trip status: GENERATING -> EDITING
	if err := g.store.Trips.UpdateStatus(ctx, tripID, domain.StatusEditing); err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	return nil
}
