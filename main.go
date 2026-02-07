package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/firasastwani/gitpulse/internal/config"
	"github.com/firasastwani/gitpulse/internal/dashboard"
	"github.com/firasastwani/gitpulse/internal/engine"
	"github.com/firasastwani/gitpulse/internal/store"
	"github.com/firasastwani/gitpulse/internal/ui"
)

const pidFile = ".gitpulse.pid"

func main() {
	// If the user runs `gitpulse push`, signal the running daemon and exit
	if len(os.Args) > 1 && os.Args[1] == "push" {
		pushCmd()
		return
	}

	// If the user runs `gitpulse dashboard`, serve the Effects Dashboard
	if len(os.Args) > 1 && os.Args[1] == "dashboard" {
		dashboardCmd()
		return
	}

	// ── Daemon mode: watch + buffer + wait for push ──
	cfg, err := config.Load("config.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Single stdin reader — shared between main loop and interactive review prompts
	stdinCh := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			stdinCh <- scanner.Text()
		}
		close(stdinCh)
	}()

	logger := ui.New(stdinCh)
	logger.Info("GitPulse starting", "path", cfg.WatchPath, "branch", cfg.Branch)

	eng, err := engine.New(cfg, logger)
	if err != nil {
		logger.Error("Failed to initialize engine", err)
		os.Exit(1)
	}

	// Daemon mode is interactive — user is at the terminal
	eng.Interactive = true

	// Write PID file so `gitpulse push` can find us
	writePID()
	defer removePID()

	// Listen for SIGUSR1 (from `gitpulse push`) to flush
	usr1 := make(chan os.Signal, 1)
	signal.Notify(usr1, syscall.SIGUSR1)

	// Listen for SIGINT/SIGTERM to shut down
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Start the engine (watches + buffers changes)
	go eng.Run()

	logger.Info("Press ENTER to commit & push (or Ctrl+C to quit)")

	for {
		select {
		case <-stdinCh:
			pending := eng.PendingCount()
			if pending > 0 {
				logger.Info("Flushing changes...", "pending", pending)
				eng.Flush()
				logger.Info("Press ENTER to commit & push (or Ctrl+C to quit)")
			} else {
				logger.Info("No pending changes to flush")
			}
		case <-usr1:
			logger.Info("Received push signal — flushing changes...")
			eng.Flush()
		case <-quit:
			logger.Info("Shutting down GitPulse...")
			eng.Stop()
			return
		}
	}
}

// pushCmd reads the PID file and sends SIGUSR1 to the running daemon.
func pushCmd() {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "GitPulse daemon is not running. Start it with `gitpulse` first.")
		os.Exit(1)
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		fmt.Fprintln(os.Stderr, "Invalid PID file. Restart the daemon.")
		os.Exit(1)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not find daemon process. Restart the daemon.")
		os.Exit(1)
	}

	if err := proc.Signal(syscall.SIGUSR1); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to signal daemon (PID %d): %v\n", pid, err)
		os.Exit(1)
	}

	fmt.Printf("Sent push signal to GitPulse daemon (PID %d)\n", pid)
}

func dashboardCmd() {
	fs := flag.NewFlagSet("dashboard", flag.ExitOnError)
	port := fs.String("port", "8080", "HTTP server port")
	_ = fs.Parse(os.Args[2:])

	cfg, err := config.Load("config.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	historyPath := filepath.Join(cfg.WatchPath, ".gitpulse", "history.json")
	s, err := store.New(historyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open history: %v\n", err)
		os.Exit(1)
	}

	svr := dashboard.NewServer(s, historyPath)
	addr := ":" + *port
	fmt.Printf("GitPulse Effects Dashboard at http://localhost%s\n", addr)
	if err := http.ListenAndServe(addr, svr.Handler()); err != nil {
		log.Fatal(err)
	}
}

func writePID() {
	pid := os.Getpid()
	path := filepath.Join(".", pidFile)
	os.WriteFile(path, []byte(strconv.Itoa(pid)), 0644)
}

func removePID() {
	os.Remove(filepath.Join(".", pidFile))
}
