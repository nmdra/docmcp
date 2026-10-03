# Pi-Agent DocMCP Validation Report

Date: 2026-10-02

## Purpose

Exercise DocMCP through a Pi agent's configured MCP connection, using real indexed
pi.dev documentation. This complements the opt-in Go live benchmark by validating
the Pi client, project MCP configuration, resolver, and query tool together.

## Setup

| Item | Value |
|---|---|
| Pi | 1.0.0 |
| MCP config | project `.pi/mcp.json`, `docmcp` with direct exposure |
| Server command | `go run ./cmd/docmcp --data-dir docmcp-data serve` |
| Project approval | `pi --approve` for this run; project trust is not saved |
| Library resolved | `/local/pi/current` |
| Indexed corpus | pi.dev docs, 40 pages fetched, 445 chunks |

The agent called `resolve-library-id` first. It then issued the 12 `query-docs`
calls through DocMCP using Pi codemode and parsed the returned ordered result
sections. No web browsing was used during these queries. The index itself had
been created from the live pi.dev docs site.

## Results

A query passes when its returned chunk meets the expected URL, heading, and
answer-content condition listed in `internal/app/live_benchmark_test.go`. Rank is
the 1-based position in the ordered `query-docs` response.

| # | Query | Matching section | Rank | Result |
|---:|---|---|---:|---|
| 1 | How are MCP codemode tools discovered? | `Control tool exposure`, `/mcp` | 1 | Pass |
| 2 | How do I add a default tool? | `Tools`, `/settings` | 2 | Pass |
| 3 | How is OAuth configured for an MCP server? | `Authenticate with OAuth`, `/mcp` | 1 | Pass |
| 4 | How are MCP tools hidden from the model? | `Control tool exposure`, `/mcp` | 1 | Pass |
| 5 | How does direct MCP exposure work? | `Control tool exposure`, `/mcp`, includes “Declared to the model like a built-in tool” | 3 | Pass |
| 6 | How do I use slash commands in the editor? | `/slash-commands` | 1 | Pass |
| 7 | What does codemode exposure do? | `Control tool exposure`, `/mcp` | 6 | Pass |
| 8 | How do I hide an MCP tool? | `Control tool exposure`, `/mcp` | 1 | Pass |
| 9 | How is oauth.clientName configured? | `Authenticate with OAuth`, `/mcp` | 1 | Pass |
| 10 | What slash commands are available? | `/slash-commands` | 1 | Pass |
| 11 | How do I enable a tool by default? | `Tools`, `/settings` | 2 | Pass |
| 12 | What does /reload do? | `Runtime and project`, `/slash-commands`, includes “Reload keybindings” | 1 | Pass |

| Metric | Result |
|---|---:|
| Top-1 | 8/12 (66.7%) |
| Top-3 | 11/12 (91.7%) |
| Top-5 | 11/12 (91.7%) |
| Within DocMCP's six-result response | 12/12 (100%) |
| Original six-query subset in Top-5 | 6/6 (100%) |

The one result outside Top-5 was “What does codemode exposure do?”, whose
matching section appeared at rank 6. Direct MCP exposure appeared at rank 3 in
this project index and returned the expected answer-bearing sentence. This rank
can vary from the fresh-crawl Go benchmark because this Pi test used the already
indexed project snapshot.

## Conclusion

**Pi MCP integration: PASS.** Pi resolved the library and retrieved expected
sections through the configured local DocMCP stdio server. The direct-exposure
question returned the concrete `direct` definition. The 12-call batch achieved
11/12 Top-5 and 12/12 within the six-result response limit. These results cover
this Pi-docs query set; they do not establish quality for other libraries or
users' corpora.

The project MCP entry remains local and uncommitted. Pi ignores it in an
untrusted project unless the user approves the project or starts Pi with
`--approve`. The indexed `docmcp-data/` directory is git-ignored.
