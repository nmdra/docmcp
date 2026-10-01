package app_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docmcp/docmcp/internal/config"
)

// TestFreshMachine_DefaultProviderWorksOffline is the release gate: with no API
// key, no Ollama, and no model server, a user indexes a site and asks a question
// through MCP and gets an answer.
//
// It is skipped unless DOCMCP_TEST_LOCAL=1, because the first run downloads the
// model and the default suite must not reach the network.
func TestFreshMachine_DefaultProviderWorksOffline(t *testing.T) {
	if os.Getenv("DOCMCP_TEST_LOCAL") != "1" {
		t.Skip("set DOCMCP_TEST_LOCAL=1 to run the default-provider release gate")
	}

	site := newMCPDocsSite(t)

	cfg := config.Default()
	cfg.Data.Path = t.TempDir()
	// The default provider: no API key, no server.
	cfg.Embedding.Provider = "default"

	configPath := writeDefaultConfig(t, cfg.Data.Path)

	added, err := runRootWithConfig(t, cfg, configPath, "add", site.URL+"/docs/mcp",
		"--name", "acme", "--version", "1.0")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, added)
	}

	if !strings.Contains(added, "indexed:") {
		t.Errorf("add did not report an indexed chunk count:\n%s", added)
	}

	proc := startServeWithConfig(t, cfg.Data.Path, configPath)
	proc.initialize(t)

	resolved := proc.call(t, 2, "resolve-library-id", map[string]any{
		"libraryName": "Acme",
		"query":       "how are codemode tools discovered",
	})
	if result, _ := resolved["result"].(map[string]any); result["isError"] == true {
		t.Fatalf("resolve failed: %s", textOf(t, resolved))
	}
	if got := textOf(t, resolved); !strings.Contains(got, "/local/acme/1.0") {
		t.Fatalf("resolve did not return the library:\n%s", got)
	}

	answered := proc.call(t, 3, "query-docs", map[string]any{
		"libraryId": "/local/acme/1.0",
		"query":     "How are codemode tools discovered?",
	})
	if result, _ := answered["result"].(map[string]any); result["isError"] == true {
		t.Fatalf("query failed: %s", textOf(t, answered))
	}

	content := textOf(t, answered)
	if !strings.Contains(content, "Tool Exposure") {
		t.Errorf("answer is missing the Tool Exposure section:\n%s", content)
	}
	if !strings.Contains(content, "discovered lazily") {
		t.Errorf("answer is missing the lazy-discovery detail:\n%s", content)
	}

	if direct := strings.Index(content, "immediately available"); direct >= 0 {
		if lazy := strings.Index(content, "discovered lazily"); direct < lazy {
			t.Errorf("the decoy section outranked the correct answer:\n%s", content)
		}
	}
}

// TestFreshMachine_SyncIsIncrementalWithRealEmbeddings proves the incremental
// path with a real model: a second sync must embed nothing.
func TestFreshMachine_SyncIsIncrementalWithRealEmbeddings(t *testing.T) {
	if os.Getenv("DOCMCP_TEST_LOCAL") != "1" {
		t.Skip("set DOCMCP_TEST_LOCAL=1 to run the default-provider release gate")
	}

	site := newMCPDocsSite(t)

	cfg := config.Default()
	cfg.Data.Path = t.TempDir()
	cfg.Embedding.Provider = "default"

	if _, err := runRoot(t, cfg, "add", site.URL+"/docs/mcp",
		"--name", "acme", "--version", "1.0"); err != nil {
		t.Fatalf("add: %v", err)
	}

	out, err := runRoot(t, cfg, "sync", "acme")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	if !strings.Contains(out, "added:      0") {
		t.Errorf("a no-op sync with a real embedder added chunks:\n%s", out)
	}
	if !strings.Contains(out, "removed:    0") {
		t.Errorf("a no-op sync removed chunks:\n%s", out)
	}
}

func writeDefaultConfig(t *testing.T, dataDir string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.toml")
	body := "[embedding]\nprovider = \"default\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
