package adapters

import (
	"context"
	"net/http"
	"testing"
)

func TestFakeVLMAdapter(t *testing.T) {
	adapter := FakeVLMAdapter{}
	ctx := context.Background()

	result, err := adapter.Analyze(ctx, "p1", []byte{0xFF, 0xD8})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if result.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", result.SchemaVersion)
	}
	if result.PhotoID != "p1" {
		t.Errorf("PhotoID = %q, want %q", result.PhotoID, "p1")
	}
	if result.Observed.Category != "scenery" {
		t.Errorf("Category = %q, want %q", result.Observed.Category, "scenery")
	}
}

func TestFakeVLMAdapterSupportsImages(t *testing.T) {
	adapter := FakeVLMAdapter{}
	ctx := context.Background()

	supported, err := adapter.ModelSupportsImages(ctx)
	if err != nil {
		t.Fatalf("ModelSupportsImages: %v", err)
	}
	if !supported {
		t.Error("fake adapter should support images")
	}
}

func TestFakeTextAdapter(t *testing.T) {
	adapter := FakeTextAdapter{}
	ctx := context.Background()

	response, err := adapter.Generate(ctx, "Write a paragraph")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if response == "" {
		t.Error("response should not be empty")
	}
}

func TestFakeServerVision(t *testing.T) {
	srv := NewFakeServer()
	defer srv.Close()

	adapter := srv.FakeVLMAdapter()
	ctx := context.Background()

	result, err := adapter.Analyze(ctx, "p1", []byte{0xFF, 0xD8, 0xFF, 0xE0})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if result.PhotoID != "p1" {
		t.Errorf("PhotoID = %q, want %q", result.PhotoID, "p1")
	}
}

func TestFakeServerText(t *testing.T) {
	srv := NewFakeServer()
	defer srv.Close()

	adapter := srv.FakeTextAdapter()
	ctx := context.Background()

	response, err := adapter.Generate(ctx, "Hello")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if response == "" {
		t.Error("response should not be empty")
	}
}

func TestFakeServerAuthRequired(t *testing.T) {
	srv := NewFakeServer()
	defer srv.Close()

	adapter := &OpenAIVisionAdapter{
		BaseURL: srv.URL() + "/v1",
		Model:   "fake-vision",
		APIKey:  "", // no key
		HTTP:    &http.Client{},
	}
	ctx := context.Background()

	_, err := adapter.Analyze(ctx, "p1", []byte{0xFF, 0xD8})
	if err == nil {
		t.Error("should fail without API key")
	}
}
