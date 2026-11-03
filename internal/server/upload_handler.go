package server

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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
	r.Post("/api/v1/trips/{tripId}/photos/upload", h.handleTripPhotoUpload)
}

func (h *UploadHandler) handleTripPhotoUpload(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "tripId")
	if filepath.Base(tripID) != tripID || tripID == "." || tripID == ".." {
		writeUploadError(w, http.StatusBadRequest, "invalid_trip", "invalid trip id")
		return
	}
	if _, err := h.store.Trips.Get(r.Context(), tripID); err != nil {
		http.NotFound(w, r)
		return
	}
	maxSize := h.cfg.MaxUploadSize
	if maxSize == 0 {
		maxSize = 500 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxSize)
	if err := r.ParseMultipartForm(maxSize); err != nil {
		writeUploadError(w, http.StatusBadRequest, "upload_failed", "request too large: "+err.Error())
		return
	}
	files := r.MultipartForm.File["files"]
	if len(files) == 0 || len(files) > h.maxUploadFiles() {
		writeUploadError(w, http.StatusBadRequest, "upload_failed", "choose between 1 and the configured maximum number of photos")
		return
	}
	tripDir := filepath.Join(h.cfg.DataDir, "assets", tripID, "source")
	if err := os.MkdirAll(tripDir, 0755); err != nil {
		writeUploadError(w, http.StatusInternalServerError, "upload_failed", "could not prepare trip photo storage")
		return
	}
	type uploaded struct {
		URL      string `json:"url"`
		FileName string `json:"fileName"`
	}
	results := make([]uploaded, 0, len(files))
	var total int64
	for _, fh := range files {
		total += fh.Size
	}
	if total > maxSize {
		writeUploadError(w, http.StatusBadRequest, "upload_failed", "total photo upload exceeds the configured size limit")
		return
	}
	for _, fh := range files {
		if fh.Size > h.cfg.MaxFileSize {
			writeUploadError(w, http.StatusBadRequest, "upload_failed", fmt.Sprintf("photo %s exceeds the per-file size limit", sanitizeFilename(fh.Filename)))
			return
		}
		f, err := fh.Open()
		if err != nil {
			writeUploadError(w, http.StatusBadRequest, "upload_failed", "could not read an uploaded photo")
			return
		}
		header := make([]byte, 512)
		_, readErr := io.ReadFull(f, header)
		if readErr != nil && readErr != io.ErrUnexpectedEOF {
			f.Close()
			writeUploadError(w, http.StatusBadRequest, "upload_failed", "could not inspect an uploaded photo")
			return
		}
		if !domain.SupportedImageMIMEs[detectUploadMIME(header)] {
			f.Close()
			writeUploadError(w, http.StatusBadRequest, "upload_failed", "only supported image files can be added")
			return
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			f.Close()
			writeUploadError(w, http.StatusBadRequest, "upload_failed", "could not read an uploaded photo")
			return
		}
		name := sanitizeFilename(fh.Filename)
		if name == "" {
			f.Close()
			writeUploadError(w, http.StatusBadRequest, "upload_failed", "an uploaded photo has an invalid filename")
			return
		}
		destPath := uniqueUploadPath(tripDir, name)
		dest, err := os.Create(destPath)
		if err != nil {
			f.Close()
			writeUploadError(w, http.StatusInternalServerError, "upload_failed", "could not save an uploaded photo")
			return
		}
		hash := sha256.New()
		_, copyErr := io.Copy(io.MultiWriter(dest, hash), io.LimitReader(f, h.cfg.MaxFileSize+1))
		dest.Close()
		f.Close()
		if copyErr != nil {
			writeUploadError(w, http.StatusInternalServerError, "upload_failed", "could not save an uploaded photo")
			return
		}
		photoID := "p_" + hex.EncodeToString(hash.Sum(nil))
		results = append(results, uploaded{URL: "/api/v1/photos/" + photoID + "/thumbnail", FileName: fh.Filename})
	}
	if err := h.importer.ImportUploadedPhotos(r.Context(), tripID, tripDir); err != nil {
		writeUploadError(w, http.StatusInternalServerError, "upload_failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{"uploads": results})
}

func writeUploadError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(apiErr(code, message))
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

	if len(files) > h.maxUploadFiles() {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(apiErr("upload_failed", fmt.Sprintf("too many files: max %d", h.maxUploadFiles())))
		return
	}

	if err := h.handleFileUpload(w, r, files); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(apiErr("upload_failed", err.Error()))
		return
	}
}

func (h *UploadHandler) maxUploadFiles() int {
	if h.cfg.MaxUploadFiles <= 0 {
		return 200
	}
	return h.cfg.MaxUploadFiles
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
	if err := h.importer.ImportUploadedPhotos(r.Context(), tripID, tmpDir); err != nil {
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

		mimeType := detectUploadMIME(buffer)
		if !domain.SupportedImageMIMEs[mimeType] {
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
		destPath := uniqueUploadPath(tmpDir, safeName)
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

	// Keep the entered notes alongside the originals during import. They are also
	// persisted on the trip so generation still works after the upload temp folder
	// is removed.
	title := strings.TrimSpace(r.FormValue("title"))
	location := strings.TrimSpace(r.FormValue("location"))
	notes := strings.TrimSpace(r.FormValue("notes"))
	if notes != "" {
		if err := os.WriteFile(filepath.Join(tmpDir, "story.txt"), []byte(notes), 0600); err != nil {
			return fmt.Errorf("save notes: %w", err)
		}
	}

	// Check total size
	if totalSize > h.cfg.MaxUploadSize {
		return fmt.Errorf("total upload size %d exceeds limit %d", totalSize, h.cfg.MaxUploadSize)
	}

	// Import
	tripID := domain.GenerateTripID(tmpDir)
	if err := h.importer.ImportUploadedPhotos(r.Context(), tripID, tmpDir); err != nil {
		return fmt.Errorf("import: %w", err)
	}
	trip, err := h.store.Trips.Get(r.Context(), tripID)
	if err != nil {
		return fmt.Errorf("load imported trip: %w", err)
	}
	if title != "" {
		trip.Name = title
	}
	trip.Location = location
	trip.Notes = notes
	if err := h.store.Trips.Save(r.Context(), *trip); err != nil {
		return fmt.Errorf("save trip details: %w", err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"trip_id": tripID})
	return nil
}

func detectUploadMIME(header []byte) string {
	if len(header) >= 12 && string(header[4:8]) == "ftyp" {
		brand := string(header[8:12])
		if brand == "heic" || brand == "heix" || brand == "hevc" || brand == "hevx" || brand == "mif1" || brand == "msf1" {
			return "image/heic"
		}
	}
	return http.DetectContentType(header)
}

func uniqueUploadPath(dir, name string) string {
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 2; ; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
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
