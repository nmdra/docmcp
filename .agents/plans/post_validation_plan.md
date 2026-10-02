# DocMCP Post-Validation Implementation Plan

## Execution status

The following checklist records implementation progress. The milestone sections
remain the specification, not a claim that every task is complete.

- [x] Run the initial unit, race, integration, local-provider, vet, and lint checks.
- [x] Add cached, serialized local-provider checks to CI and `make ci`.
- [x] Expand synthetic queries from 38 to 41 and live queries from 6 to 12.
- [x] Add declarative URL, heading, and answer-content relevance checks.
- [x] Preserve original-subset MRR separately from expanded-set metrics.
- [x] Enrich embedding input with title and heading without changing stored text.
- [x] Require reindex for the new embedding-text format.
- [x] Reject incompatible scoped reindex when other libraries retain old vectors.
- [x] Reembed chunks when title metadata changes without body changes.
- [x] Add reversible adjacent-run diversification and stable score ties.
- [x] Preserve nonzero chunk indices through Chroma query metadata.
- [x] Add BM25 candidates and RRF for explicit technical-identifier queries.
- [x] Complete retrieval review fixes for cancellation and discriminating tests.
- [x] Bypass adjacent-run suppression when chunk indices are ambiguous.
- [x] Freeze query, resolver, and public MCP contract output with golden tests.
- [x] Document empty-crawl semantics and literal trailing-slash robots behavior.
- [x] Centralize finite-value and dimension checks across embedding providers.
- [x] Reject inconsistent unknown vector widths within and across service batches.
- [x] Persist actual dimensions for providers whose configured width is unknown.
- [ ] Fix the remaining direct-MCP-exposure miss without benchmark regression.
- [x] Correct the GoReleaser repository identity and pass `goreleaser check`.
- [ ] Complete snapshot builds. The isolated attempt failed at dependency DNS lookup.
- [ ] Complete table-context, UX, security, diagnostics, and release tasks.

Latest measured synthetic results: topic Recall@5 1.000, expanded MRR 0.874,
and original-subset MRR 0.864. Gold-page Top-3 is 92.7%; Top-5 is 97.6%.

Latest live results: Top-1 8/12, Top-3 10/12, Top-5 11/12, MRR 0.771.
The original six queries reach Top-5 in 5/6 cases. Direct exposure remains a miss.
These measurements do not establish the complete public-alpha release gate.

Unrestricted natural-language lexical fusion regressed synthetic MRR, so it was
rejected. Identifier routing preserves dense retrieval for ordinary paraphrases.
Identifier queries scan the local library corpus. No second database was added.

A bounded leaf-heading promotion experiment reduced original-subset MRR from
0.864 to 0.850, so it was also rejected. Candidate diagnostics place the remaining
direct-exposure answer at rank 7 after diversification. Further work needs a new
hypothesis, not a larger manually tuned promotion.

## 1. Objective

Move DocMCP from:

```text
Local alpha
```

to:

```text
Public alpha
```

without expanding scope unnecessarily.

The validation report establishes that:

- crawling works on a real documentation site;
- Chroma persistence works;
- embeddings succeed reliably;
- incremental sync works;
- version isolation is correct;
- MCP schemas match the frozen Context7-style interface;
- stdio MCP framing is clean;
- previously discovered crawler/security/parser defects have regression tests.

The main remaining product weakness is retrieval quality:

```text
Live benchmark:
Top-1: 2/6
Top-3: 4/6
Top-5: 4/6
```

Two normal user queries fail despite the correct content being present in the index.

Therefore the implementation priority is:

```text
1. Retrieval correctness
2. Release correctness
3. Crawl/index robustness
4. First-run UX/performance
5. Diagnostics/observability
6. Public-alpha packaging
```

Do not add Git indexing, PDFs, browser rendering, extra MCP tools, or other large features before these are complete.

---

# 2. Current Issue Status

## Fixed and protected by regression tests

These issues were found and fixed during validation:

```text
CRITICAL
- robots.txt fetched incorrectly as //robots.txt

MEDIUM
- disclosure/navigation chrome polluted chunks
- empty crawl reported success
- rejected OpenAI key leaked into errors

LOW
- zero-length Ollama embeddings accepted
- chunk count mislabeled as Indexed Pages
```

The report confirms regression tests exist for each fixed defect.

These fixes should not be redesigned unless a new failing test demonstrates a problem.

## Known open issues

### Retrieval

Two live queries currently miss:

```text
"How does direct MCP exposure work?"
```

and:

```text
"How do I use slash commands in the editor?"
```

The first reaches the right page but ranks the wrong section first.

The second suffers because short explanatory content is overshadowed by command/table chunks.

### Release configuration

`.goreleaser.yaml` still refers to:

```text
docmcp/docmcp
```

instead of:

```text
nmdra/docmcp
```

according to the validation report.

### Crawl edge case

The trailing-slash robots rule behavior remains documented rather than changed:

```text
Disallow: /docs/secret/
```

does not match the normalized path:

```text
/docs/secret
```

The report explicitly treats this as predictable literal matching rather than a correctness defect.

---

# 3. Development Rule

Continue using strict TDD.

Every change follows:

```text
RED
 ↓
GREEN
 ↓
REFACTOR
 ↓
FULL TEST SUITE
```

For retrieval changes:

```text
existing benchmark fails
        ↓
write/add focused regression case
        ↓
implement smallest ranking improvement
        ↓
run synthetic benchmark
        ↓
run live Pi benchmark
        ↓
keep only if overall metrics improve
```

Do not tune individual queries at the expense of the whole benchmark.

---

# 4. Milestone 1 — Freeze the Current Correctness Baseline

Before changing retrieval, lock down everything currently working.

## Tasks

- Ensure all validation regression tests live in normal repository test suites.
- Verify no test relies on temporary validation scripts.
- Add comments linking regressions to issue IDs where useful.
- Ensure CI runs:
  - unit tests
  - race tests
  - integration tests
  - provider tests with local/fake providers
  - `go vet`
  - linter
- Keep the live Pi benchmark opt-in.

## Required commands

```bash
go test ./...
go test -race ./...
go test -p 1 -tags=integration ./...
DOCMCP_TEST_LOCAL=1 go test -tags=provider ./...
go vet ./...
golangci-lint run
```

The validation report states all these were green after the fixes.

## Acceptance criteria

No retrieval work begins until the baseline stays green.

---

# 5. Milestone 2 — Retrieval Benchmark Expansion

The existing benchmark is too small to safely optimize against only two misses.

## Goal

Expand retrieval evaluation before changing ranking.

## Current baseline

Synthetic:

```text
Recall@5 = 1.000
MRR      = 0.860
```

Live Pi benchmark:

```text
Top-1 = 2/6
Top-3 = 4/6
Top-5 = 4/6
```

## Add query categories

Create benchmark queries covering:

```text
exact API/tool names
conceptual paraphrases
heading-oriented queries
CLI commands
configuration questions
authentication
version-specific behavior
short identifiers
multiword technical concepts
```

Examples:

```text
"What does codemode exposure do?"
"How do I hide an MCP tool?"
"How is oauth.clientName configured?"
"What slash commands are available?"
"How do I enable a tool by default?"
"What is direct MCP exposure?"
```

## Store benchmark cases as data

Suggested:

```go
type BenchmarkCase struct {
    Query          string
    LibraryID      string
    ExpectedURLs   []string
    ExpectedHeads  []string
}
```

Keep expected relevance declarative rather than embedding ranking logic in tests.

## Metrics

Track:

```text
Top-1 accuracy
Top-3 accuracy
Top-5 accuracy
MRR
Recall@5
```

## Initial public-alpha gate

Proposed target:

```text
Top-5 >= 90%
Top-3 >= 80%
MRR >= existing baseline
Synthetic Recall@5 must not regress below 1.000
```

These are project targets, not values from the report.

---

# 6. Milestone 3 — Improve Embedding Input

This should be the first retrieval experiment because DocMCP already stores structural metadata.

## Hypothesis

Embedding only chunk body text may lose important context such as:

```text
Slash Commands
Control tool exposure
Settings > Tools
```

A short chunk becomes more searchable if its structural path is part of the embedding representation.

## Proposed embedding input

Instead of:

```text
<chunk content>
```

embed:

```text
<title>

<heading path>

<chunk content>
```

For example:

```text
Pi Documentation

MCP > Control tool exposure

Tools with direct exposure are ...
```

## Important rule

Do not alter the text returned to the model.

The enriched text is only for embedding.

Stored source content remains clean.

## TDD sequence

### RED

Add focused tests for:

```text
direct MCP exposure
slash commands
```

that assert the intended section reaches the configured Top-K.

### GREEN

Introduce:

```go
func EmbeddingText(chunk Chunk) string
```

### Tests

```go
TestEmbeddingText_IncludesHeadingPath
TestEmbeddingText_IncludesTitle
TestEmbeddingText_DoesNotModifyStoredContent
TestEmbeddingText_Stable
```

### Benchmark

Run all retrieval benchmarks.

Keep the change only if:

```text
problem queries improve
AND
existing benchmark does not materially regress
```

---

# 7. Milestone 4 — Adjacent Chunk Deduplication

The report indicates table-heavy or structurally repetitive content can dominate retrieval.

## Goal

Prevent multiple nearly identical chunks from the same section from occupying most of the result budget.

## Proposed behavior

If the top candidates are:

```text
Slash Commands > /help
Slash Commands > /reload
Slash Commands > /model
Slash Commands > /tools
```

do not necessarily return all four before the section overview.

## Strategy

Group candidates using:

```text
document ID
heading parent
adjacent chunk indices
```

Then apply one of:

```text
max N chunks per heading
or
merge adjacent chunks
```

Start with the simplest deterministic rule.

## Tests

```go
TestSearch_DeduplicatesAdjacentChunks
TestSearch_PreservesDistinctSections
TestSearch_DoesNotCollapseUnrelatedContent
```

## Acceptance criteria

Result diversity improves without losing relevant content.

---

# 8. Milestone 5 — Add Lexical Retrieval

Only implement this after the embedding-context experiment is measured.

## Why

Technical docs contain identifiers where lexical matching is valuable:

```text
codemode
oauth.clientName
defaultTools
query-docs
resolve-library-id
```

Dense embeddings alone may under-rank exact terminology.

## Goal

Generate two candidate sets:

```text
dense semantic candidates
lexical candidates
```

without changing the MCP API.

## Architecture

```text
query
  │
  ├── dense search
  │      ↓
  │   top N
  │
  └── lexical search
         ↓
      top N

        ↓
       fusion
        ↓
     final result
```

## Storage options

Evaluate the lowest-complexity local implementation.

Possible approaches:

```text
Chroma-supported lexical capabilities
or
small side index
or
SQLite FTS if justified later
```

Do not introduce a second database unless benchmark evidence justifies it.

---

# 9. Milestone 6 — Reciprocal Rank Fusion

If lexical retrieval is added, combine rankings with RRF rather than raw score mixing.

## Why

Dense similarity scores and lexical ranking scores do not naturally share the same scale.

## Proposed function

```go
func RRFScore(rank int, k float64) float64 {
    return 1.0 / (k + float64(rank))
}
```

Suggested starting constant:

```text
k = 60
```

Treat it as an internal value, never an MCP parameter.

## Example

```text
Dense:

A rank 1
B rank 2
C rank 3

Lexical:

C rank 1
D rank 2
A rank 3
```

Fusion:

```text
A = dense rank1 + lexical rank3
C = dense rank3 + lexical rank1
B = dense rank2
D = lexical rank2
```

## Tests

```go
TestRRF_ResultInBothListsRanksHigher
TestRRF_Deterministic
TestRRF_MissingCandidateHandled
TestRRF_TieBreakStable
```

---

# 10. Milestone 7 — Lightweight Structural Ranking Signals

Only add these if hybrid search still misses benchmark cases.

Avoid large hand-tuned scoring systems.

Potential small signals:

```text
heading contains exact query phrase
heading contains important query token
title contains exact library/API name
very short generic chunk penalty
```

Example:

```text
query:
"direct MCP exposure"
```

Candidate:

```text
heading:
Control tool exposure
```

should receive a small deterministic structural boost.

## Do not

Create dozens of manually tuned weights.

Any boost must have a benchmark-backed reason.

## Tests

```go
TestRanker_HeadingMatchGetsSmallBoost
TestRanker_BodyRelevanceStillDominates
TestRanker_NoQuerySpecificRules
```

---

# 11. Milestone 8 — Improve Short Intro/Table Handling

The slash-command failure indicates a structural indexing problem in addition to ranking.

## Goal

Ensure section introductions remain competitive against many child/table chunks.

## Possible solution

When chunking:

```text
heading
intro paragraph
table
```

preserve the intro as contextual text for table-derived chunks.

Example embedding representation:

```text
Slash Commands

Slash commands provide commands available in the current session.

Command: /reload
...
```

instead of embedding only:

```text
/reload
...
```

## Tests

Create a fixture reproducing the current slash-command failure.

```go
TestChunking_TableRowsKeepSectionContext
TestSearch_SectionOverviewRanksForGeneralQuery
TestSearch_SpecificCommandStillFindsSpecificRow
```

---

# 12. Milestone 9 — Retrieval Output Formatting

The MCP contract currently passes, so do not change tool names or parameters.

Focus only on model-readable output consistency.

Standardize each result as:

```text
### <heading path>

Source: <canonical URL>
Library: <library ID>

<content>
```

Separate results with:

```text
---
```

## Tests

Golden tests:

```go
TestQueryDocs_FormatSingleResult
TestQueryDocs_FormatMultipleResults
TestQueryDocs_IncludesSourceURL
TestQueryDocs_IncludesLibraryID
TestQueryDocs_DoesNotExposeScores
```

Do not expose:

```text
distance
embedding IDs
topK
internal rank scores
```

---

# 13. Milestone 10 — Preserve Context7-Compatible MCP Contract

The report confirms the current contract is correct:

```text
resolve-library-id:
  libraryName
  query

query-docs:
  libraryId
  query
```

with all fields required and `additionalProperties: false`.

Freeze this contract.

## Add explicit compatibility golden tests

Snapshot:

```text
tool names
tool descriptions
input JSON schemas
required fields
annotations
```

Tests must fail if someone later adds:

```text
limit
topK
version
source
mode
threshold
```

to the public tools.

Internal retrieval complexity must remain invisible.

---

# 14. Milestone 11 — GoReleaser Fix

Fix the known release configuration problem immediately after retrieval work begins or in parallel.

## Task

Change repository path in:

```text
.goreleaser.yaml
```

from the stale repository identity to:

```text
nmdra/docmcp
```

as identified in the validation report.

## Tests/checks

Run:

```bash
goreleaser check
```

Then dry-run snapshot build:

```bash
goreleaser release --snapshot --clean
```

Verify:

```text
Linux amd64
Linux arm64
macOS amd64/arm64 if supported
Windows if supported
```

Do not publish during CI validation.

---

# 15. Milestone 12 — First-Run Model UX

The report shows:

```text
cold model bootstrap:
~189 MB
~51 seconds

warm startup:
~0.8 seconds
```

and Chroma runtime bootstrap adds another cached component.

This is not a correctness issue, but it is a major first-run UX concern.

## Improvements

### Add explicit progress

Instead of appearing hung:

```text
Preparing local embedding model...
Downloading all-MiniLM-L6-v2...
```

### Display cache location

```text
~/.cache/docmcp/models
```

### Add optional prefetch command

Proposed:

```bash
docmcp models pull
```

or:

```bash
docmcp setup
```

Do not require this command for normal operation.

### Tests

Abstract the downloader/cache layer enough to test:

```go
TestModelCache_WarmDoesNotRedownload
TestModelCache_ColdReportsProgress
TestModelCache_UsesMachineCache
```

---

# 16. Milestone 13 — Crawl Diagnostics

Crawling itself performs well:

```text
39 pages in ~2 seconds
```

and is not currently a bottleneck.

Do not optimize crawler performance.

Instead improve visibility.

## Add crawl summary

Example:

```text
Discovery
  robots.txt: found
  sitemap: none
  strategy: recursive

URLs
  discovered: 39
  accepted: 39
  rejected: 0

Fetch
  success: 39
  redirect: 0
  failed: 0
```

## Debug mode

```bash
docmcp add ... --log-level debug
```

Should explain:

```text
rejected outside base path
rejected by robots
duplicate normalized URL
unsupported content type
```

Keep normal output compact.

---

# 17. Milestone 14 — Empty/Partial Crawl Semantics

The empty-crawl defect is fixed, but formalize the behavior as product semantics.

The report now expects:

```text
add + zero usable pages:
    fail
    roll back library

sync + zero usable pages:
    warn
    retain library
```

Document this behavior.

## Tests

Keep:

```text
TestAdd_EmptyCrawlFailsLoudly
TestAdd_NonHTMLSourceFailsLoudly
TestAdd_PartialCrawlSucceeds
TestSync_DeletingEverythingRemovesChunksButKeepsLibrary
```

Add documentation examples for the CLI.

---

# 18. Milestone 15 — Secret Redaction Hardening

The OpenAI error leak was fixed after a provider echoed the supplied API key back in an error message.

Generalize redaction so the protection does not only recognize one OpenAI error pattern.

## Proposed redaction layer

Before displaying provider errors:

```go
func SanitizeProviderError(err error, secrets []string) error
```

Replace known configured secrets with:

```text
[REDACTED]
```

## Tests

```go
TestErrors_RedactsExactSecret
TestErrors_RedactsSecretInsideJSON
TestErrors_DoesNotModifyUnrelatedText
TestErrors_NeverLogsAPIKey
```

Reuse for:

```text
OpenAI-compatible keys
future provider tokens
HTTP auth headers
```

---

# 19. Milestone 16 — Embedding Integrity Checks

The zero-length Ollama vector issue is fixed.

Strengthen vector validation at one common boundary.

## Add shared validator

```go
func ValidateEmbedding(
    vec []float32,
    expectedDimensions int,
) error
```

Check:

```text
non-empty
correct dimensionality
all values finite
no NaN
no Inf
```

## Tests

```go
TestEmbeddingValidator_Empty
TestEmbeddingValidator_DimensionMismatch
TestEmbeddingValidator_NaN
TestEmbeddingValidator_Infinity
TestEmbeddingValidator_Valid
```

All providers should use the same validator.

---

# 20. Milestone 17 — Sync Observability

Incremental sync already works correctly:

```text
no-change → no indexing
changed page → update
deleted page → removed
site gone → library retained
```

Improve user visibility.

## Standard sync output

```text
Pages
  discovered: 39
  unchanged: 38
  changed: 1
  added: 0
  removed: 0

Chunks
  unchanged: 421
  updated: 14
  added: 0
  removed: 0

Embeddings generated: 14
```

This makes incremental behavior verifiable without debug logs.

---

# 21. Milestone 18 — Performance Baselines

Do not optimize blindly.

Record reproducible baselines:

```text
cold startup
warm startup
crawl duration
parse duration
embedding duration
Chroma write duration
first query latency
warm query latency
```

Current report:

```text
crawl ~2 s
parse/chunk <1 s
embedding ~5 s warm
query 90–256 ms warm
first query ~595 ms
```

## Add benchmark tests where practical

```go
BenchmarkChunker
BenchmarkResultFormatting
BenchmarkResolver
```

Avoid brittle wall-clock assertions in CI.

---

# 22. Milestone 19 — MCP stdio Hardening

The validation report found clean stdio behavior and no framing corruption.

Preserve that with integration tests.

## Required tests

```go
TestStdio_Initialize
TestStdio_ListTools
TestStdio_ResolveLibraryID
TestStdio_QueryDocs
TestStdio_LogsNeverReachStdout
TestStdio_InvalidRequestDoesNotCrash
TestStdio_Shutdown
```

All logs remain on:

```text
stderr
```

Protocol only:

```text
stdout
```

---

# 23. Milestone 20 — Resolve-Library Output Quality

`resolve-library-id` currently resolves Pi correctly.

Add output golden tests matching the intended Context7-like style.

Example:

```text
Available Libraries:

- Title: Pi
- Context7-compatible library ID: /local/pi/current
- Description: Pi documentation
- Indexed Chunks: 435
- Versions: current
- Source: https://pi.dev/docs/latest/
```

Ensure the corrected label remains:

```text
Indexed Chunks
```

rather than the old incorrect:

```text
Indexed Pages
```

The report specifically found this mismatch.

---

# 24. Milestone 21 — Robots Behavior Documentation

Keep RFC-style literal matching predictable.

Do not silently transform:

```text
Disallow: /path/
```

into:

```text
Disallow: /path
```

The validation report explicitly documents this normalization interaction rather than treating it as a bug.

## Action

Document the behavior in developer docs.

Keep regression test:

```text
TestRobots_TrailingSlashInDisallowDoesNotCoverTheNormalizedPath
```

Do not modify without a broader crawler policy decision.

---

# 25. Milestone 22 — Public Alpha Documentation

Before public alpha, README must cover:

## Installation

```bash
docmcp version
```

## Add docs

```bash
docmcp add https://pi.dev/docs/latest/ \
  --name pi \
  --version current
```

## Search

```bash
docmcp search \
  --library /local/pi/current \
  "How does MCP exposure work?"
```

## MCP

```json
{
  "mcpServers": {
    "docmcp": {
      "command": "docmcp",
      "args": ["serve"],
      "exposure": "codemode"
    }
  }
}
```

## MCP tools

```text
resolve-library-id
query-docs
```

## Storage/cache locations

Explain:

```text
data
model cache
Chroma runtime cache
```

## Limitations

Explicitly list:

```text
no JS rendering
no PDF
no Git repositories
HTML documentation only
local MCP over stdio
```

---

# 26. Public Alpha Release Gate

Do not release publicly until all of these hold.

## Correctness

```text
go test ./...            PASS
go test -race ./...      PASS
integration tests        PASS
provider tests           PASS
go vet                   PASS
lint                     PASS
```

## Retrieval

At minimum:

```text
synthetic Recall@5 does not regress
live benchmark >= 90% Top-5
no known ordinary query completely misses available content
```

Prefer:

```text
>= 80% Top-3
```

## MCP

```text
exactly 2 tools
schemas unchanged
annotations unchanged
stdio clean
Context7-style output stable
```

## Release

```text
goreleaser check PASS
snapshot binaries build
fresh install test PASS
```

## Security

```text
no secrets in provider errors
robots respected
no corrupted partial embeddings
```

---

# 27. Implementation Order

Recommended order:

```text
1. Freeze current regression suite

2. Expand retrieval benchmark

3. Add heading/title context to embedding input

4. Improve table/section context

5. Add adjacent-chunk deduplication

6. Re-run benchmarks

7. Add lexical candidate retrieval if still needed

8. Add RRF if lexical retrieval is added

9. Add only benchmark-justified structural boosts

10. Standardize query-docs output

11. Freeze MCP output/schema golden tests

12. Fix GoReleaser config

13. Improve first-run model progress/cache UX

14. Generalize secret redaction

15. Centralize embedding validation

16. Improve crawl/sync diagnostics

17. Add performance baseline benchmarks

18. Harden stdio integration tests

19. Complete README/public-alpha docs

20. Run full real-world validation again
```

---

# 28. Second Real-World Validation

After implementation, repeat the original validation against:

```text
https://pi.dev/docs/latest/
```

Compare directly against the previous run.

Record:

```text
documents
chunks
embedding count
crawl time
index time
database size
Top-1
Top-3
Top-5
MRR
Recall@5
query latency
```

The most important comparison is:

```text
Before:

Top-1 2/6
Top-3 4/6
Top-5 4/6
```

The next validation must demonstrate measurable retrieval improvement rather than merely different rankings.

---

# 29. Features to Defer

Do not add these during this implementation cycle:

```text
PDF
Git repositories
Git clone/update
JS browser rendering
Lightpanda integration
remote HTTP MCP
write MCP tools
web UI
multi-user support
LLM reranker
hosted service
```

They increase scope without addressing the blocker identified by the validation report.

---

# 30. Target State

At the end of this plan, DocMCP should have:

```text
reliable crawling
clean parsing
incremental indexing
validated embeddings
version isolation
stable local persistence
stronger hybrid/structural retrieval
Context7-compatible two-tool MCP interface
clean stdio transport
safe error handling
reproducible releases
documented public-alpha installation
```

The core workflow remains intentionally simple:

```text
docmcp add <documentation>
        ↓
crawl + index
        ↓
docmcp serve
        ↓
resolve-library-id
        ↓
query-docs
        ↓
accurate, source-backed documentation
```

The next major feature should only be considered after this workflow performs reliably on the live retrieval benchmark.
