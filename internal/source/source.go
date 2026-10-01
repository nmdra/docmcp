package source

import (
	"context"
	"errors"
	"time"
)

var (
	ErrSourceNotFound = errors.New("source not found")
	ErrSourceExists   = errors.New("source already exists")
)

// Source is one indexed documentation site at one version. LibraryID is the
// public handle ("/local/pi/0.99.2"); ID is the internal record key.
type Source struct {
	ID          string
	LibraryID   string
	Name        string
	Description string
	BaseURL     string
	Version     string

	Includes []string
	Excludes []string

	CreatedAt time.Time
	UpdatedAt time.Time
	SyncedAt  *time.Time
}

// Repository stores source configuration outside the vector index, so it can
// be read without querying vectors.
type Repository interface {
	Add(ctx context.Context, src Source) (string, error)
	Get(ctx context.Context, id string) (Source, error)
	List(ctx context.Context) ([]Source, error)
	Update(ctx context.Context, src Source) error
	Delete(ctx context.Context, id string) error
}
