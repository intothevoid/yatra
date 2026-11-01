package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigDefaults(t *testing.T) {
	// Clear env vars that affect defaults
	os.Unsetenv("DATA_DIR")
	os.Unsetenv("IMPORT_ROOTS")
	os.Unsetenv("OMLX_BASE_URL")
	os.Unsetenv("OMLX_API_KEY")
	os.Unsetenv("GHOST_URL")
	os.Unsetenv("GHOST_ADMIN_API_KEY")
	os.Unsetenv("PORT")

	cfg := LoadConfig()
	if cfg.DataDir != "./data" {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, "./data")
	}
	if len(cfg.ImportRoots) != 1 || cfg.ImportRoots[0] != "./imports" {
		t.Errorf("ImportRoots = %v, want %v", cfg.ImportRoots, []string{"./imports"})
	}
	if cfg.OMLXBaseURL != "http://127.0.0.1:8000/v1" {
		t.Errorf("OMLXBaseURL = %q, want %q", cfg.OMLXBaseURL, "http://127.0.0.1:8000/v1")
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want %d", cfg.Port, 8080)
	}
	if cfg.OMLXAPIKey != "" {
		t.Error("OMLXAPIKey should be empty when env var is unset")
	}
}

func TestLoadConfigEnvOverride(t *testing.T) {
	os.Setenv("DATA_DIR", "/custom/data")
	os.Setenv("IMPORT_ROOTS", "/a,/b")
	os.Setenv("OMLX_API_KEY", "secret-key")
	os.Setenv("PORT", "9090")
	defer func() {
		os.Unsetenv("DATA_DIR")
		os.Unsetenv("IMPORT_ROOTS")
		os.Unsetenv("OMLX_API_KEY")
		os.Unsetenv("PORT")
	}()

	cfg := LoadConfig()
	if cfg.DataDir != "/custom/data" {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, "/custom/data")
	}
	if len(cfg.ImportRoots) != 2 || cfg.ImportRoots[0] != "/a" || cfg.ImportRoots[1] != "/b" {
		t.Errorf("ImportRoots = %v, want %v", cfg.ImportRoots, []string{"/a", "/b"})
	}
	if cfg.OMLXAPIKey != "secret-key" {
		t.Errorf("OMLXAPIKey = %q, want %q", cfg.OMLXAPIKey, "secret-key")
	}
	if cfg.Port != 9090 {
		t.Errorf("Port = %d, want %d", cfg.Port, 9090)
	}
}

func TestValidateRequiredFields(t *testing.T) {
	cfg := &Config{}
	err := cfg.Validate()
	if err == nil {
		t.Error("Validate() on empty config returned nil, want error")
	}
}

func TestValidatePasses(t *testing.T) {
	cfg := &Config{
		DataDir:     "/tmp/data",
		ImportRoots: []string{"/tmp/imports"},
	}
	err := cfg.Validate()
	if err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

func TestIsPathAllowed(t *testing.T) {
	cfg := &Config{
		ImportRoots: []string{"/safe"},
	}

	tests := []struct {
		path string
		want bool
	}{
		{"/safe", true},
		{"/safe/subdir", true},
		{"/safe/subdir/deep", true},
		{"/unsafe", false},
		{"/safe_trick", false}, // starts with /safe but not a subdirectory
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := cfg.IsPathAllowed(tt.path)
			if got != tt.want {
				t.Errorf("IsPathAllowed(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestIsPathAllowedTraversal(t *testing.T) {
	cfg := &Config{
		ImportRoots: []string{"/safe"},
	}
	// A path that tries to escape via ..
	escaped, _ := filepath.Abs("/safe/../unsafe")
	got := cfg.IsPathAllowed("/safe/../unsafe")
	if got {
		t.Error("IsPathAllowed should reject path traversal")
	}
	_ = escaped // suppress unused warning
}

func TestSplitComma(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"a,b,c", []string{"a", "b", "c"}},
		{"a", []string{"a"}},
		{"", nil},
		{",,", nil},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := splitComma(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("splitComma(%q) = %v, want %v", tt.input, got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitComma(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}
