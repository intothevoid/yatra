package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/karan/travel-diary/internal/adapters"
	"github.com/karan/travel-diary/internal/config"
	"github.com/karan/travel-diary/internal/domain"
	"github.com/karan/travel-diary/internal/store"
)

// Importer handles trip import from a server-side directory.
type Importer struct {
	cfg   *config.Config
	store *store.Store
	proc  adapters.ImageProcessor
}

// New creates a new importer.
func New(cfg *config.Config, s *store.Store, proc adapters.ImageProcessor) *Importer {
	return &Importer{cfg: cfg, store: s, proc: proc}
}

// ImportTrip reads files from sourcePath, computes hashes, extracts EXIF,
// and creates a Trip + SourceFiles + Photos in the database.
func (i *Importer) ImportTrip(ctx context.Context, tripID, sourcePath string) error {
	// Validate path is within allowed roots
	if !i.cfg.IsPathAllowed(sourcePath) {
		return fmt.Errorf("path not allowed: %s", sourcePath)
	}
	return i.importTrip(ctx, tripID, sourcePath)
}

// ImportUploadedPhotos imports files saved by the browser upload handler
// into a trip's private data directory.
func (i *Importer) ImportUploadedPhotos(ctx context.Context, tripID, sourcePath string) error {
	return i.importTrip(ctx, tripID, sourcePath)
}

func (i *Importer) importTrip(ctx context.Context, tripID, sourcePath string) error {

	// Read notes file if present
	notesPath := filepath.Join(sourcePath, "story.txt")
	var notesHash string
	if info, err := os.Stat(notesPath); err == nil {
		hash, _, err := hashFile(notesPath)
		if err != nil {
			return fmt.Errorf("hash notes: %w", err)
		}
		notesHash = hash
		_ = info
	}

	// Create trip
	trip := domain.Trip{
		ID:         tripID,
		Name:       filepath.Base(sourcePath),
		SourcePath: sourcePath,
		NotesHash:  notesHash,
		Status:     domain.StatusImported,
	}
	if existing, err := i.store.Trips.Get(ctx, tripID); err == nil {
		trip.Name = existing.Name
		trip.Location = existing.Location
		trip.Notes = existing.Notes
		trip.Status = existing.Status
		if trip.NotesHash == "" {
			trip.NotesHash = existing.NotesHash
		}
	}
	if err := i.store.Trips.Save(ctx, trip); err != nil {
		return fmt.Errorf("save trip: %w", err)
	}

	// Enumerate files
	entries, err := os.ReadDir(sourcePath)
	if err != nil {
		return fmt.Errorf("read directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		path := filepath.Join(sourcePath, name)

		// Compute hash
		hash, size, err := hashFile(path)
		if err != nil {
			// Record file with error
			sf := domain.SourceFile{
				TripID:       tripID,
				RelativePath: name,
				ScanError:    err.Error(),
			}
			i.store.Trips.SaveSourceFile(ctx, sf)
			continue
		}

		// Detect MIME type
		mime := detectMIME(name)
		isImage := domain.SupportedImageMIMEs[mime]

		// Create source file record
		sf := domain.SourceFile{
			TripID:       tripID,
			RelativePath: name,
			MIME:         mime,
			Bytes:        size,
			SHA256:       hash,
		}
		if err := i.store.Trips.SaveSourceFile(ctx, sf); err != nil {
			return fmt.Errorf("save source file %s: %w", name, err)
		}

		// Create photo record for images
		if isImage {
			photo := domain.Photo{
				ID:           "p_" + strings.TrimPrefix(hash, ""),
				SourceFileID: domain.GenerateSourceFileID(tripID, name),
				Status:       domain.PhotoStatusUnknown,
			}

			// Extract EXIF
			if exif, err := i.proc.ExtractEXIF(ctx, path); err == nil {
				photo.EXIF = marshalMap(exif)
			}

			// Generate thumbnail
			thumbName := derivativeName(name)
			thumbPath := filepath.Join(i.cfg.DataDir, "assets", tripID, "thumb", thumbName)
			if err := i.proc.GenerateThumbnail(ctx, path, thumbPath); err != nil {
				return fmt.Errorf("generate thumbnail for %s: %w", name, err)
			}
			photo.ThumbnailPath = thumbPath

			// Generate preview
			previewPath := filepath.Join(i.cfg.DataDir, "assets", tripID, "preview", derivativeName(name))
			if err := i.proc.GeneratePreview(ctx, path, previewPath); err != nil {
				return fmt.Errorf("generate preview for %s: %w", name, err)
			}
			photo.PreviewPath = previewPath

			if err := i.store.Photos.Upsert(ctx, photo); err != nil {
				return fmt.Errorf("save photo %s: %w", name, err)
			}
		}
	}

	return nil
}

// derivativeName keeps JPEG filenames as-is and gives other source formats a
// JPEG suffix because VipsProcessor writes browser-compatible JPEG derivatives.
func derivativeName(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".jpg" || ext == ".jpeg" {
		return name
	}
	return name + ".jpg"
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

func detectMIME(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".heic", ".heif":
		return "image/heic"
	default:
		return "application/octet-stream"
	}
}

func marshalMap(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	// Simple JSON encoding
	var b strings.Builder
	b.WriteByte('{')
	first := true
	for k, v := range m {
		if !first {
			b.WriteByte(',')
		}
		first = false
		fmt.Fprintf(&b, "%q:%q", k, v)
	}
	b.WriteByte('}')
	return b.String()
}
