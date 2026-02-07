package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds all GitPulse configuration.
type Config struct {
	WatchPath       string   `yaml:"watch_path"`
	DebounceSeconds int      `yaml:"debounce_seconds"`
	AutoPush        bool     `yaml:"auto_push"`
	Remote          string   `yaml:"remote"`
	Branch          string   `yaml:"branch"`
	AI              AIConfig `yaml:"ai"`
	IgnorePatterns  []string `yaml:"ignore_patterns"`
}

// AIConfig holds AI provider settings.
type AIConfig struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	APIKey   string `yaml:"api_key"` // can also use ANTHROPIC_API_KEY env var
}

// Load reads and parses the YAML config file.
// Falls back to sensible defaults if the file doesn't exist.
func Load(path string) (*Config, error) {
	cfg := defaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// No config file -- use defaults
			return cfg, nil
		}
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	// Override API key from env var if set
	if envKey := os.Getenv("ANTHROPIC_API_KEY"); envKey != "" {
		cfg.AI.APIKey = envKey
	}

	return cfg, nil
}

func defaultConfig() *Config {
	return &Config{
		WatchPath:       ".",
		DebounceSeconds: 30,
		AutoPush:        true,
		Remote:          "origin",
		Branch:          "main",
		AI: AIConfig{
			Provider: "claude",
			Model:    "claude-sonnet-4-20250514",
		},
		IgnorePatterns: []string{
			"*.log",
			"node_modules/",
			".git/",
			"vendor/",
			".gitpulse/",
		},
	}
}
