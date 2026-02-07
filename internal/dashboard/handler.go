package dashboard

import (
	"embed"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/firasastwani/gitpulse/internal/store"
)

//go:embed static/*
var staticFS embed.FS

// Server serves the GitPulse Effects Dashboard.
type Server struct {
	store *store.Store
	path  string // history path for display
}

// NewServer creates a dashboard server for the given store.
func NewServer(s *store.Store, historyPath string) *Server {
	return &Server{store: s, path: historyPath}
}

// Handler returns an http.Handler for the dashboard.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Static dashboard
	mux.HandleFunc("GET /", s.serveIndex)

	// API
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("GET /api/history", s.handleHistory)
	mux.HandleFunc("GET /api/commits/", s.handleCommitByHash)
	mux.HandleFunc("GET /api/files", s.handleFilesByPath)

	return mux
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := s.store.Stats()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	records := s.store.All()
	// Reverse chronological (newest first)
	for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
		records[i], records[j] = records[j], records[i]
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(records)
}

func (s *Server) handleCommitByHash(w http.ResponseWriter, r *http.Request) {
	hash := strings.TrimPrefix(r.URL.Path, "/api/commits/")
	if hash == "" {
		http.Error(w, "hash required", http.StatusBadRequest)
		return
	}
	record := s.store.GetByHash(hash)
	if record == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(record)
}

func (s *Server) handleFilesByPath(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path query param required", http.StatusBadRequest)
		return
	}
	records := s.store.GetByFile(path)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(records)
}
