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
	"strings"
	"syscall"

	"github.com/firasastwani/gitpulse/internal/config"
	"github.com/firasastwani/gitpulse/internal/dashboard"
	"github.com/firasastwani/gitpulse/internal/engine"
	"github.com/firasastwani/gitpulse/internal/store"
	"github.com/firasastwani/gitpulse/internal/ui"
)

const pidFile = ".gitpulse.pid"

func main() {
	// gitpulse init [path]
	if len(os.Args) > 1 && os.Args[1] == "init" {
		initCmd()
		return
	}

	// gitpulse push [-C path]
	if len(os.Args) > 1 && os.Args[1] == "push" {
		pushCmd()
		return
	}

	// gitpulse dashboard [-C path] [-port 8080]
	if len(os.Args) > 1 && os.Args[1] == "dashboard" {
		dashboardCmd()
		return
	}

	// ── Daemon mode: resolve -C/path, load config, run ──
	watchDir := resolveWatchDir()
	cfg, err := config.LoadFromDir(watchDir, watchDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}
	// Ensure WatchPath is absolute so watcher/git/store work from any cwd
	if cfg.WatchPath != "" {
		abs, err := filepath.Abs(cfg.WatchPath)
		if err == nil {
			cfg.WatchPath = abs
		}
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

	// Write PID file in watch dir so `gitpulse push` (from that dir or -C) can find us
	writePID(cfg.WatchPath)
	defer removePID(cfg.WatchPath)

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
	fs := flag.NewFlagSet("push", flag.ExitOnError)
	path := fs.String("C", "", "Run as if GitPulse was started in <path>")
	_ = fs.Parse(os.Args[2:])

	dir := "."
	if *path != "" {
		abs, err := filepath.Abs(*path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid path: %v\n", err)
			os.Exit(1)
		}
		dir = abs
	}
	pidPath := filepath.Join(dir, pidFile)
	data, err := os.ReadFile(pidPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "GitPulse daemon is not running. Start it with `gitpulse` (or `gitpulse -C "+dir+"`) first.")
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
	path := fs.String("C", "", "Path to project (for history)")
	port := fs.String("port", "8080", "HTTP server port")
	_ = fs.Parse(os.Args[2:])

	dir := "."
	if *path != "" {
		abs, err := filepath.Abs(*path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid path: %v\n", err)
			os.Exit(1)
		}
		dir = abs
	}
	historyPath := filepath.Join(dir, ".gitpulse", "history.json")
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

func writePID(watchDir string) {
	pid := os.Getpid()
	path := filepath.Join(watchDir, pidFile)
	os.WriteFile(path, []byte(strconv.Itoa(pid)), 0644)
}

func removePID(watchDir string) {
	os.Remove(filepath.Join(watchDir, pidFile))
}

// resolveWatchDir returns the directory to watch: -C path, or first positional arg, or ".".
func resolveWatchDir() string {
	fs := flag.NewFlagSet("gitpulse", flag.ContinueOnError)
	path := fs.String("C", "", "Run as if GitPulse was started in <path>")
	_ = fs.Parse(os.Args[1:])

	if *path != "" {
		abs, _ := filepath.Abs(*path)
		return abs
	}
	// First non-flag arg can be the path (e.g. gitpulse /path/to/project)
	for _, a := range fs.Args() {
		if a != "" && a[0] != '-' {
			abs, _ := filepath.Abs(a)
			return abs
		}
	}
	abs, _ := filepath.Abs(".")
	return abs
}

func initCmd() {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	_ = fs.Parse(os.Args[2:])

	dir := "."
	if len(fs.Args()) > 0 {
		dir = fs.Arg(0)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid path: %v\n", err)
		os.Exit(1)
	}
	dir = abs

	created, err := config.WriteDefault(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create config: %v\n", err)
		os.Exit(1)
	}
	// Optionally append GitPulse entries to .gitignore
	gitignorePath := filepath.Join(dir, ".gitignore")
	if addGitignoreEntries(gitignorePath, ".gitpulse/", ".gitpulse.pid") {
		fmt.Printf("  Updated %s\n", gitignorePath)
	}
	fmt.Printf("GitPulse initialized in %s\n", dir)
	fmt.Printf("  Config: %s\n", created)
	fmt.Printf("  Run: cd %s && gitpulse\n", dir)
	fmt.Printf("  Or: gitpulse -C %s\n", dir)
}

// addGitignoreEntries appends lines to .gitignore if they are not already present. Returns true if file was modified.
func addGitignoreEntries(path string, entries ...string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			content := "# GitPulse\n" + strings.Join(entries, "\n") + "\n"
			_ = os.WriteFile(path, []byte(content), 0644)
			return true
		}
		return false
	}
	content := string(data)
	modified := false
	for _, e := range entries {
		if !strings.Contains(content, strings.TrimSpace(e)) {
			content = strings.TrimRight(content, "\n") + "\n" + e + "\n"
			modified = true
		}
	}
	if modified {
		_ = os.WriteFile(path, []byte(content), 0644)
	}
	return modified
}
