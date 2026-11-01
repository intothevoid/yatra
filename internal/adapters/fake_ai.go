package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"

	"github.com/karan/travel-diary/internal/domain"
)

// FakeVLMAdapter returns deterministic VisionResults for testing.
type FakeVLMAdapter struct {
	mu         sync.Mutex
	photoCount int
}

var _ VLMAdapter = (*FakeVLMAdapter)(nil)

// Analyze returns a pre-stated VisionResult based on the photo ID.
func (f *FakeVLMAdapter) Analyze(ctx context.Context, photoID string, imageBytes []byte) (*domain.VisionResult, error) {
	f.mu.Lock()
	f.photoCount++
	count := f.photoCount
	f.mu.Unlock()

	return &domain.VisionResult{
		SchemaVersion: 1,
		PhotoID:       photoID,
		Observed: domain.ObservedResult{
			Description: fmt.Sprintf("A photograph of a scenic view (photo %d)", count),
			Subjects:    []string{"landscape", "building", "sky"},
			Category:    "scenery",
			VisibleText: []string{},
		},
		Hypotheses: []domain.Hypothesis{
			{Type: "place", Value: "Kyoto", Confidence: 0.35},
		},
		QualityFlags: []string{},
		SafetyFlags:  []string{},
	}, nil
}

// ModelSupportsImages always returns true for the fake adapter.
func (f *FakeVLMAdapter) ModelSupportsImages(ctx context.Context) (bool, error) {
	return true, nil
}

// FakeTextAdapter returns deterministic text for testing.
type FakeTextAdapter struct{}

var _ TextAdapter = FakeTextAdapter{}

// Generate returns a simple response.
func (f FakeTextAdapter) Generate(ctx context.Context, prompt string) (string, error) {
	return "We reached the destination early in the morning. The views were spectacular.", nil
}

// FakeServer is a standalone HTTP server that simulates an OpenAI-compatible API.
// It supports /v1/models and /v1/chat/completions with configurable behaviors.
type FakeServer struct {
	server *httptest.Server
}

// NewFakeServer creates a fake server on a random port.
func NewFakeServer() *FakeServer {
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"id":"fake-vision","object":"model"},{"id":"fake-text","object":"model"}]}`)
	})

	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error":{"message":"API key required","type":"authentication_error"}}`)
			return
		}

		// Check if the request contains an image
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)

		// Return a vision result if there's an image, otherwise text
		if messages, ok := req["messages"].([]any); ok && len(messages) > 0 {
			msg := messages[0].(map[string]any)
			if content, ok := msg["content"].([]any); ok {
				for _, part := range content {
					if p, ok := part.(map[string]any); ok && p["type"] == "image_url" {
						// Vision request — return a valid JSON vision result
						result := map[string]any{
							"schema_version": 1,
							"photo_id":       "p1",
							"observed": map[string]any{
								"description":  "a test image",
								"subjects":     []string{"test"},
								"category":     "test",
								"visible_text": []string{},
							},
							"hypotheses":    []any{},
							"quality_flags": []any{},
							"safety_flags":  []any{},
						}
						resultJSON, _ := json.Marshal(result)
						// content must be a string containing the JSON
						response := map[string]any{
							"choices": []any{
								map[string]any{
									"message": map[string]any{
										"content": string(resultJSON),
									},
								},
							},
						}
						json.NewEncoder(w).Encode(response)
						return
					}
				}
			}
		}

		// Text request
		fmt.Fprint(w, `{"choices":[{"message":{"content":"Generated text response."}}]}`)
	})

	return &FakeServer{
		server: httptest.NewServer(mux),
	}
}

// URL returns the server's base URL (without /v1).
func (f *FakeServer) URL() string {
	return f.server.URL
}

// Close shuts down the server.
func (f *FakeServer) Close() {
	f.server.Close()
}

// FakeVLMAdapterWithServer creates a VLMAdapter that talks to the fake server.
func (f *FakeServer) FakeVLMAdapter() *OpenAIVisionAdapter {
	return &OpenAIVisionAdapter{
		BaseURL: f.URL() + "/v1",
		Model:   "fake-vision",
		APIKey:  "test-key",
		HTTP:    &http.Client{},
	}
}

// FakeTextAdapterWithServer creates a TextAdapter that talks to the fake server.
func (f *FakeServer) FakeTextAdapter() *OpenAITextAdapter {
	return &OpenAITextAdapter{
		BaseURL: f.URL() + "/v1",
		Model:   "fake-text",
		APIKey:  "test-key",
		HTTP:    &http.Client{},
	}
}
