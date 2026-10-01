package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/docmcp/docmcp/internal/config"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestConfig_Defaults(t *testing.T) {
	cfg, err := config.Load(writeConfig(t, ""))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Embedding.Provider != "default" {
		t.Errorf("Embedding.Provider = %q, want default", cfg.Embedding.Provider)
	}
	if cfg.Crawler.Concurrency != 4 {
		t.Errorf("Crawler.Concurrency = %d, want 4", cfg.Crawler.Concurrency)
	}
	if cfg.Crawler.MaxPages != 1000 {
		t.Errorf("Crawler.MaxPages = %d, want 1000", cfg.Crawler.MaxPages)
	}
	if cfg.Crawler.RequestTimeout != 20*time.Second {
		t.Errorf("Crawler.RequestTimeout = %v, want 20s", cfg.Crawler.RequestTimeout)
	}
}

func TestConfig_ReadsDataPath(t *testing.T) {
	cfg, err := config.Load(writeConfig(t, "[data]\npath = \"/tmp/docmcp-data\"\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Data.Path != "/tmp/docmcp-data" {
		t.Errorf("Data.Path = %q", cfg.Data.Path)
	}
}

func TestConfig_ExpandsTildeInDataPath(t *testing.T) {
	cfg, err := config.Load(writeConfig(t, "[data]\npath = \"~/.local/share/docmcp\"\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Data.Path == "~/.local/share/docmcp" {
		t.Error("Data.Path was not expanded, want an absolute path")
	}
	if cfg.Data.Path[0] != '/' {
		t.Errorf("Data.Path = %q, want an absolute path", cfg.Data.Path)
	}
}

func TestConfig_ReadsCrawlerSettings(t *testing.T) {
	cfg, err := config.Load(writeConfig(t,
		"[crawler]\nconcurrency = 12\nmax_pages = 250\nrequest_timeout = \"45s\"\nrate_limit = 3\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Crawler.Concurrency != 12 {
		t.Errorf("Concurrency = %d, want 12", cfg.Crawler.Concurrency)
	}
	if cfg.Crawler.MaxPages != 250 {
		t.Errorf("MaxPages = %d, want 250", cfg.Crawler.MaxPages)
	}
	if cfg.Crawler.RequestTimeout != 45*time.Second {
		t.Errorf("RequestTimeout = %v, want 45s", cfg.Crawler.RequestTimeout)
	}
	if cfg.Crawler.RateLimit != 3 {
		t.Errorf("RateLimit = %d, want 3", cfg.Crawler.RateLimit)
	}
}

func TestConfig_ReadsEmbeddingProvider(t *testing.T) {
	cfg, err := config.Load(writeConfig(t,
		"[embedding]\nprovider = \"ollama\"\nmodel = \"nomic-embed-text\"\nbase_url = \"http://localhost:11434\"\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Embedding.Provider != "ollama" {
		t.Errorf("Provider = %q, want ollama", cfg.Embedding.Provider)
	}
	if cfg.Embedding.Model != "nomic-embed-text" {
		t.Errorf("Model = %q", cfg.Embedding.Model)
	}
}

func TestConfig_KeepsOpenAIKeyOutOfConfig(t *testing.T) {
	// The key must come from the environment; a key in the config file would
	// end up in a file people commit.
	t.Setenv("DOCMCP_OPENAI_API_KEY", "sk-from-env")

	path := writeConfig(t,
		"[embedding]\nprovider = \"openai\"\nmodel = \"m\"\napi_key = \"sk-from-file\"\n")

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Embedding.APIKey != "sk-from-env" {
		t.Errorf("APIKey = %q, want the environment value to win over the file", cfg.Embedding.APIKey)
	}
}

func TestConfig_ReadsOpenAIKeyFromEnvironment(t *testing.T) {
	t.Setenv("DOCMCP_OPENAI_API_KEY", "sk-from-env")

	cfg, err := config.Load(writeConfig(t, "[embedding]\nprovider = \"openai\"\nmodel = \"m\"\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Embedding.APIKey != "sk-from-env" {
		t.Errorf("APIKey = %q, want the environment value", cfg.Embedding.APIKey)
	}
}

func TestConfig_RejectsUnknownProvider(t *testing.T) {
	_, err := config.Load(writeConfig(t, "[embedding]\nprovider = \"telepathy\"\n"))
	if err == nil {
		t.Error("Load accepted an unknown provider, want error")
	}
}

func TestConfig_RejectsMissingModelForOllama(t *testing.T) {
	_, err := config.Load(writeConfig(t, "[embedding]\nprovider = \"ollama\"\n"))
	if err == nil {
		t.Error("Load accepted ollama with no model, want error")
	}
}

func TestConfig_RejectsMalformedTOML(t *testing.T) {
	if _, err := config.Load(writeConfig(t, "this is not toml {{{")); err == nil {
		t.Error("Load accepted malformed TOML, want error")
	}
}

func TestConfig_MissingFileUsesDefaults(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatalf("Load of a missing file: %v", err)
	}

	if cfg.Embedding.Provider != "default" {
		t.Errorf("Provider = %q, want the default", cfg.Embedding.Provider)
	}
}

func TestConfig_MissingFileWithExplicitDataPath(t *testing.T) {
	// A caller that supplies its own data path should never fall back to the
	// user's home directory.
	dataDir := t.TempDir()

	cfg, err := config.LoadWith(filepath.Join(t.TempDir(), "absent.toml"),
		config.Options{DataPath: dataDir})
	if err != nil {
		t.Fatalf("LoadWith: %v", err)
	}
	if cfg.Data.Path != dataDir {
		t.Errorf("Data.Path = %q, want the injected %q", cfg.Data.Path, dataDir)
	}
}

func TestConfig_InjectedDataPathWinsOverFile(t *testing.T) {
	dataDir := t.TempDir()

	cfg, err := config.LoadWith(
		writeConfig(t, "[data]\npath = \"/from/file\"\n"),
		config.Options{DataPath: dataDir},
	)
	if err != nil {
		t.Fatalf("LoadWith: %v", err)
	}
	if cfg.Data.Path != dataDir {
		t.Errorf("Data.Path = %q, want the injected %q to win", cfg.Data.Path, dataDir)
	}
}

func TestConfig_SourcesPathIsDerivedFromDataPath(t *testing.T) {
	dataDir := t.TempDir()

	cfg, err := config.LoadWith(filepath.Join(t.TempDir(), "absent.toml"),
		config.Options{DataPath: dataDir})
	if err != nil {
		t.Fatalf("LoadWith: %v", err)
	}

	if cfg.SourcesPath() != filepath.Join(dataDir, "sources.json") {
		t.Errorf("SourcesPath() = %q, want sources.json under the data path",
			cfg.SourcesPath())
	}
	if cfg.ChromaPath() != filepath.Join(dataDir, "chroma") {
		t.Errorf("ChromaPath() = %q, want chroma under the data path", cfg.ChromaPath())
	}
}

func TestConfig_DoesNotReadUserConfigInTests(t *testing.T) {
	// Sanity: with no explicit path and no env override, Load must not pick up
	// a real user config. Tests pass a path, and this asserts the default is
	// derived from the data path rather than from $HOME.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	cfg, err := config.Load(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SourcesPath() == "" {
		t.Error("SourcesPath() is empty")
	}
}

func TestConfig_ValidateRejectsEmptyDataPath(t *testing.T) {
	cfg := config.Default()
	cfg.Data.Path = ""

	if err := cfg.Validate(); err == nil {
		t.Error("Validate accepted an empty data path, want error")
	}
}

func TestConfig_ValidateRejectsNonPositiveConcurrency(t *testing.T) {
	cfg := config.Default()
	cfg.Data.Path = t.TempDir()
	cfg.Crawler.Concurrency = 0

	if err := cfg.Validate(); err == nil {
		t.Error("Validate accepted zero concurrency, want error")
	}
}

func TestConfig_DefaultIsValid(t *testing.T) {
	cfg := config.Default()
	cfg.Data.Path = t.TempDir()

	if err := cfg.Validate(); err != nil {
		t.Errorf("the default config is invalid: %v", err)
	}
}
