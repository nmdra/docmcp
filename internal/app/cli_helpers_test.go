package app_test

import (
	"os"
	"testing"

	"github.com/docmcp/docmcp/internal/config"
	"github.com/docmcp/docmcp/internal/store"
)

func writeFile(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o600)
}

// listChunks opens the index directly, so assertions about what landed are
// independent of the CLI's own reporting.
func listChunks(t *testing.T, cfg config.Config) []store.Chunk {
	t.Helper()

	st, err := store.NewChromaStore(t.Context(), store.ChromaConfig{Path: cfg.ChromaPath()})
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	defer st.Close()

	chunks, err := st.ListChunks(t.Context(), store.ListFilter{})
	if err != nil {
		t.Fatalf("list chunks: %v", err)
	}
	return chunks
}

// openStore opens the index directly so a test can inspect or corrupt state the
// CLI does not expose.
func openStore(t *testing.T, cfg config.Config) *store.ChromaStore {
	t.Helper()

	st, err := store.NewChromaStore(t.Context(), store.ChromaConfig{Path: cfg.ChromaPath()})
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	return st
}
