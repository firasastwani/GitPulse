package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// CommitRecord stores metadata about a single commit made by GitPulse.
type CommitRecord struct {
	Hash        string    `json:"hash"`
	Message     string    `json:"message"`
	Files       []string  `json:"files"`
	GroupReason string    `json:"group_reason"`
	AIGenerated bool      `json:"ai_generated"`
	Pushed      bool      `json:"pushed"`
	CreatedAt   time.Time `json:"created_at"`
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
		path = filepath.Join(home, ".gitpulse", "history.json")
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

// Recent returns the last n commit records.
func (s *Store) Recent(n int) []CommitRecord {
	if n >= len(s.records) {
		return s.records
	}
	return s.records[len(s.records)-n:]
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
