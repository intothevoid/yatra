package adapters

import (
	"context"
	"os"
	"testing"
)

func TestFakeImageProcessorExtractEXIF(t *testing.T) {
	proc := FakeImageProcessor{}
	ctx := context.Background()

	result, err := proc.ExtractEXIF(ctx, "/any/path.jpg")
	if err != nil {
		t.Fatalf("ExtractEXIF: %v", err)
	}
	if result["Make"] != "NIKON CORPORATION" {
		t.Errorf("Make = %q, want %q", result["Make"], "NIKON CORPORATION")
	}
	if result["Model"] != "NIKON Z f" {
		t.Errorf("Model = %q, want %q", result["Model"], "NIKON Z f")
	}
}

func TestFakeImageProcessorGenerateThumbnail(t *testing.T) {
	proc := FakeImageProcessor{}
	ctx := context.Background()

	dest := t.TempDir() + "/thumb.jpg"
	if err := proc.GenerateThumbnail(ctx, "/any/path.jpg", dest); err != nil {
		t.Fatalf("GenerateThumbnail: %v", err)
	}

	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat dest: %v", err)
	}
	if info.Size() <= 0 {
		t.Error("thumbnail should have content")
	}
}

func TestFakeImageProcessorGeneratePreview(t *testing.T) {
	proc := FakeImageProcessor{}
	ctx := context.Background()

	dest := t.TempDir() + "/preview.jpg"
	if err := proc.GeneratePreview(ctx, "/any/path.jpg", dest); err != nil {
		t.Fatalf("GeneratePreview: %v", err)
	}

	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat dest: %v", err)
	}
	if info.Size() <= 0 {
		t.Error("preview should have content")
	}
}

func TestFakeImageProcessorGenerateExport(t *testing.T) {
	proc := FakeImageProcessor{}
	ctx := context.Background()

	dest := t.TempDir() + "/export.jpg"
	if err := proc.GenerateExport(ctx, "/any/path.jpg", dest); err != nil {
		t.Fatalf("GenerateExport: %v", err)
	}

	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat dest: %v", err)
	}
	if info.Size() <= 0 {
		t.Error("export should have content")
	}
}

func TestFakeImageProcessorCanProcessHEIC(t *testing.T) {
	proc := FakeImageProcessor{}
	ctx := context.Background()

	if !proc.CanProcessHEIC(ctx) {
		t.Error("fake processor should support HEIC")
	}
}
