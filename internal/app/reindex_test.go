package app_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestReindexCommand_RequiresName(t *testing.T) {
	cfg := newTestCommand(t)

	if _, err := runRoot(t, cfg, "reindex"); err == nil {
		t.Error("reindex with no library name succeeded, want error")
	}
}

func TestReindexCommand_UnknownSource(t *testing.T) {
	cfg := newTestCommand(t)

	if _, err := runRoot(t, cfg, "reindex", "nope"); err == nil {
		t.Error("reindex of an unknown library succeeded, want error")
	}
}

func TestReindexCommand_RebuildsAfterEmbeddingChange(t *testing.T) {
	site := newFixtureSite(t)
	cfg := newTestCommand(t)

	if _, err := runRoot(t, cfg, "add", site.URL+"/docs/", "--name", "fixture", "--version", "1"); err != nil {
		t.Fatalf("add: %v", err)
	}
	before := len(listChunks(t, cfg))
	if before == 0 {
		t.Fatal("nothing was indexed")
	}

	out, err := runRoot(t, cfg, "reindex", "fixture")
	if err != nil {
		t.Fatalf("reindex: %v\n%s", err, out)
	}

	after := len(listChunks(t, cfg))
	if after == 0 {
		t.Error("reindex left the index empty")
	}
	if !strings.Contains(out, "/local/fixture/1") {
		t.Errorf("reindex output should name the library:\n%s", out)
	}
}

func TestReindexCommand_ClearsStaleEmbeddingIdentity(t *testing.T) {
	site := newFixtureSite(t)
	cfg := newTestCommand(t)

	if _, err := runRoot(t, cfg, "add", site.URL+"/docs/", "--name", "fixture", "--version", "1"); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Corrupt the recorded identity the way a model change would.
	st := openStore(t, cfg)
	if err := st.SetIdentity(t.Context(), "ollama/nomic-embed-text/768"); err != nil {
		t.Fatalf("set identity: %v", err)
	}
	st.Close()

	// With a foreign identity recorded, an ordinary sync must refuse.
	syncOut, syncErr := runRoot(t, cfg, "sync", "fixture")
	if syncErr == nil {
		t.Errorf("sync wrote into an index written by another model:\n%s", syncOut)
	}

	// reindex is the documented escape hatch, so it must succeed.
	out, err := runRoot(t, cfg, "reindex", "fixture")
	if err != nil {
		t.Fatalf("reindex should recover from a model change: %v\n%s", err, out)
	}

	st2 := openStore(t, cfg)
	defer st2.Close()

	identity, err := st2.Identity(t.Context())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	if strings.Contains(identity, "ollama") {
		t.Errorf("reindex left the old identity in place: %q", identity)
	}
}

func TestReindexCommand_RefusesScopedMigrationOfSharedIndex(t *testing.T) {
	site := newFixtureSite(t)
	cfg := newTestCommand(t)
	for _, name := range []string{"first", "second"} {
		if _, err := runRoot(t, cfg, "add", site.URL+"/docs/", "--name", name, "--version", "1"); err != nil {
			t.Fatalf("add %s: %v", name, err)
		}
	}
	before := len(listChunks(t, cfg))
	st := openStore(t, cfg)
	const oldIdentity = "fake/fake-model/256"
	if err := st.SetIdentity(t.Context(), oldIdentity); err != nil {
		t.Fatalf("set legacy identity: %v", err)
	}
	st.Close()

	_, err := runRoot(t, cfg, "reindex", "first")
	if err == nil || !strings.Contains(err.Error(), "other libraries") {
		t.Fatalf("reindex error = %v, want shared-index migration refusal", err)
	}
	if after := len(listChunks(t, cfg)); after != before {
		t.Errorf("refused reindex changed chunk count from %d to %d", before, after)
	}
	st = openStore(t, cfg)
	defer st.Close()
	identity, err := st.Identity(t.Context())
	if err != nil || identity != oldIdentity {
		t.Errorf("refused reindex changed identity: %q, error %v", identity, err)
	}
}

func TestReindexCommand_IsIdempotent(t *testing.T) {
	site := newFixtureSite(t)
	cfg := newTestCommand(t)

	if _, err := runRoot(t, cfg, "add", site.URL+"/docs/", "--name", "fixture", "--version", "1"); err != nil {
		t.Fatalf("add: %v", err)
	}

	first, err := runRoot(t, cfg, "reindex", "fixture")
	if err != nil {
		t.Fatalf("first reindex: %v", err)
	}
	second, err := runRoot(t, cfg, "reindex", "fixture")
	if err != nil {
		t.Fatalf("second reindex: %v", err)
	}

	if !strings.Contains(first, "indexed:") || !strings.Contains(second, "indexed:") {
		t.Errorf("reindex output missing counts:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if count(t, second) == 0 {
		t.Error("a second reindex indexed nothing")
	}
}

// count reads the "indexed:" figure out of a command report.
func count(t *testing.T, out string) int {
	t.Helper()

	for _, line := range strings.Split(out, "\n") {
		_, value, found := strings.Cut(line, "indexed:")
		if !found {
			continue
		}

		n := 0
		for _, r := range strings.TrimSpace(value) {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		return n
	}
	return 0
}

func TestReindexCommand_SharedUnknownWidthRetainsFingerprintAndRejectsDrift(t *testing.T) {
	site := newFixtureSite(t)
	cfg := newTestCommand(t)
	var width atomic.Int64
	width.Store(4)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vector := make([]float32, int(width.Load()))
		vector[0] = 1
		if err := json.NewEncoder(w).Encode(map[string]any{"embedding": vector}); err != nil {
			t.Error(err)
		}
	}))
	defer provider.Close()
	cfg.Embedding.Provider = "ollama"
	cfg.Embedding.Model = "test-model"
	cfg.Embedding.BaseURL = provider.URL
	configPath := filepath.Join(t.TempDir(), "ollama.toml")
	if err := writeFile(configPath, fmt.Sprintf("[embedding]\nprovider = %q\nmodel = %q\nbase_url = %q\n", "ollama", "test-model", provider.URL)); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, error) { return runRootWithConfig(t, cfg, configPath, args...) }
	for _, name := range []string{"first", "second"} {
		if _, err := run("add", site.URL+"/docs/", "--name", name, "--version", "1"); err != nil {
			t.Fatalf("add %s: %v", name, err)
		}
	}
	const want = "ollama/test-model/4/title-heading-v1"
	assertIdentity := func() {
		t.Helper()
		st := openStore(t, cfg)
		defer st.Close()
		got, err := st.Identity(t.Context())
		if err != nil || got != want {
			t.Fatalf("identity = %q, error %v, want %q", got, err, want)
		}
	}
	assertIdentity()
	if _, err := run("reindex", "first"); err != nil {
		t.Fatalf("compatible reindex: %v", err)
	}
	assertIdentity()
	width.Store(6)
	_, err := run("reindex", "first")
	if err == nil || !strings.Contains(err.Error(), "reindex") {
		t.Fatalf("width drift error = %v", err)
	}
	assertIdentity()
	// Clearing the target is part of reindex, but no replacement vectors may be mixed.
	for _, chunk := range listChunks(t, cfg) {
		if chunk.LibraryID == "/local/first/1" {
			t.Fatalf("drift wrote target chunk: %s", chunk.ID)
		}
	}
}

func TestReindexCommand_RefusesForeignModelAndKnownWidthInSharedIndex(t *testing.T) {
	for _, identity := range []string{"fake/foreign-model/256/title-heading-v1", "fake/fake-model/128/title-heading-v1"} {
		t.Run(identity, func(t *testing.T) {
			site := newFixtureSite(t)
			cfg := newTestCommand(t)
			for _, name := range []string{"first", "second"} {
				if _, err := runRoot(t, cfg, "add", site.URL+"/docs/", "--name", name); err != nil {
					t.Fatal(err)
				}
			}
			before := len(listChunks(t, cfg))
			st := openStore(t, cfg)
			if err := st.SetIdentity(t.Context(), identity); err != nil {
				t.Fatal(err)
			}
			st.Close()
			if _, err := runRoot(t, cfg, "reindex", "first"); err == nil || !strings.Contains(err.Error(), "other libraries") {
				t.Fatalf("error = %v", err)
			}
			if len(listChunks(t, cfg)) != before {
				t.Fatal("refusal changed chunks")
			}
		})
	}
}
