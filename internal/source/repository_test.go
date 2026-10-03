package source_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/nmdra/docmcp/internal/source"
)

func TestSourceRepository_Add(t *testing.T) {
	repo := newRepo(t)

	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	src := source.Source{
		Name:        "pi",
		Description: "Pi coding agent documentation",
		BaseURL:     "https://pi.dev/docs/latest/",
		Version:     "0.99.2",
		Includes:    []string{"/docs/**"},
		Excludes:    []string{"/docs/archive/**"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	id, err := repo.Add(t.Context(), src)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if id == "" {
		t.Fatal("Add returned empty ID")
	}

	got, err := repo.Get(t.Context(), id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "pi" || got.Version != "0.99.2" {
		t.Errorf("stored source = %+v, want name=pi version=0.99.2", got)
	}
	if got.LibraryID != "/local/pi/0.99.2" {
		t.Errorf("LibraryID = %q, want /local/pi/0.99.2", got.LibraryID)
	}
}

func TestSourceRepository_AddRejectsDuplicateLibrary(t *testing.T) {
	repo := newRepo(t)

	src := source.Source{Name: "pi", BaseURL: "https://pi.dev/docs/", Version: "0.99.2"}
	if _, err := repo.Add(t.Context(), src); err != nil {
		t.Fatalf("first Add: %v", err)
	}

	if _, err := repo.Add(t.Context(), src); !errors.Is(err, source.ErrSourceExists) {
		t.Errorf("second Add error = %v, want ErrSourceExists", err)
	}
}

func TestSourceRepository_GetMissingReturnsErrSourceNotFound(t *testing.T) {
	repo := newRepo(t)

	if _, err := repo.Get(t.Context(), "nope"); !errors.Is(err, source.ErrSourceNotFound) {
		t.Errorf("Get error = %v, want ErrSourceNotFound", err)
	}
}

func TestSourceRepository_List(t *testing.T) {
	repo := newRepo(t)

	if got, err := repo.List(t.Context()); err != nil || len(got) != 0 {
		t.Fatalf("List on empty repo = %v, %v; want empty, nil", got, err)
	}

	for _, s := range []source.Source{
		{Name: "pi", BaseURL: "https://pi.dev/docs/", Version: "0.99.1"},
		{Name: "pi", BaseURL: "https://pi.dev/docs/", Version: "0.99.2"},
		{Name: "lightpanda", BaseURL: "https://lightpanda.dev/docs/", Version: "0.4.1"},
	} {
		if _, err := repo.Add(t.Context(), s); err != nil {
			t.Fatalf("Add(%v): %v", s, err)
		}
	}

	got, err := repo.List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("List returned %d sources, want 3", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].LibraryID > got[i].LibraryID {
			t.Errorf("List not sorted by library ID: %q before %q",
				got[i-1].LibraryID, got[i].LibraryID)
		}
	}
}

func TestSourceRepository_VersionsCoexist(t *testing.T) {
	repo := newRepo(t)

	first, err := repo.Add(t.Context(), source.Source{
		Name: "pi", BaseURL: "https://pi.dev/docs/v1/", Version: "1",
	})
	if err != nil {
		t.Fatalf("Add v1: %v", err)
	}
	second, err := repo.Add(t.Context(), source.Source{
		Name: "pi", BaseURL: "https://pi.dev/docs/v2/", Version: "2",
	})
	if err != nil {
		t.Fatalf("Add v2: %v", err)
	}

	if first == second {
		t.Errorf("versions share ID %q, want distinct IDs", first)
	}

	for _, id := range []string{first, second} {
		if _, err := repo.Get(t.Context(), id); err != nil {
			t.Errorf("Get(%q): %v", id, err)
		}
	}
}

func TestSourceRepository_Delete(t *testing.T) {
	repo := newRepo(t)

	id, err := repo.Add(t.Context(), source.Source{
		Name: "pi", BaseURL: "https://pi.dev/docs/", Version: "0.99.2",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if err := repo.Delete(t.Context(), id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Get(t.Context(), id); !errors.Is(err, source.ErrSourceNotFound) {
		t.Errorf("Get after Delete error = %v, want ErrSourceNotFound", err)
	}
	if err := repo.Delete(t.Context(), id); !errors.Is(err, source.ErrSourceNotFound) {
		t.Errorf("second Delete error = %v, want ErrSourceNotFound", err)
	}
}

func TestSourceRepository_Update(t *testing.T) {
	repo := newRepo(t)

	id, err := repo.Add(t.Context(), source.Source{
		Name: "pi", BaseURL: "https://pi.dev/docs/", Version: "0.99.2",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	updated, err := repo.Get(t.Context(), id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	updated.SyncedAt = ptr(time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC))

	if err := repo.Update(t.Context(), updated); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := repo.Get(t.Context(), id)
	if err != nil {
		t.Fatalf("Get after Update: %v", err)
	}
	if got.SyncedAt == nil || !got.SyncedAt.Equal(*updated.SyncedAt) {
		t.Errorf("SyncedAt = %v, want %v", got.SyncedAt, updated.SyncedAt)
	}
	if got.CreatedAt != updated.CreatedAt {
		t.Errorf("Update changed CreatedAt: %v -> %v", updated.CreatedAt, got.CreatedAt)
	}
}

func TestSourceRepository_PersistsAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	ctx := t.Context()

	first, err := source.NewFileRepository(filepath.Join(dir, "sources.json"))
	if err != nil {
		t.Fatalf("NewFileRepository: %v", err)
	}

	id, err := first.Add(ctx, source.Source{
		Name: "pi", BaseURL: "https://pi.dev/docs/", Version: "0.99.2",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	second, err := source.NewFileRepository(filepath.Join(dir, "sources.json"))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}

	got, err := second.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if got.LibraryID != "/local/pi/0.99.2" {
		t.Errorf("LibraryID = %q, want /local/pi/0.99.2", got.LibraryID)
	}
}

func TestSourceRepository_IDIsDeterministic(t *testing.T) {
	dirA := filepath.Join(t.TempDir(), "sources.json")
	dirB := filepath.Join(t.TempDir(), "sources.json")

	src := source.Source{Name: "pi", BaseURL: "https://pi.dev/docs/", Version: "0.99.2"}

	idA := addAndGetID(t, dirA, src)
	idB := addAndGetID(t, dirB, src)

	if idA != idB {
		t.Errorf("source ID differs across runs: %q vs %q", idA, idB)
	}
}

func newRepo(t *testing.T) source.Repository {
	t.Helper()

	repo, err := source.NewFileRepository(filepath.Join(t.TempDir(), "sources.json"))
	if err != nil {
		t.Fatalf("NewFileRepository: %v", err)
	}
	return repo
}

func addAndGetID(t *testing.T, path string, s source.Source) string {
	t.Helper()

	repo, err := source.NewFileRepository(path)
	if err != nil {
		t.Fatalf("NewFileRepository: %v", err)
	}
	id, err := repo.Add(t.Context(), s)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	return id
}

func ptr[T any](v T) *T { return &v }
