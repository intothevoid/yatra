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
	"time"

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

// ConfigureImageSettings applies derivative settings to future imports.
func (i *Importer) ConfigureImageSettings(cfg *config.Config) {
	if system, ok := i.proc.(*adapters.SystemImageProcessor); ok && system.Vips != nil {
		system.Vips.Configure(cfg.ThumbnailSize, cfg.PreviewSize, cfg.ExportSize, cfg.JPEGQuality)
	}
}

// ImportTrip reads files from sourcePath, computes hashes, extracts EXIF,
// and creates a Trip + SourceFiles + Photos in the database.
func (i *Importer) ImportTrip(ctx context.Context, tripID, sourcePath string) error {
	// Validate path is within allowed roots
	if !i.cfg.IsPathAllowed(sourcePath) {
		return fmt.Errorf("path not allowed: %s", sourcePath)
	}
	return i.importTrip(ctx, tripID, sourcePath, nil, "")
}

// ImportUploadedPhotos imports files saved by the browser upload handler
// into a trip's private data directory.
func (i *Importer) ImportUploadedPhotos(ctx context.Context, tripID, sourcePath string) error {
	return i.importTrip(ctx, tripID, sourcePath, nil, "")
}

// ImportUploadedMedia imports photos and videos with optional per-file trim ranges.
func (i *Importer) ImportUploadedMedia(ctx context.Context, tripID, sourcePath string, trims map[string]VideoTrim, profile string) error {
	return i.importTrip(ctx, tripID, sourcePath, trims, profile)
}

func (i *Importer) importTrip(ctx context.Context, tripID, sourcePath string, trims map[string]VideoTrim, profileOverride string) error {

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
		trip.FeaturePhotoID = existing.FeaturePhotoID
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
		if strings.HasPrefix(mime, "video/") {
			videoID := "v_" + hash[:24]
			videoPath := filepath.Join(i.cfg.DataDir, "assets", tripID, "videos", videoID+".mp4")
			posterPath := filepath.Join(i.cfg.DataDir, "assets", tripID, "videos", videoID+".webp")
			if size > i.cfg.VideoMaxSourceSize {
				return fmt.Errorf("video %s exceeds the source video size limit of %d bytes", name, i.cfg.VideoMaxSourceSize)
			}
			sourcePath := filepath.Join(i.cfg.DataDir, "assets", tripID, "source-video", videoID+strings.ToLower(filepath.Ext(name)))
			if err := copyImmutableFile(path, sourcePath); err != nil {
				return fmt.Errorf("preserve source video %s: %w", name, err)
			}
			profile := profileOverride
			if profile == "" {
				profile = i.cfg.VideoDefaultProfile
			}
			if profile == "" {
				profile = "standard"
			}
			video := domain.Video{ID: videoID, TripID: tripID, SourceFileID: domain.GenerateSourceFileID(tripID, name), FileName: name, Container: "mp4", Codec: "h264", SourcePath: sourcePath, SourceBytes: size, Path: videoPath, PosterPath: posterPath, Profile: profile, Status: "processing", Selected: true}
			if err := i.store.Videos.Upsert(ctx, video); err != nil {
				return fmt.Errorf("save video processing state %s: %w", name, err)
			}
			processCtx := ctx
			cancel := func() {}
			if i.cfg.VideoProcessingTimeoutSec > 0 {
				processCtx, cancel = context.WithTimeout(ctx, time.Duration(i.cfg.VideoProcessingTimeoutSec)*time.Second)
			}
			defer cancel()
			result, err := transcodeVideo(processCtx, i.cfg, sourcePath, videoPath, posterPath, trims[name], profile)
			if err != nil {
				video.Status = "failed"
				video.Error = err.Error()
				_ = i.store.Videos.Upsert(context.WithoutCancel(ctx), video)
				return fmt.Errorf("process video %s: %w", name, err)
			}
			if result.VideoBytes > i.cfg.MaxFileSize {
				video.Status = "failed"
				video.Error = fmt.Sprintf("optimized video is too large: %d bytes (max %d)", result.VideoBytes, i.cfg.MaxFileSize)
				_ = i.store.Videos.Upsert(context.WithoutCancel(ctx), video)
				return fmt.Errorf("optimized video %s is too large: %d bytes (max %d); shorten the clip or use the lightweight profile", name, result.VideoBytes, i.cfg.MaxFileSize)
			}
			video.SourceContainer = result.Source.Container
			video.SourceCodec = result.Source.Codec
			video.SourceDuration = result.Source.Duration
			video.SourceWidth = result.Source.Width
			video.SourceHeight = result.Source.Height
			video.SourceFrameRate = result.Source.FrameRate
			video.SourceHasAudio = result.Source.HasAudio
			video.SourceCreatedAt = result.Source.CreationTime
			video.Rotation = result.Source.Rotation
			video.StartTime = trims[name].Start
			video.EndTime = trims[name].Start + result.Duration
			video.Container = "mp4"
			video.Codec = result.Processed.Codec
			video.FrameRate = result.Processed.FrameRate
			video.Duration = result.Duration
			video.Width = result.Processed.Width
			video.Height = result.Processed.Height
			video.HasAudio = result.Processed.HasAudio
			video.OutputBytes = result.VideoBytes
			video.PosterBytes = result.PosterBytes
			video.Status = "completed"
			video.Error = ""
			if err := i.store.Videos.Upsert(ctx, video); err != nil {
				return fmt.Errorf("save video %s: %w", name, err)
			}
		}
	}

	return nil
}

// ReprocessVideo re-encodes a preserved source video with a new trim or profile.
func (i *Importer) ReprocessVideo(ctx context.Context, tripID, videoID string, trim VideoTrim, profile string) error {
	video, err := i.store.Videos.Get(ctx, videoID)
	if err != nil {
		return err
	}
	if video.TripID != tripID {
		return fmt.Errorf("video does not belong to this trip")
	}
	if profile != "standard" && profile != "lightweight" {
		return fmt.Errorf("profile must be standard or lightweight")
	}
	video.Status = "processing"
	video.Error = ""
	if err = i.store.Videos.Upsert(ctx, *video); err != nil {
		return err
	}
	processCtx := ctx
	cancel := func() {}
	if i.cfg.VideoProcessingTimeoutSec > 0 {
		processCtx, cancel = context.WithTimeout(ctx, time.Duration(i.cfg.VideoProcessingTimeoutSec)*time.Second)
	}
	defer cancel()
	result, err := transcodeVideo(processCtx, i.cfg, video.SourcePath, video.Path, video.PosterPath, trim, profile)
	if err != nil {
		video.Status = "failed"
		video.Error = err.Error()
		_ = i.store.Videos.Upsert(context.WithoutCancel(ctx), *video)
		return err
	}
	if result.VideoBytes > i.cfg.MaxFileSize {
		video.Status = "failed"
		video.Error = fmt.Sprintf("optimized video is too large: %d bytes (max %d)", result.VideoBytes, i.cfg.MaxFileSize)
		_ = i.store.Videos.Upsert(context.WithoutCancel(ctx), *video)
		return fmt.Errorf("%s", video.Error)
	}
	video.Profile = profile
	video.SourceContainer = result.Source.Container
	video.SourceCodec = result.Source.Codec
	video.SourceDuration = result.Source.Duration
	video.SourceWidth = result.Source.Width
	video.SourceHeight = result.Source.Height
	video.SourceFrameRate = result.Source.FrameRate
	video.SourceHasAudio = result.Source.HasAudio
	video.SourceCreatedAt = result.Source.CreationTime
	video.Rotation = result.Source.Rotation
	video.Container = "mp4"
	video.Codec = result.Processed.Codec
	video.FrameRate = result.Processed.FrameRate
	video.Duration = result.Duration
	video.Width = result.Processed.Width
	video.Height = result.Processed.Height
	video.HasAudio = result.Processed.HasAudio
	video.OutputBytes = result.VideoBytes
	video.PosterBytes = result.PosterBytes
	video.StartTime = trim.Start
	video.EndTime = trim.Start + result.Duration
	video.Status = "completed"
	video.Error = ""
	return i.store.Videos.Upsert(ctx, *video)
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

func copyImmutableFile(source, destination string) error {
	if _, err := os.Stat(destination); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".source-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err = io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmpPath, destination); err != nil {
		if _, statErr := os.Stat(destination); statErr == nil {
			return nil
		}
		return err
	}
	return nil
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
	case ".mp4", ".m4v", ".mov":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mkv":
		return "video/x-matroska"
	case ".avi":
		return "video/x-msvideo"
	case ".mpeg", ".mpg":
		return "video/mpeg"
	case ".3gp":
		return "video/3gpp"
	case ".mts", ".m2ts":
		return "video/mp2t"
	case ".wmv":
		return "video/x-ms-wmv"
	case ".flv":
		return "video/x-flv"
	case ".ogv":
		return "video/ogg"
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
