package adapters

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/karan/travel-diary/internal/config"
)

// OpenAITextAdapter sends text prompts to an OpenAI-compatible endpoint.
type OpenAITextAdapter struct {
	BaseURL string
	Model   string
	APIKey  string
	HTTP    *http.Client
	Timeout *config.ModelTimeout
	Stream  bool
}

// Generate sends a text prompt and returns the model's response.
func (a *OpenAITextAdapter) Generate(ctx context.Context, prompt string) (string, error) {
	return a.generate(ctx, prompt, nil)
}

func (a *OpenAITextAdapter) GenerateWithProgress(ctx context.Context, prompt string, report func(int)) (string, error) {
	if !a.Stream {
		response, err := a.Generate(ctx, prompt)
		if err == nil && report != nil {
			report(len(response))
		}
		return response, err
	}
	return a.generate(ctx, prompt, report)
}

func (a *OpenAITextAdapter) generate(ctx context.Context, prompt string, report func(int)) (string, error) {
	request := map[string]any{
		"model": a.Model,
		"messages": []map[string]any{
			{
				"role":    "user",
				"content": prompt,
			},
		},
	}
	if a.Stream {
		request["stream"] = true
	}

	body, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	requestCtx := ctx
	cancel := func() {}
	if timeout := a.Timeout.Duration(); timeout > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, "POST", a.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if a.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.APIKey)
	}
	if a.Stream {
		req.Header.Set("Accept", "text/event-stream")
	}

	resp, err := a.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		message := string(respBody)
		if a.APIKey != "" {
			message = strings.ReplaceAll(message, a.APIKey, "[redacted]")
		}
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, message)
	}
	if a.Stream {
		return a.decodeStream(resp.Body, report)
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

func (a *OpenAITextAdapter) decodeStream(body io.Reader, report func(int)) (string, error) {
	reader := bufio.NewReader(body)
	var content strings.Builder
	characters := 0
	for {
		line, err := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				break
			}
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				return "", fmt.Errorf("decode response: %w", err)
			}
			if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
				delta := chunk.Choices[0].Delta.Content
				content.WriteString(delta)
				characters += utf8.RuneCountInString(delta)
				if report != nil {
					report(characters)
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", fmt.Errorf("read response: %w", err)
		}
	}
	if content.Len() == 0 {
		return "", fmt.Errorf("no content in response")
	}
	return content.String(), nil
}

// NewOpenAITextAdapter creates a text adapter from config.
func NewOpenAITextAdapter(cfg *config.Config) *OpenAITextAdapter {
	baseURL := cfg.OMLXWriterBaseURL
	if baseURL == "" {
		baseURL = cfg.OMLXBaseURL
	}
	return NewOpenAICompatibleTextAdapter(baseURL, cfg.OMLXWriterModel, cfg.OMLXAPIKey, cfg.ModelTimeout)
}

func NewOpenAICompatibleTextAdapter(baseURL, model, apiKey string, timeout *config.ModelTimeout) *OpenAITextAdapter {
	return &OpenAITextAdapter{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Model:   model,
		APIKey:  apiKey,
		HTTP:    &http.Client{},
		Timeout: timeout,
		Stream:  true,
	}
}

// NewOpenRouterTextAdapter creates a text adapter for OpenRouter's
// OpenAI-compatible API. Its key is held in memory and is never persisted.
func NewOpenRouterTextAdapter(model, apiKey string, timeout *config.ModelTimeout) *OpenAITextAdapter {
	return NewOpenAICompatibleTextAdapter("https://openrouter.ai/api/v1", model, apiKey, timeout)
}
