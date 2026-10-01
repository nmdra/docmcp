//go:build integration

package store_test

import (
	"context"
	"testing"

	"github.com/docmcp/docmcp/internal/store"
)

// TestChromaStoreContract runs the shared contract against a real, persistent
// Chroma. It is the check the in-memory fake cannot give: that the metadata
// predicates, ordering, and upsert semantics actually hold in Chroma.
func TestChromaStoreContract(t *testing.T) {
	RunStoreContractTests(t, newTestChromaStore)
}

func TestChromaStore_PersistsAcrossRestart(t *testing.T) {
	// The path is fixed per test but the dir is a t.TempDir, so a failure never
	// touches a real index.
	dir := t.TempDir()

	PersistAcrossRestart(t, func(t *testing.T) store.Store {
		s, err := store.NewChromaStore(t.Context(), store.ChromaConfig{Path: dir})
		if err != nil {
			t.Fatalf("NewChromaStore: %v", err)
		}
		return s
	})
}

// newTestChromaStore returns a Chroma-backed store on its own temp directory.
func newTestChromaStore(t *testing.T) store.Store {
	t.Helper()

	s, err := store.NewChromaStore(t.Context(), store.ChromaConfig{Path: t.TempDir()})
	if err != nil {
		t.Fatalf("NewChromaStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	return s
}

// TestChromaStore_IdentitySurvivesRestart proves the embedding fingerprint is
// persisted with the index, which is what makes the "one model per index" check
// work on the second run.
func TestChromaStore_IdentitySurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	first, err := store.NewChromaStore(ctx, store.ChromaConfig{Path: dir})
	if err != nil {
		t.Fatalf("NewChromaStore: %v", err)
	}
	if err := first.SetIdentity(ctx, "ollama/nomic-embed-text/768"); err != nil {
		t.Fatalf("SetIdentity: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, err := store.NewChromaStore(ctx, store.ChromaConfig{Path: dir})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer second.Close()

	got, err := second.Identity(ctx)
	if err != nil {
		t.Fatalf("Identity: %v", err)
	}
	if got != "ollama/nomic-embed-text/768" {
		t.Errorf("Identity() = %q after reopen, want the persisted value", got)
	}
}
