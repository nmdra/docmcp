// Package config loads DocMCP's settings from a TOML file. Tests never read the
// user's config: a path is always passed explicitly.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// DefaultEmbeddingModel is the built-in model, used when the provider is
// "default" and no model was configured.
const DefaultEmbeddingModel = "all-MiniLM-L6-v2"

// APIKeyEnvVar is where an OpenAI-compatible key comes from. A key in the config
// file would end up in a file people commit.
const APIKeyEnvVar = "DOCMCP_OPENAI_API_KEY"

// Config is the resolved settings for one run.
type Config struct {
	Data      DataConfig      `toml:"data"`
	Crawler   CrawlerConfig   `toml:"crawler"`
	Embedding EmbeddingConfig `toml:"embedding"`
	Search    SearchConfig    `toml:"search"`
}

// DataConfig locates the local state: the source list and the vector index.
type DataConfig struct {
	Path string `toml:"path"`
}

// CrawlerConfig bounds one crawl.
type CrawlerConfig struct {
	Concurrency    int           `toml:"concurrency"`
	MaxPages       int           `toml:"max_pages"`
	MaxDepth       int           `toml:"max_depth"`
	RequestTimeout time.Duration `toml:"request_timeout"`
	RateLimit      int           `toml:"rate_limit"`
	MaxBodyBytes   int64         `toml:"max_body_bytes"`
}

// EmbeddingConfig selects the embedding provider.
type EmbeddingConfig struct {
	Provider string `toml:"provider"`
	Model    string `toml:"model"`
	BaseURL  string `toml:"base_url"`

	// APIKey is read from the environment, never from this file.
	APIKey string `toml:"api_key"`
}

// SearchConfig bounds retrieval. These are internal: they never cross the MCP
// boundary.
type SearchConfig struct {
	// CandidateCount is how many vector hits are pulled before filtering.
	CandidateCount int `toml:"candidate_count"`

	// FinalChunks is how many chunks a single query returns.
	FinalChunks int `toml:"final_chunks"`
}

// Options override file values. Tests use DataPath to guarantee they never touch
// the user's real state.
type Options struct {
	DataPath   string
	ConfigPath string
}

// Default returns the built-in settings.
func Default() Config {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}

	return Config{
		Data: DataConfig{Path: defaultDataPath(home)},
		Crawler: CrawlerConfig{
			Concurrency:    4,
			MaxPages:       1000,
			MaxDepth:       20,
			RequestTimeout: 20 * time.Second,
			RateLimit:      0,
			MaxBodyBytes:   10 << 20,
		},
		Embedding: EmbeddingConfig{
			Provider: "default",
			// Only meaningful for the built-in provider. Ollama and OpenAI need
			// an explicit model, because guessing one would silently index a
			// different vector space than the user intended.
			Model: "",
		},
		Search: SearchConfig{
			CandidateCount: 20,
			FinalChunks:    6,
		},
	}
}

// Load reads a config file, filling anything absent from the defaults.
func Load(path string) (Config, error) {
	return LoadWith(path, Options{})
}

// LoadWith reads a config file and applies explicit overrides.
func LoadWith(path string, opts Options) (Config, error) {
	cfg := Default()

	if opts.ConfigPath != "" {
		path = opts.ConfigPath
	}

	if path != "" {
		if err := applyFile(&cfg, path); err != nil {
			return Config{}, err
		}
	}

	if opts.DataPath != "" {
		cfg.Data.Path = opts.DataPath
	}

	cfg.Data.Path = expandHome(cfg.Data.Path)
	cfg.Embedding.APIKey = strings.TrimSpace(os.Getenv(APIKeyEnvVar))

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// applyFile overlays a TOML file onto cfg. A missing file is not an error: the
// defaults stand and the caller keeps working.
func applyFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read config %s: %w", path, err)
	}

	// Overlay onto a copy of the defaults so absent keys keep their value.
	overlaid := *cfg
	if err := toml.Unmarshal(data, &overlaid); err != nil {
		return fmt.Errorf("parse config %s: %w", path, err)
	}

	// An api_key in the file is discarded on purpose; see EmbeddingConfig.
	if _, declared := findKey(data, "api_key"); declared {
		overlaid.Embedding.APIKey = ""
	}

	*cfg = overlaid
	return nil
}

// Validate rejects settings that cannot produce a working run.
func (c Config) Validate() error {
	if strings.TrimSpace(c.Data.Path) == "" {
		return fmt.Errorf("config: data.path is required")
	}
	if c.Crawler.Concurrency <= 0 {
		return fmt.Errorf("config: crawler.concurrency must be positive, got %d", c.Crawler.Concurrency)
	}
	if c.Crawler.MaxPages <= 0 {
		return fmt.Errorf("config: crawler.max_pages must be positive, got %d", c.Crawler.MaxPages)
	}
	if c.Crawler.RequestTimeout <= 0 {
		return fmt.Errorf("config: crawler.request_timeout must be positive, got %v", c.Crawler.RequestTimeout)
	}

	switch c.Embedding.Provider {
	case "default", "":
		if strings.TrimSpace(c.Embedding.Model) == "" {
			c.Embedding.Model = DefaultEmbeddingModel
		}
	case "ollama":
		if strings.TrimSpace(c.Embedding.Model) == "" {
			return fmt.Errorf("config: embedding.model is required for the ollama provider")
		}
	case "openai":
		if strings.TrimSpace(c.Embedding.Model) == "" {
			return fmt.Errorf("config: embedding.model is required for the openai provider")
		}
		if strings.TrimSpace(c.Embedding.APIKey) == "" {
			return fmt.Errorf("config: the openai provider needs %s", APIKeyEnvVar)
		}
	default:
		return fmt.Errorf("config: unknown embedding provider %q", c.Embedding.Provider)
	}

	if c.Search.FinalChunks <= 0 {
		return fmt.Errorf("config: search.final_chunks must be positive, got %d", c.Search.FinalChunks)
	}
	if c.Search.CandidateCount < c.Search.FinalChunks {
		return fmt.Errorf(
			"config: search.candidate_count (%d) must be at least search.final_chunks (%d)",
			c.Search.CandidateCount, c.Search.FinalChunks)
	}

	return nil
}

// SourcesPath is where the source list lives.
func (c Config) SourcesPath() string {
	return filepath.Join(c.Data.Path, "sources.json")
}

// ChromaPath is where the vector index lives.
func (c Config) ChromaPath() string {
	return filepath.Join(c.Data.Path, "chroma")
}

// DefaultPath is the per-user config location.
func DefaultPath() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "docmcp", "config.toml")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "docmcp", "config.toml")
	}
	return ""
}

func defaultDataPath(home string) string {
	if home == "" {
		return ""
	}
	if dir, err := os.UserCacheDir(); err == nil && dir != "" {
		return filepath.Join(dir, "docmcp")
	}
	return filepath.Join(home, ".local", "share", "docmcp")
}

func expandHome(path string) string {
	if path == "" || !strings.HasPrefix(path, "~") {
		return path
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

// findKey reports whether a bare key name appears anywhere in the file. It only
// exists to notice a credential in a config file, which must be ignored.
func findKey(data []byte, key string) (string, bool) {
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, key) {
			return key, true
		}
	}
	return "", false
}
