package adapters

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

// VipsProcessor calls vips CLI for image transformations.
type VipsProcessor struct {
	VipsPath    string
	ThumbnailSize int
	PreviewSize   int
	ExportSize    int
	JPEGQuality   int
}

func (a *VipsProcessor) vipsCmd() string {
	if a.VipsPath != "" {
		return a.VipsPath
	}
	return "vips"
}

// GenerateThumbnail creates a 400px long-edge JPEG derivative.
func (a *VipsProcessor) GenerateThumbnail(ctx context.Context, srcPath, destPath string) error {
	return a.resize(ctx, srcPath, destPath, a.ThumbnailSize)
}

// GeneratePreview creates a 1200px long-edge JPEG derivative.
func (a *VipsProcessor) GeneratePreview(ctx context.Context, srcPath, destPath string) error {
	return a.resize(ctx, srcPath, destPath, a.PreviewSize)
}

// GenerateExport creates a 2560px long-edge JPEG derivative with GPS stripped.
func (a *VipsProcessor) GenerateExport(ctx context.Context, srcPath, destPath string) error {
	return a.resize(ctx, srcPath, destPath, a.ExportSize)
}

func (a *VipsProcessor) resize(ctx context.Context, srcPath, destPath string, size int) error {
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, a.vipsCmd(), "resize", srcPath, destPath,
		"--size", strconv.Itoa(size),
		"--quality", strconv.Itoa(a.JPEGQuality),
	)
	return cmd.Run()
}

// CanProcessHEIC checks if vips can read HEIC files.
func (a *VipsProcessor) CanProcessHEIC(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, a.vipsCmd(), "format", "heif")
	return cmd.Run() == nil
}
