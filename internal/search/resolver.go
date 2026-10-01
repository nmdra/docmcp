// Package search resolves a library name to a library ID and retrieves
// documentation from the local index. Both operations are what the two MCP tools
// expose, and both are internal: the surface an agent sees never changes even
// when this does.
package search

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/docmcp/docmcp/internal/source"
)

var ErrLibraryNotFound = errors.New("library not found")

// Match is one candidate library, as resolve-library-id reports it.
type Match struct {
	LibraryID    string
	Name         string
	Version      string
	Description  string
	BaseURL      string
	IndexedPages int
}

// Resolver ranks a set of indexed libraries against a name and a query.
//
// Ranking is deterministic and lexical: no LLM, no model call. That is a
// deliberate constraint — resolution must work offline and identically on every
// run, and an agent re-asking the same question must get the same library.
type Resolver struct {
	libraries []source.Source

	// indexed maps library ID to its chunk count. A library with no chunks is
	// not offered: sending an agent to an empty library wastes its turn.
	indexed map[string]int
}

func NewResolver(libraries []source.Source) *Resolver {
	return &Resolver{
		libraries: append([]source.Source(nil), libraries...),
		indexed:   map[string]int{},
	}
}

// SetIndexed records which libraries actually have content. With no call, every
// library is treated as indexed.
func (r *Resolver) SetIndexed(counts map[string]int) {
	r.indexed = counts
}

// Resolve ranks libraries for a name and query. An exact library ID resolves to
// itself and nothing else.
func (r *Resolver) Resolve(_ context.Context, libraryName, query string) ([]Match, error) {
	name := strings.TrimSpace(libraryName)
	if name == "" {
		return nil, fmt.Errorf("resolve library: a library name is required")
	}

	if strings.HasPrefix(name, "/") {
		return r.resolveExact(name)
	}

	target := normalizeName(name)
	words := tokenize(query)
	mentionedVersion := findVersionMention(query)

	type scored struct {
		match Match
		score int
	}

	var candidates []scored

	for _, lib := range r.libraries {
		if !r.isIndexed(lib.LibraryID) {
			continue
		}

		score, ok := scoreLibrary(lib, target, words, mentionedVersion)
		if !ok {
			continue
		}
		candidates = append(candidates, scored{match: r.toMatch(lib), score: score})
	}

	// Sort by score, then by library ID so equal scores never reorder between runs.
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].match.LibraryID < candidates[j].match.LibraryID
	})

	out := make([]Match, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, c.match)
	}
	return out, nil
}

func (r *Resolver) resolveExact(libraryID string) ([]Match, error) {
	for _, lib := range r.libraries {
		if lib.LibraryID != libraryID {
			continue
		}
		if !r.isIndexed(libraryID) {
			return nil, fmt.Errorf("%w: %s has no indexed documentation", ErrLibraryNotFound, libraryID)
		}
		return []Match{r.toMatch(lib)}, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrLibraryNotFound, libraryID)
}

func (r *Resolver) isIndexed(libraryID string) bool {
	if len(r.indexed) == 0 {
		return true
	}
	return r.indexed[libraryID] > 0
}

func (r *Resolver) toMatch(lib source.Source) Match {
	return Match{
		LibraryID:    lib.LibraryID,
		Name:         lib.Name,
		Version:      lib.Version,
		Description:  lib.Description,
		BaseURL:      lib.BaseURL,
		IndexedPages: r.indexed[lib.LibraryID],
	}
}

// scoreLibrary ranks one library. Higher is better; ok is false when the library
// is not a candidate at all.
//
// Name agreement dominates query overlap: asking for "Pi" must not be won by
// "Pi SDK" just because the query happened to mention "sdk".
func scoreLibrary(lib source.Source, target string, words []string, mentionedVersion string) (int, bool) {
	nameSlug := normalizeName(lib.Name)

	switch {
	case nameSlug == target:
		score := 1000
		// The unversioned library is the safer default when a caller named no
		// version; a specific version only wins if the query asked for it.
		if lib.Version == "" {
			score += 20
		}
		if mentionedVersion != "" && lib.Version == mentionedVersion {
			score += 200
		}
		return score + overlap(lib, words), true

	case strings.HasPrefix(nameSlug, target) || strings.HasPrefix(target, nameSlug):
		return 600 + overlap(lib, words), true

	case strings.Contains(nameSlug, target) || strings.Contains(target, nameSlug):
		return 400 + overlap(lib, words), true
	}

	// No name agreement: fall back to description overlap so a library can still
	// be found by what it documents rather than what it is called.
	if score := descriptionOverlap(lib, words); score > 0 {
		return 100 + score, true
	}

	return 0, false
}

// overlap counts query words shared with the library name. Name words count for
// more than description words.
func overlap(lib source.Source, words []string) int {
	if len(words) == 0 {
		return 0
	}

	nameWords := map[string]bool{}
	for _, w := range tokenize(lib.Name) {
		nameWords[w] = true
	}

	score := 0
	for _, w := range words {
		switch {
		case nameWords[w]:
			score += 10
		case containsWord(lib.Description, w):
			score += 3
		}
	}
	return score
}

func descriptionOverlap(lib source.Source, words []string) int {
	score := 0
	for _, w := range words {
		if containsWord(lib.Description, w) {
			score++
		}
	}
	return score
}

func containsWord(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	for _, w := range tokenize(haystack) {
		if w == needle {
			return true
		}
	}
	return false
}

var (
	splitWords   = regexp.MustCompile(`[^a-z0-9]+`)
	versionMatch = regexp.MustCompile(`\bv?(\d+\.\d+(?:\.\d+)?)\b`)
)

var camelBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)

// normalizeName folds the ways one name gets written — Pico CSS, pico-css,
// pico_css, PicoCSS — onto a single comparable form. Resolution must not depend
// on which punctuation a user happened to type.
func normalizeName(s string) string {
	spaced := camelBoundary.ReplaceAllString(strings.TrimSpace(s), "$1 $2")

	parts := splitWords.Split(strings.ToLower(spaced), -1)

	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}

// tokenize lowercases and splits on anything that is not alphanumeric.
func tokenize(s string) []string {
	parts := splitWords.Split(strings.ToLower(s), -1)

	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// findVersionMention pulls a version like "0.99.1" or "v2" out of a query, so a
// caller who names a version gets that version ranked first.
func findVersionMention(query string) string {
	m := versionMatch.FindStringSubmatch(query)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}
