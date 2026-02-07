package config

import (
	"os"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

// Config holds all GitPulse configuration.
type Config struct {
	WatchPath       string   `yaml:"watch_path"`
	DebounceSeconds int      `yaml:"debounce_seconds"` // safety timer — auto-flushes if user forgets to `gitpulse push`
	AutoPush        bool     `yaml:"auto_push"`
	Remote          string   `yaml:"remote"`
	Branch          string   `yaml:"branch"`
	AI              AIConfig `yaml:"ai"`
	IgnorePatterns  []string `yaml:"ignore_patterns"`
}

// AIConfig holds AI provider settings.
type AIConfig struct {
	Provider   string `yaml:"provider"`
	Model      string `yaml:"model"`
	APIKey     string `yaml:"api_key"`     // can also use ANTHROPIC_API_KEY env var
	CodeReview bool   `yaml:"code_review"` // enable AI code review before push (default: true)
}

// Load reads and parses the YAML config file.
// Falls back to sensible defaults if the file doesn't exist.
func Load(path string) (*Config, error) {
	// Load .env file if it exists (does not override existing env vars)
	_ = godotenv.Load()

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

	// Override API key from env var if set (check both names)
	if envKey := os.Getenv("CLAUDE_API_KEY"); envKey != "" {
		cfg.AI.APIKey = envKey
	} else if envKey := os.Getenv("ANTHROPIC_API_KEY"); envKey != "" {
		cfg.AI.APIKey = envKey
	}

	return cfg, nil
}

func defaultConfig() *Config {
	return &Config{
		WatchPath:       ".",
		DebounceSeconds: 900, // 15 min safety net
		AutoPush:        true,
		Remote:          "origin",
		Branch:          "main",
		AI: AIConfig{
			Provider:   "claude",
			Model:      "claude-sonnet-4-20250514",
			CodeReview: true,
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


