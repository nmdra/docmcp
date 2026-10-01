package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// FileRepository keeps sources in a single JSON document. Source IDs are
// derived from the library ID, so the same add on a fresh file yields the same
// ID across runs.
type FileRepository struct {
	path string
	mu   sync.Mutex
}

func NewFileRepository(path string) (*FileRepository, error) {
	return &FileRepository{path: path}, nil
}

type fileFormat struct {
	Sources []Source `json:"sources"`
}

func (r *FileRepository) Add(ctx context.Context, src Source) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	doc, err := r.read()
	if err != nil {
		return "", err
	}

	libraryID, err := NewLibraryID(src.Name, src.Version)
	if err != nil {
		return "", fmt.Errorf("library ID for %q: %w", src.Name, err)
	}

	for _, existing := range doc.Sources {
		if existing.LibraryID == libraryID {
			return "", fmt.Errorf("%w: %s", ErrSourceExists, libraryID)
		}
	}

	now := time.Now().UTC()
	src.ID = sourceID(libraryID)
	src.LibraryID = libraryID
	src.CreatedAt = now
	src.UpdatedAt = now

	doc.Sources = append(doc.Sources, src)
	sortSources(doc.Sources)

	if err := r.write(doc); err != nil {
		return "", err
	}
	return src.ID, nil
}

func (r *FileRepository) Get(_ context.Context, id string) (Source, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	doc, err := r.read()
	if err != nil {
		return Source{}, err
	}

	for _, src := range doc.Sources {
		if src.ID == id {
			return src, nil
		}
	}
	return Source{}, fmt.Errorf("%w: %s", ErrSourceNotFound, id)
}

func (r *FileRepository) List(_ context.Context) ([]Source, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	doc, err := r.read()
	if err != nil {
		return nil, err
	}
	return doc.Sources, nil
}

func (r *FileRepository) Update(_ context.Context, src Source) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	doc, err := r.read()
	if err != nil {
		return err
	}

	idx := slices.IndexFunc(doc.Sources, func(s Source) bool { return s.ID == src.ID })
	if idx < 0 {
		return fmt.Errorf("%w: %s", ErrSourceNotFound, src.ID)
	}

	src.CreatedAt = doc.Sources[idx].CreatedAt
	src.UpdatedAt = time.Now().UTC()
	doc.Sources[idx] = src

	return r.write(doc)
}

func (r *FileRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	doc, err := r.read()
	if err != nil {
		return err
	}

	idx := slices.IndexFunc(doc.Sources, func(s Source) bool { return s.ID == id })
	if idx < 0 {
		return fmt.Errorf("%w: %s", ErrSourceNotFound, id)
	}

	doc.Sources = slices.Delete(doc.Sources, idx, idx+1)
	return r.write(doc)
}

func (r *FileRepository) read() (fileFormat, error) {
	data, err := os.ReadFile(r.path)
	if os.IsNotExist(err) {
		return fileFormat{}, nil
	}
	if err != nil {
		return fileFormat{}, fmt.Errorf("read sources: %w", err)
	}
	if len(data) == 0 {
		return fileFormat{}, nil
	}

	var doc fileFormat
	if err := json.Unmarshal(data, &doc); err != nil {
		return fileFormat{}, fmt.Errorf("parse %s: %w", r.path, err)
	}
	return doc, nil
}

func (r *FileRepository) write(doc fileFormat) error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return fmt.Errorf("create sources dir: %w", err)
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode sources: %w", err)
	}

	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write sources: %w", err)
	}
	if err := os.Rename(tmp, r.path); err != nil {
		return fmt.Errorf("commit sources: %w", err)
	}
	return nil
}

func sourceID(libraryID string) string {
	sum := sha256.Sum256([]byte(libraryID))
	return hex.EncodeToString(sum[:])[:16]
}

func sortSources(list []Source) {
	slices.SortFunc(list, func(a, b Source) int {
		switch {
		case a.LibraryID < b.LibraryID:
			return -1
		case a.LibraryID > b.LibraryID:
			return 1
		default:
			return 0
		}
	})
}
