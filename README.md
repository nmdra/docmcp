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
downloads the model once into `path/models`; after that everything works
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

**One embedding model per index.** The provider, model, and vector width are
recorded with the index. Changing them is refused with an explanation, because
mixing vectors from two models into one index silently destroys retrieval. Run
`docmcp reindex` to rebuild — or index into a fresh `--data-dir`.

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
go test -tags=integration ./...           # real Chroma
DOCMCP_TEST_LOCAL=1 go test ./internal/embedding/   # real local model
```

`make ci` runs all of it.

Most tests need no network, no model, and no external service: embedding is
faked, providers are exercised against local `httptest` servers, and every data
path is a `t.TempDir()`. The suites that do need a real model are gated behind
`DOCMCP_TEST_LOCAL=1` so the fast suite stays fast.

For agents working in this repository, `AGENTS.md` holds the rules that survive
every task: the frozen MCP contract, determinism requirements, and the
dependency direction between packages.