package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/karan/travel-diary/internal/config"
)

// OpenAITextAdapter sends text prompts to an OpenAI-compatible endpoint.
type OpenAITextAdapter struct {
	BaseURL string
	Model   string
	APIKey  string
	HTTP    *http.Client
}

// Generate sends a text prompt and returns the model's response.
func (a *OpenAITextAdapter) Generate(ctx context.Context, prompt string) (string, error) {
	request := map[string]any{
		"model": a.Model,
		"messages": []map[string]any{
			{
				"role":    "user",
				"content": prompt,
			},
		},
	}

	body, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", a.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if a.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.APIKey)
	}

	resp, err := a.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if len(apiResp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}
	return apiResp.Choices[0].Message.Content, nil
}

// NewOpenAITextAdapter creates a text adapter from config.
func NewOpenAITextAdapter(cfg *config.Config) *OpenAITextAdapter {
	return &OpenAITextAdapter{
		BaseURL: cfg.OMLXBaseURL,
		Model:   cfg.OMLXWriterModel,
		APIKey:  cfg.OMLXAPIKey,
		HTTP: &http.Client{
			Timeout: 10 * 60 * time.Second,
		},
	}
}
