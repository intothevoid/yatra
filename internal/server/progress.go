package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/karan/travel-diary/internal/domain"
	"github.com/karan/travel-diary/internal/store"
)

// ProgressHandler provides SSE job progress streaming.
type ProgressHandler struct {
	store *store.Store
}

// NewProgressHandler creates a new progress handler.
func NewProgressHandler(s *store.Store) *ProgressHandler {
	return &ProgressHandler{store: s}
}

// Register routes on the chi router.
func (h *ProgressHandler) Register(r chi.Router) {
	r.Get("/api/v1/jobs/{id}/progress", h.streamProgress)
}

// streamProgress sends job progress as an SSE stream.
func (h *ProgressHandler) streamProgress(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Send initial event
	job, err := h.store.Jobs.Get(r.Context(), jobID)
	if err != nil {
		sendEvent(w, flusher, "error", map[string]any{"message": "job not found"})
		return
	}

	// Stream progress updates
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			job, err = h.store.Jobs.Get(r.Context(), jobID)
			if err != nil {
				sendEvent(w, flusher, "error", map[string]any{"message": err.Error()})
				return
			}

			data := map[string]any{
				"state":    string(job.State),
				"step":     job.Step,
				"progress": job.Progress,
			}
			if job.Error != "" {
				data["error"] = job.Error
			}

			event := "progress"
			if job.State == domain.JobStateSucceeded || job.State == domain.JobStateFailed || job.State == domain.JobStateCancelled {
				event = "complete"
			}

			sendEvent(w, flusher, event, data)

			// Stop streaming when job is done
			if job.State == domain.JobStateSucceeded || job.State == domain.JobStateFailed || job.State == domain.JobStateCancelled {
				return
			}
		}
	}
}

func sendEvent(w http.ResponseWriter, flusher http.Flusher, event string, data map[string]any) {
	payload, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload)
	flusher.Flush()
}
