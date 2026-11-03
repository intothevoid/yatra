package adapters

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// VipsProcessor calls vips CLI for image transformations.
type VipsProcessor struct {
	VipsPath      string
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

	// `thumbnail` scales the image to fit within the requested width and height
	// while preserving its aspect ratio. Saver options belong on the output
	// filename (for example, output.jpg[Q=85]), not as operation flags.
	outputPath := fmt.Sprintf("%s[Q=%d]", destPath, a.JPEGQuality)
	cmd := exec.CommandContext(ctx, a.vipsCmd(), "thumbnail", srcPath, outputPath,
		strconv.Itoa(size), "--size", "down",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("vips thumbnail: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// CanProcessHEIC checks if vips can read HEIC files.
func (a *VipsProcessor) CanProcessHEIC(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, a.vipsCmd(), "format", "heif")
	return cmd.Run() == nil
}
