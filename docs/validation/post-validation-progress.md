# Post-validation progress

This report covers the current implementation checkpoint. It does not establish
public-alpha readiness. The full plan remains in
`.agents/plans/post_validation_plan.md`.

## Accepted changes

- Embedding input includes document titles and heading paths. Stored and returned
  documentation text remains unchanged.
- The index records the embedding-text format and actual vector dimensions.
  Unknown-width providers cannot change the index width during sync.
- A scoped reindex cannot mark other libraries' incompatible vectors as valid.
  Compatible shared reindex retains the recorded width.
- Title changes cause reembedding even when the body hash stays unchanged.
- Adjacent chunks from one full heading receive a two-result soft cap. Deferred
  chunks fill unused slots. Ambiguous indices bypass this policy.
- Chroma queries retain nonzero chunk indices.
- Identifier syntax and all-caps tokens in normally cased queries use
  library-scoped BM25 candidates and RRF. Other prose retains semantic ranking.
  All-caps emphasis can also select hybrid retrieval.
- The direct-exposure live relevance check requires the answer-bearing phrase
  `Declared to the model like a built-in tool`, not the ambiguous substring
  `direct` that also matched `indirect`.
- Lexical processing observes context cancellation.
- Shared embedding validation rejects empty, wrong-width, and nonfinite vectors.
  Unknown widths must remain consistent within each embedding call and index.
- Golden tests freeze MCP output, tool descriptions, schemas, and annotations.
- CI includes cached, serialized local-provider checks. Provider initialization
  failures no longer become successful skips after explicit opt-in.
- The GoReleaser repository identity now points to `nmdra/docmcp`.

No dependency, second database, or public MCP parameter was added.

## Retrieval measurements

### Synthetic corpus

The corpus now has 41 queries, including the original 38-query subset.

| Metric | Original recorded baseline | Current checkpoint |
|---|---:|---:|
| Topic Recall@5 | 1.000 | 1.000 |
| MRR, original query subset | 0.860 | 0.864 |
| MRR, expanded query set | Not measured | 0.874 |
| Gold-page Top-1 | Not measured | 80.5% |
| Gold-page Top-3 | Not measured | 92.7% |
| Gold-page Top-5 | Not measured | 97.6% |

Topic Recall@5 measures broad topic coverage. Gold-page accuracy and MRR use the
declared answer page. They are separate metrics, not interchangeable labels.

### Live Pi documentation

The live benchmark now has 12 queries. Expected URLs, headings, and selected
answer-content checks define relevance. It remains opt-in.

| Metric | Expanded pre-change | Before acronym routing | Current |
|---|---:|---:|---:|
| Top-1 | 3/12 | 8/12 | 8/12 |
| Top-3 | 8/12 | 10/12 | 11/12 |
| Top-5 | 9/12 | 11/12 | 12/12 |
| MRR | 0.461 | 0.771 | 0.812 |
| Recall@5 | 0.750 | 0.917 | 1.000 |

The current original six-query subset reaches Top-5 in 6/6 cases. The general
slash-command query finds the overview at rank 1, `/reload` finds its command
description, and the OAuth identifier query finds its section at rank 1. Direct
MCP exposure now ranks at 2 with the answer-bearing description. Top-1 held
steady relative to the prior checkpoint, Top-3 and Top-5 improved, and MRR rose
from 0.771 to 0.812. Live documentation can change between runs.

A Pi agent test used the project MCP server over stdio, resolved `pi` to
`/local/pi/current`, then called `query-docs` for “How does direct MCP exposure
work?”. It returned the `Control tool exposure` section, including: “Declared to
the model like a built-in tool and also callable from codemode.” The project MCP
entry is in `.pi/mcp.json`; because the project is not yet trusted, the test ran
with `pi --approve`. The index is stored in ignored `docmcp-data/`. A follow-up
12-query Pi-agent batch on that persistent snapshot reached Top-5 in 11/12 cases;
the direct-exposure answer ranked 3. See the [Pi-agent report](2026-10-02-pi-agent-mcp-validation.md)
for the per-query results. It differs from the fresh-crawl benchmark above.

## Rejected experiments

Unrestricted natural-language lexical fusion reduced original-subset MRR to
0.798. Identifier routing preserves semantic retrieval for ordinary paraphrases.

A two-position leaf-heading promotion reduced original-subset MRR from 0.864 to
0.850. That experiment was removed. No query-specific ranking rules remain.

## Verification

The following commands passed on the current implementation:

```bash
go test ./...
go test -race ./...
go test -p 1 -count=1 -tags=integration ./...
DOCMCP_TEST_LOCAL=1 go test -p 1 -count=1 -tags=provider ./...
go vet ./...
golangci-lint run
make ci
git diff --check
goreleaser check
goreleaser check --config .goreleaser-alpha.yaml
DOCMCP_TEST_LIVE_BENCH=1 go test ./internal/app -run '^TestLivePiBenchmark$' -count=1 -v
```

Active LSP checks found no language errors in the changed Go files. Auxiliary
rules flag CLI text formatting and existing workflow action references.

A workspace analyzer also flagged the crawler's seed-slice append as a race.
Manual inspection shows that this append precedes worker creation. Worker
appends use the existing mutex. The race suite passed. This finding did not
cause a change to the crawler.

The first isolated snapshot attempt failed during `go mod tidy` because
`proxy.golang.org` DNS lookup timed out. A later default-profile attempt resolved
dependencies but failed to cross-compile Linux ARM64 cgo on this host. The
approved `.goreleaser-alpha.yaml` profile then built a Linux amd64 snapshot. I
extracted its archive and ran `docmcp version` and `docmcp --help` successfully.
This first-alpha profile has no signatures or SBOMs. The default full-platform
profile remains unverified. `make ci`, both GoReleaser config checks, and a local
`go install ./cmd/docmcp` smoke test passed.

## Remaining work and limitations

- Complete table-context evaluation and the remaining UX, redaction, diagnostics,
  performance, stdio, and installation tasks in the plan.
- Complete the full signed multi-platform snapshot with cross-compilers.
  The limited Linux amd64 alpha archive passed its install smoke test.
- Identifier queries scan the full scoped library corpus. There is no separate
  lexical index or cache.
- Failed reindex can remove the target library's chunks before a replacement
  succeeds. The shared-index guard prevents mixed vectors, not transactional
  restoration.
- Existing indexes require migration for the new embedding-text format. Shared
  incompatible indexes require a fresh data directory or explicit removal of
  other libraries before scoped reindex.
