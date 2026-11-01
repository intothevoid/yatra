package adapters

import (
	"context"
	"os"
	"path/filepath"
)

// FakeImageProcessor produces deterministic output for testing.
// It does not call external binaries.
type FakeImageProcessor struct{}

var _ ImageProcessor = FakeImageProcessor{}

func (FakeImageProcessor) ExtractEXIF(ctx context.Context, srcPath string) (map[string]string, error) {
	return map[string]string{
		"Make":        "NIKON CORPORATION",
		"Model":       "NIKON Z f",
		"DateTimeOriginal": "2025:06:09 14:47:46",
		"ImageWidth":  "6048",
		"ImageHeight": "4032",
	}, nil
}

func (FakeImageProcessor) GenerateThumbnail(ctx context.Context, srcPath, destPath string) error {
	return createFakeImage(destPath, 400, 267)
}

func (FakeImageProcessor) GeneratePreview(ctx context.Context, srcPath, destPath string) error {
	return createFakeImage(destPath, 1200, 800)
}

func (FakeImageProcessor) GenerateExport(ctx context.Context, srcPath, destPath string) error {
	return createFakeImage(destPath, 2560, 1707)
}

func (FakeImageProcessor) CanProcessHEIC(ctx context.Context) bool {
	return true
}

// createFakeImage creates a minimal valid JPEG file.
func createFakeImage(path string, width, height int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	// Minimal JPEG header (valid but tiny)
	header := []byte{
		0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x01,
		0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0xFF, 0xDB, 0x00, 0x43,
	}
	// Write a minimal but valid JPEG
	return os.WriteFile(path, header, 0644)
}
