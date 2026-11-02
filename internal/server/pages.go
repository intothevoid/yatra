package server

import (
	"embed"
	"encoding/json"
	"html/template"
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"

	"github.com/karan/travel-diary/internal/config"
	"github.com/karan/travel-diary/internal/domain"
	"github.com/karan/travel-diary/internal/services"
	"github.com/karan/travel-diary/internal/store"
)

//go:embed templates/*.html
var templates embed.FS

// PageHandler serves HTMX page fragments.
type PageHandler struct {
	cfg      *config.Config
	store    *store.Store
	importer *services.Importer
	genie    *services.Genie
	clusterer *services.Clusterer
	ranker  *services.Ranker
	tmpl    *template.Template
}

// NewPageHandler creates a new page handler.
func NewPageHandler(cfg *config.Config, s *store.Store, imp *services.Importer, gen *services.Genie) *PageHandler {
	tmpl := template.Must(template.New("").Funcs(template.FuncMap{
		"statusClass": func(s domain.TripStatus) string {
			switch s {
			case domain.StatusImported:
				return "badge"
			case domain.StatusAnalysing:
				return "badge"
			case domain.StatusPhotoReview:
				return "badge"
			default:
				return "badge"
			}
		},
	}).ParseFS(templates, "templates/*.html"))

	return &PageHandler{
		cfg:       cfg,
		store:     s,
		importer:  imp,
		genie:     gen,
		clusterer: services.NewClusterer(s),
		ranker:    services.NewRanker(s),
		tmpl:      tmpl,
	}
}

// Register routes on the chi router.
func (h *PageHandler) Register(r chi.Router) {
	r.Get("/", h.index)
	r.Get("/api/v1/page/trips", h.listTrips)
	r.Get("/api/v1/page/trip/{id}", h.tripDetail)
	r.Get("/api/v1/page/article/{id}", h.articleEditor)
	r.Post("/api/v1/page/import", h.importTrip)
	r.Post("/api/v1/page/analyse/{tripId}", h.analyseTrip)
	r.Post("/api/v1/page/rank/{tripId}", h.rankPhotos)
	r.Post("/api/v1/page/generate/{tripId}", h.generateArticle)
	r.Put("/api/v1/articles/{articleId}/blocks/{blockId}", h.updateBlock)
}

func (h *PageHandler) index(w http.ResponseWriter, r *http.Request) {
	h.tmpl.ExecuteTemplate(w, "index", nil)
}

func (h *PageHandler) listTrips(w http.ResponseWriter, r *http.Request) {
	trips, err := h.store.Trips.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := struct {
		Trips []domain.Trip
	}{Trips: trips}

	w.Header().Set("Content-Type", "text/html")
	h.tmpl.ExecuteTemplate(w, "trips", data)
}

func (h *PageHandler) tripDetail(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "id")
	trip, err := h.store.Trips.Get(r.Context(), tripID)
	if err != nil {
		http.Error(w, "trip not found", http.StatusNotFound)
		return
	}

	photos, _ := h.store.Photos.GetByTrip(r.Context(), tripID)

	data := struct {
		Trip   *domain.Trip
		Photos []domain.Photo
	}{Trip: trip, Photos: photos}

	w.Header().Set("Content-Type", "text/html")
	h.tmpl.ExecuteTemplate(w, "trip", data)
}

func (h *PageHandler) articleEditor(w http.ResponseWriter, r *http.Request) {
	articleID := chi.URLParam(r, "id")
	doc, err := h.store.Articles.GetLatest(r.Context(), articleID)
	if err != nil {
		http.Error(w, "article not found", http.StatusNotFound)
		return
	}

	data := struct {
		Document *domain.ArticleDocument
	}{Document: doc}

	w.Header().Set("Content-Type", "text/html")
	h.tmpl.ExecuteTemplate(w, "editor", data)
}

func (h *PageHandler) importTrip(w http.ResponseWriter, r *http.Request) {
	sourcePath := r.FormValue("source_path")
	if sourcePath == "" {
		http.Error(w, "source_path required", http.StatusBadRequest)
		return
	}

	tripID := filepath.Base(sourcePath)
	if err := h.importer.ImportTrip(r.Context(), tripID, sourcePath); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	trips, _ := h.store.Trips.List(r.Context())
	w.Header().Set("Content-Type", "text/html")
	h.tmpl.ExecuteTemplate(w, "trips", struct {
		Trips []domain.Trip
	}{Trips: trips})
}

func (h *PageHandler) analyseTrip(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "tripId")

	// Run analysis (includes status transitions: ANALYSING -> PHOTO_REVIEW).
	if err := h.genie.AnalyseTrip(r.Context(), tripID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	trip, _ := h.store.Trips.Get(r.Context(), tripID)
	photos, _ := h.store.Photos.GetByTrip(r.Context(), tripID)
	w.Header().Set("Content-Type", "text/html")
	h.tmpl.ExecuteTemplate(w, "trip", struct {
		Trip   *domain.Trip
		Photos []domain.Photo
	}{Trip: trip, Photos: photos})
}

func (h *PageHandler) rankPhotos(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "tripId")
	if err := h.ranker.Rank(r.Context(), tripID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Update trip status through the proper state machine.
	// If the trip was just imported (no analysis done), first transition
	// IMPORTED -> PHOTO_REVIEW, then -> PHOTOS_APPROVED.
	current, _ := h.store.Trips.Get(r.Context(), tripID)
	if current.Status == domain.StatusImported {
		// Go IMPORTED -> PHOTO_REVIEW -> PHOTOS_APPROVED
		if err := h.store.Trips.UpdateStatus(r.Context(), tripID, domain.StatusPhotoReview); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if err := h.store.Trips.UpdateStatus(r.Context(), tripID, domain.StatusPhotosApproved); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	trip, _ := h.store.Trips.Get(r.Context(), tripID)
	photos, _ := h.store.Photos.GetByTrip(r.Context(), tripID)
	w.Header().Set("Content-Type", "text/html")
	h.tmpl.ExecuteTemplate(w, "trip", struct {
		Trip   *domain.Trip
		Photos []domain.Photo
	}{Trip: trip, Photos: photos})
}

func (h *PageHandler) generateArticle(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "tripId")

	// Generate article (includes status transitions: GENERATING -> EDITING).
	if err := h.genie.GenerateArticle(r.Context(), tripID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	trip, _ := h.store.Trips.Get(r.Context(), tripID)
	photos, _ := h.store.Photos.GetByTrip(r.Context(), tripID)
	w.Header().Set("Content-Type", "text/html")
	h.tmpl.ExecuteTemplate(w, "trip", struct {
		Trip   *domain.Trip
		Photos []domain.Photo
	}{Trip: trip, Photos: photos})
}

func (h *PageHandler) updateBlock(w http.ResponseWriter, r *http.Request) {
	articleID := chi.URLParam(r, "articleId")
	blockID := chi.URLParam(r, "blockId")

	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Get current document and update block
	doc, err := h.store.Articles.GetLatest(r.Context(), articleID)
	if err != nil {
		http.Error(w, "article not found", http.StatusNotFound)
		return
	}

	for i, block := range doc.Blocks {
		if block.ID == blockID {
			block.Text = req.Text
			doc.Blocks[i] = block
			break
		}
	}

	// Save as new revision
	docJSON, _ := json.Marshal(doc)
	rev := domain.ArticleRevision{
		ID:        "rev_" + blockID,
		ArticleID: articleID,
		Author:    "user",
		Reason:    "block edit",
		Document:  string(docJSON),
	}
	if err := h.store.Articles.SaveRevision(r.Context(), rev); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
}
