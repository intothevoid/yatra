package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/karan/travel-diary/internal/adapters"
	"github.com/karan/travel-diary/internal/config"
	"github.com/karan/travel-diary/internal/domain"
	"github.com/karan/travel-diary/internal/store"
)

// Genie runs the analysis pipeline: VLM analysis of photos,
// experience extraction, photo matching, and article generation.
type Genie struct {
	cfg             *config.Config
	store           *store.Store
	vlm             *adapters.SwitchableVLMAdapter
	text            *adapters.SwitchableTextAdapter
	modelMu         sync.RWMutex
	provider        string
	model           string
	endpoint        string
	startupText     adapters.TextAdapter
	startupProvider string
	startupModel    string
	startupEndpoint string
	localText       adapters.TextAdapter
	openRouterText  adapters.TextAdapter
	localModel      string
	localEndpoint   string
	openRouterModel string
	visionText      adapters.VLMAdapter
	visionModel     string
	visionEndpoint  string
	proc            adapters.ImageProcessor
	extractor       *EvidenceExtractor
	matcher         *PhotoMatcher
	drafter         *SectionDrafter
	auditor         *GroundingAuditor
}

type TextProviderSettings struct {
	Provider             string `json:"model_provider"`
	Model                string `json:"model"`
	Endpoint             string `json:"endpoint"`
	LocalModel           string `json:"local_model"`
	LocalEndpoint        string `json:"local_endpoint"`
	LocalConfigured      bool   `json:"local_configured"`
	OpenRouterModel      string `json:"openrouter_model"`
	OpenRouterConfigured bool   `json:"openrouter_configured"`
	VisionModel          string `json:"vision_model"`
	VisionEndpoint       string `json:"vision_endpoint"`
	VisionConfigured     bool   `json:"vision_configured"`
}

// NewGenie creates a new Genie service.
func NewGenie(cfg *config.Config, s *store.Store, vlm adapters.VLMAdapter, text adapters.TextAdapter, proc adapters.ImageProcessor) *Genie {
	cfg.EnsureModelTimeout()
	switchableText := adapters.NewSwitchableTextAdapter(text)
	switchableVLM := adapters.NewSwitchableVLMAdapter(vlm)
	extractor := NewEvidenceExtractor(s, switchableText)
	matcher := NewPhotoMatcher(s)
	auditor := NewGroundingAuditor(s)
	drafter := NewSectionDrafter(s, switchableText, nil)
	drafter.SetConfig(cfg)
	provider, model, endpoint := "fake", "", ""
	localModel, localEndpoint := "", ""
	if cfg != nil {
		localEndpoint = cfg.OMLXWriterBaseURL
		if localEndpoint == "" {
			localEndpoint = cfg.OMLXBaseURL
		}
		localModel = cfg.OMLXWriterModel
	}
	var localText adapters.TextAdapter
	if localModel != "" {
		provider, model, endpoint = "local", localModel, localEndpoint
		localText = text
	}
	visionModel, visionEndpoint := "", ""
	if cfg != nil {
		visionModel, visionEndpoint = cfg.OMLXVisionModel, cfg.OMLXVisionBaseURL
		if visionEndpoint == "" {
			visionEndpoint = cfg.OMLXBaseURL
		}
		if visionModel == "" {
			visionModel = cfg.OMLXWriterModel
		}
	}
	var visionText adapters.VLMAdapter
	if visionModel != "" {
		visionText = vlm
	}

	return &Genie{
		cfg:             cfg,
		store:           s,
		vlm:             switchableVLM,
		text:            switchableText,
		provider:        provider,
		model:           model,
		endpoint:        endpoint,
		startupText:     text,
		startupProvider: provider,
		startupModel:    model,
		startupEndpoint: endpoint,
		localText:       localText,
		localModel:      localModel,
		localEndpoint:   localEndpoint,
		visionText:      visionText,
		visionModel:     visionModel,
		visionEndpoint:  visionEndpoint,
		proc:            proc,
		extractor:       extractor,
		matcher:         matcher,
		drafter:         drafter,
		auditor:         auditor,
	}
}

// ConfigureOpenRouter switches text generation to OpenRouter for this process.
// The key is held only in memory and is not saved to the database or config.
func (g *Genie) ConfigureOpenRouter(model, apiKey string) error {
	model = strings.TrimSpace(model)
	apiKey = strings.TrimSpace(apiKey)
	if model == "" {
		return fmt.Errorf("OpenRouter model is required")
	}
	if apiKey == "" {
		return fmt.Errorf("OpenRouter API key is required")
	}
	var timeout *config.ModelTimeout
	if g.cfg != nil {
		timeout = g.cfg.ModelTimeout
	}
	adapter := adapters.NewOpenRouterTextAdapter(model, apiKey, timeout)
	g.modelMu.Lock()
	g.text.Set(adapter)
	g.openRouterText = adapter
	g.openRouterModel = model
	g.provider, g.model, g.endpoint = "openrouter", model, "https://openrouter.ai/api/v1"
	g.modelMu.Unlock()
	return nil
}

// ConfigureLocalModel configures a local OpenAI-compatible text endpoint.
// The endpoint and model are safe to persist; the API key remains in memory.
func (g *Genie) ConfigureLocalModel(endpoint, model, apiKey string) error {
	endpoint, err := normalizeOpenAIEndpoint(endpoint)
	if err != nil {
		return err
	}
	model = strings.TrimSpace(model)
	apiKey = strings.TrimSpace(apiKey)
	if model == "" {
		return fmt.Errorf("local model ID is required")
	}
	if apiKey == "" && g.cfg != nil {
		apiKey = g.cfg.OMLXAPIKey
	}
	var timeout *config.ModelTimeout
	if g.cfg != nil {
		timeout = g.cfg.ModelTimeout
	}
	adapter := adapters.NewOpenAICompatibleTextAdapter(endpoint, model, apiKey, timeout)
	g.modelMu.Lock()
	g.text.Set(adapter)
	g.localText = adapter
	g.localModel, g.localEndpoint = model, endpoint
	g.provider, g.model, g.endpoint = "local", model, endpoint
	g.modelMu.Unlock()
	return nil
}

// ConfigureVisionModel configures a separate OpenAI-compatible vision endpoint.
// The endpoint and model may be persisted; API credentials remain in memory.
func (g *Genie) ConfigureVisionModel(endpoint, model, apiKey string) error {
	endpoint, err := normalizeOpenAIEndpoint(endpoint)
	if err != nil {
		return err
	}
	model = strings.TrimSpace(model)
	apiKey = strings.TrimSpace(apiKey)
	if model == "" {
		return fmt.Errorf("vision model ID is required")
	}
	if apiKey == "" && g.cfg != nil {
		apiKey = g.cfg.OMLXAPIKey
	}
	var timeout *config.ModelTimeout
	if g.cfg != nil {
		timeout = g.cfg.ModelTimeout
	}
	adapter := adapters.NewOpenAIVisionAdapterForEndpoint(endpoint, model, apiKey, timeout)
	g.modelMu.Lock()
	g.vlm.Set(adapter)
	g.visionText = adapter
	g.visionModel, g.visionEndpoint = model, endpoint
	g.modelMu.Unlock()
	return nil
}

func normalizeOpenAIEndpoint(endpoint string) (string, error) {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	parsed, err := url.Parse(endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("endpoint must be an http or https URL without credentials, query, or fragment")
	}
	return endpoint, nil
}

func (g *Genie) ActivateTextProvider(provider string) error {
	g.modelMu.Lock()
	defer g.modelMu.Unlock()
	switch provider {
	case "local":
		if g.localText == nil || g.localModel == "" {
			return fmt.Errorf("local model is not configured")
		}
		g.text.Set(g.localText)
		g.provider, g.model, g.endpoint = "local", g.localModel, g.localEndpoint
	case "openrouter":
		if g.openRouterText == nil || g.openRouterModel == "" {
			return fmt.Errorf("OpenRouter is not configured in this session")
		}
		g.text.Set(g.openRouterText)
		g.provider, g.model, g.endpoint = "openrouter", g.openRouterModel, "https://openrouter.ai/api/v1"
	default:
		return fmt.Errorf("unsupported model provider")
	}
	return nil
}

// ResetTextProvider drops the in-memory OpenRouter configuration and returns
// text generation to the configured local or startup adapter.
func (g *Genie) ResetTextProvider() {
	g.modelMu.Lock()
	g.openRouterText, g.openRouterModel = nil, ""
	if g.localText != nil && g.localModel != "" {
		g.text.Set(g.localText)
		g.provider, g.model, g.endpoint = "local", g.localModel, g.localEndpoint
	} else {
		g.text.Reset()
		g.provider, g.model, g.endpoint = g.startupProvider, g.startupModel, g.startupEndpoint
	}
	g.modelMu.Unlock()
}

func (g *Genie) TextProviderStatus() TextProviderSettings {
	g.modelMu.RLock()
	defer g.modelMu.RUnlock()
	return TextProviderSettings{
		Provider: g.provider, Model: g.model, Endpoint: g.endpoint,
		LocalModel: g.localModel, LocalEndpoint: g.localEndpoint,
		LocalConfigured:      g.localText != nil && g.localModel != "",
		OpenRouterModel:      g.openRouterModel,
		OpenRouterConfigured: g.openRouterText != nil && g.openRouterModel != "",
		VisionModel:          g.visionModel,
		VisionEndpoint:       g.visionEndpoint,
		VisionConfigured:     g.visionText != nil && g.visionModel != "",
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
	} else if trip.Status == domain.StatusGenerating {
		// A backend restart can leave the trip in GENERATING after its worker
		// disappeared. Treat a new generation request as a resume attempt.
		rollbackStatus = domain.StatusPhotosApproved
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
	if trip.Status != domain.StatusPhotosApproved && trip.Status != domain.StatusEditing && trip.Status != domain.StatusGenerating {
		return fmt.Errorf("trip must be in photo review or editing before generation (status %s)", trip.Status)
	}
	if trip.Status != domain.StatusGenerating {
		if err := g.store.Trips.UpdateStatus(ctx, tripID, domain.StatusGenerating); err != nil {
			return fmt.Errorf("begin generation: %w", err)
		}
	}
	defer func() {
		if retErr != nil {
			// Cancellation also cancels ctx; use a detached context so the trip
			// does not remain stuck in GENERATING.
			_ = g.store.Trips.UpdateStatus(context.WithoutCancel(ctx), tripID, rollbackStatus)
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
