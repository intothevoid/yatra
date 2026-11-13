package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type appSettings struct {
	LLMBaseURL         string `json:"llm_base_url"`
	LLMAPIKey          string `json:"llm_api_key,omitempty"`
	LLMKeyConfigured   bool   `json:"llm_key_configured"`
	WriterModel        string `json:"writer_model"`
	VisionModel        string `json:"vision_model"`
	TimeoutSeconds     int    `json:"timeout_seconds"`
	GhostURL           string `json:"ghost_url"`
	GhostAdminKey      string `json:"ghost_admin_key,omitempty"`
	GhostKeyConfigured bool   `json:"ghost_key_configured"`
	WebResearch        bool   `json:"web_research"`
	BraveKey           string `json:"brave_key,omitempty"`
	BraveKeyConfigured bool   `json:"brave_key_configured"`
	ThumbnailSize      int    `json:"thumbnail_size"`
	PreviewSize        int    `json:"preview_size"`
	ExportSize         int    `json:"export_size"`
	JPEGQuality        int    `json:"jpeg_quality"`
	MaxFileSizeMB      int    `json:"max_file_size_mb"`
	MaxUploadSizeMB    int    `json:"max_upload_size_mb"`
	MaxUploadFiles     int    `json:"max_upload_files"`
}

func (a *App) registerSettingsRoutes(r interface {
	Get(string, http.HandlerFunc)
	Put(string, http.HandlerFunc)
}) {
	r.Get("/api/v1/app-settings", a.getSettings)
	r.Put("/api/v1/app-settings", a.putSettings)
}

func (a *App) settingsPath() string { return filepath.Join(a.cfg.DataDir, "app-settings.enc") }
func sealSettings(key, plain []byte) ([]byte, error) {
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	g, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	nonce := make([]byte, g.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return nil, e
	}
	return append(nonce, g.Seal(nil, nonce, plain, nil)...), nil
}
func openSettings(key, data []byte) ([]byte, error) {
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	g, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	if len(data) < g.NonceSize() {
		return nil, fmt.Errorf("invalid encrypted settings")
	}
	return g.Open(nil, data[:g.NonceSize()], data[g.NonceSize():], nil)
}
func (a *App) settingsFromConfig() appSettings {
	c := a.cfg
	return appSettings{LLMBaseURL: c.OMLXBaseURL, WriterModel: c.OMLXWriterModel, VisionModel: c.OMLXVisionModel, TimeoutSeconds: c.OMLXTimeoutSec, GhostURL: c.GhostURL, WebResearch: c.LocationWebSearch, ThumbnailSize: c.ThumbnailSize, PreviewSize: c.PreviewSize, ExportSize: c.ExportSize, JPEGQuality: c.JPEGQuality, MaxFileSizeMB: int(c.MaxFileSize >> 20), MaxUploadSizeMB: int(c.MaxUploadSize >> 20), MaxUploadFiles: c.MaxUploadFiles, LLMKeyConfigured: c.OMLXAPIKey != "", GhostKeyConfigured: c.GhostAPIKey != "", BraveKeyConfigured: c.BraveSearchAPIKey != ""}
}
func (a *App) getSettings(w http.ResponseWriter, r *http.Request) {
	out := a.settingsFromConfig()
	key := a.auth.SessionKey(r)
	if len(key) == 0 {
		http.Error(w, "authentication required", 401)
		return
	}
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	if b, e := os.ReadFile(a.settingsPath()); e == nil {
		plain, e := openSettings(key, b)
		if e != nil {
			http.Error(w, "could not decrypt settings; unlock with the original app password", 500)
			return
		}
		var saved appSettings
		if json.Unmarshal(plain, &saved) != nil {
			http.Error(w, "settings file is invalid", 500)
			return
		}
		a.applySettings(saved)
		saved.LLMKeyConfigured = saved.LLMAPIKey != ""
		saved.GhostKeyConfigured = saved.GhostAdminKey != ""
		saved.BraveKeyConfigured = saved.BraveKey != ""
		saved.LLMAPIKey = ""
		saved.GhostAdminKey = ""
		saved.BraveKey = ""
		out = saved
	} else if !os.IsNotExist(e) {
		http.Error(w, "could not read settings", 500)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(out)
}
func (a *App) putSettings(w http.ResponseWriter, r *http.Request) {
	key := a.auth.SessionKey(r)
	if len(key) == 0 {
		http.Error(w, "authentication required", 401)
		return
	}
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	var in appSettings
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in) != nil {
		http.Error(w, "invalid settings", 400)
		return
	}
	cur := a.settingsFromConfig()
	if in.LLMAPIKey == "" && cur.LLMKeyConfigured {
		in.LLMAPIKey = a.cfg.OMLXAPIKey
	}
	if in.GhostAdminKey == "" && cur.GhostKeyConfigured {
		in.GhostAdminKey = a.cfg.GhostAPIKey
	}
	if in.BraveKey == "" && cur.BraveKeyConfigured {
		in.BraveKey = a.cfg.BraveSearchAPIKey
	}
	if err := validateSettings(in); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	plain, err := json.Marshal(in)
	if err != nil {
		http.Error(w, "could not encode settings", 500)
		return
	}
	sealed, err := sealSettings(key, plain)
	if err != nil {
		http.Error(w, "could not encrypt settings", 500)
		return
	}
	if err = os.MkdirAll(filepath.Dir(a.settingsPath()), 0700); err != nil {
		http.Error(w, "could not store settings", 500)
		return
	}
	tmp := a.settingsPath() + ".tmp"
	if err = os.WriteFile(tmp, sealed, 0600); err != nil {
		http.Error(w, "could not store settings", 500)
		return
	}
	if err = os.Rename(tmp, a.settingsPath()); err != nil {
		os.Remove(tmp)
		http.Error(w, "could not store settings", 500)
		return
	}
	a.applySettings(in)
	in.LLMKeyConfigured = in.LLMAPIKey != ""
	in.GhostKeyConfigured = in.GhostAdminKey != ""
	in.BraveKeyConfigured = in.BraveKey != ""
	in.LLMAPIKey = ""
	in.GhostAdminKey = ""
	in.BraveKey = ""
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(in)
}

func (a *App) applySettings(s appSettings) {
	a.cfg.OMLXBaseURL, a.cfg.OMLXAPIKey = s.LLMBaseURL, s.LLMAPIKey
	a.cfg.OMLXWriterModel, a.cfg.OMLXVisionModel, a.cfg.OMLXTimeoutSec = s.WriterModel, s.VisionModel, s.TimeoutSeconds
	a.cfg.GhostURL, a.cfg.GhostAPIKey = s.GhostURL, s.GhostAdminKey
	a.cfg.LocationWebSearch, a.cfg.BraveSearchAPIKey = s.WebResearch, s.BraveKey
	a.cfg.ThumbnailSize, a.cfg.PreviewSize, a.cfg.ExportSize, a.cfg.JPEGQuality = s.ThumbnailSize, s.PreviewSize, s.ExportSize, s.JPEGQuality
	a.cfg.MaxFileSize, a.cfg.MaxUploadSize, a.cfg.MaxUploadFiles = int64(s.MaxFileSizeMB)<<20, int64(s.MaxUploadSizeMB)<<20, s.MaxUploadFiles
	a.genie.Configure(a.cfg)
	a.exporter.Configure(a.cfg.GhostURL, a.cfg.GhostAPIKey)
	a.importer.ConfigureImageSettings(a.cfg)
}
func validateSettings(s appSettings) error {
	modelURL, err := url.ParseRequestURI(strings.TrimSpace(s.LLMBaseURL))
	if err != nil || (modelURL.Scheme != "http" && modelURL.Scheme != "https") || modelURL.Host == "" || s.TimeoutSeconds < 1 || s.TimeoutSeconds > 3600 {
		return fmt.Errorf("enter a model URL and timeout between 1 and 3600 seconds")
	}
	if s.ThumbnailSize < 100 || s.PreviewSize < s.ThumbnailSize || s.ExportSize < s.PreviewSize || s.ExportSize > 12000 || s.JPEGQuality < 30 || s.JPEGQuality > 100 {
		return fmt.Errorf("check image sizes and JPEG quality (30–100)")
	}
	if s.MaxFileSizeMB < 1 || s.MaxFileSizeMB > 2048 || s.MaxUploadSizeMB < s.MaxFileSizeMB || s.MaxUploadSizeMB > 10240 || s.MaxUploadFiles < 1 || s.MaxUploadFiles > 1000 {
		return fmt.Errorf("check upload limits and photo count (1–1000)")
	}
	if s.WebResearch && s.BraveKey == "" {
		return fmt.Errorf("enter a Brave Search API key to enable web research")
	}
	if strings.TrimSpace(s.GhostURL) != "" {
		ghostURL, err := url.ParseRequestURI(strings.TrimSpace(s.GhostURL))
		if err != nil || (ghostURL.Scheme != "http" && ghostURL.Scheme != "https") || ghostURL.Host == "" {
			return fmt.Errorf("enter a valid Ghost site URL beginning with http:// or https://")
		}
	}
	return nil
}
