package config

import (
	"os"
	"path/filepath"

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

// LoadFromDir looks for config in dir: dir/config.yaml, then dir/.gitpulse/config.yaml.
// If watchPath is non-empty and no config found, returns default config with WatchPath set to watchPath.
// Loads .env from dir so the project's API key is used even when running with -C from another directory.
func LoadFromDir(dir, watchPath string) (*Config, error) {
	// Load project's .env first (so -C /path/to/repo still picks up that repo's .env)
	_ = godotenv.Load(filepath.Join(dir, ".env"))
	// Then cwd .env so local overrides work
	_ = godotenv.Load()
	cfg := defaultConfig()

	try := []string{
		filepath.Join(dir, "config.yaml"),
		filepath.Join(dir, ".gitpulse", "config.yaml"),
	}
	for _, p := range try {
		data, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, err
		}
		if watchPath != "" {
			cfg.WatchPath = watchPath
		}
		if envKey := os.Getenv("CLAUDE_API_KEY"); envKey != "" {
			cfg.AI.APIKey = envKey
		} else if envKey := os.Getenv("ANTHROPIC_API_KEY"); envKey != "" {
			cfg.AI.APIKey = envKey
		}
		return cfg, nil
	}

	// No config in dir — use defaults and set watch path
	if watchPath != "" {
		cfg.WatchPath = watchPath
	}
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

// WriteDefault writes the default config to dir/.gitpulse/config.yaml (creates .gitpulse if needed).
func WriteDefault(dir string) (string, error) {
	cfgDir := filepath.Join(dir, ".gitpulse")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(cfgDir, "config.yaml")
	data, err := yaml.Marshal(defaultConfig())
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", err
	}
	return path, nil
}
