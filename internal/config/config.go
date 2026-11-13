package config

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// ModelTimeout keeps the model request timeout adjustable while jobs are
// running. A value of zero means no client-side timeout is configured.
type ModelTimeout struct{ seconds atomic.Int64 }

func NewModelTimeout(seconds int64) *ModelTimeout {
	t := &ModelTimeout{}
	if seconds > 0 && seconds <= maxTimeoutSeconds {
		t.seconds.Store(seconds)
	}
	return t
}

const maxTimeoutSeconds = int64((1<<63 - 1) / int64(time.Second))

func (t *ModelTimeout) Seconds() int64 {
	if t == nil {
		return 0
	}
	return t.seconds.Load()
}

func (t *ModelTimeout) SetSeconds(seconds int64) error {
	if t == nil || seconds < 0 || seconds > maxTimeoutSeconds {
		return &ConfigError{Field: "OMLX_TIMEOUT_SEC", Reason: "must be zero or a positive number of seconds within the supported range"}
	}
	t.seconds.Store(seconds)
	return nil
}

func (t *ModelTimeout) Duration() time.Duration {
	return time.Duration(t.Seconds()) * time.Second
}

// Config holds all application configuration.
// Startup secrets come from environment variables. UI-configured model keys are
// held in process memory only and are never persisted.
type Config struct {
	// Data directory for SQLite DB and derived assets.
	DataDir string

	// Server-side import allowlist (directories that can be imported).
	ImportRoots []string

	// OMLX (OpenAI-compatible) API settings.
	OMLXBaseURL       string
	OMLXWriterBaseURL string
	OMLXVisionBaseURL string
	OMLXVisionModel   string
	OMLXWriterModel   string
	OMLXTimeoutSec    int
	ModelTimeout      *ModelTimeout
	OMLXConcurrency   int
	OMLXAPIKey        string // from env only

	// Optional destination research. Disabled by default. When enabled, Brave Search
	// supplies source snippets to the writer model (the browser never sees the key).
	LocationWebSearch bool
	BraveSearchAPIKey string

	// Server settings.
	Port     int
	LogLevel string

	// Image processing.
	ThumbnailSize int
	PreviewSize   int
	ExportSize    int
	JPEGQuality   int

	// Ghost Admin API (for Milestone 5).
	GhostURL    string
	GhostAPIKey string // from env only

	// Upload limits.
	MaxFileSize    int64 // default 50MB
	MaxUploadSize  int64 // default 500MB
	MaxUploadFiles int   // default 200
}

func (c *Config) EnsureModelTimeout() {
	if c != nil && c.ModelTimeout == nil {
		c.ModelTimeout = NewModelTimeout(int64(c.OMLXTimeoutSec))
	}
}

// LoadConfig reads configuration from environment variables.
// In later milestones, this will also read from a config file.
func LoadConfig() *Config {
	cfg := &Config{
		DataDir:           envOr("DATA_DIR", "./data"),
		OMLXBaseURL:       envOr("OMLX_BASE_URL", "http://127.0.0.1:8000/v1"),
		OMLXVisionModel:   envOr("OMLX_VISION_MODEL", ""),
		OMLXWriterModel:   envOr("OMLX_WRITER_MODEL", ""),
		OMLXTimeoutSec:    envIntOr("OMLX_TIMEOUT_SEC", 0),
		OMLXConcurrency:   envIntOr("OMLX_CONCURRENCY", 1),
		OMLXAPIKey:        os.Getenv("OMLX_API_KEY"),
		LocationWebSearch: envBoolOr("LOCATION_WEB_SEARCH", false),
		BraveSearchAPIKey: os.Getenv("BRAVE_SEARCH_API_KEY"),
		Port:              envIntOr("PORT", 8080),
		LogLevel:          envOr("LOG_LEVEL", "info"),
		ThumbnailSize:     envIntOr("THUMBNAIL_SIZE", 400),
		PreviewSize:       envIntOr("PREVIEW_SIZE", 1200),
		ExportSize:        envIntOr("EXPORT_SIZE", 2560),
		JPEGQuality:       envIntOr("JPEG_QUALITY", 85),
		GhostURL:          envOr("GHOST_URL", ""),
		GhostAPIKey:       os.Getenv("GHOST_ADMIN_API_KEY"),
		MaxFileSize:       int64(envIntOr("MAX_FILE_SIZE_MB", 50)) << 20,
		MaxUploadSize:     int64(envIntOr("MAX_UPLOAD_SIZE_MB", 500)) << 20,
		MaxUploadFiles:    envIntOr("MAX_UPLOAD_FILES", 200),
	}
	cfg.OMLXWriterBaseURL = cfg.OMLXBaseURL
	cfg.OMLXVisionBaseURL = cfg.OMLXBaseURL
	cfg.ModelTimeout = NewModelTimeout(int64(cfg.OMLXTimeoutSec))

	// ImportRoots: comma-separated list from env, or default to ./imports
	if roots := os.Getenv("IMPORT_ROOTS"); roots != "" {
		for _, r := range splitComma(roots) {
			cfg.ImportRoots = append(cfg.ImportRoots, r)
		}
	} else {
		cfg.ImportRoots = []string{"./imports"}
	}

	return cfg
}

// Validate checks that required configuration is present.
func (c *Config) Validate() error {
	if c.DataDir == "" {
		return &ConfigError{Field: "DATA_DIR", Reason: "required"}
	}
	if len(c.ImportRoots) == 0 {
		return &ConfigError{Field: "IMPORT_ROOTS", Reason: "at least one root required"}
	}
	return nil
}

// IsPathAllowed returns true if the path is within one of the import roots.
func (c *Config) IsPathAllowed(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	for _, root := range c.ImportRoots {
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		// Check if abs starts with rootAbs (is a subdirectory)
		if abs == rootAbs || (len(abs) > len(rootAbs) && abs[:len(rootAbs)] == rootAbs && abs[len(rootAbs)] == '/') {
			return true
		}
	}
	return false
}

// ConfigError describes a configuration validation failure.
type ConfigError struct {
	Field  string
	Reason string
}

func (e *ConfigError) Error() string {
	return "config error: " + e.Field + " — " + e.Reason
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envIntOr(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	// Simple parse — errors fall back to default
	n := 0
	for _, r := range v {
		if r < '0' || r > '9' {
			return fallback
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func envBoolOr(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v == "1" || v == "true" || v == "TRUE" || v == "yes" || v == "on"
}

func splitComma(s string) []string {
	var parts []string
	var current string
	for _, r := range s {
		if r == ',' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(r)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}
