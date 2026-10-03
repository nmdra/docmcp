# DocMCP

Point it at a documentation site. It crawls the site, converts each page to
Markdown, splits it into sections, embeds them, stores them locally, and serves
the result to a coding agent over MCP.

The whole interface a model sees is two tools:

```
resolve-library-id   libraryName, query
query-docs           libraryId,  query
```

That is deliberate. Crawler, embedder, store, and retrieval can all be replaced
without an agent ever learning a new interface.

## Install

```bash
go build -o docmcp ./cmd/docmcp
```

Or from the Makefile, which stamps the version from git:

```bash
make build
```

## Quick start

```bash
docmcp add https://example.com/docs/ --name example
docmcp list
docmcp serve
```

`add` prints what it found:

```text
Discovering documentation...

Library: /local/example

Pages:
  discovered: 84
  fetched:    84
  skipped:    0

Chunks:
  unchanged:  0
  updated:    0
  added:      612
  removed:    0
  indexed:    612
```

Run it again later and it re-indexes only what changed:

```bash
docmcp sync example
```

The counts are the point. A sync of an unchanged site reports
`added: 0, removed: 0` and embeds nothing, because each chunk's identity is its
position and its content hash is its content. Editing a page re-embeds that
page's chunks and leaves the rest alone.

## Commands

| Command | Purpose |
|---|---|
| `docmcp add <url> --name <name>` | Index a site as a new library |
| `docmcp sync <name>` | Re-index, embedding only what changed |
| `docmcp list` | Show indexed libraries |
| `docmcp info <library-id>` | Detail for one library |
| `docmcp search --library <id> <query>` | Search from the terminal |
| `docmcp remove <name>` | Delete a library and its chunks |
| `docmcp reindex <name>` | Rebuild vectors, for example after changing the embedding model |
| `docmcp serve` | Serve the index over MCP on stdio |

Useful flags:

```bash
# Narrow the crawl to specific paths
docmcp add https://example.com/docs \
  --name example --version 2 \
  --include "/docs/api/**" --include "/docs/guides/**" \
  --exclude "/docs/archive/**"

# Search by short name instead of an ID
docmcp search --library example "how does key rotation work"
```

A source's URL path is the crawl boundary. `add https://example.com/docs` will
never fetch `https://example.com/blog`, and it honours `robots.txt` on every
request.

## Connect an MCP client

The server speaks stdio, so any MCP client can launch it as a subprocess.

```json
{
  "mcpServers": {
    "docmcp": {
      "command": "docmcp",
      "args": ["serve"]
    }
  }
}
```

For a client that resolves binaries from your `PATH`, that block is the whole
setup. `examples/` has ready-made files for Claude Code, Cursor, and Pi.

### Pi agent

Install DocMCP and index a documentation site:

```bash
go install github.com/nmdra/docmcp/cmd/docmcp@latest
docmcp add https://example.com/docs/ --name example
```

Register DocMCP with Pi:

```bash
pi mcp add docmcp --exposure direct -- "$(command -v docmcp)" serve
pi mcp list
```

This command writes to Pi's user-level MCP config. Direct exposure makes both
DocMCP tools available to the model. The index uses DocMCP's default data path.

Start Pi and use this integration prompt:

```text
Use resolve-library-id to find the Example library. Then use query-docs to
answer: How does key rotation work? Cite the source heading and URL.
```

Pi calls `resolve-library-id` before `query-docs`, unless you provide an exact
library ID. To use a project-level Pi config, add `--local` to `pi mcp add`.
Pi loads `.pi/mcp.json` only after you trust the project.

## Configuration

Config lives at `~/.config/docmcp/config.toml`. It is optional: every value has a
default, and `add` works with no config file at all.

```toml
[data]
path = "~/.local/share/docmcp"

[crawler]
concurrency = 6
max_pages = 1000
request_timeout = "20s"
rate_limit = 0        # requests per second; 0 means unthrottled

[embedding]
provider = "default" # "default", "ollama", or "openai"
model = "all-MiniLM-L6-v2"
```

The default embedder runs locally with no API key and no server. On first use it
downloads the model — about 190 MB — into the machine cache
(`~/.cache/docmcp/models`). It is shared by every index, so it is downloaded once
per machine rather than once per `--data-dir`. After that everything works
offline.

### Other embedders

```toml
[embedding]
provider = "ollama"
model = "nomic-embed-text"
base_url = "http://localhost:11434"
```

```toml
[embedding]
provider = "openai"
model = "text-embedding-3-small"
```

For OpenAI-compatible endpoints the key comes from the environment, never from
the config file:

```bash
export DOCMCP_OPENAI_API_KEY=sk-...
```

**One embedding configuration per index.** The index records the provider,
model, vector width, and embedding-text format. Chunk embeddings include the
page title and heading path. Search results still contain the original text.

If the model or embedding-text format changes, sync refuses to mix old and new
vectors. For an index with one library, run `docmcp reindex <name>`.
For a shared index with incompatible vectors, rebuild all libraries in a fresh
`--data-dir`. A scoped reindex refuses to mark the other libraries' old vectors
as compatible.

Search keeps semantic ranking for natural-language queries. It combines semantic
retrieval with local BM25 and rank fusion when a query contains technical
identifier syntax or an all-caps token in a mixed-case query, such as `/reload`,
`oauth.clientName`, `query-docs`, or `MCP`. All-caps emphasis can also select
hybrid retrieval. These queries scan the library's local chunks; no extra
database or network access is required.

## Scope and safety

- The crawl is bounded to one host and one path prefix. `--include` and
  `--exclude` narrow it further; exclude always wins.
- `robots.txt` is consulted before every fetch. A disallowed path is never
  requested, and robots is never bypassed.
- Fetched bodies are capped, requests are rate-limited, and cross-host redirects
  are refused.
- Only `http` and `https` URLs are accepted; `file:`, `ftp:`, `mailto:`, and
  friends are rejected.
- MCP is read-only. Adding, syncing, and removing all happen through the CLI.
- A query never touches the network. It reads the local index.
- Documentation is treated as untrusted text. Retrieved content is returned as
  plain sections with `### heading`, `Source:`, and the text — never wrapped in
  anything that reads like an instruction.

## Development

```bash
go test ./...                              # unit and default suite
go test -race ./...
go vet ./...
go test -p 1 -tags=integration ./...       # real Chroma
DOCMCP_TEST_LOCAL=1 go test -p 1 -tags=provider ./...  # real local model
DOCMCP_TEST_LIVE_BENCH=1 go test ./internal/app -run '^TestLivePiBenchmark$' # live site
```

`make ci` runs the default, race, integration, and local-provider suites, vet,
and lint. It does not run the live-site benchmark. A cold provider run needs
network access to download the model. The live benchmark always needs network
access to crawl the documentation site.

Most tests need no network, no model, and no external service: embedding is
faked, providers are exercised against local `httptest` servers, and every data
path is a `t.TempDir()`. The suites that do need a real model are gated behind
`DOCMCP_TEST_LOCAL=1` so the fast suite stays fast.

For agents working in this repository, `AGENTS.md` holds the rules that survive
every task: the frozen MCP contract, determinism requirements, and the
dependency direction between packages.