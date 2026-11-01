package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"

	"github.com/karan/travel-diary/internal/adapters"
	"github.com/karan/travel-diary/internal/config"
	"github.com/karan/travel-diary/internal/services"
	"github.com/karan/travel-diary/internal/server"
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

	// Create adapters
	proc := adapters.FakeImageProcessor{}
	vlm := &adapters.FakeVLMAdapter{}
	text := adapters.FakeTextAdapter{}

	// Create services
	importer := services.New(cfg, s, proc)
	genie := services.NewGenie(cfg, s, vlm, text, proc)

	// Create app
	app := server.NewApp(cfg, s, importer, genie)

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
