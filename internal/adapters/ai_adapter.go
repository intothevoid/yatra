package adapters

import (
	"context"
	"sync"

	"github.com/karan/travel-diary/internal/domain"
)

// VLMAdapter sends images to a vision-language model and returns structured analysis.
type VLMAdapter interface {
	// Analyze sends an image to the VLM and returns a structured VisionResult.
	Analyze(ctx context.Context, photoID string, imageBytes []byte) (*domain.VisionResult, error)

	// ModelSupportsImages checks if the configured model accepts image input.
	ModelSupportsImages(ctx context.Context) (bool, error)
}

// SwitchableVLMAdapter changes image-analysis providers at runtime.
type SwitchableVLMAdapter struct {
	mu       sync.RWMutex
	fallback VLMAdapter
	current  VLMAdapter
}

func NewSwitchableVLMAdapter(fallback VLMAdapter) *SwitchableVLMAdapter {
	return &SwitchableVLMAdapter{fallback: fallback, current: fallback}
}

func (a *SwitchableVLMAdapter) Set(adapter VLMAdapter) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.current = adapter
}

func (a *SwitchableVLMAdapter) Reset() {
	a.Set(a.fallback)
}

func (a *SwitchableVLMAdapter) Analyze(ctx context.Context, photoID string, imageBytes []byte) (*domain.VisionResult, error) {
	a.mu.RLock()
	adapter := a.current
	a.mu.RUnlock()
	return adapter.Analyze(ctx, photoID, imageBytes)
}

func (a *SwitchableVLMAdapter) ModelSupportsImages(ctx context.Context) (bool, error) {
	a.mu.RLock()
	adapter := a.current
	a.mu.RUnlock()
	return adapter.ModelSupportsImages(ctx)
}

// TextAdapter generates text content (narrative sections, captions, etc.).
type TextAdapter interface {
	// Generate sends a text prompt and returns the model's response.
	Generate(ctx context.Context, prompt string) (string, error)
}

// ProgressTextAdapter can report how much response text has arrived so far.
type ProgressTextAdapter interface {
	GenerateWithProgress(ctx context.Context, prompt string, report func(int)) (string, error)
}

type progressContextKey struct{}

func WithGenerationProgress(ctx context.Context, report func(int)) context.Context {
	return context.WithValue(ctx, progressContextKey{}, report)
}

func generationProgress(ctx context.Context) func(int) {
	report, _ := ctx.Value(progressContextKey{}).(func(int))
	return report
}

// SwitchableTextAdapter changes text providers at runtime without persisting
// credentials. The fallback is normally the local or fake adapter.
type SwitchableTextAdapter struct {
	mu       sync.RWMutex
	fallback TextAdapter
	current  TextAdapter
}

func NewSwitchableTextAdapter(fallback TextAdapter) *SwitchableTextAdapter {
	return &SwitchableTextAdapter{fallback: fallback, current: fallback}
}

func (a *SwitchableTextAdapter) Set(adapter TextAdapter) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.current = adapter
}

func (a *SwitchableTextAdapter) Reset() {
	a.Set(a.fallback)
}

func (a *SwitchableTextAdapter) Generate(ctx context.Context, prompt string) (string, error) {
	a.mu.RLock()
	adapter := a.current
	a.mu.RUnlock()
	if report := generationProgress(ctx); report != nil {
		if streaming, ok := adapter.(ProgressTextAdapter); ok {
			return streaming.GenerateWithProgress(ctx, prompt, report)
		}
		response, err := adapter.Generate(ctx, prompt)
		if err == nil {
			report(len(response))
		}
		return response, err
	}
	return adapter.Generate(ctx, prompt)
}
