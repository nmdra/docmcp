package chunker

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// Chunk is a retrievable unit of documentation: one structural section of a
// document, carrying the heading path that locates it.
type Chunk struct {
	ID        string
	SourceID  string
	LibraryID string
	Version   string

	DocumentID  string
	URL         string
	Title       string
	HeadingPath string

	Index   int
	Content string

	ContentHash string
}

// Locator binds a chunk to the library it belongs to.
type Locator struct {
	SourceID  string
	LibraryID string
	Version   string
}

// Document is the chunker's input. It is deliberately the same shape the parser
// produces, so the ingestion service can hand documents straight over.
type Document struct {
	ID           string
	URL          string
	CanonicalURL string
	Title        string
	Markdown     string
	ContentHash  string
}

// Source returns the URL a chunk should cite: the canonical one when present.
func (d Document) Source() string {
	if strings.TrimSpace(d.CanonicalURL) != "" {
		return d.CanonicalURL
	}
	return d.URL
}

// Chunker splits a Document into chunks along its structure.
type Chunker interface {
	Chunk(doc Document, loc Locator) ([]Chunk, error)
}

const (
	// defaultMaxTokens is a soft target, not a hard boundary: a section is
	// subdivided only at block edges, so a long code block is never split.
	defaultMaxTokens = 500

	// defaultMinTokens is the floor below which a section is left small rather
	// than merged, because a short section is still a coherent answer.
	defaultMinTokens = 20
)

// roughTokensPerWord approximates a token count without shipping a tokenizer.
// Embedding models land within roughly 20% of this, and the target is soft.
const roughTokensPerWord = 1.3

type options struct {
	maxTokens int
	minTokens int
}

// Option configures a chunker.
type Option func(*options)

// WithMaxTokens sets the soft upper bound for a chunk. Sections longer than this
// are subdivided at block boundaries.
func WithMaxTokens(n int) Option {
	return func(o *options) { o.maxTokens = n }
}

// WithMinTokens sets the floor under which no subdivision is attempted.
func WithMinTokens(n int) Option {
	return func(o *options) { o.minTokens = n }
}

// MarkdownChunker splits on the document's own heading structure, using the
// goldmark AST to find boundaries and slicing the original source. A "#" inside
// a code fence is not a heading, and the AST is what knows the difference.
//
// Slicing the original text rather than re-rendering the AST is deliberate:
// round-tripping would reflow code blocks and rewrite tables.
type MarkdownChunker struct {
	opts options
}

func NewMarkdownChunker(optFuncs ...Option) (*MarkdownChunker, error) {
	o := options{maxTokens: defaultMaxTokens, minTokens: defaultMinTokens}
	for _, apply := range optFuncs {
		apply(&o)
	}
	if o.maxTokens <= 0 {
		return nil, fmt.Errorf("chunker: max tokens must be positive, got %d", o.maxTokens)
	}
	if o.minTokens < 0 {
		return nil, fmt.Errorf("chunker: min tokens must not be negative, got %d", o.minTokens)
	}
	return &MarkdownChunker{opts: o}, nil
}

func (c *MarkdownChunker) Chunk(doc Document, loc Locator) ([]Chunk, error) {
	if strings.TrimSpace(loc.SourceID) == "" {
		return nil, fmt.Errorf("chunker: locator requires a source ID")
	}
	if strings.TrimSpace(doc.Markdown) == "" {
		return nil, nil
	}

	source := doc.Source()
	sections := splitSections(doc.Markdown)

	var chunks []Chunk

	for _, sec := range sections {
		for _, piece := range c.subdivide(sec) {
			index := len(chunks)
			chunks = append(chunks, Chunk{
				ID:          ChunkID(loc.SourceID, source, sec.headingPath, index),
				SourceID:    loc.SourceID,
				LibraryID:   loc.LibraryID,
				Version:     loc.Version,
				DocumentID:  doc.ID,
				URL:         source,
				Title:       doc.Title,
				HeadingPath: sec.headingPath,
				Index:       index,
				Content:     piece,
				ContentHash: hashNormalizedContent(piece),
			})
		}
	}

	return chunks, nil
}

type section struct {
	headingPath string
	text        string
}

// splitSections cuts the document at every heading, so each section spans one
// heading and the blocks beneath it.
func splitSections(markdown string) []section {
	source := []byte(markdown)
	doc := goldmark.New().Parser().Parse(text.NewReader(source))

	type boundary struct {
		start   int
		level   int
		heading string
	}
	var boundaries []boundary

	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		h, ok := n.(*ast.Heading)
		if !ok {
			return ast.WalkContinue, nil
		}
		boundaries = append(boundaries, boundary{
			start:   blockStart(source, h),
			level:   h.Level,
			heading: headingText(source, h),
		})
		return ast.WalkContinue, nil
	})

	sort.SliceStable(boundaries, func(i, j int) bool {
		return boundaries[i].start < boundaries[j].start
	})

	var sections []section
	stack := map[int]string{}

	// hasBody reports whether a slice holds anything beyond its own heading
	// line. A section that is only its heading has no content to index; its
	// subsections already carry the full path.
	hasBody := func(text string) bool {
		for _, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			if strings.TrimSpace(line) != "" {
				return true
			}
		}
		return false
	}

	emit := func(path []string, start, end int) {
		body := strings.TrimSpace(markdown[start:end])
		if !hasBody(body) {
			return
		}
		sections = append(sections, section{
			headingPath: strings.Join(path, " > "),
			text:        body,
		})
	}

	if len(boundaries) == 0 {
		if body := strings.TrimSpace(markdown); body != "" {
			sections = append(sections, section{text: body})
		}
		return sections
	}

	// Text before the first heading is a preamble with no heading path, but it
	// is real content and must survive.
	if preamble := strings.TrimSpace(markdown[:boundaries[0].start]); preamble != "" {
		sections = append(sections, section{text: preamble})
	}

	for i, b := range boundaries {
		end := len(markdown)
		if i+1 < len(boundaries) {
			end = boundaries[i+1].start
		}

		stack[b.level] = b.heading
		for level := range stack {
			if level > b.level {
				delete(stack, level)
			}
		}
		path := make([]string, 0, len(stack))
		for level := 1; level <= 6; level++ {
			if title, ok := stack[level]; ok {
				path = append(path, title)
			}
		}

		emit(path, b.start, end)
	}

	return sections
}

// headingText reads a heading's plain text from its own lines. goldmark's
// Heading.Text is deprecated and would also lose inline formatting markers that
// the heading path should not carry.
func headingText(source []byte, h *ast.Heading) string {
	lines := h.Lines()
	if lines == nil || lines.Len() == 0 {
		return ""
	}

	segment := lines.At(0)
	return strings.TrimSpace(string(source[segment.Start:segment.Stop]))
}

// blockStart returns the byte offset where a heading's source line begins.
// goldmark's Lines() segments start *after* the "##" markers, so the offset is
// found by scanning back to the start of that line.
func blockStart(source []byte, n ast.Node) int {
	lines := n.Lines()
	if lines == nil || lines.Len() == 0 {
		return 0
	}

	offset := lines.At(0).Start
	for offset > 0 && source[offset-1] != '\n' {
		offset--
	}
	return offset
}

// blockBoundary describes one top-level block inside a section, used when a
// section must be subdivided.
type blockBoundary struct {
	start int
	stop  int
}

// text returns the block's source slice.
func (b blockBoundary) text(markdown string) string {
	return strings.TrimSpace(markdown[b.start:b.stop])
}

// subdivide splits an oversized section at block boundaries — never inside a
// code fence, and never to satisfy the token target at the cost of a code block.
func (c *MarkdownChunker) subdivide(sec section) []string {
	if estimateTokens(sec.text) <= c.opts.maxTokens {
		return []string{sec.text}
	}

	blocks := topLevelBlocks(sec.text)
	if len(blocks) <= 1 {
		// A single indivisible block — a huge code block or table. Keeping it
		// whole beats cutting a code sample in half.
		return []string{sec.text}
	}

	var (
		out     []string
		current strings.Builder
	)

	flush := func() {
		if piece := strings.TrimSpace(current.String()); piece != "" {
			out = append(out, piece)
		}
		current.Reset()
	}

	for _, block := range blocks {
		text := block.text(sec.text)
		if current.Len() > 0 &&
			estimateTokens(current.String()+text) > c.opts.maxTokens {
			flush()
		}
		current.WriteString(text)
		current.WriteString("\n\n")
	}
	flush()

	if len(out) == 0 {
		return []string{sec.text}
	}
	return out
}

// topLevelBlocks splits a section into blank-line-separated blocks outside code
// fences. A blank line inside a fence is not a boundary.
func topLevelBlocks(markdown string) []blockBoundary {
	var (
		out   []blockBoundary
		start int
		inFce bool
		fence string
	)

	offset := 0
	for _, line := range strings.SplitAfter(markdown, "\n") {
		trimmed := strings.TrimSpace(line)

		switch {
		case inFce && strings.HasPrefix(trimmed, fence):
			inFce = false
		case !inFce && strings.HasPrefix(trimmed, "```"):
			inFce = true
			fence = trimmed
		case !inFce && trimmed == "" && offset > start:
			out = append(out, blockBoundary{start: start, stop: offset})
			start = offset + len(line)
		}

		offset += len(line)
	}

	if start < len(markdown) {
		out = append(out, blockBoundary{start: start, stop: len(markdown)})
	}

	blocks := make([]blockBoundary, 0, len(out))
	for _, b := range out {
		if text := strings.TrimSpace(markdown[b.start:b.stop]); text != "" {
			blocks = append(blocks, b)
		}
	}
	return blocks
}

func estimateTokens(text string) int {
	return int(float64(len(strings.Fields(text))) * roughTokensPerWord)
}

// blankRuns collapses runs of blank lines, which carry no meaning in Markdown.
var blankRuns = regexp.MustCompile(`\n{3,}`)

// hashNormalizedContent hashes chunk content with insignificant whitespace
// collapsed, so re-wrapping a paragraph or adding a blank line is not treated
// as a content change. Whitespace inside a code fence is left alone: there, a
// space can be part of a command.
func hashNormalizedContent(content string) string {
	lines := strings.Split(content, "\n")

	var (
		out       []string
		inFence   bool
		fenceMark = "```"
	)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		switch {
		case inFence && strings.HasPrefix(trimmed, fenceMark):
			inFence = false
		case !inFence && strings.HasPrefix(trimmed, fenceMark):
			inFence = true
			fenceMark = trimmed
		}

		if !inFence {
			line = strings.TrimRight(line, " \t")
		}
		out = append(out, line)
	}

	normalized := blankRuns.ReplaceAllString(strings.Join(out, "\n"), "\n\n")

	sum := sha256.Sum256([]byte(strings.TrimSpace(normalized)))
	return hex.EncodeToString(sum[:])
}

// ChunkID is SHA256(sourceID + canonicalURL + headingPath + chunkIndex). It
// identifies a chunk's logical position, so a content edit keeps the same ID
// and only changes the hash — that is what makes sync incremental.
func ChunkID(sourceID, canonicalURL, headingPath string, index int) string {
	sum := sha256.Sum256([]byte(
		sourceID + canonicalURL + headingPath + fmt.Sprint(index),
	))
	return hex.EncodeToString(sum[:])
}
