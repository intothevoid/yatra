package server

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/karan/travel-diary/internal/adapters"
	"github.com/karan/travel-diary/internal/config"
	"github.com/karan/travel-diary/internal/domain"
	"github.com/karan/travel-diary/internal/services"
	"github.com/karan/travel-diary/internal/store"
)

// App holds all dependencies for HTTP handlers.
type App struct {
	cfg      *config.Config
	store    *store.Store
	importer *services.Importer
	genie    *services.Genie
	exporter *services.GhostExporter
}

// NewApp creates a new application.
func NewApp(cfg *config.Config, s *store.Store, imp *services.Importer, gen *services.Genie, exp *services.GhostExporter) *App {
	cfg.EnsureModelTimeout()
	return &App{cfg: cfg, store: s, importer: imp, genie: gen, exporter: exp}
}

// Router returns the chi router with all routes registered.
func (a *App) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"http://localhost:*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type"},
	}))

	r.Get("/health/live", a.healthLive)
	r.Get("/health/ready", a.healthReady)
	r.Get("/api/v1/settings", a.publicSettings)
	r.Put("/api/v1/settings/model", a.configureModel)
	r.Post("/api/v1/settings/model/activate", a.activateModel)
	r.Delete("/api/v1/settings/model", a.resetModel)
	r.Put("/api/v1/settings/vision", a.configureVisionModel)
	r.Put("/api/v1/settings/timeout", a.configureModelTimeout)
	r.Delete("/api/v1/settings/timeout", a.resetModelTimeout)

	r.Get("/api/v1/trips", a.listTrips)
	r.Delete("/api/v1/trips/{id}", a.deleteTrip)
	r.Post("/api/v1/trips/import-path", a.importTrip)
	r.Get("/api/v1/trips/{id}", a.getTrip)
	r.Put("/api/v1/trips/{id}/feature-photo", a.setFeaturePhoto)
	r.Post("/api/v1/trips/{id}/analyse", a.analyseTrip)

	r.Get("/api/v1/trips/{id}/photos", a.listPhotos)
	r.Post("/api/v1/trips/{id}/photos/select-all", a.selectAllPhotos)
	r.Get("/api/v1/photos/{photoId}/thumbnail", a.photoThumbnail)
	r.Patch("/api/v1/trips/{id}/photos/{photoId}", a.updatePhotoSelection)
	r.Post("/api/v1/trips/{id}/photos/approve", a.approvePhotos)
	r.Post("/api/v1/trips/{id}/generate", a.generateArticle)

	r.Get("/api/v1/articles/{id}", a.getArticle)
	r.Put("/api/v1/articles/{id}", a.updateArticle)
	r.Post("/api/v1/articles/{id}/approve", a.approveArticle)

	r.Get("/api/v1/jobs/{id}", a.getJob)
	r.Post("/api/v1/jobs/{id}/cancel", a.cancelJob)
	r.Post("/api/v1/trips/{id}/ghost-draft", a.createGhostDraft)

	// Register upload, progress, and page handlers
	uploadHandler := NewUploadHandler(a.cfg, a.store, a.importer)
	uploadHandler.Register(r)
	progressHandler := NewProgressHandler(a.store)
	progressHandler.Register(r)
	pageHandler := NewPageHandler(a.cfg, a.store, a.importer, a.genie)
	pageHandler.Register(r)
	staticFS, _ := fs.Sub(templates, "static")
	r.Handle("/assets/*", http.FileServer(http.FS(staticFS)))
	r.Handle("/vite.svg", http.FileServer(http.FS(staticFS)))

	return r
}

func (a *App) publicSettings(w http.ResponseWriter, r *http.Request) {
	maxUploadFiles := a.cfg.MaxUploadFiles
	if maxUploadFiles <= 0 {
		maxUploadFiles = 200
	}
	_, hasUIOverride, err := a.store.Settings.Get(r.Context(), store.ModelTimeoutSettingKey)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "settings_unavailable", "could not load model settings")
		return
	}
	timeoutSource := "unset"
	if a.cfg.OMLXTimeoutSec > 0 {
		timeoutSource = "env"
	}
	if hasUIOverride {
		timeoutSource = "settings"
	}
	w.Header().Set("Content-Type", "application/json")
	modelSettings := a.genie.TextProviderStatus()
	json.NewEncoder(w).Encode(map[string]any{
		"max_upload_files":      maxUploadFiles,
		"model_provider":        modelSettings.Provider,
		"model":                 modelSettings.Model,
		"model_endpoint":        modelSettings.Endpoint,
		"model_configured":      modelSettings.Provider == "openrouter" || modelSettings.Provider == "local",
		"local_model":           modelSettings.LocalModel,
		"local_endpoint":        modelSettings.LocalEndpoint,
		"local_configured":      modelSettings.LocalConfigured,
		"openrouter_model":      modelSettings.OpenRouterModel,
		"openrouter_configured": modelSettings.OpenRouterConfigured,
		"vision_model":          modelSettings.VisionModel,
		"vision_endpoint":       modelSettings.VisionEndpoint,
		"vision_configured":     modelSettings.VisionConfigured,
		"model_timeout_sec":     a.cfg.ModelTimeout.Seconds(),
		"timeout_source":        timeoutSource,
	})
}

func (a *App) configureModelTimeout(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	var req struct {
		Seconds int64 `json:"seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid timeout settings")
		return
	}
	previous := a.cfg.ModelTimeout.Seconds()
	if err := a.cfg.ModelTimeout.SetSeconds(req.Seconds); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_timeout", err.Error())
		return
	}
	if err := a.store.Settings.Set(r.Context(), store.ModelTimeoutSettingKey, fmt.Sprint(req.Seconds)); err != nil {
		_ = a.cfg.ModelTimeout.SetSeconds(previous)
		writeAPIError(w, http.StatusInternalServerError, "settings_save_failed", "could not save timeout setting")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"model_timeout_sec": req.Seconds, "timeout_source": "settings"})
}

func (a *App) resetModelTimeout(w http.ResponseWriter, r *http.Request) {
	baselineSeconds := int64(a.cfg.OMLXTimeoutSec)
	if baselineSeconds < 0 {
		baselineSeconds = 0
	}
	previous := a.cfg.ModelTimeout.Seconds()
	if err := a.cfg.ModelTimeout.SetSeconds(baselineSeconds); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "invalid_env_timeout", "OMLX_TIMEOUT_SEC must be a non-negative number of seconds")
		return
	}
	if err := a.store.Settings.Delete(r.Context(), store.ModelTimeoutSettingKey); err != nil {
		_ = a.cfg.ModelTimeout.SetSeconds(previous)
		writeAPIError(w, http.StatusInternalServerError, "settings_reset_failed", "could not reset timeout setting")
		return
	}
	source := "unset"
	if a.cfg.OMLXTimeoutSec > 0 {
		source = "env"
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"model_timeout_sec": a.cfg.ModelTimeout.Seconds(), "timeout_source": source})
}

func (a *App) configureModel(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	var req struct {
		Provider string `json:"provider"`
		Endpoint string `json:"endpoint"`
		Model    string `json:"model"`
		APIKey   string `json:"api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid settings request")
		return
	}
	switch req.Provider {
	case "openrouter":
		if err := a.genie.ConfigureOpenRouter(req.Model, req.APIKey); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_settings", err.Error())
			return
		}
	case "local":
		if err := a.genie.ConfigureLocalModel(req.Endpoint, req.Model, req.APIKey); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_settings", err.Error())
			return
		}
		if err := a.store.Settings.Set(r.Context(), store.LocalModelEndpointSettingKey, strings.TrimRight(strings.TrimSpace(req.Endpoint), "/")); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "settings_save_failed", "could not save local model endpoint")
			return
		}
		if err := a.store.Settings.Set(r.Context(), store.LocalModelIDSettingKey, strings.TrimSpace(req.Model)); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "settings_save_failed", "could not save local model ID")
			return
		}
	default:
		writeAPIError(w, http.StatusBadRequest, "unsupported_provider", "choose a local OpenAI-compatible endpoint or OpenRouter")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(a.genie.TextProviderStatus())
}

func (a *App) activateModel(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	var req struct {
		Provider string `json:"provider"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid provider selection")
		return
	}
	if err := a.genie.ActivateTextProvider(req.Provider); err != nil {
		writeAPIError(w, http.StatusBadRequest, "provider_unavailable", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(a.genie.TextProviderStatus())
}

func (a *App) configureVisionModel(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	var req struct {
		Endpoint string `json:"endpoint"`
		Model    string `json:"model"`
		APIKey   string `json:"api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid vision settings")
		return
	}
	if err := a.genie.ConfigureVisionModel(req.Endpoint, req.Model, req.APIKey); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_vision_settings", err.Error())
		return
	}
	endpoint := strings.TrimRight(strings.TrimSpace(req.Endpoint), "/")
	if err := a.store.Settings.Set(r.Context(), store.VisionModelEndpointSettingKey, endpoint); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "settings_save_failed", "could not save vision endpoint")
		return
	}
	if err := a.store.Settings.Set(r.Context(), store.VisionModelIDSettingKey, strings.TrimSpace(req.Model)); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "settings_save_failed", "could not save vision model ID")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(a.genie.TextProviderStatus())
}

func (a *App) resetModel(w http.ResponseWriter, r *http.Request) {
	a.genie.ResetTextProvider()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(a.genie.TextProviderStatus())
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(apiErr(code, message))
}

// healthLive returns 200 if the process is running.
func (a *App) healthLive(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"ok"}`)
}

// healthReady returns 200 if the database is accessible.
func (a *App) healthReady(w http.ResponseWriter, r *http.Request) {
	_, err := a.store.Trips.List(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(apiErr("database_unavailable", err.Error()))
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"ok"}`)
}

// listTrips returns all trips.
func (a *App) listTrips(w http.ResponseWriter, r *http.Request) {
	trips, err := a.store.Trips.List(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(apiErr("internal_error", err.Error()))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(trips)
}

func (a *App) deleteTrip(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if filepath.Base(id) != id || id == "." || id == ".." {
		http.Error(w, "invalid trip id", http.StatusBadRequest)
		return
	}
	if _, err := a.store.Trips.Get(r.Context(), id); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := a.store.Trips.Delete(r.Context(), id); err != nil {
		if err.Error() == "trip has an active job" {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(apiErr("trip_busy", err.Error()))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(apiErr("delete_failed", err.Error()))
		return
	}
	if err := os.RemoveAll(filepath.Join(a.cfg.DataDir, "assets", id)); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(apiErr("asset_cleanup_failed", "trip deleted but its local photo files could not be fully removed"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// importTrip imports a trip from a server-side directory.
func (a *App) importTrip(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TripID     string `json:"trip_id"`
		SourcePath string `json:"source_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(apiErr("bad_request", "invalid JSON"))
		return
	}
	if req.SourcePath == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(apiErr("bad_request", "source_path required"))
		return
	}

	// Generate trip ID if not provided
	if req.TripID == "" {
		req.TripID = domain.GenerateTripID(req.SourcePath)
	}

	if err := a.importer.ImportTrip(r.Context(), req.TripID, req.SourcePath); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(apiErr("import_failed", err.Error()))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"trip_id": req.TripID})
}

// getTrip returns a trip by ID.
func (a *App) getTrip(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "id")
	trip, err := a.store.Trips.Get(r.Context(), tripID)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(apiErr("not_found", "trip not found"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(trip)
}

func (a *App) setFeaturePhoto(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "id")
	if _, err := a.store.Trips.Get(r.Context(), tripID); err != nil {
		http.NotFound(w, r)
		return
	}
	var req struct {
		PhotoID string `json:"photo_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(apiErr("bad_request", "invalid JSON"))
		return
	}
	if err := a.store.Trips.SetFeaturePhoto(r.Context(), tripID, req.PhotoID); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(apiErr("invalid_feature_photo", err.Error()))
		return
	}
	trip, err := a.store.Trips.Get(r.Context(), tripID)
	if err != nil {
		http.Error(w, "could not reload trip", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(trip)
}

// analyseTrip queues analysis for a trip.
func (a *App) analyseTrip(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "id")

	// Check trip exists
	if _, err := a.store.Trips.Get(r.Context(), tripID); err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(apiErr("not_found", "trip not found"))
		return
	}

	// Queue analysis job
	job := domain.Job{ID: fmt.Sprintf("job_%s_analyse_%d", tripID, time.Now().UnixNano()), Type: domain.JobTypeAnalysePhoto, TargetID: tripID}
	if err := a.store.Jobs.Create(r.Context(), job); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(apiErr("internal_error", err.Error()))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"job_id": job.ID})
}

// listPhotos returns all photos for a trip.
func (a *App) listPhotos(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "id")
	photos, err := a.store.Photos.GetByTrip(r.Context(), tripID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(apiErr("internal_error", err.Error()))
		return
	}
	type photoView struct {
		ID          string             `json:"id"`
		Status      domain.PhotoStatus `json:"status"`
		Thumbnail   string             `json:"thumbnail"`
		Description string             `json:"description,omitempty"`
		Width       int                `json:"width,omitempty"`
		Height      int                `json:"height,omitempty"`
	}
	views := make([]photoView, 0, len(photos))
	for _, p := range photos {
		var description string
		if p.VLMResult != "" {
			var result domain.VisionResult
			if json.Unmarshal([]byte(p.VLMResult), &result) == nil {
				description = result.Observed.Description
			}
		}
		previewPath := p.PreviewPath
		if previewPath == "" {
			previewPath = p.ThumbnailPath
		}
		width, height, dimErr := adapters.ImageDimensions(previewPath)
		if dimErr != nil {
			width, height, _ = adapters.ImageDimensions(p.ThumbnailPath)
		}
		views = append(views, photoView{ID: p.ID, Status: p.Status, Thumbnail: "/api/v1/photos/" + p.ID + "/thumbnail", Description: description, Width: width, Height: height})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(views)
}

func (a *App) photoThumbnail(w http.ResponseWriter, r *http.Request) {
	photo, err := a.store.Photos.Get(r.Context(), chi.URLParam(r, "photoId"))
	if err != nil || photo.ThumbnailPath == "" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, photo.ThumbnailPath)
}

func (a *App) selectAllPhotos(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "id")
	if _, err := a.store.Trips.Get(r.Context(), tripID); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := a.store.Photos.SelectAllByTrip(r.Context(), tripID); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(apiErr("selection_failed", err.Error()))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"ok":true}`)
}

// updatePhotoSelection changes a photo's selection status.
func (a *App) updatePhotoSelection(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "id")
	photoID := chi.URLParam(r, "photoId")

	var req struct {
		Status domain.PhotoStatus `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(apiErr("bad_request", "invalid JSON"))
		return
	}

	trip, err := a.store.Trips.Get(r.Context(), tripID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	photos, err := a.store.Photos.GetByTrip(r.Context(), tripID)
	if err != nil {
		http.Error(w, "could not load trip photos", http.StatusInternalServerError)
		return
	}
	belongs := false
	for _, photo := range photos {
		if photo.ID == photoID {
			belongs = true
			break
		}
	}
	if !belongs {
		http.NotFound(w, r)
		return
	}
	if err := a.store.Photos.UpdateSelection(r.Context(), photoID, req.Status); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(apiErr("internal_error", err.Error()))
		return
	}
	if req.Status == domain.PhotoStatusNotSelected && trip.FeaturePhotoID == photoID {
		if err := a.store.Trips.ClearFeaturePhotoIf(r.Context(), tripID, photoID); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(apiErr("internal_error", err.Error()))
			return
		}
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"ok":true}`)
}

// approvePhotos approves the current photo selection.
func (a *App) approvePhotos(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "id")
	trip, err := a.store.Trips.Get(r.Context(), tripID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if trip.Status == domain.StatusImported {
		if err := a.store.Trips.UpdateStatus(r.Context(), tripID, domain.StatusPhotoReview); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	if err := a.store.Trips.UpdateStatus(r.Context(), tripID, domain.StatusPhotosApproved); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(apiErr("invalid_transition", err.Error()))
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"ok":true}`)
}

func (a *App) generateArticle(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "id")
	if _, err := a.store.Trips.Get(r.Context(), tripID); err != nil {
		http.NotFound(w, r)
		return
	}
	job := domain.Job{ID: fmt.Sprintf("job_%s_generate_%d", tripID, time.Now().UnixNano()), Type: domain.JobTypeGenerateStory, TargetID: tripID}
	if err := a.store.Jobs.Create(r.Context(), job); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(apiErr("job_create_failed", err.Error()))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"job_id": job.ID})
}

// getArticle returns the latest article revision.
func (a *App) getArticle(w http.ResponseWriter, r *http.Request) {
	articleID := chi.URLParam(r, "id")
	doc, err := a.store.Articles.GetLatest(r.Context(), articleID)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(apiErr("not_found", "article not found"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(doc)
}

// updateArticle saves an edited article document.
func (a *App) updateArticle(w http.ResponseWriter, r *http.Request) {
	articleID := chi.URLParam(r, "id")

	var doc domain.ArticleDocument
	if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(apiErr("bad_request", "invalid JSON"))
		return
	}

	docJSON, _ := json.Marshal(doc)
	rev := domain.ArticleRevision{
		ID:        fmt.Sprintf("rev_%d", time.Now().UnixNano()),
		ArticleID: articleID,
		Author:    "user",
		Reason:    "user edit",
		Document:  string(docJSON),
	}
	if err := a.store.Articles.SaveRevision(r.Context(), rev); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(apiErr("internal_error", err.Error()))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rev)
}

// approveArticle marks an article revision as approved.
func (a *App) approveArticle(w http.ResponseWriter, r *http.Request) {
	articleID := chi.URLParam(r, "id")

	var req struct {
		RevisionID string `json:"revision_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(apiErr("bad_request", "invalid JSON"))
		return
	}

	if err := a.store.Articles.ApproveRevision(r.Context(), articleID, req.RevisionID); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(apiErr("internal_error", err.Error()))
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"ok":true}`)
}

// getJob returns a job's status.
func (a *App) getJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")
	job, err := a.store.Jobs.Get(r.Context(), jobID)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(apiErr("not_found", "job not found"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(job)
}

// cancelJob requests job cancellation.
func (a *App) cancelJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")
	if err := a.store.Jobs.Cancel(r.Context(), jobID); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(apiErr("internal_error", err.Error()))
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"ok":true}`)
}

// createGhostDraft creates or updates a Ghost draft.
func (a *App) createGhostDraft(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "id")
	if a.exporter == nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(apiErr("not_configured", "Ghost integration not configured"))
		return
	}
	if err := a.exporter.CreateDraft(r.Context(), tripID); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(apiErr("export_failed", err.Error()))
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"ok":true}`)
}

// apiErr is the API error response shape.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

func apiErr(code, message string) APIError {
	return APIError{Code: code, Message: message}
}
