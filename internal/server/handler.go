package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/karan/travel-diary/internal/config"
	"github.com/karan/travel-diary/internal/domain"
	"github.com/karan/travel-diary/internal/services"
	"github.com/karan/travel-diary/internal/store"
)

// App holds all dependencies for HTTP handlers.
type App struct {
	cfg       *config.Config
	store     *store.Store
	importer  *services.Importer
	genie     *services.Genie
	exporter  *services.GhostExporter
}

// NewApp creates a new application.
func NewApp(cfg *config.Config, s *store.Store, imp *services.Importer, gen *services.Genie, exp *services.GhostExporter) *App {
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

	r.Get("/api/v1/trips", a.listTrips)
	r.Post("/api/v1/trips/import-path", a.importTrip)
	r.Get("/api/v1/trips/{id}", a.getTrip)
	r.Post("/api/v1/trips/{id}/analyse", a.analyseTrip)

	r.Get("/api/v1/trips/{id}/photos", a.listPhotos)
	r.Patch("/api/v1/trips/{id}/photos/{photoId}", a.updatePhotoSelection)
	r.Post("/api/v1/trips/{id}/photos/approve", a.approvePhotos)

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

	return r
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
	job := domain.Job{
		ID:           fmt.Sprintf("job_%s_analyse", tripID),
		Type:         domain.JobTypeAnalysePhoto,
		TargetID:     tripID,
		IdempotencyKey: tripID,
	}
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
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(photos)
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

	if err := a.store.Photos.UpdateSelection(r.Context(), photoID, req.Status); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(apiErr("internal_error", err.Error()))
		return
	}
	_ = tripID
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"ok":true}`)
}

// approvePhotos approves the current photo selection.
func (a *App) approvePhotos(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "id")

	if err := a.store.Trips.UpdateStatus(r.Context(), tripID, domain.StatusPhotosApproved); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(apiErr("invalid_transition", err.Error()))
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"ok":true}`)
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
