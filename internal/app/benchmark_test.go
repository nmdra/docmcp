package app_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// benchmarkDoc is one synthetic documentation chunk with the topic it belongs
// to. The benchmark is a regression detector, not a claim about absolute
// quality: it exists so a retrieval change that quietly degrades results fails a
// test.
type benchmarkDoc struct {
	topic string
	path  string
	title string
	md    string
}

// benchmarkCorpus covers the topics the plan names. Each topic has several
// chunks so ranking within a topic is actually exercised, not just
// topic-identification.
func benchmarkCorpus() []benchmarkDoc {
	return []benchmarkDoc{
		{"authentication", "/docs/auth/tokens", "Authentication > Bearer tokens",
			"# Authentication\n\n## Bearer tokens\n\nSend the access token in the " +
				"Authorization header as a bearer credential. Tokens expire after one hour " +
				"and are refreshed with the refresh token."},
		{"authentication", "/docs/auth/oauth", "Authentication > OAuth flow",
			"# Authentication\n\n## OAuth flow\n\nThe OAuth 2.0 authorization code flow " +
				"exchanges an authorization code for an access token. Register a redirect URI " +
				"and exchange the code server-side."},
		{"authentication", "/docs/auth/keys", "Authentication > API keys",
			"# Authentication\n\n## API keys\n\nStatic API keys authenticate server-to-server " +
				"calls. Rotate keys by creating a replacement before revoking the old key."},
		{"authentication", "/docs/auth/passwords", "Authentication > Password hashing",
			"# Authentication\n\n## Password hashing\n\nPasswords are hashed with a " +
				"memory-hard function such as Argon2id, never stored in plain text."},

		{"routing", "/docs/routing/paths", "Routing > Path patterns",
			"# Routing\n\n## Path patterns\n\nRoutes map a URL path to a handler. Patterns " +
				"may contain parameters such as /users/{id} which are passed to the handler."},
		{"routing", "/docs/routing/methods", "Routing > HTTP methods",
			"# Routing\n\n## HTTP methods\n\nRegister a route for a specific HTTP method " +
				"such as GET or POST. A route with no method matches every method."},
		{"routing", "/docs/routing/middleware", "Routing > Middleware",
			"# Routing\n\n## Middleware\n\nMiddleware wraps a handler to run code before and " +
				"after it, for logging, authentication, or response headers."},
		{"routing", "/docs/routing/groups", "Routing > Route groups",
			"# Routing\n\n## Route groups\n\nGroup related routes under a common prefix so " +
				"middleware and parameters apply to the whole group."},

		{"caching", "/docs/cache/ttl", "Caching > Time to live",
			"# Caching\n\n## Time to live\n\nEvery cache entry has a time to live. When the " +
				"TTL expires the entry is evicted and the next read repopulates it."},
		{"caching", "/docs/cache/invalidation", "Caching > Invalidation",
			"# Caching\n\n## Invalidation\n\nInvalidate a cache entry explicitly when the " +
				"underlying data changes, rather than waiting for the TTL to expire."},
		{"caching", "/docs/cache/keys", "Caching > Key design",
			"# Caching\n\n## Key design\n\nA cache key must encode every input that affects " +
				"the response, or two different requests will collide."},
		{"caching", "/docs/cache/eviction", "Caching > Eviction policy",
			"# Caching\n\n## Eviction policy\n\nLeast-recently-used eviction discards the " +
				"entry that has gone unused the longest when the cache is full."},

		{"sessions", "/docs/sessions/storage", "Sessions > Storage",
			"# Sessions\n\n## Storage\n\nA session is stored server-side under an opaque " +
				"identifier that the client sends on every request."},
		{"sessions", "/docs/sessions/expiry", "Sessions > Expiry",
			"# Sessions\n\n## Expiry\n\nSessions expire after a period of inactivity. Refresh " +
				"the session cookie on each authenticated response to keep it alive."},
		{"sessions", "/docs/sessions/cookies", "Sessions > Cookies",
			"# Sessions\n\n## Cookies\n\nSet the session cookie with HttpOnly and Secure " +
				"flags so client-side script cannot read it."},

		{"middleware", "/docs/mw/order", "Middleware > Execution order",
			"# Middleware\n\n## Execution order\n\nMiddleware runs in registration order on " +
				"the way in, and in reverse order on the way out."},
		{"middleware", "/docs/mw/errors", "Middleware > Error handling",
			"# Middleware\n\n## Error handling\n\nA middleware that recovers from a panic " +
				"prevents one failed request from taking down the process."},

		{"deployment", "/docs/deploy/containers", "Deployment > Containers",
			"# Deployment\n\n## Containers\n\nBuild one container image and promote it " +
				"unchanged through environments, so what you test is what you ship."},
		{"deployment", "/docs/deploy/health", "Deployment > Health checks",
			"# Deployment\n\n## Health checks\n\nA readiness probe reports whether the " +
				"instance can serve traffic, separate from liveness."},

		{"configuration", "/docs/config/env", "Configuration > Environment variables",
			"# Configuration\n\n## Environment variables\n\nRead configuration from environment " +
				"variables so a deployment can change settings without rebuilding."},
		{"configuration", "/docs/config/files", "Configuration > Config files",
			"# Configuration\n\n## Config files\n\nA config file holds defaults; environment " +
				"variables override them at startup."},

		{"logging", "/docs/log/levels", "Logging > Levels",
			"# Logging\n\n## Levels\n\nLog at error for conditions needing attention, warn for " +
				"recoverable problems, info for lifecycle events, and debug only while " +
				"diagnosing."},
		{"logging", "/docs/log/structure", "Logging > Structured fields",
			"# Logging\n\n## Structured fields\n\nEmit machine-readable fields alongside the " +
				"message so logs can be queried rather than grepped."},

		{"database", "/docs/db/migrations", "Database > Migrations",
			"# Database\n\n## Migrations\n\nA migration changes the schema and must be " +
				"reversible. Run migrations before deploying code that depends on them."},
		{"database", "/docs/db/transactions", "Database > Transactions",
			"# Database\n\n## Transactions\n\nWrap a write sequence in a transaction so a " +
				"failure midway rolls back the whole change."},

		{"mcp-tools", "/docs/mcp/tools", "MCP tools > query-docs",
			"# MCP tools\n\n## query-docs\n\nCall `query-docs` with a library ID and a " +
				"natural-language query to retrieve documentation. Call " +
				"`resolve-library-id` first when the user has not supplied an exact ID."},
		{"cli", "/docs/cli/add", "CLI > docmcp add",
			"# CLI\n\n## docmcp add\n\nRun `docmcp add <url> --name <name>` to crawl a " +
				"documentation website and add its pages to the local index."},
		{"versions", "/docs/library/versions", "Libraries > Version-specific IDs",
			"# Libraries\n\n## Version-specific IDs\n\nUse `/local/pi/0.99.2` to query " +
				"that pinned documentation release. `/local/pi/current` selects the " +
				"current indexed version instead."},
	}
}

// benchmarkQuery is one question with the topic whose chunks should answer it.
// The phrasing is deliberately different from the chunk wording: a benchmark that
// only matched on shared keywords would not detect a semantic regression.
type benchmarkQuery struct {
	text string

	// topic is the group of chunks that can answer the question.
	topic string

	// gold is the single chunk that best answers it. Recall is scored on topic;
	// MRR is scored on this chunk, so a change that reorders results is caught
	// even though every chunk in the topic is technically relevant.
	gold string
}

// benchmarkQueries deliberately paraphrase rather than reuse chunk wording: a
// benchmark whose queries copy the corpus verbatim would pass even with a
// keyword matcher, and so would not detect a semantic regression.
func baselineBenchmarkQueries() []benchmarkQuery {
	return []benchmarkQuery{
		// Queries whose vocabulary differs from the chunk they should find.
		{"authenticate a caller without a browser", "authentication", "/docs/auth/oauth"},
		{"swap a credential without dropping traffic", "authentication", "/docs/auth/keys"},
		{"which scheme signs me in from a native app", "authentication", "/docs/auth/tokens"},
		{"point one name at a different handler", "routing", "/docs/routing/paths"},
		{"only allow GET on this endpoint", "routing", "/docs/routing/methods"},
		{"apply the same checks to a whole subtree of endpoints", "routing", "/docs/routing/middleware"},
		{"stop serving a value the client already has", "caching", "/docs/cache/ttl"},
		{"force a fresh read after a write", "caching", "/docs/cache/invalidation"},
		{"two users get the same cached page", "caching", "/docs/cache/keys"},
		{"what happens when the cache has no room", "caching", "/docs/cache/eviction"},
		{"keep a user identified across visits", "sessions", "/docs/sessions/storage"},
		{"stop a stolen cookie being read by script", "sessions", "/docs/sessions/cookies"},
		{"remember a user between page loads", "sessions", "/docs/sessions/expiry"},
		{"how do I authenticate a request", "authentication", "/docs/auth/tokens"},
		{"what is an OAuth authorization code flow", "authentication", "/docs/auth/oauth"},
		{"rotate an API key without downtime", "authentication", "/docs/auth/keys"},
		{"how should passwords be stored", "authentication", "/docs/auth/passwords"},
		{"how do URL paths map to handlers", "routing", "/docs/routing/paths"},
		{"how do I register a route for one HTTP verb", "routing", "/docs/routing/methods"},
		{"run code before and after a handler", "routing", "/docs/routing/middleware"},
		{"apply middleware to many routes at once", "routing", "/docs/routing/groups"},
		{"when does a cached entry get dropped", "caching", "/docs/cache/ttl"},
		{"drop a cached value after the data changes", "caching", "/docs/cache/invalidation"},
		{"what makes a cache key safe", "caching", "/docs/cache/keys"},
		{"which entry is discarded when the cache fills", "caching", "/docs/cache/eviction"},
		{"how long does a session stay valid", "sessions", "/docs/sessions/expiry"},
		{"keep a user signed in across requests", "sessions", "/docs/sessions/cookies"},
		{"stop client script reading the session cookie", "sessions", "/docs/sessions/cookies"},
		{"what order does middleware run in", "middleware", "/docs/mw/order"},
		{"stop one bad request from crashing the server", "middleware", "/docs/mw/errors"},
		{"ship the same build to staging and production", "deployment", "/docs/deploy/containers"},
		{"tell the orchestrator if an instance can take traffic", "deployment", "/docs/deploy/health"},
		{"change settings without rebuilding", "configuration", "/docs/config/env"},
		{"where do settings come from at startup", "configuration", "/docs/config/files"},
		{"which severity should I use for a failure", "logging", "/docs/log/levels"},
		{"make logs searchable by field", "logging", "/docs/log/structure"},
		{"change the database schema safely", "database", "/docs/db/migrations"},
		{"undo a half-finished write", "database", "/docs/db/transactions"},
	}
}

func benchmarkQueries() []benchmarkQuery {
	return append(baselineBenchmarkQueries(), []benchmarkQuery{
		// Exact tool names, a CLI command, and version-specific behavior extend
		// coverage beyond the generic API concepts above.
		{"what does query-docs accept", "mcp-tools", "/docs/mcp/tools"},
		{"index a documentation website from the command line", "cli", "/docs/cli/add"},
		{"how do I query a pinned release instead of current", "versions", "/docs/library/versions"},
	}...)
}

// benchmarkFloors preserve the pre-change synthetic baseline while applying
// the proposed public-alpha recall and MRR gates.
//
// The original 38-query baseline on all-MiniLM-L6-v2 measured Recall@5 1.00 and
// MRR 0.86. Deliberately reversing result order moved MRR to 0.67, confirming
// that the metric detects ranking regressions rather than merely reporting a
// number.
const (
	recallAt5Floor = 1.00
	mrrFloor       = 0.86
)

func TestRetrievalBenchmark_RecallAt5(t *testing.T) {
	requireLocalProvider(t)

	engine, docs := benchmarkEngine(t)

	recall := benchmarkRecall(engine, docs, 5)
	t.Logf("Recall@5 = %.3f (floor %.2f)", recall, recallAt5Floor)

	if recall < recallAt5Floor {
		t.Errorf("Recall@5 = %.3f, below the agreed floor %.2f", recall, recallAt5Floor)
	}
}

func TestRetrievalBenchmark_MRR(t *testing.T) {
	requireLocalProvider(t)

	engine, _ := benchmarkEngine(t)

	mrr := benchmarkMRR(engine, benchmarkQueries())
	baselineMRR := benchmarkMRR(engine, baselineBenchmarkQueries())
	t.Logf("MRR = %.3f, original subset MRR = %.3f (floor %.2f)", mrr, baselineMRR, mrrFloor)
	if baselineMRR < mrrFloor {
		t.Errorf("original subset MRR = %.3f, below baseline %.2f", baselineMRR, mrrFloor)
	}

	if mrr < mrrFloor {
		t.Errorf("MRR = %.3f, below the agreed floor %.2f", mrr, mrrFloor)
	}
}

func TestRetrievalBenchmark_TopKAccuracy(t *testing.T) {
	requireLocalProvider(t)

	engine, _ := benchmarkEngine(t)
	top1 := benchmarkGoldAccuracy(engine, 1)
	top3 := benchmarkGoldAccuracy(engine, 3)
	top5 := benchmarkGoldAccuracy(engine, 5)
	t.Logf("Top-1 = %.3f, Top-3 = %.3f, Top-5 = %.3f", top1, top3, top5)

	if top3 < 0.80 {
		t.Errorf("Top-3 = %.3f, below the proposed floor 0.80", top3)
	}
	if top5 < 0.90 {
		t.Errorf("Top-5 = %.3f, below the proposed floor 0.90", top5)
	}
}

// TestRetrievalBenchmark_ReportsPerQueryFailures turns a threshold breach into an
// actionable list. A single aggregate number tells you something broke, not what.
func TestRetrievalBenchmark_ReportsPerQueryFailures(t *testing.T) {
	requireLocalProvider(t)

	engine, docs := benchmarkEngine(t)

	var missed []string
	for _, q := range benchmarkQueries() {
		hits := benchmarkHits(engine, q.text, 5)
		if !anyHitsTopic(hits, docs, q.topic) {
			missed = append(missed, fmt.Sprintf("%q (want topic %q)", q.text, q.topic))
		}
	}

	sort.Strings(missed)

	t.Logf("%d of %d queries found no chunk from the wanted topic in the top 5",
		len(missed), len(benchmarkQueries()))
	for _, m := range missed {
		t.Logf("  missed: %s", m)
	}

	// Some misses are expected and acceptable; a large majority is not.
	if len(missed) > 5 {
		t.Errorf("%d of %d queries missed entirely:\n%s",
			len(missed), len(benchmarkQueries()), strings.Join(missed, "\n"))
	}
}

func TestRetrievalBenchmark_IsDeterministic(t *testing.T) {
	requireLocalProvider(t)

	engine, _ := benchmarkEngine(t)

	first := benchmarkHits(engine, benchmarkQueries()[0].text, 5)
	for range 3 {
		again := benchmarkHits(engine, benchmarkQueries()[0].text, 5)
		for i := range first {
			if first[i].Chunk.ID != again[i].Chunk.ID {
				t.Fatalf("retrieval order changed between runs at %d: %q then %q",
					i, first[i].Chunk.ID, again[i].Chunk.ID)
			}
		}
	}
}

type retrievalBenchmarkCase struct {
	query           string
	libraryID       string
	expectedURLs    []string
	expectedHeads   []string
	expectedContent []string
	baseline        bool
}

type retrievalBenchmarkMetrics struct {
	Top1      float64
	Top3      float64
	Top5      float64
	MRR       float64
	RecallAt5 float64
}

func measureRetrievalBenchmark(
	cases []retrievalBenchmarkCase,
	resultsByQuery map[string][]headingHit,
) retrievalBenchmarkMetrics {
	if len(cases) == 0 {
		return retrievalBenchmarkMetrics{}
	}

	var metrics retrievalBenchmarkMetrics
	for _, tc := range cases {
		hits := resultsByQuery[tc.query]
		firstRelevantRank := 0
		matchedURLs := make(map[string]struct{}, len(tc.expectedURLs))
		for i, hit := range hits {
			if benchmarkResultMatches(tc, hit) && firstRelevantRank == 0 {
				firstRelevantRank = i + 1
			}
			if i >= 5 {
				continue
			}
			for _, expectedURL := range tc.expectedURLs {
				if hit.url == expectedURL && benchmarkHeadingMatches(tc, hit.heading) &&
					benchmarkContentMatches(tc, hit.content) {
					matchedURLs[expectedURL] = struct{}{}
				}
			}
		}

		if firstRelevantRank == 1 {
			metrics.Top1++
		}
		if firstRelevantRank > 0 && firstRelevantRank <= 3 {
			metrics.Top3++
		}
		if firstRelevantRank > 0 && firstRelevantRank <= 5 {
			metrics.Top5++
		}
		if firstRelevantRank > 0 {
			metrics.MRR += 1 / float64(firstRelevantRank)
		}
		if len(tc.expectedURLs) > 0 {
			metrics.RecallAt5 += float64(len(matchedURLs)) / float64(len(tc.expectedURLs))
		}
	}

	count := float64(len(cases))
	metrics.Top1 /= count
	metrics.Top3 /= count
	metrics.Top5 /= count
	metrics.MRR /= count
	metrics.RecallAt5 /= count
	return metrics
}

func benchmarkResultMatches(tc retrievalBenchmarkCase, hit headingHit) bool {
	for _, expectedURL := range tc.expectedURLs {
		if hit.url == expectedURL && benchmarkHeadingMatches(tc, hit.heading) &&
			benchmarkContentMatches(tc, hit.content) {
			return true
		}
	}
	return false
}

func benchmarkHeadingMatches(tc retrievalBenchmarkCase, heading string) bool {
	return benchmarkContainsAny(heading, tc.expectedHeads)
}

func benchmarkContentMatches(tc retrievalBenchmarkCase, content string) bool {
	return benchmarkContainsAny(content, tc.expectedContent)
}

func benchmarkContainsAny(value string, expected []string) bool {
	if len(expected) == 0 {
		return true
	}
	for _, match := range expected {
		if strings.Contains(strings.ToLower(value), strings.ToLower(match)) {
			return true
		}
	}
	return false
}

func TestRetrievalBenchmark_MetricsUseExpectedURLHeadingAndContent(t *testing.T) {
	cases := []retrievalBenchmarkCase{
		{
			query:           "direct exposure",
			libraryID:       "/local/pi/current",
			expectedURLs:    []string{"https://pi.dev/docs/latest/mcp"},
			expectedHeads:   []string{"Control tool exposure"},
			expectedContent: []string{"direct"},
		},
		{
			query:         "oauth",
			libraryID:     "/local/pi/current",
			expectedURLs:  []string{"https://pi.dev/docs/latest/mcp"},
			expectedHeads: []string{"OAuth"},
		},
		{
			query:        "slash commands",
			libraryID:    "/local/pi/current",
			expectedURLs: []string{"https://pi.dev/docs/latest/slash-commands"},
		},
	}
	results := map[string][]headingHit{
		"direct exposure": {
			{heading: "Control tool exposure", url: "https://pi.dev/docs/latest/mcp", content: "MCP authentication settings."},
			{heading: "Control tool exposure", url: "https://pi.dev/docs/latest/mcp", content: "Direct tools are exposed here."},
		},
		"oauth": {
			{heading: "Authenticate with OAuth", url: "https://pi.dev/docs/latest/mcp", content: "OAuth configuration."},
		},
		"slash commands": {
			{heading: "Text editing", url: "https://pi.dev/docs/latest/actions"},
			{heading: "Actions", url: "https://pi.dev/docs/latest/actions"},
			{heading: "Editor", url: "https://pi.dev/docs/latest/editor"},
			{heading: "Commands", url: "https://pi.dev/docs/latest/commands"},
			{heading: "Slash Commands", url: "https://pi.dev/docs/latest/slash-commands"},
		},
	}

	got := measureRetrievalBenchmark(cases, results)
	if got.Top1 != 1.0/3 || got.Top3 != 2.0/3 || got.Top5 != 1.0 {
		t.Errorf("top-k accuracy = (%.3f, %.3f, %.3f), want (0.333, 0.667, 1.000)",
			got.Top1, got.Top3, got.Top5)
	}
	if got.MRR < 0.566 || got.MRR > 0.567 {
		t.Errorf("MRR = %.3f, want approximately 0.567", got.MRR)
	}
	if got.RecallAt5 != 1.0 {
		t.Errorf("Recall@5 = %.3f, want 1.000", got.RecallAt5)
	}
}
