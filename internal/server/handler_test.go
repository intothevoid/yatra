package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/karan/travel-diary/internal/adapters"
	"github.com/karan/travel-diary/internal/config"
	"github.com/karan/travel-diary/internal/services"
	"github.com/karan/travel-diary/internal/store"
)

func setupApp(t *testing.T) (*App, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()

	cfg := &config.Config{
		DataDir:     dir,
		ImportRoots: []string{dir},
	}

	s, err := store.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	proc := adapters.FakeImageProcessor{}
	vlm := &adapters.FakeVLMAdapter{}
	text := adapters.FakeTextAdapter{}
	importer := services.New(cfg, s, proc)
	genie := services.NewGenie(cfg, s, vlm, text, proc)

	app := NewApp(cfg, s, importer, genie, nil)
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)

	return app, srv
}

func TestHealthLive(t *testing.T) {
	_, srv := setupApp(t)
	resp, err := srv.Client().Get(srv.URL + "/health/live")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestHealthReady(t *testing.T) {
	_, srv := setupApp(t)
	resp, err := srv.Client().Get(srv.URL + "/health/ready")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestListTripsEmpty(t *testing.T) {
	_, srv := setupApp(t)
	resp, err := srv.Client().Get(srv.URL + "/api/v1/trips")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var trips []interface{}
	json.NewDecoder(resp.Body).Decode(&trips)
	if len(trips) != 0 {
		t.Errorf("got %d trips, want 0", len(trips))
	}
}

func TestImportTripBadPath(t *testing.T) {
	_, srv := setupApp(t)
	req := map[string]string{"source_path": "/etc/passwd"}
	body, _ := json.Marshal(req)
	resp, err := srv.Client().Post(srv.URL+"/api/v1/trips/import-path", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestImportTripMissingPath(t *testing.T) {
	_, srv := setupApp(t)
	req := map[string]string{}
	body, _ := json.Marshal(req)
	resp, err := srv.Client().Post(srv.URL+"/api/v1/trips/import-path", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestGetTripNotFound(t *testing.T) {
	_, srv := setupApp(t)
	resp, err := srv.Client().Get(srv.URL + "/api/v1/trips/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestAnalyseTripNotFound(t *testing.T) {
	_, srv := setupApp(t)
	resp, err := srv.Client().Post(srv.URL+"/api/v1/trips/nonexistent/analyse", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestJobNotFound(t *testing.T) {
	_, srv := setupApp(t)
	resp, err := srv.Client().Get(srv.URL + "/api/v1/jobs/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}
