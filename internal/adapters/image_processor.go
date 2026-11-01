package adapters

import "context"

// ImageProcessor handles image metadata extraction and derivative generation.
type ImageProcessor interface {
	// ExtractEXIF runs exiftool on the source file and returns structured metadata.
	ExtractEXIF(ctx context.Context, srcPath string) (map[string]string, error)

	// GenerateThumbnail creates a 400px long-edge derivative.
	GenerateThumbnail(ctx context.Context, srcPath, destPath string) error

	// GeneratePreview creates a 1200px long-edge derivative.
	GeneratePreview(ctx context.Context, srcPath, destPath string) error

	// GenerateExport creates a 2560px long-edge derivative with GPS stripped.
	GenerateExport(ctx context.Context, srcPath, destPath string) error

	// CanProcessHEIC returns true if HEIC files can be decoded.
	CanProcessHEIC(ctx context.Context) bool
}
