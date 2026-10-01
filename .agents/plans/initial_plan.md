# DocMCP — Test-Driven Development Build Plan

## 1. Project Goal

Build a small, local-first documentation indexing and retrieval tool written in Go.

DocMCP should allow a user to:

```bash
docmcp add https://pi.dev/docs/latest/ \
  --name pi \
  --version 0.99.2

docmcp sync pi

docmcp search "how does codemode tool discovery work"

docmcp serve
```

The tool will:

```text
documentation website
        ↓
crawl
        ↓
HTML → clean Markdown
        ↓
structural chunks
        ↓
embeddings
        ↓
local Chroma database
        ↓
Context7-style MCP interface
```

The public MCP interface should intentionally resemble Context7 so that existing coding models already understand the expected workflow:

```text
resolve-library-id
        ↓
query-docs
```

The main design principle is:

> Keep the MCP interface extremely small and stable while allowing the crawler, embedding engine, storage engine, and retrieval strategy to evolve internally.

---

# 2. v0.1 Scope

## Included

- Go CLI
- Local persistent Chroma storage
- Website ingestion
- `robots.txt`
- sitemap discovery
- sitemap index support
- same-site link crawling fallback
- URL normalization
- include/exclude rules
- documentation versions
- HTML → Markdown
- structural Markdown chunking
- content hashing
- incremental sync
- default local embedding model
- Ollama embeddings
- OpenAI-compatible embeddings
- semantic retrieval
- source/version metadata filtering
- Context7-compatible MCP tool shape
- stdio MCP transport
- local CLI search
- deterministic source IDs
- basic crawl limits/rate limiting
- full unit/integration/E2E test suite

## Explicitly excluded from v0.1

Do not implement these yet:

```text
Git repositories
git clone/update
PDF parsing
JavaScript browser rendering
Lightpanda integration
Playwright
authentication-protected docs
web UI
multi-user server
remote hosted service
team synchronization
reranking LLM
knowledge graph
backlinks
code repository indexing
MCP write tools
```

These exclusions are intentional.

The objective of v0.1 is:

> Point DocMCP at a normal technical documentation site and make that documentation reliably searchable by an AI agent.

---

# 3. External MCP Compatibility Target

DocMCP should deliberately model its read interface after Context7.

Context7 currently exposes only:

```text
resolve-library-id
query-docs
```

The expected tool workflow is:

```text
User asks:
"How does Pi codemode exposure work?"

        ↓

resolve-library-id
{
  "libraryName": "Pi",
  "query": "How does codemode exposure work?"
}

        ↓

/local/pi/0.99.2

        ↓

query-docs
{
  "libraryId": "/local/pi/0.99.2",
  "query": "How does codemode exposure work?"
}

        ↓

relevant documentation
```

Context7 specifically encourages natural-language, single-concept queries rather than keyword-only or overly broad queries.

---

# 4. Public MCP Contract

## 4.1 `resolve-library-id`

Input:

```json
{
  "libraryName": "Pi",
  "query": "How does codemode MCP exposure work?"
}
```

Canonical Go input:

```go
type ResolveLibraryIDInput struct {
    LibraryName string `json:"libraryName"`
    Query       string `json:"query"`
}
```

Both fields are required.

Tool description should closely follow Context7 semantics:

```text
Resolves a package, product, framework, CLI, API, or documentation
name into a DocMCP-compatible library ID.

Call this before query-docs unless the user already supplied an
exact library ID.

Use the user's question in query to rank matching libraries and
versions.
```

Output should be model-readable text.

Example:

```text
Available Libraries:

- Library ID: /local/pi
  Name: Pi
  Description: Pi coding agent documentation
  Indexed Pages: 61
  Versions:
    - /local/pi/0.99.2
    - /local/pi/0.99.1

- Library ID: /local/pi-sdk
  Name: Pi SDK
  Description: Pi SDK reference
  Indexed Pages: 24
```

Do not expose internal Chroma IDs.

---

# 5. `query-docs`

Input:

```json
{
  "libraryId": "/local/pi/0.99.2",
  "query": "How does lazy MCP tool discovery work?"
}
```

Canonical Go type:

```go
type QueryDocsInput struct {
    LibraryID string `json:"libraryId"`
    Query     string `json:"query"`
}
```

Tool description:

```text
Retrieves relevant documentation and code examples for an indexed
library.

Use resolve-library-id first unless an exact DocMCP-compatible
library ID was supplied by the user.

The query should describe one documentation concept clearly and
specifically.
```

Example result:

```text
### MCP > Control tool exposure

Source: https://pi.dev/docs/latest/mcp
Library: /local/pi/0.99.2

MCP servers using codemode exposure ...

---

### Codemode > Tool discovery

Source: https://pi.dev/docs/latest/cli
Library: /local/pi/0.99.2

searchTools() discovers ...
```

Do not expose:

```text
embedding distance
vector IDs
HNSW internals
topK
similarity threshold
raw metadata blobs
```

The result should already be useful context for a model.

---

# 6. MCP Tool Annotations

Both tools should be:

```text
readOnlyHint: true
destructiveHint: false
idempotentHint: true
openWorldHint: false
```

`openWorldHint` should be false because MCP querying only accesses the local indexed corpus.

Crawling happens through the CLI, not while answering MCP queries.

MCP currently supports tool descriptions, JSON Schema inputs, optional output schemas, and behavioral annotations.

---

# 7. Server Instructions

Give the MCP server a strong server-level instruction.

Suggested wording:

```text
Use this server for documentation questions about locally indexed
libraries, frameworks, APIs, SDKs, CLI tools, products, and services.

Use it for:

- API syntax
- configuration
- setup
- version-specific behavior
- migration information
- library-specific debugging
- CLI usage
- framework behavior

Prefer this indexed documentation over general web search when the
required library is available.

Do not use this server for:

- general programming concepts
- arbitrary web research
- application business logic
- code review
- unrelated debugging
- refactoring requests
```

This intentionally mirrors the usage pattern Context7 teaches coding agents.

---

# 8. Library ID Format

Use a Context7-like path structure.

Base library:

```text
/local/pi
```

Versioned library:

```text
/local/pi/0.99.2
```

Examples:

```text
/local/lightpanda
/local/lightpanda/0.4.1

/local/nextjs
/local/nextjs/16

/local/company-api
/local/company-api/v2
```

Grammar:

```text
/<namespace>/<library>[/<version>]
```

For v0.1:

```text
namespace = local
```

Keep the namespace because it gives future compatibility with:

```text
/team/...
/remote/...
/shared/...
```

without changing IDs later.

---

# 9. CLI Contract

Initial commands:

```text
docmcp add
docmcp list
docmcp info
docmcp sync
docmcp remove
docmcp search
docmcp serve
```

Examples:

```bash
docmcp add https://pi.dev/docs/latest/ \
  --name pi \
  --version 0.99.2
```

```bash
docmcp add https://example.com/docs \
  --name example \
  --version 2 \
  --include "/docs/**" \
  --exclude "/docs/archive/**"
```

```bash
docmcp sync pi
```

```bash
docmcp search \
  --library /local/pi/0.99.2 \
  "how does deferred MCP tool loading work"
```

```bash
docmcp remove pi
```

```bash
docmcp serve
```

---

# 10. No MCP Write Tools in v0.1

Do not expose:

```text
add-library
remove-library
sync-library
crawl
reindex
```

through MCP.

Use only CLI commands for mutation.

Therefore:

```text
MCP
├── resolve-library-id
└── query-docs
```

while:

```text
CLI
├── add
├── list
├── info
├── sync
├── remove
├── search
└── serve
```

This makes the MCP server:

- safe
- deterministic
- read-only
- easy for models to understand

---

# 11. Repository Layout

Recommended structure:

```text
docmcp/
├── cmd/
│   └── docmcp/
│       └── main.go
│
├── internal/
│   ├── app/
│   │   └── app.go
│   │
│   ├── source/
│   │   ├── source.go
│   │   ├── repository.go
│   │   └── id.go
│   │
│   ├── crawler/
│   │   ├── crawler.go
│   │   ├── sitemap.go
│   │   ├── robots.go
│   │   ├── filter.go
│   │   ├── normalize.go
│   │   └── fetch.go
│   │
│   ├── parser/
│   │   ├── html.go
│   │   └── markdown.go
│   │
│   ├── chunker/
│   │   ├── chunker.go
│   │   └── markdown.go
│   │
│   ├── embedding/
│   │   ├── embedder.go
│   │   ├── minilm.go
│   │   ├── ollama.go
│   │   └── openai.go
│   │
│   ├── store/
│   │   ├── store.go
│   │   └── chroma.go
│   │
│   ├── search/
│   │   ├── engine.go
│   │   └── resolver.go
│   │
│   ├── mcpserver/
│   │   ├── server.go
│   │   ├── resolve_library.go
│   │   └── query_docs.go
│   │
│   └── config/
│       └── config.go
│
├── testdata/
│   ├── sites/
│   ├── markdown/
│   ├── sitemap/
│   └── responses/
│
├── go.mod
├── go.sum
├── Makefile
├── README.md
└── LICENSE
```

---

# 12. Core Domain Types

## Source

```go
type Source struct {
    ID          string
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
```

## Document

```go
type Document struct {
    ID           string
    SourceID     string
    URL          string
    CanonicalURL string
    Title        string
    Markdown     string
    ContentHash  string
    LastModified string
    ETag         string
}
```

## Chunk

```go
type Chunk struct {
    ID          string
    SourceID    string
    LibraryID   string
    Version     string

    DocumentID  string
    URL         string
    Title       string
    HeadingPath string

    Index       int
    Content     string
    ContentHash string
}
```

## Search result

```go
type SearchResult struct {
    Chunk    Chunk
    Score    float64
}
```

Do not expose `Score` over MCP in v0.1.

---

# 13. Interfaces First

TDD becomes easier when external systems sit behind small interfaces.

## Fetcher

```go
type Fetcher interface {
    Fetch(ctx context.Context, uri string) (Page, error)
}
```

## Crawler

```go
type Crawler interface {
    Crawl(ctx context.Context, source Source) ([]Page, error)
}
```

## Parser

```go
type Parser interface {
    Parse(page Page) (Document, error)
}
```

## Chunker

```go
type Chunker interface {
    Chunk(doc Document) ([]Chunk, error)
}
```

## Embedder

```go
type Embedder interface {
    Embed(
        ctx context.Context,
        texts []string,
    ) ([][]float32, error)

    Name() string
    Dimensions() int
}
```

## Store

```go
type Store interface {
    AddSource(ctx context.Context, source Source) error
    GetSource(ctx context.Context, id string) (Source, error)
    ListSources(ctx context.Context) ([]Source, error)
    DeleteSource(ctx context.Context, id string) error

    UpsertChunks(ctx context.Context, chunks []Chunk) error
    DeleteChunks(ctx context.Context, ids []string) error

    Search(
        ctx context.Context,
        libraryID string,
        query string,
    ) ([]SearchResult, error)
}
```

These interfaces allow most tests to run without Chroma or the network.

---

# 14. Storage Strategy

Use Chroma as the primary document/vector store.

One main collection:

```text
docmcp_chunks
```

Each entry:

```text
ID
document text
embedding
metadata
```

Metadata:

```json
{
  "source_id": "pi",
  "library_id": "/local/pi/0.99.2",
  "version": "0.99.2",
  "url": "https://pi.dev/docs/latest/mcp",
  "title": "MCP",
  "heading_path": "MCP > Control tool exposure",
  "chunk_index": 4,
  "content_hash": "..."
}
```

Chroma supports text queries, metadata filtering, and document-content filtering. Its Go client exposes collection query operations and metadata filters.

Do not access Chroma's internal SQLite schema.

Only use supported APIs.

---

# 15. Embedding Providers

Support three providers.

## Provider 1 — default local

Default:

```text
all-MiniLM-L6-v2
```

This should require no API key.

Chroma's default embedding model is currently `all-MiniLM-L6-v2`, running locally.

Configuration:

```toml
[embedding]
provider = "default"
```

## Provider 2 — Ollama

```toml
[embedding]
provider = "ollama"
model = "nomic-embed-text"
base_url = "http://localhost:11434"
```

## Provider 3 — OpenAI-compatible

```toml
[embedding]
provider = "openai"
model = "text-embedding-3-small"
base_url = "https://api.openai.com/v1"
```

API key should come from an environment variable:

```text
DOCMCP_OPENAI_API_KEY
```

Do not put credentials into source metadata.

---

# 16. Embedding Compatibility Rule

Never mix vectors from different embedding models in the same active index.

Persist:

```text
provider
model
dimensions
```

with the collection/index configuration.

If configuration changes from:

```text
all-MiniLM-L6-v2
```

to:

```text
nomic-embed-text
```

the application must fail clearly:

```text
Embedding configuration changed.

Existing index:
  default/all-MiniLM-L6-v2
  dimensions: 384

Configured:
  ollama/nomic-embed-text

Run:
  docmcp reindex
```

`reindex` can be implemented after the core ingestion path but before v0.1 release.

---

# 17. Crawler Design

Crawler strategy:

```text
start URL
   ↓
robots.txt
   ↓
sitemap declaration?
   ├── yes → sitemap crawler
   └── no
         ↓
   /sitemap.xml
         ↓
   /sitemap_index.xml
         ↓
   still none?
         ↓
   recursive same-site crawl
```

Priority:

```text
1. robots sitemap
2. sitemap.xml
3. sitemap index
4. recursive links
```

---

# 18. Crawl Scope

Given:

```bash
docmcp add https://pi.dev/docs/latest/
```

default constraints:

```text
host:
  pi.dev

path prefix:
  /docs/latest/
```

Allowed:

```text
https://pi.dev/docs/latest/mcp
https://pi.dev/docs/latest/settings
```

Rejected:

```text
https://pi.dev/blog/
https://github.com/...
https://pi.dev/
```

unless explicitly allowed later.

---

# 19. URL Normalization

Normalize before deduplication.

These:

```text
/docs/mcp
/docs/mcp#exposure
/docs/mcp?utm_source=twitter
```

should usually resolve to:

```text
/docs/mcp
```

Rules:

- remove fragments
- normalize scheme/host casing
- resolve relative URLs
- remove tracking parameters
- normalize trailing slashes consistently
- reject unsupported schemes
- preserve meaningful query parameters only when configured

Unsupported:

```text
mailto:
tel:
javascript:
data:
```

---

# 20. Include / Exclude Rules

Use glob-like path rules.

Example:

```bash
docmcp add https://example.com/docs \
  --include "/docs/**" \
  --include "/guides/**" \
  --exclude "/docs/archive/**" \
  --exclude "/docs/internal/**"
```

Order:

```text
host validation
      ↓
base path validation
      ↓
include rules
      ↓
exclude rules
      ↓
accepted
```

Exclude always wins.

Suggested library:

```text
github.com/bmatcuk/doublestar/v4
```

for `**` matching.

---

# 21. Sitemap Data Types

Use Go's standard `encoding/xml`.

```go
type SitemapURLSet struct {
    URLs []SitemapURL `xml:"url"`
}

type SitemapURL struct {
    Loc     string `xml:"loc"`
    LastMod string `xml:"lastmod"`
}
```

Index:

```go
type SitemapIndex struct {
    Sitemaps []SitemapEntry `xml:"sitemap"`
}

type SitemapEntry struct {
    Loc     string `xml:"loc"`
    LastMod string `xml:"lastmod"`
}
```

Avoid another dependency unless sitemap complexity requires it.

---

# 22. HTML Processing

Pipeline:

```text
HTML
 ↓
remove irrelevant DOM
 ↓
main/article extraction
 ↓
HTML → Markdown
 ↓
Markdown AST
 ↓
chunks
```

Remove or heavily downweight:

```text
nav
footer
aside
script
style
cookie banners
menu elements
breadcrumbs when repetitive
```

Preserve:

```text
headings
paragraphs
lists
code
pre
tables where practical
links
```

---

# 23. Markdown Chunking

Do not start with arbitrary character splitting.

Use heading-aware structural chunking.

Example:

```markdown
# Authentication

## OAuth

OAuth explanation...

```go
client.Login(...)
```

## API Keys

...
```

Possible chunks:

```text
Authentication > OAuth
Authentication > API Keys
```

Each chunk should know its heading path.

Preferred chunk size:

```text
~300–800 tokens
```

This is a target, not a hard boundary.

Never split a small code block just to satisfy the token target.

Large sections may be sub-chunked by paragraphs.

---

# 24. Chunk ID Strategy

Chunk IDs must be deterministic.

Example formula:

```text
SHA256(
    sourceID +
    canonicalURL +
    headingPath +
    chunkIndex
)
```

Content hash:

```text
SHA256(normalizedContent)
```

ID identifies logical position.

Hash identifies content.

This allows:

```text
same ID + same hash
    → skip

same ID + different hash
    → re-embed + upsert
```

---

# 25. Incremental Sync

`docmcp sync` should avoid unnecessary embeddings.

Flow:

```text
crawl
 ↓
normalize document
 ↓
chunk
 ↓
calculate hashes
 ↓
compare existing index

 ├─ unchanged → skip
 ├─ changed   → embed + upsert
 ├─ new       → embed + insert
 └─ missing   → delete
```

Expected output:

```text
Source: pi

Pages:
  discovered: 63
  fetched:    9
  unchanged:  54

Chunks:
  unchanged: 412
  updated:     11
  added:        7
  removed:      3
```

---

# 26. Search Strategy — v0.1

Public interface:

```text
query-docs
```

Internal implementation begins with:

```text
query
  ↓
embedding
  ↓
Chroma similarity search
  ↓
library/version metadata filter
  ↓
deduplicate
  ↓
context formatting
```

Do not expose separate:

```text
vector_search
keyword_search
semantic_search
```

to MCP.

---

# 27. Full-Text Search Strategy

Keep the public contract unchanged.

Chroma supports document-content `$contains` filtering, while its richer dense+sparse hybrid Search API is documented separately from the basic local Query API.

Therefore use this rollout:

## v0.1

Semantic retrieval plus exact lexical filtering/boosting where useful.

## v0.2

Introduce ranked lexical/BM25 retrieval if tests show it improves technical identifier queries.

Potential internal pipeline:

```text
semantic candidates
        +
lexical candidates
        ↓
RRF
        ↓
final chunks
```

Do not alter:

```text
query-docs {
    libraryId,
    query
}
```

when retrieval evolves.

---

# 28. Dependency Set

Initial dependencies:

```text
github.com/spf13/cobra
github.com/gocolly/colly/v2
github.com/temoto/robotstxt
github.com/JohannesKaufmann/html-to-markdown/v2
github.com/yuin/goldmark
github.com/bmatcuk/doublestar/v4
github.com/amikos-tech/chroma-go/pkg/api/v2
github.com/modelcontextprotocol/go-sdk/mcp
golang.org/x/time/rate
golang.org/x/net/html/charset
```

Use standard library for:

```text
net/http
net/url
encoding/xml
encoding/json
crypto/sha256
context
sync
path/filepath
testing
httptest
```

The official MCP Go SDK supports binding typed Go handlers to tools and can infer JSON schemas from Go input/output types.

---

# 29. TDD Philosophy

Use outside-in TDD.

Each feature follows:

```text
1. Write failing acceptance/contract test
2. Write smallest unit test needed
3. Implement minimum code
4. Make test green
5. Refactor
6. Run entire suite
7. Commit
```

Never implement an entire subsystem before testing it.

Use:

```text
RED
 ↓
GREEN
 ↓
REFACTOR
```

for every behavior.

---

# 30. Test Layers

Use five layers.

```text
                 E2E
               /     \
          MCP contract
             /        \
       integrations
          /          \
        units
```

More specifically:

## Unit tests

Fast, no disk/network when possible.

Test:

```text
URL normalization
filters
ID generation
chunking
version parsing
resolver ranking
content hashing
configuration
```

## Integration tests

Use real:

```text
httptest.Server
filesystem temp dirs
Chroma persistent client
```

## Contract tests

Validate exact MCP:

```text
tool names
parameter names
required fields
annotations
result shape
```

## E2E tests

Run actual CLI or MCP server against fixture documentation site.

## Golden tests

Useful for:

```text
HTML → Markdown
Markdown chunking
MCP result formatting
```

---

# 31. Testing Rules

Prefer hand-written fakes over mocking frameworks.

Example:

```go
type FakeEmbedder struct {
    Vectors [][]float32
    Calls   [][]string
}
```

Avoid tests that depend on:

```text
internet availability
real external documentation
OpenAI account
Ollama installation
random vector models
wall clock
```

External providers should use local fake HTTP servers.

---

# 32. Phase 0 — Repository Bootstrap

## RED

Create test:

```go
func TestVersionCommand(t *testing.T)
```

Expected:

```text
docmcp version
```

returns non-empty version.

Test fails because CLI doesn't exist.

## GREEN

Implement:

```text
Cobra root command
version command
build version variable
```

## REFACTOR

Create:

```text
cmd/docmcp
internal/app
```

## Exit criteria

```bash
go test ./...
go vet ./...
```

green.

---

# 33. Phase 1 — Library IDs

This is foundational because storage and MCP depend on stable IDs.

## Tests first

```go
TestLibraryID_Base
TestLibraryID_Versioned
TestLibraryID_NormalizesName
TestLibraryID_RejectsEmptyName
TestParseLibraryID_Base
TestParseLibraryID_Version
```

Examples:

```text
Pi
→ /local/pi
```

```text
Pi + 0.99.2
→ /local/pi/0.99.2
```

```text
Next.js
→ /local/next-js
```

## Implementation

Create:

```text
internal/source/id.go
```

API:

```go
func NewLibraryID(name, version string) (string, error)
func ParseLibraryID(id string) (LibraryID, error)
```

## Exit criteria

IDs deterministic across runs.

---

# 34. Phase 2 — Source Repository

Before Chroma, implement source metadata repository abstraction.

Tests:

```go
TestSourceRepository_Add
TestSourceRepository_DuplicateName
TestSourceRepository_Get
TestSourceRepository_List
TestSourceRepository_Delete
TestSourceRepository_VersionsCoexist
```

Example:

```text
pi 0.99.1
pi 0.99.2
```

must coexist.

Possible implementation:

```text
small JSON metadata file
or Chroma collection metadata
```

Prefer keeping source configuration outside vector chunks so it can be read without querying vectors.

Example:

```text
~/.docmcp/
├── config.toml
├── sources.json
└── chroma/
```

Keep implementation replaceable behind repository interface.

---

# 35. Phase 3 — URL Normalization

Tests before crawler.

```go
TestNormalizeURL_RemovesFragment
TestNormalizeURL_RemovesTrackingParameters
TestNormalizeURL_ResolvesRelativeURL
TestNormalizeURL_NormalizesTrailingSlash
TestNormalizeURL_RejectsMailto
TestNormalizeURL_PreservesMeaningfulQuery
```

Example:

```text
https://example.com/docs/a?utm_source=x#intro
```

becomes:

```text
https://example.com/docs/a
```

No network required.

---

# 36. Phase 4 — Include/Exclude Rules

Tests:

```go
TestFilter_AllowsSameHost
TestFilter_RejectsOtherHost
TestFilter_RejectsOutsideBasePath
TestFilter_IncludeMatches
TestFilter_IncludeMissingRejects
TestFilter_ExcludeWins
TestFilter_DoubleStarPattern
```

Given:

```text
include:
/docs/**

exclude:
/docs/archive/**
```

expect:

```text
/docs/api/auth        ACCEPT
/docs/guides/start    ACCEPT
/docs/archive/v1      REJECT
/blog/news            REJECT
```

Only after tests pass connect filters to crawler.

---

# 37. Phase 5 — Sitemap Parsing

Build parser without HTTP first.

Fixtures:

```text
testdata/sitemap/urlset.xml
testdata/sitemap/index.xml
testdata/sitemap/malformed.xml
```

Tests:

```go
TestParseSitemap_URLSet
TestParseSitemap_Index
TestParseSitemap_LastModified
TestParseSitemap_Malformed
TestParseSitemap_Empty
```

Then implement sitemap discovery using `httptest.Server`.

Tests:

```go
TestDiscoverSitemap_FromRobots
TestDiscoverSitemap_DefaultPath
TestDiscoverSitemap_IndexFallback
TestDiscoverSitemap_None
```

---

# 38. Phase 6 — Robots

Tests using fixture robots file:

```go
TestRobots_AllowsPath
TestRobots_DisallowsPath
TestRobots_SitemapDeclaration
TestRobots_MissingDefaultsToAllowed
```

Crawler must never fetch disallowed paths.

Do not silently bypass robots rules.

---

# 39. Phase 7 — Basic HTTP Fetcher

Use `httptest.Server`.

Tests:

```go
TestFetcher_GET
TestFetcher_ContentType
TestFetcher_ETag
TestFetcher_LastModified
TestFetcher_Timeout
TestFetcher_404
TestFetcher_TooLarge
TestFetcher_Charset
```

Set maximum body size.

Example default:

```text
5–10 MB per HTML page
```

Avoid unbounded downloads.

---

# 40. Phase 8 — Crawler

Start with sitemap mode.

Acceptance test fixture site:

```text
/
├── robots.txt
├── sitemap.xml
└── docs/
    ├── index.html
    ├── install.html
    └── api.html
```

Test:

```go
TestCrawler_SitemapOnlyCrawlsAllowedDocs
```

Expected pages:

```text
/docs/
/docs/install
/docs/api
```

Not:

```text
/blog
/admin
```

Then add recursive fallback.

Tests:

```go
TestCrawler_FallsBackToLinksWithoutSitemap
TestCrawler_DoesNotLeaveHost
TestCrawler_DoesNotLeavePathPrefix
TestCrawler_DeduplicatesURLs
TestCrawler_HonorsMaxPages
TestCrawler_HonorsDepth
TestCrawler_RateLimits
```

---

# 41. Phase 9 — HTML → Markdown

Create HTML fixtures.

Tests:

```go
TestHTMLConverter_Title
TestHTMLConverter_MainContent
TestHTMLConverter_RemovesNav
TestHTMLConverter_RemovesFooter
TestHTMLConverter_PreservesCode
TestHTMLConverter_PreservesHeadings
TestHTMLConverter_PreservesLinks
```

Use golden tests.

Example:

```text
testdata/html/auth.html
testdata/markdown/auth.golden.md
```

Run converter and compare exact Markdown.

---

# 42. Phase 10 — Structural Chunking

Write tests before chunker implementation.

Tests:

```go
TestChunker_SplitsOnH2
TestChunker_PreservesHeadingPath
TestChunker_KeepsCodeBlockTogether
TestChunker_SubdividesLargeSection
TestChunker_StableChunkIDs
TestChunker_ContentHashes
TestChunker_EmptyDocument
```

Input:

```markdown
# API

## Authentication

Use OAuth.

## Requests

Send JSON.
```

Expected:

```text
Chunk 1:
heading_path = API > Authentication

Chunk 2:
heading_path = API > Requests
```

Use Goldmark AST instead of regex.

---

# 43. Phase 11 — Embedder Contract

First create fake implementation and tests around batch behavior.

```go
func TestEmbeddingService_BatchesChunks(t *testing.T)
func TestEmbeddingService_PreservesOrder(t *testing.T)
func TestEmbeddingService_ReturnsDimensionMismatchError(t *testing.T)
```

Only after service API stabilizes implement providers.

---

# 44. Phase 12 — Default MiniLM

Integration tests:

```go
TestMiniLM_EmbedsSingleText
TestMiniLM_EmbedsBatch
TestMiniLM_DimensionsStable
TestMiniLM_SameInputSimilarOutput
```

Do not assert exact float vectors.

Assert:

```text
vector != empty
dimensions correct
finite floats
```

Mark slow model tests if necessary.

---

# 45. Phase 13 — Ollama Provider

Use `httptest.Server`.

Tests:

```go
TestOllamaEmbedder_Request
TestOllamaEmbedder_Batch
TestOllamaEmbedder_Error
TestOllamaEmbedder_MalformedResponse
TestOllamaEmbedder_Timeout
```

Never require a real Ollama process in unit tests.

Optional real-provider integration test:

```bash
DOCMCP_TEST_OLLAMA=1 go test -tags=provider ./...
```

---

# 46. Phase 14 — OpenAI-Compatible Provider

Again use fake HTTP server.

Tests:

```go
TestOpenAIEmbedder_Endpoint
TestOpenAIEmbedder_AuthHeader
TestOpenAIEmbedder_Model
TestOpenAIEmbedder_Batch
TestOpenAIEmbedder_ErrorPayload
TestOpenAIEmbedder_MissingAPIKey
```

Do not use real API calls in CI.

---

# 47. Phase 15 — Chroma Store

Define tests against `Store` contract first.

Shared store test suite:

```go
func RunStoreContractTests(
    t *testing.T,
    create func(t *testing.T) Store,
)
```

Tests:

```text
upsert chunk
replace chunk
delete chunk
filter by library
filter by version
query
delete source chunks
persist across restart
```

Then run same contract against real Chroma.

Example:

```go
func TestChromaStoreContract(t *testing.T) {
    RunStoreContractTests(t, newTestChromaStore)
}
```

Use temporary directory:

```go
dir := t.TempDir()
```

Never write integration tests into user's real Chroma directory.

---

# 48. Phase 16 — Ingestion Service

Now join:

```text
crawler
parser
chunker
embedder
store
```

Interface:

```go
type Ingestor struct {
    crawler  Crawler
    parser   Parser
    chunker  Chunker
    embedder Embedder
    store    Store
}
```

Acceptance tests:

```go
TestIngestor_AddWebsite
TestIngestor_StoresChunks
TestIngestor_StoresMetadata
TestIngestor_SkipsUnchangedChunks
TestIngestor_UpdatesChangedChunks
TestIngestor_RemovesDeletedChunks
```

All dependencies except Store may be fake initially.

Then add full integration fixture.

---

# 49. Phase 17 — `docmcp add`

Outside-in CLI test:

```bash
docmcp add http://fixture/docs \
  --name fixture \
  --version 1.0
```

Expected:

```text
Added library: /local/fixture/1.0

Pages discovered: 4
Pages indexed:    4
Chunks indexed:   13
```

Tests:

```go
TestAddCommand_RequiresURL
TestAddCommand_RequiresName
TestAddCommand_Version
TestAddCommand_Includes
TestAddCommand_Excludes
TestAddCommand_Duplicate
```

---

# 50. Phase 18 — `docmcp list`

Expected:

```text
LIBRARY                 VERSION   PAGES   LAST SYNC
/local/pi               0.99.2    61      2026-10-01
/local/lightpanda       0.4.1     38      2026-10-01
```

Tests:

```go
TestListCommand_Empty
TestListCommand_MultipleLibraries
TestListCommand_Versions
```

---

# 51. Phase 19 — `docmcp sync`

Critical TDD feature.

Fixture server should expose mutable page state.

Test flow:

```text
initial:
page A
page B

sync

change B
add C
delete A

sync again
```

Assert:

```text
A chunks removed
B chunks replaced
C chunks added
unchanged chunks not embedded again
```

Use fake embedder call count to prove incremental behavior.

---

# 52. Phase 20 — Library Resolver

Implement Context7-style resolution.

Tests:

```go
TestResolver_ExactNameWins
TestResolver_CaseInsensitive
TestResolver_NormalizedName
TestResolver_UsesQueryForAmbiguousLibraries
TestResolver_ReturnsVersions
TestResolver_VersionMentionRanksVersion
TestResolver_NoMatch
```

Example:

```text
libraries:

Pi
Pi SDK
Pico CSS
```

Input:

```json
{
  "libraryName": "Pi",
  "query": "codemode MCP exposure"
}
```

Expected:

```text
Pi
```

not:

```text
Pico CSS
```

For v0.1 ranking can be deterministic:

```text
exact normalized name
prefix match
substring match
description/query token overlap
```

Do not use an LLM.

---

# 53. Phase 21 — Search Engine

Tests:

```go
TestSearch_FiltersLibrary
TestSearch_FiltersVersion
TestSearch_ReturnsRelevantChunks
TestSearch_DeduplicatesAdjacentChunks
TestSearch_LimitsContextInternally
TestSearch_PreservesSourceURLs
TestSearch_NoResults
```

Keep internal result budget configurable but do not expose it over MCP.

Example internal defaults:

```text
initial vector candidates: 20
final chunks: 6
```

Numbers may change based on benchmark tests.

---

# 54. Phase 22 — CLI Search

Test:

```bash
docmcp search \
  --library /local/pi/0.99.2 \
  "MCP exposure"
```

CLI should reuse exact same search service as MCP.

Do not build separate search implementations.

---

# 55. Phase 23 — MCP Server Skeleton

Use official Go SDK.

Create:

```go
server := mcp.NewServer(...)
```

Run stdio transport.

The official SDK supports typed tool handlers and schema inference.

First contract test should connect an MCP client programmatically.

Test:

```go
TestMCPServer_AdvertisesExactlyTwoTools
```

Expected names:

```text
query-docs
resolve-library-id
```

No additional tools.

---

# 56. Phase 24 — MCP Schema Contract Tests

This is important.

Context7 itself tests that its tool schemas contain exactly the expected argument names.

DocMCP should do the same.

Test:

```go
func TestResolveLibraryIDSchema(t *testing.T)
```

Assert properties exactly:

```text
libraryName
query
```

Test:

```go
func TestQueryDocsSchema(t *testing.T)
```

Assert properties exactly:

```text
libraryId
query
```

Assert all required.

Assert no:

```text
limit
page
topic
mode
source
version
topK
threshold
```

This protects compatibility.

---

# 57. Phase 25 — `resolve-library-id` MCP Tool

Handler test:

```go
result := callTool(
    "resolve-library-id",
    map[string]any{
        "libraryName": "Pi",
        "query": "codemode exposure",
    },
)
```

Assert:

```text
result contains "Available Libraries"
result contains "/local/pi"
result contains "/local/pi/0.99.2"
```

Tests:

```text
valid request
missing query
missing libraryName
empty libraryName
no results
multiple versions
```

---

# 58. Phase 26 — `query-docs` MCP Tool

Test:

```go
result := callTool(
    "query-docs",
    map[string]any{
        "libraryId": "/local/pi/0.99.2",
        "query": "codemode exposure",
    },
)
```

Assert result contains:

```text
heading
documentation text
source URL
library ID/version
```

Tests:

```text
invalid library ID
unknown library
missing query
empty query
query success
no result
```

---

# 59. Phase 27 — Argument Alias Compatibility

Context7 currently rewrites some hallucinated argument names before validation.

Implement aliases but do not advertise them.

Canonical:

```text
query
libraryName
libraryId
```

Potential aliases:

```text
userQuery → query
libraryID → libraryId
sourceId → libraryId
sourceID → libraryId
```

Tests:

```go
TestQueryDocs_AcceptsLibraryIDAlias
TestQueryDocs_AcceptsUserQueryAlias
```

But MCP schema remains canonical.

---

# 60. Phase 28 — Tool Annotations Tests

Assert:

```text
readOnlyHint == true
destructiveHint == false
idempotentHint == true
openWorldHint == false
```

This prevents accidental annotation regression.

---

# 61. Phase 29 — End-to-End Fixture Documentation Site

Create an in-process website fixture:

```text
/docs/
├── index
├── installation
├── configuration
└── mcp
```

Sitemap:

```text
/sitemap.xml
```

Run:

```text
docmcp add
        ↓
crawl
        ↓
convert
        ↓
chunk
        ↓
embed
        ↓
store
        ↓
MCP serve
        ↓
resolve-library-id
        ↓
query-docs
```

Single E2E test should prove full system behavior.

---

# 62. Golden E2E Scenario

Fixture docs:

```markdown
# MCP

## Tool Exposure

Tools using codemode exposure are discovered lazily.

## Direct Exposure

Direct tools are immediately available.
```

Question:

```text
How are codemode tools discovered?
```

Expected result includes:

```text
Tool Exposure
discovered lazily
```

and does not primarily return:

```text
Direct Exposure
```

This becomes a basic retrieval quality regression test.

---

# 63. Retrieval Benchmark Fixture

Create approximately 20–50 synthetic documentation chunks covering:

```text
authentication
routing
middleware
database
configuration
MCP
logging
deployment
caching
sessions
```

Create known queries:

```text
"how are MCP tools loaded lazily"
"configure oauth refresh tokens"
"set request timeout"
```

For each query define expected relevant chunk IDs.

Compute:

```text
Recall@5
MRR
```

Do not require sophisticated ML evaluation.

The objective is detecting retrieval regressions.

---

# 64. Versioning Tests

Libraries:

```text
/local/example/1
/local/example/2
```

v1 docs:

```text
Authentication uses API keys.
```

v2 docs:

```text
Authentication uses OAuth.
```

Query:

```text
libraryId=/local/example/1
authentication
```

must not return v2 docs.

This must be an integration test.

---

# 65. Security Tests

Test URL protections.

Reject:

```text
file://
ftp://
unix://
javascript:
```

Consider blocking or explicitly handling private-network targets if remote server mode is ever added.

For local CLI mode, localhost documentation may be intentional, so SSRF policy differs from hosted software.

Also test:

```text
redirect outside allowed host
redirect loop
huge response
malformed gzip
```

---

# 66. Prompt-Injection Handling

Documentation is untrusted content.

Do not execute instructions found in indexed docs.

DocMCP simply returns documentation.

Avoid system-like wrappers such as:

```text
IMPORTANT SYSTEM INSTRUCTION:
```

around retrieved content.

Return neutral sections:

```text
### Heading
Source: ...
Content...
```

Server instructions should make clear that retrieved text is documentation, not MCP server instructions.

---

# 67. Failure Handling

Use typed errors.

Examples:

```go
var ErrSourceNotFound
var ErrLibraryNotFound
var ErrInvalidLibraryID
var ErrEmbeddingMismatch
var ErrCrawlLimitExceeded
```

CLI converts them into human-readable messages.

MCP returns model-readable error content.

Avoid leaking stack traces.

---

# 68. Logging

Logs must go to stderr when running MCP over stdio.

Never write logging to stdout because it would corrupt MCP framing.

Support:

```text
--log-level error
--log-level warn
--log-level info
--log-level debug
```

Potential standard library:

```text
log/slog
```

No external logging dependency required.

---

# 69. Configuration

Suggested location:

```text
~/.config/docmcp/config.toml
```

Data:

```text
~/.local/share/docmcp/
```

Example:

```toml
[data]
path = "~/.local/share/docmcp"

[crawler]
concurrency = 6
max_pages = 1000
request_timeout = "20s"

[embedding]
provider = "default"
```

Keep CLI flags capable of overriding config.

---

# 70. Testing Configuration

Tests should never read the user's config.

Inject:

```go
Config
```

explicitly.

Use:

```go
t.TempDir()
```

for all data directories.

---

# 71. CI Pipeline

Every pull request:

```text
go fmt check
go vet
go test
go test -race
staticcheck or golangci-lint
```

Suggested:

```bash
make test
make test-race
make lint
make integration
```

Do not run real OpenAI/Ollama tests on every PR.

---

# 72. Test Commands

Makefile:

```make
test:
	go test ./...

test-race:
	go test -race ./...

integration:
	go test -tags=integration ./...

provider:
	go test -tags=provider ./...

lint:
	golangci-lint run

ci: test test-race lint integration
```

---

# 73. Test Naming Convention

Use behavior-oriented names:

```text
TestResolver_ExactNameWins
TestCrawler_RejectsExternalHost
TestSync_SkipsUnchangedChunks
```

Not:

```text
TestResolver1
TestCrawler2
TestStuff
```

Tests should communicate design intent.

---

# 74. Commit Strategy

Each commit should represent one green TDD increment.

Examples:

```text
test: define library ID normalization behavior
feat: implement library ID normalization

test: define sitemap parser behavior
feat: implement sitemap URL sets

test: define include/exclude precedence
feat: implement path filters
```

Avoid:

```text
feat: implement entire crawler
```

as a single large commit.

---

# 75. Milestone 1 — Foundation

Deliver:

```text
CLI skeleton
config
library IDs
source model
source repository
URL normalization
include/exclude rules
```

Exit condition:

```text
all pure unit tests green
```

---

# 76. Milestone 2 — Crawler

Deliver:

```text
robots
sitemap parser
sitemap discovery
HTTP fetcher
recursive crawler
rate limiting
crawl boundaries
```

Exit condition:

```text
fixture site fully crawlable
no external network required by tests
```

---

# 77. Milestone 3 — Document Processing

Deliver:

```text
HTML → Markdown
Markdown AST
structural chunker
stable IDs
content hashes
```

Exit condition:

```text
golden documentation fixtures stable
```

---

# 78. Milestone 4 — Embeddings + Chroma

Deliver:

```text
default embedder
Ollama embedder
OpenAI-compatible embedder
Chroma store
persistent index
```

Exit condition:

```text
documents survive process restart
queries return relevant chunks
```

---

# 79. Milestone 5 — Ingestion / Sync

Deliver:

```text
docmcp add
docmcp sync
docmcp remove
docmcp list
docmcp info
```

Exit condition:

```text
incremental sync proven by embedder call-count tests
```

---

# 80. Milestone 6 — Search

Deliver:

```text
resolver
semantic search
version filtering
CLI search
retrieval formatting
```

Exit condition:

```text
retrieval benchmark passes agreed Recall@5 floor
```

---

# 81. Milestone 7 — MCP Compatibility

Deliver:

```text
resolve-library-id
query-docs
stdio transport
server instructions
tool annotations
argument aliases
contract tests
```

Exit condition:

```text
MCP client integration test passes
exactly two tools advertised
schemas match expected public contract
```

---

# 82. Milestone 8 — Release Hardening

Deliver:

```text
race tests
error messages
cross-platform paths
binary releases
README
sample Pi config
sample Claude config
sample Cursor config
```

Exit condition:

```text
fresh machine:
download binary
add docs
serve MCP
query successfully
```

---

# 83. v0.1 Definition of Done

The release is done when this exact workflow works:

```bash
docmcp add https://pi.dev/docs/latest/ \
  --name pi \
  --version 0.99.2
```

Then:

```bash
docmcp serve
```

MCP client calls:

```json
{
  "name": "resolve-library-id",
  "arguments": {
    "libraryName": "Pi",
    "query": "How does codemode MCP tool discovery work?"
  }
}
```

and receives:

```text
/local/pi/0.99.2
```

Then:

```json
{
  "name": "query-docs",
  "arguments": {
    "libraryId": "/local/pi/0.99.2",
    "query": "How does codemode MCP tool discovery work?"
  }
}
```

and receives the relevant indexed Pi documentation with source URLs.

No internet access is needed during the MCP query itself.

---

# 84. Recommended First TDD Sequence

The first implementation sequence should be exactly:

```text
1. CLI bootstrap
2. Library IDs
3. Source model/repository
4. URL normalization
5. include/exclude filters
6. sitemap XML parser
7. robots parser
8. HTTP fetcher
9. crawler
10. HTML → Markdown
11. structural chunker
12. content hashes
13. embedder interface
14. default embedder
15. Chroma store
16. ingestion service
17. add command
18. sync command
19. resolver
20. search engine
21. CLI search
22. MCP server
23. resolve-library-id
24. query-docs
25. contract tests
26. E2E fixture
27. Ollama provider
28. OpenAI-compatible provider
29. retrieval benchmarks
30. release hardening
```

This sequence keeps each step testable and avoids prematurely building provider integrations before the main domain architecture works.

---

# 85. Architecture Boundary to Protect

The most important dependency direction is:

```text
CLI        MCP
 │          │
 └────┬─────┘
      ↓
application services
      ↓
domain interfaces
      ↓
────────────────────────
      ↓
crawler / Chroma / HTTP
```

Do not allow:

```text
MCP handlers → raw Chroma calls
CLI commands → raw Chroma calls
crawler → Chroma
parser → MCP
```

Instead:

```text
MCP
 ↓
SearchService
 ↓
Store
```

and:

```text
CLI
 ↓
IngestionService
 ↓
Crawler + Parser + Chunker + Embedder + Store
```

---

# 86. Key Architectural Rule

The MCP API is the stable part.

Everything behind it should remain replaceable.

Today:

```text
query-docs
   ↓
Chroma dense search
```

Tomorrow:

```text
query-docs
   ↓
BM25
 +
dense vectors
 +
RRF
 +
reranker
```

The AI model should never need to know that anything changed.

That is the primary benefit of copying Context7's minimal query-driven interface.

---

# 87. v0.2 Candidates

After v0.1 only:

```text
ranked BM25 lexical retrieval
hybrid RRF
reranking
JS rendering
Git repositories
multiple workspaces
remote MCP transport
MCP ingestion tools
library aliases
automatic version detection
sitemap incremental lastmod optimization
ETag / If-None-Match sync
HTTP Last-Modified sync
```

Each should be added only after a failing test or benchmark demonstrates a real need.

---

# 88. Success Criteria

DocMCP succeeds if:

```text
installation is easy
indexing is deterministic
queries are fast
retrieval is relevant
versions never leak into one another
sync avoids unnecessary re-embedding
models naturally understand the MCP tools
the MCP interface remains tiny
the system works offline after indexing
```

The desired user experience is:

```text
$ docmcp add https://some-library.dev/docs --name some-library

Discovering documentation...
Crawled 84 pages
Created 612 chunks
Indexed 612 chunks

Library added:
  /local/some-library
```

Then an AI agent simply sees:

```text
resolve-library-id
query-docs
```

and already knows how to use the system.
