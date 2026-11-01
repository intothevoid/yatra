package domain

import "time"

// SourceFile represents a single file found during trip import.
type SourceFile struct {
	ID           string    `json:"id"`
	TripID       string    `json:"trip_id"`
	RelativePath string    `json:"relative_path"`
	MIME         string    `json:"mime_type"`
	Bytes        int64     `json:"file_size"`
	SHA256       string    `json:"sha256"`
	CaptureTime  *time.Time `json:"capture_time,omitempty"`
	ScanError    string    `json:"scan_error,omitempty"`
}

// SupportedImageMIMEs lists MIME types accepted as photo input.
var SupportedImageMIMEs = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/heic": true,
	"image/heif": true,
}

// IsSupportedImage returns true if the MIME type is a supported image format.
func (sf SourceFile) IsSupportedImage() bool {
	return SupportedImageMIMEs[sf.MIME]
}

// GenerateSourceFileID creates a deterministic ID from trip and path.
func GenerateSourceFileID(tripID, path string) string {
	return tripID + "_" + path
}
