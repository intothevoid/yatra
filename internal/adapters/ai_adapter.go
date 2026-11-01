package adapters

import (
	"context"

	"github.com/karan/travel-diary/internal/domain"
)

// VLMAdapter sends images to a vision-language model and returns structured analysis.
type VLMAdapter interface {
	// Analyze sends an image to the VLM and returns a structured VisionResult.
	Analyze(ctx context.Context, photoID string, imageBytes []byte) (*domain.VisionResult, error)

	// ModelSupportsImages checks if the configured model accepts image input.
	ModelSupportsImages(ctx context.Context) (bool, error)
}

// TextAdapter generates text content (narrative sections, captions, etc.).
type TextAdapter interface {
	// Generate sends a text prompt and returns the model's response.
	Generate(ctx context.Context, prompt string) (string, error)
}
