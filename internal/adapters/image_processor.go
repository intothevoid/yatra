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

// SystemImageProcessor combines the image transformer and EXIF tool adapters.
type SystemImageProcessor struct {
	EXIF *ExifToolAdapter
	Vips *VipsProcessor
}

func (p *SystemImageProcessor) ExtractEXIF(ctx context.Context, path string) (map[string]string, error) {
	return p.EXIF.ExtractEXIF(ctx, path)
}
func (p *SystemImageProcessor) GenerateThumbnail(ctx context.Context, src, dst string) error {
	return p.Vips.GenerateThumbnail(ctx, src, dst)
}
func (p *SystemImageProcessor) GeneratePreview(ctx context.Context, src, dst string) error {
	return p.Vips.GeneratePreview(ctx, src, dst)
}
func (p *SystemImageProcessor) GenerateExport(ctx context.Context, src, dst string) error {
	return p.Vips.GenerateExport(ctx, src, dst)
}
func (p *SystemImageProcessor) CanProcessHEIC(ctx context.Context) bool {
	return p.Vips.CanProcessHEIC(ctx)
}
