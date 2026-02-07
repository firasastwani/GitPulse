package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/firasastwani/gitpulse/internal/config"
	"github.com/firasastwani/gitpulse/internal/engine"
	"github.com/firasastwani/gitpulse/internal/ui"
)

func main() {
	// Load configuration
	cfg, err := config.Load("config.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Initialize styled logger
	logger := ui.New()
	logger.Info("GitPulse starting", "path", cfg.WatchPath, "branch", cfg.Branch)

	// Create and start the engine
	eng, err := engine.New(cfg, logger)
	if err != nil {
		logger.Error("Failed to initialize engine", err)
		os.Exit(1)
	}

	// Start the engine in a goroutine
	go eng.Run()

	// Wait for interrupt signal (Ctrl+C) to gracefully shut down
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("Shutting down GitPulse...")
	eng.Stop()
}
