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
- Explicit technical identifiers use library-scoped BM25 candidates and RRF.
  Ordinary natural-language queries retain semantic retrieval.
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

| Metric | Expanded pre-change run | Current checkpoint |
|---|---:|---:|
| Top-1 | 3/12 | 8/12 |
| Top-3 | 8/12 | 10/12 |
| Top-5 | 9/12 | 11/12 |
| MRR | 0.461 | 0.771 |
| Recall@5 | 0.750 | 0.917 |

The original six-query subset improved from 4/6 to 5/6 Top-5. The general
slash-command query now finds the overview at rank 1. The `/reload` query finds
its command description. The OAuth identifier query finds its section at rank 1.

Direct MCP exposure remains a miss in Top-5. Candidate diagnostics find the
content-qualified answer at rank 7 after diversification. Live documentation and
approximate candidate retrieval can change between runs.

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
git diff --check
goreleaser check
DOCMCP_TEST_LIVE_BENCH=1 go test ./internal/app -run '^TestLivePiBenchmark$' -count=1 -v
```

Active LSP checks found no language errors in the changed Go files. Auxiliary
rules flag CLI text formatting and existing workflow action references.

A workspace analyzer also flagged the crawler's seed-slice append as a race.
Manual inspection shows that this append precedes worker creation. Worker
appends use the existing mutex. The race suite passed. This finding did not
cause a change to the crawler.

The isolated snapshot attempt passed configuration validation but failed during
`go mod tidy` because `proxy.golang.org` DNS lookup timed out. No platform binary
was built or published. That staging run used HEAD implementation with the
updated release configuration, not the current retrieval implementation.

## Remaining work and limitations

- Resolve the direct-exposure miss with a new benchmark-supported hypothesis.
- Complete table-context evaluation and the remaining UX, redaction, diagnostics,
  performance, stdio, and installation tasks in the plan.
- Complete snapshot and fresh-install checks in a suitable build environment.
- Identifier queries scan the full scoped library corpus. There is no separate
  lexical index or cache.
- Failed reindex can remove the target library's chunks before a replacement
  succeeds. The shared-index guard prevents mixed vectors, not transactional
  restoration.
- Existing indexes require migration for the new embedding-text format. Shared
  incompatible indexes require a fresh data directory or explicit removal of
  other libraries before scoped reindex.
