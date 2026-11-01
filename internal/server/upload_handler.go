package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/karan/travel-diary/internal/config"
	"github.com/karan/travel-diary/internal/domain"
	"github.com/karan/travel-diary/internal/services"
	"github.com/karan/travel-diary/internal/store"
)

// UploadHandler handles browser-based file uploads.
type UploadHandler struct {
	cfg      *config.Config
	store    *store.Store
	importer *services.Importer
}

// NewUploadHandler creates a new upload handler.
func NewUploadHandler(cfg *config.Config, s *store.Store, imp *services.Importer) *UploadHandler {
	return &UploadHandler{cfg: cfg, store: s, importer: imp}
}

// Register routes on the chi router.
func (h *UploadHandler) Register(r chi.Router) {
	r.Post("/api/v1/trips/import-upload", h.handleUpload)
}

// handleUpload accepts multipart form data with files or a single zip archive.
func (h *UploadHandler) handleUpload(w http.ResponseWriter, r *http.Request) {
	// Parse form with size limit
	maxSize := h.cfg.MaxUploadSize
	if maxSize == 0 {
		maxSize = 500 << 20 // 500MB default
	}
	r.Body = http.MaxBytesReader(w, r.Body, int64(maxSize))
	if err := r.ParseMultipartForm(int64(maxSize)); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(apiErr("upload_failed", "request too large: "+err.Error()))
		return
	}

	// Check for zip file first
	if zipFile, handler, err := r.FormFile("archive"); err == nil {
		zipFile.Close()
		if err := h.handleZipUpload(w, r, handler); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(apiErr("upload_failed", err.Error()))
			return
		}
		return
	}

	// Handle individual files
	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(apiErr("upload_failed", "no files provided"))
		return
	}

	if len(files) > h.cfg.MaxUploadFiles {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(apiErr("upload_failed", fmt.Sprintf("too many files: max %d", h.cfg.MaxUploadFiles)))
		return
	}

	if err := h.handleFileUpload(w, r, files); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(apiErr("upload_failed", err.Error()))
		return
	}
}

// handleZipUpload extracts a zip archive and imports the trip.
func (h *UploadHandler) handleZipUpload(w http.ResponseWriter, r *http.Request, header *multipart.FileHeader) error {
	// Open the zip file
	f, err := header.Open()
	if err != nil {
		return err
	}
	defer f.Close()

	// Read into memory for zip reader
	data, err := io.ReadAll(io.LimitReader(f, h.cfg.MaxUploadSize))
	if err != nil {
		return fmt.Errorf("read zip: %w", err)
	}

	// Create zip reader
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("invalid zip: %w", err)
	}

	// Validate zip contents
	if err := h.validateZip(reader); err != nil {
		return err
	}

	// Create temp directory for extraction
	tmpDir, err := os.MkdirTemp("", "upload-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Extract files
	if err := h.extractZip(reader, tmpDir); err != nil {
		return fmt.Errorf("extract zip: %w", err)
	}

	// Import the extracted directory
	tripID := domain.GenerateTripID(tmpDir)
	if err := h.importer.ImportTrip(r.Context(), tripID, tmpDir); err != nil {
		return fmt.Errorf("import: %w", err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"trip_id": tripID})
	return nil
}

// handleFileUpload processes individual file uploads.
func (h *UploadHandler) handleFileUpload(w http.ResponseWriter, r *http.Request, files []*multipart.FileHeader) error {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "upload-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Process each file
	var totalSize int64
	for _, fh := range files {
		// Check file size
		if fh.Size > h.cfg.MaxFileSize {
			return fmt.Errorf("file %s too large: %d bytes (max %d)", fh.Filename, fh.Size, h.cfg.MaxFileSize)
		}
		totalSize += fh.Size

		// Open and validate
		f, err := fh.Open()
		if err != nil {
			return fmt.Errorf("open %s: %w", fh.Filename, err)
		}

		// Validate MIME type from content
		buffer := make([]byte, 512)
		if _, err := io.ReadFull(f, buffer); err != nil && err != io.ErrUnexpectedEOF {
			f.Close()
			return fmt.Errorf("read %s: %w", fh.Filename, err)
		}
		f.Seek(0, 0)

		mimeType := http.DetectContentType(buffer)
		if !domain.SupportedImageMIMEs[mimeType] && mimeType != "text/plain" {
			f.Close()
			return fmt.Errorf("file %s has unsupported type: %s", sanitizeFilename(fh.Filename), mimeType)
		}

		// Validate filename
		safeName := sanitizeFilename(fh.Filename)
		if safeName == "" {
			f.Close()
			return fmt.Errorf("invalid filename: %s", fh.Filename)
		}

		// Save to temp dir
		destPath := filepath.Join(tmpDir, safeName)
		dest, err := os.Create(destPath)
		if err != nil {
			f.Close()
			return fmt.Errorf("create %s: %w", safeName, err)
		}

		if _, err := io.Copy(dest, f); err != nil {
			dest.Close()
			f.Close()
			return fmt.Errorf("save %s: %w", safeName, err)
		}
		dest.Close()
		f.Close()
	}

	// Check total size
	if totalSize > h.cfg.MaxUploadSize {
		return fmt.Errorf("total upload size %d exceeds limit %d", totalSize, h.cfg.MaxUploadSize)
	}

	// Import
	tripID := domain.GenerateTripID(tmpDir)
	if err := h.importer.ImportTrip(r.Context(), tripID, tmpDir); err != nil {
		return fmt.Errorf("import: %w", err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"trip_id": tripID})
	return nil
}

// validateZip checks zip contents for security issues.
func (h *UploadHandler) validateZip(reader *zip.Reader) error {
	if len(reader.File) > 10000 {
		return fmt.Errorf("zip contains too many files: %d (max 10000)", len(reader.File))
	}

	var totalUncompressed int64
	for _, f := range reader.File {
		// Check for path traversal
		name := f.Name
		if strings.Contains(name, "..") || strings.HasPrefix(name, "/") {
			return fmt.Errorf("zip contains invalid path: %s", sanitizeFilename(name))
		}

		// Check for symlinks (extra3 bit 0x800)
		if f.Flags&0x800 != 0 {
			return fmt.Errorf("zip contains symlink: %s", sanitizeFilename(name))
		}

		// Accumulate uncompressed size
		totalUncompressed += int64(f.UncompressedSize64)
		if totalUncompressed > h.cfg.MaxUploadSize {
			return fmt.Errorf("zip expansion too large: %d bytes (max %d)", totalUncompressed, h.cfg.MaxUploadSize)
		}
	}

	return nil
}

// extractZip safely extracts files from a zip reader.
func (h *UploadHandler) extractZip(reader *zip.Reader, destDir string) error {
	for _, f := range reader.File {
		// Skip directories
		if f.FileInfo().IsDir() {
			continue
		}

		// Validate path
		target := filepath.Join(destDir, f.Name)
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)) {
			return fmt.Errorf("path traversal in zip: %s", f.Name)
		}

		// Create parent directory
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return fmt.Errorf("create dir: %w", err)
		}

		// Extract file
		if err := h.extractFile(f, target); err != nil {
			return fmt.Errorf("extract %s: %w", f.Name, err)
		}
	}
	return nil
}

// extractFile extracts a single zip entry.
func (h *UploadHandler) extractFile(f *zip.File, target string) error {
	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	dest, err := os.Create(target)
	if err != nil {
		return err
	}
	defer dest.Close()

	// Limit copy to max file size
	_, err = io.CopyN(dest, src, h.cfg.MaxFileSize+1)
	if err == nil {
		return fmt.Errorf("file %s exceeds size limit", f.Name)
	}
	if err != io.EOF {
		return err
	}

	// Truncate to exact size
	return os.Truncate(target, int64(f.UncompressedSize64))
}

// sanitizeFilename removes path components and dangerous characters.
func sanitizeFilename(name string) string {
	// Remove directory components
	name = filepath.Base(name)
	// Remove null bytes
	name = strings.ReplaceAll(name, "\x00", "")
	// Remove leading dots (hidden files)
	name = strings.TrimPrefix(name, ".")
	return name
}
