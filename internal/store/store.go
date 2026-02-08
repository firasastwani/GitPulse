package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// FileChange stores per-file diff and line stats for a commit.
type FileChange struct {
	Path         string `json:"path"`
	Diff         string `json:"diff"`
	LinesAdded   int    `json:"lines_added"`
	LinesRemoved int    `json:"lines_removed"`
	Status       string `json:"status"` // "modified", "added", "deleted"
}

// ReviewFinding is a standalone copy of ai.ReviewFinding to avoid import cycles.
type ReviewFinding struct {
	File        string `json:"file"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
	Severity    string `json:"severity"`
	Description string `json:"description"`
	Suggestion  string `json:"suggestion"`
}

// FixRecord stores info about a fix applied before commit.
type FixRecord struct {
	File        string `json:"file"`
	Description string `json:"description"`
	FixType     string `json:"fix_type"` // "ai" or "manual"
}

// ReviewRecord stores code review results for a commit.
type ReviewRecord struct {
	Findings     []ReviewFinding `json:"findings"`
	HasBlockers  bool            `json:"has_blockers"`
	Action       string          `json:"action"` // "manual", "aifix", "continue", ""
	FixesApplied []FixRecord     `json:"fixes_applied,omitempty"`
}

// CommitRecord stores enriched metadata about a single commit made by GitPulse.
type CommitRecord struct {
	Hash        string        `json:"hash"`
	Message     string        `json:"message"`
	Files       []FileChange  `json:"files"`
	GroupReason string        `json:"group_reason"`
	AIGenerated bool          `json:"ai_generated"`
	Review      *ReviewRecord `json:"review,omitempty"`
	Pushed      bool          `json:"pushed"`
	PushedAt    *time.Time    `json:"pushed_at,omitempty"`
	Remote      string        `json:"remote,omitempty"`
	Branch      string        `json:"branch,omitempty"`
	CreatedAt   time.Time     `json:"created_at"`
}

// StoreStats provides summary statistics for the web UI dashboard.
type StoreStats struct {
	TotalCommits      int `json:"total_commits"`
	TotalFiles        int `json:"total_files_changed"`
	TotalLinesAdded   int `json:"total_lines_added"`
	TotalLinesRemoved int `json:"total_lines_removed"`
	ReviewsRun        int `json:"reviews_run"`
	ReviewsBlocked    int `json:"reviews_blocked"`
}

// Store persists commit history to a JSON file.
type Store struct {
	path    string
	records []CommitRecord
}

// New creates a new Store. If path is empty, uses ~/.gitpulse/history.json.
func New(path string) (*Store, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, "gitpulse", "history.json")
	}

	s := &Store{path: path}

	// Create directory if it doesn't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	// Load existing records if file exists
	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return s, nil
}

// Save appends a commit record and writes to disk.
func (s *Store) Save(record CommitRecord) error {
	record.CreatedAt = time.Now()
	s.records = append(s.records, record)
	return s.flush()
}

// Recent returns the last n commit records (newest last).
func (s *Store) Recent(n int) []CommitRecord {
	if n >= len(s.records) {
		return s.records
	}
	return s.records[len(s.records)-n:]
}

// GetByHash returns the commit record matching the given hash, or nil if not found.
func (s *Store) GetByHash(hash string) *CommitRecord {
	for i := range s.records {
		if s.records[i].Hash == hash {
			return &s.records[i]
		}
	}
	return nil
}

// GetByFile returns all commit records that touch the given file path.
func (s *Store) GetByFile(path string) []CommitRecord {
	var results []CommitRecord
	for _, r := range s.records {
		for _, f := range r.Files {
			if f.Path == path {
				results = append(results, r)
				break
			}
		}
	}
	return results
}

// GetByDateRange returns all commit records within the given time range (inclusive).
func (s *Store) GetByDateRange(from, to time.Time) []CommitRecord {
	var results []CommitRecord
	for _, r := range s.records {
		if !r.CreatedAt.Before(from) && !r.CreatedAt.After(to) {
			results = append(results, r)
		}
	}
	return results
}

// Stats computes summary statistics across all stored commit records.
func (s *Store) Stats() StoreStats {
	stats := StoreStats{
		TotalCommits: len(s.records),
	}

	fileSet := make(map[string]bool)
	for _, r := range s.records {
		for _, f := range r.Files {
			fileSet[f.Path] = true
			stats.TotalLinesAdded += f.LinesAdded
			stats.TotalLinesRemoved += f.LinesRemoved
		}
		if r.Review != nil {
			stats.ReviewsRun++
			if r.Review.HasBlockers {
				stats.ReviewsBlocked++
			}
		}
	}
	stats.TotalFiles = len(fileSet)

	return stats
}

// MarkPushed updates all records matching the given hashes as pushed.
func (s *Store) MarkPushed(hashes []string, remote, branch string) error {
	hashSet := make(map[string]bool, len(hashes))
	for _, h := range hashes {
		hashSet[h] = true
	}

	now := time.Now()
	for i := range s.records {
		if hashSet[s.records[i].Hash] {
			s.records[i].Pushed = true
			s.records[i].PushedAt = &now
			s.records[i].Remote = remote
			s.records[i].Branch = branch
		}
	}

	return s.flush()
}

// All returns every stored commit record.
func (s *Store) All() []CommitRecord {
	return s.records
}

// Reload re-reads the history file from disk. Use when serving a dashboard
// that should reflect commits made by another process (e.g., the daemon).
func (s *Store) Reload() error {
	return s.load()
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &s.records)
}

func (s *Store) flush() error {
	data, err := json.MarshalIndent(s.records, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0644)
}

func (s *Store) isEmpty() bool {
	return len(s.records) == 0
}



