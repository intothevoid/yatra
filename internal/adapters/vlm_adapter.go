package adapters

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/karan/travel-diary/internal/config"
	"github.com/karan/travel-diary/internal/domain"
)

// OpenAIVisionAdapter sends images to an OpenAI-compatible endpoint.
type OpenAIVisionAdapter struct {
	BaseURL string
	Model   string
	APIKey  string
	Timeout *config.ModelTimeout
	HTTP    *http.Client
}

// Analyze sends an image to the VLM and returns a structured result.
func (a *OpenAIVisionAdapter) Analyze(ctx context.Context, photoID string, imageBytes []byte) (*domain.VisionResult, error) {
	// Build the request
	request := map[string]any{
		"model": a.Model,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []any{
					map[string]any{
						"type": "text",
						"text": "Analyse this photograph. Return a JSON object with schema_version, photo_id, observed (description, subjects, category, visible_text), hypotheses (type, value, confidence), quality_flags, and safety_flags.",
					},
					map[string]any{
						"type": "image_url",
						"image_url": map[string]any{
							"url": fmt.Sprintf("data:image/jpeg;base64,%s", base64.StdEncoding.EncodeToString(imageBytes)),
						},
					},
				},
			},
		},
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "vision_analysis",
				"schema": visionSchema(),
			},
		},
	}

	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	requestCtx := ctx
	cancel := func() {}
	if timeout := a.Timeout.Duration(); timeout > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, "POST", a.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if a.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.APIKey)
	}

	resp, err := a.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		message := string(respBody)
		if a.APIKey != "" {
			message = strings.ReplaceAll(message, a.APIKey, "[redacted]")
		}
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, message)
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	var result domain.VisionResult
	if err := json.Unmarshal([]byte(apiResp.Choices[0].Message.Content), &result); err != nil {
		return nil, fmt.Errorf("parse vision result: %w", err)
	}
	result.PhotoID = photoID
	return &result, nil
}

// ModelSupportsImages sends a test image to verify the model accepts vision input.
func (a *OpenAIVisionAdapter) ModelSupportsImages(ctx context.Context) (bool, error) {
	// Send a minimal JPEG to test
	testImage := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46}
	_, err := a.Analyze(ctx, "test", testImage)
	if err != nil {
		// Check if the error is about unsupported input vs other errors
		return false, err
	}
	return true, nil
}

func visionSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"schema_version": map[string]any{"type": "integer"},
			"photo_id":       map[string]any{"type": "string"},
			"observed": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"description":  map[string]any{"type": "string"},
					"subjects":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"category":     map[string]any{"type": "string"},
					"visible_text": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
				"required": []string{"description", "subjects", "category"},
			},
			"hypotheses": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"type":       map[string]any{"type": "string"},
						"value":      map[string]any{"type": "string"},
						"confidence": map[string]any{"type": "number"},
					},
				},
			},
			"quality_flags": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"safety_flags":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required": []string{"schema_version", "photo_id", "observed"},
	}
}

// NewOpenAIVisionAdapter creates a vision adapter from config.
func NewOpenAIVisionAdapter(cfg *config.Config) *OpenAIVisionAdapter {
	baseURL := cfg.OMLXVisionBaseURL
	if baseURL == "" {
		baseURL = cfg.OMLXBaseURL
	}
	model := cfg.OMLXVisionModel
	if model == "" {
		model = cfg.OMLXWriterModel
	}
	return NewOpenAIVisionAdapterForEndpoint(baseURL, model, cfg.OMLXAPIKey, cfg.ModelTimeout)
}

func NewOpenAIVisionAdapterForEndpoint(baseURL, model, apiKey string, timeout *config.ModelTimeout) *OpenAIVisionAdapter {
	return &OpenAIVisionAdapter{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Model:   model,
		APIKey:  apiKey,
		Timeout: timeout,
		HTTP:    &http.Client{},
	}
}
