package app_test

import (
	"strings"
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
