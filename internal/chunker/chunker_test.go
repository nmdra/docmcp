package chunker_test

import (
	"strings"
	"testing"

	"github.com/nmdra/docmcp/internal/chunker"
)

func chunkDoc(t *testing.T, markdown string, opts ...chunker.Option) []chunker.Chunk {
	t.Helper()

	c, err := chunker.NewMarkdownChunker(opts...)
	if err != nil {
		t.Fatalf("NewMarkdownChunker: %v", err)
	}

	chunks, err := c.Chunk(chunker.Document{
		ID:           "doc-1",
		URL:          "https://docs.acme.test/latest/api",
		CanonicalURL: "https://docs.acme.test/latest/api",
		Title:        "API",
		Markdown:     markdown,
		ContentHash:  "hash-1",
	}, chunker.Locator{SourceID: "acme", LibraryID: "/local/acme/1"})
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	return chunks
}

func headingPaths(chunks []chunker.Chunk) []string {
	out := make([]string, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, c.HeadingPath)
	}
	return out
}

func TestChunker_SplitsOnH2(t *testing.T) {
	chunks := chunkDoc(t, "# API\n\n## Authentication\n\nUse OAuth.\n\n## Requests\n\nSend JSON.\n")

	want := []string{"API > Authentication", "API > Requests"}
	got := headingPaths(chunks)

	if len(got) != len(want) {
		t.Fatalf("got %d chunks %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chunk[%d].HeadingPath = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestChunker_PreservesHeadingPath(t *testing.T) {
	chunks := chunkDoc(t, "# Guide\n\n## Setup\n\nStep one.\n\n### Linux\n\nUse apt.\n")

	want := "Guide > Setup > Linux"
	found := false
	for _, c := range chunks {
		if c.HeadingPath == want {
			found = true
		}
	}
	if !found {
		t.Errorf("no chunk with heading path %q; got %v", want, headingPaths(chunks))
	}
}

func TestChunker_ChunkCarriesDocumentMetadata(t *testing.T) {
	chunks := chunkDoc(t, "# API\n\n## Auth\n\nUse OAuth.\n")
	if len(chunks) == 0 {
		t.Fatal("no chunks produced")
	}

	c := chunks[0]
	if c.DocumentID != "doc-1" {
		t.Errorf("DocumentID = %q, want doc-1", c.DocumentID)
	}
	if c.SourceID != "acme" || c.LibraryID != "/local/acme/1" {
		t.Errorf("locator not applied: SourceID=%q LibraryID=%q", c.SourceID, c.LibraryID)
	}
	if c.URL != "https://docs.acme.test/latest/api" {
		t.Errorf("URL = %q", c.URL)
	}
	if c.Title != "API" {
		t.Errorf("Title = %q, want API", c.Title)
	}
}

func TestChunker_ChunkContentIncludesItsHeading(t *testing.T) {
	chunks := chunkDoc(t, "# API\n\n## Authentication\n\nUse OAuth.\n")
	if len(chunks) == 0 {
		t.Fatal("no chunks produced")
	}

	if !strings.Contains(chunks[0].Content, "Authentication") {
		t.Errorf("chunk content omits its own heading:\n%s", chunks[0].Content)
	}
	if !strings.Contains(chunks[0].Content, "Use OAuth.") {
		t.Errorf("chunk content missing body text:\n%s", chunks[0].Content)
	}
}

func TestChunker_KeepsCodeBlockTogether(t *testing.T) {
	markdown := "# Install\n\n## Manual\n\nRun this:\n\n```bash\ncurl -X POST https://api.acme.test/token \\\n  -d grant_type=client_credentials\n```\n\nDone.\n"

	chunks := chunkDoc(t, markdown)

	for _, c := range chunks {
		if strings.Count(c.Content, "```")%2 != 0 {
			t.Errorf("chunk %q has an unclosed code fence:\n%s", c.HeadingPath, c.Content)
		}
	}

	found := false
	for _, c := range chunks {
		if strings.Contains(c.Content, "grant_type=client_credentials") {
			found = true
			if !strings.Contains(c.Content, "curl -X POST") {
				t.Errorf("code block was split:\n%s", c.Content)
			}
		}
	}
	if !found {
		t.Error("code block content is missing from every chunk")
	}
}

func TestChunker_SubdividesLargeSection(t *testing.T) {
	chunks := chunkDoc(t, "# Guide\n\n## Long\n\n"+strings.Repeat(
		"Paragraph number with enough words to matter for the size target.\n\n", 200),
		chunker.WithMaxTokens(120))

	if len(chunks) < 2 {
		t.Fatalf("a %d-token section produced %d chunk(s), want it subdivided",
			200*12, len(chunks))
	}

	indices := make([]int, 0, len(chunks))
	for _, c := range chunks {
		indices = append(indices, c.Index)
	}
	for i, idx := range indices {
		if idx != i {
			t.Errorf("chunk index %d at position %d, want sequential", idx, i)
		}
	}
}

func TestChunker_SplitsOnlyOnBoundaries(t *testing.T) {
	chunks := chunkDoc(t, "# Guide\n\n## A\n\nAlpha.\n\n## B\n\nBravo.\n",
		chunker.WithMaxTokens(20))

	for _, c := range chunks {
		if strings.Contains(c.Content, "Alpha.\n\n## B") {
			t.Errorf("chunk swallowed the next section boundary:\n%s", c.Content)
		}
	}
}

func TestChunker_StableChunkIDs(t *testing.T) {
	markdown := "# API\n\n## Auth\n\nUse OAuth.\n\n## Requests\n\nSend JSON.\n"

	first := chunkDoc(t, markdown)
	second := chunkDoc(t, markdown)

	if len(first) != len(second) {
		t.Fatalf("chunk counts differ across runs: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].ID == "" {
			t.Fatalf("chunk[%d].ID is empty", i)
		}
		if first[i].ID != second[i].ID {
			t.Errorf("chunk[%d].ID differs across runs: %q vs %q", i, first[i].ID, second[i].ID)
		}
	}
}

func TestChunker_ChunkIDsAreUnique(t *testing.T) {
	markdown := "# API\n\n## Auth\n\nUse OAuth.\n\n## Requests\n\nSend JSON.\n\n## Limits\n\nTen per minute.\n"

	seen := map[string]bool{}
	for _, c := range chunkDoc(t, markdown) {
		if seen[c.ID] {
			t.Errorf("duplicate chunk ID %q", c.ID)
		}
		seen[c.ID] = true
	}
}

func TestChunker_ContentHashes(t *testing.T) {
	chunks := chunkDoc(t, "# API\n\n## Auth\n\nUse OAuth.\n")
	if len(chunks) == 0 {
		t.Fatal("no chunks produced")
	}

	if chunks[0].ContentHash == "" {
		t.Fatal("ContentHash is empty")
	}

	// Reformatting alone must not count as a content change.
	reformatted := chunkDoc(t, "# API\n\n\n\n## Auth\n\n\nUse OAuth.   \n\n")
	if len(reformatted) != 1 {
		t.Fatalf("reformatted markdown produced %d chunks, want 1", len(reformatted))
	}
	if reformatted[0].ContentHash != chunks[0].ContentHash {
		t.Error("ContentHash changed for whitespace-only reformatting")
	}

	edited := chunkDoc(t, "# API\n\n## Auth\n\nUse OAuth2 instead.\n")
	if edited[0].ContentHash == chunks[0].ContentHash {
		t.Error("ContentHash unchanged after a real content edit")
	}
}

func TestChunker_EmptyDocument(t *testing.T) {
	chunks := chunkDoc(t, "")

	if len(chunks) != 0 {
		t.Errorf("empty markdown produced %d chunks, want 0", len(chunks))
	}
}

func TestChunker_WhitespaceOnlyDocument(t *testing.T) {
	chunks := chunkDoc(t, "\n\n   \n\n")

	if len(chunks) != 0 {
		t.Errorf("whitespace-only markdown produced %d chunks, want 0", len(chunks))
	}
}

func TestChunker_PreambleBeforeFirstHeading(t *testing.T) {
	chunks := chunkDoc(t, "Intro text with no heading at all.\n\n# Later\n\nSection body.\n")

	if len(chunks) == 0 {
		t.Fatal("no chunks produced")
	}
	if chunks[0].HeadingPath != "" {
		t.Errorf("preamble chunk HeadingPath = %q, want empty", chunks[0].HeadingPath)
	}
	if !strings.Contains(chunks[0].Content, "Intro text") {
		t.Errorf("preamble text was dropped:\n%s", chunks[0].Content)
	}
}

func TestChunker_SkipsEmptySections(t *testing.T) {
	chunks := chunkDoc(t, "# API\n\n## Empty\n\n## Full\n\nReal content here.\n")

	for _, c := range chunks {
		if c.HeadingPath == "API > Empty" {
			t.Errorf("produced a chunk for a section with no body: %q", c.Content)
		}
	}
}

func TestChunker_RejectsEmptySourceID(t *testing.T) {
	c, err := chunker.NewMarkdownChunker()
	if err != nil {
		t.Fatalf("NewMarkdownChunker: %v", err)
	}

	_, err = c.Chunk(chunker.Document{ID: "d", Markdown: "# A\n\ntext"}, chunker.Locator{})
	if err == nil {
		t.Error("Chunk with no source ID succeeded, want error")
	}
}

func TestChunker_ListAndTableContentSurvives(t *testing.T) {
	markdown := "# Reference\n\n## Options\n\n- `--fast` skips checks\n- `--slow` runs everything\n\n" +
		"| Flag | Effect |\n| --- | --- |\n| `--fast` | skip |\n"

	chunks := chunkDoc(t, markdown)

	var joined strings.Builder
	for _, c := range chunks {
		joined.WriteString(c.Content)
		joined.WriteString("\n")
	}

	for _, want := range []string{"--fast", "--slow", "| Flag | Effect |"} {
		if !strings.Contains(joined.String(), want) {
			t.Errorf("lost %q from chunk content:\n%s", want, joined.String())
		}
	}
}
