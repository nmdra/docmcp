package source

import (
	"errors"
	"fmt"
	"strings"
)

// NamespaceLocal is the only namespace v0.1 emits. The segment is reserved so
// /team, /remote, and /shared can appear later without changing existing IDs.
const NamespaceLocal = "local"

var (
	ErrInvalidLibraryID = errors.New("invalid library ID")
)

// LibraryID is the parsed form of a Context7-like library path:
//
//	/<namespace>/<library>[/<version>]
type LibraryID struct {
	Namespace string
	Name      string
	Version   string
}

// NewLibraryID builds a library ID from a human name and optional version.
// The name is normalized to a lowercase, dash-separated slug; the version is
// kept verbatim.
func NewLibraryID(name, version string) (string, error) {
	slug := Slug(name)
	if slug == "" {
		return "", fmt.Errorf("library name: %w", ErrInvalidLibraryID)
	}

	if version == "" {
		return "/" + NamespaceLocal + "/" + slug, nil
	}
	return "/" + NamespaceLocal + "/" + slug + "/" + version, nil
}

// ParseLibraryID splits a library ID into its segments.
func ParseLibraryID(id string) (LibraryID, error) {
	if id == "" || !strings.HasPrefix(id, "/") {
		return LibraryID{}, fmt.Errorf("%w: %q must start with /", ErrInvalidLibraryID, id)
	}

	parts := strings.Split(strings.TrimPrefix(id, "/"), "/")
	for _, p := range parts {
		if p == "" {
			return LibraryID{}, fmt.Errorf("%w: %q has an empty segment", ErrInvalidLibraryID, id)
		}
	}

	switch len(parts) {
	case 2:
	case 3:
		return LibraryID{
			Namespace: parts[0],
			Name:      parts[1],
			Version:   parts[2],
		}, nil
	default:
		return LibraryID{}, fmt.Errorf("%w: %q must have 2 or 3 segments", ErrInvalidLibraryID, id)
	}

	if parts[0] != NamespaceLocal {
		return LibraryID{}, fmt.Errorf("%w: unknown namespace %q", ErrInvalidLibraryID, parts[0])
	}

	return LibraryID{Namespace: parts[0], Name: parts[1]}, nil
}

// String renders the LibraryID back to its path form.
func (l LibraryID) String() (string, error) {
	id, err := NewLibraryID(l.Name, l.Version)
	if err != nil {
		return "", err
	}
	if l.Namespace == "" {
		return id, nil
	}
	if l.Namespace != NamespaceLocal {
		return "", fmt.Errorf("%w: unknown namespace %q", ErrInvalidLibraryID, l.Namespace)
	}
	return id, nil
}

// Slug normalizes a human name into a library path segment: lowercase,
// non-alphanumeric runs collapsed to a single dash, trimmed of edge dashes.
func Slug(name string) string {
	var b strings.Builder
	lastDash := true

	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}

	return strings.Trim(b.String(), "-")
}
