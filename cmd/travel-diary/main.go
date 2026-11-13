package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/karan/travel-diary/internal/adapters"
	"github.com/karan/travel-diary/internal/config"
	"github.com/karan/travel-diary/internal/server"
	"github.com/karan/travel-diary/internal/services"
	"github.com/karan/travel-diary/internal/store"
)

func main() {
	cfg := config.LoadConfig()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("config error: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Open database
	s, err := store.Open(ctx, cfg.DataDir)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer s.Close()
	if timeoutSetting, ok, err := s.Settings.Get(ctx, store.ModelTimeoutSettingKey); err != nil {
		log.Fatalf("load model timeout setting: %v", err)
	} else if ok {
		seconds, parseErr := strconv.ParseInt(timeoutSetting, 10, 64)
		if parseErr != nil || cfg.ModelTimeout.SetSeconds(seconds) != nil {
			log.Printf("ignoring invalid saved model timeout setting")
		}
	}
	if endpoint, ok, err := s.Settings.Get(ctx, store.LocalModelEndpointSettingKey); err != nil {
		log.Fatalf("load local model endpoint setting: %v", err)
	} else if ok && endpoint != "" {
		cfg.OMLXWriterBaseURL = endpoint
	}
	if model, ok, err := s.Settings.Get(ctx, store.LocalModelIDSettingKey); err != nil {
		log.Fatalf("load local model ID setting: %v", err)
	} else if ok && model != "" {
		cfg.OMLXWriterModel = model
	}
	if endpoint, ok, err := s.Settings.Get(ctx, store.VisionModelEndpointSettingKey); err != nil {
		log.Fatalf("load vision model endpoint setting: %v", err)
	} else if ok && endpoint != "" {
		cfg.OMLXVisionBaseURL = endpoint
	}
	if model, ok, err := s.Settings.Get(ctx, store.VisionModelIDSettingKey); err != nil {
		log.Fatalf("load vision model ID setting: %v", err)
	} else if ok && model != "" {
		cfg.OMLXVisionModel = model
	}

	// Create adapters
	proc := &adapters.SystemImageProcessor{
		EXIF: &adapters.ExifToolAdapter{},
		Vips: &adapters.VipsProcessor{ThumbnailSize: cfg.ThumbnailSize, PreviewSize: cfg.PreviewSize, ExportSize: cfg.ExportSize, JPEGQuality: cfg.JPEGQuality},
	}
	var vlm adapters.VLMAdapter = &adapters.FakeVLMAdapter{}
	visionModel := cfg.OMLXVisionModel
	if visionModel == "" {
		visionModel = cfg.OMLXWriterModel
	}
	if visionModel != "" {
		vlm = adapters.NewOpenAIVisionAdapter(cfg)
		vlm.(*adapters.OpenAIVisionAdapter).Model = visionModel
	}
	var text adapters.TextAdapter = adapters.FakeTextAdapter{}
	if cfg.OMLXWriterModel != "" {
		text = adapters.NewOpenAITextAdapter(cfg)
	}

	// Create Ghost client if configured
	var ghostClient *services.GhostClient
	if cfg.GhostURL != "" && cfg.GhostAPIKey != "" {
		ghostClient = services.NewGhostClient(cfg.GhostURL, cfg.GhostAPIKey)
	}

	// Create services
	importer := services.New(cfg, s, proc)
	genie := services.NewGenie(cfg, s, vlm, text, proc)
	exporter := services.NewGhostExporter(ghostClient, s)

	// Create app
	app := server.NewApp(cfg, s, importer, genie, exporter)

	// Start worker pool
	pool := services.NewPool(s, genie, cfg.OMLXConcurrency)
	pool.Start(ctx)
	defer pool.Stop()

	// Start HTTP server
	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("Travel Diary AI listening on %s", addr)

	srv := &http.Server{
		Addr:    addr,
		Handler: app.Router(),
	}

	go func() {
		<-ctx.Done()
		log.Println("shutting down...")
		srv.Shutdown(context.Background())
	}()

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
