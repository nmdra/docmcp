package app_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docmcp/docmcp/internal/config"
)

// TestLivePiBenchmark measures retrieval against the real pi.dev index.
//
// The synthetic benchmark in benchmark_test.go guards ranking between clean
// synthetic chunks. This one guards the two things synthetic data cannot: that
// a real documentation site is chunked into sections an agent can find, and
// that chrome stripping did not remove the heading structure ranking relies on.
//
// It is opt-in for the same reason the provider tests are: it needs the ~190 MB
// local model and network access to crawl pi.dev.
func TestLivePiBenchmark(t *testing.T) {
	liveBenchmark(t)
}

// liveBenchmarkCases are declarative questions and relevance expectations for
// the real pi.dev index. The set spans exact names, paraphrases, headings,
// configuration, authentication, short identifiers, and multiword concepts.
var liveBenchmarkCases = []retrievalBenchmarkCase{
	{
		query:         "How are MCP codemode tools discovered?",
		libraryID:     "/local/pi/current",
		expectedURLs:  []string{"https://pi.dev/docs/latest/mcp"},
		expectedHeads: []string{"Control tool exposure"},
		baseline:      true,
	},
	{
		query:         "How do I add a default tool?",
		libraryID:     "/local/pi/current",
		expectedURLs:  []string{"https://pi.dev/docs/latest/settings"},
		expectedHeads: []string{"Tools"},
		baseline:      true,
	},
	{
		query:         "How is OAuth configured for an MCP server?",
		libraryID:     "/local/pi/current",
		expectedURLs:  []string{"https://pi.dev/docs/latest/mcp"},
		expectedHeads: []string{"OAuth"},
		baseline:      true,
	},
	{
		query:         "How are MCP tools hidden from the model?",
		libraryID:     "/local/pi/current",
		expectedURLs:  []string{"https://pi.dev/docs/latest/mcp"},
		expectedHeads: []string{"exposure"},
		baseline:      true,
	},
	{
		// The `direct` row of the exposure table lives in the "Control tool
		// exposure" section, so this query must surface that section.
		query:           "How does direct MCP exposure work?",
		libraryID:       "/local/pi/current",
		expectedURLs:    []string{"https://pi.dev/docs/latest/mcp"},
		expectedHeads:   []string{"Control tool exposure"},
		expectedContent: []string{"direct"},
		baseline:        true,
	},
	{
		query:        "How do I use slash commands in the editor?",
		libraryID:    "/local/pi/current",
		expectedURLs: []string{"https://pi.dev/docs/latest/slash-commands"},
		baseline:     true,
	},
	{
		query:         "What does codemode exposure do?",
		libraryID:     "/local/pi/current",
		expectedURLs:  []string{"https://pi.dev/docs/latest/mcp"},
		expectedHeads: []string{"Control tool exposure"},
	},
	{
		query:         "How do I hide an MCP tool?",
		libraryID:     "/local/pi/current",
		expectedURLs:  []string{"https://pi.dev/docs/latest/mcp"},
		expectedHeads: []string{"Control tool exposure"},
	},
	{
		query:         "How is oauth.clientName configured?",
		libraryID:     "/local/pi/current",
		expectedURLs:  []string{"https://pi.dev/docs/latest/mcp"},
		expectedHeads: []string{"OAuth"},
	},
	{
		query:        "What slash commands are available?",
		libraryID:    "/local/pi/current",
		expectedURLs: []string{"https://pi.dev/docs/latest/slash-commands"},
	},
	{
		query:         "How do I enable a tool by default?",
		libraryID:     "/local/pi/current",
		expectedURLs:  []string{"https://pi.dev/docs/latest/settings"},
		expectedHeads: []string{"Tools"},
	},
	{
		query:           "What does /reload do?",
		libraryID:       "/local/pi/current",
		expectedURLs:    []string{"https://pi.dev/docs/latest/slash-commands"},
		expectedHeads:   []string{"Runtime and project"},
		expectedContent: []string{"Reload keybindings"},
	},
}

func liveBenchmark(t *testing.T) {
	t.Helper()

	if os.Getenv("DOCMCP_TEST_LIVE_BENCH") == "" {
		t.Skip("set DOCMCP_TEST_LIVE_BENCH=1 to crawl pi.dev and measure retrieval")
	}

	dataDir := t.TempDir()
	cfg := newTestCommand(t)
	cfg.Data.Path = dataDir
	// Both retrieval benchmarks use the real local model. A fake embedder's
	// ranking says nothing about whether real documentation is findable.

	// A config file with no embedding section at all selects the built-in local
	// model, which is what a real user gets.
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("[crawler]\nmax_pages = 400\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// A second configuration exposes the full candidate budget for diagnostics
	// only. Quality metrics below still measure the normal result budget.
	candidateConfigPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(candidateConfigPath, []byte("[search]\ncandidate_count = 20\nfinal_chunks = 20\n"), 0o600); err != nil {
		t.Fatalf("write candidate config: %v", err)
	}

	start := time.Now()
	if out, err := runRootWithConfig(t, cfg, configPath, "add", "https://pi.dev/docs/latest/",
		"--name", "pi", "--version", "current"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	t.Logf("crawl+index took %s", time.Since(start).Round(time.Millisecond))

	ranked := make(map[string][]headingHit, len(liveBenchmarkCases))
	baselineCases, baselineTop5 := 0, 0
	for _, tc := range liveBenchmarkCases {
		results := searchHeadings(t, cfg, configPath, tc.libraryID, tc.query, 5)
		ranked[tc.query] = results

		rank := 0
		for i, hit := range results {
			if benchmarkResultMatches(tc, hit) {
				rank = i + 1
				break
			}
		}
		if tc.baseline {
			baselineCases++
			if rank > 0 && rank <= 5 {
				baselineTop5++
			}
		}

		status := "MISS"
		switch {
		case rank == 1:
			status = "top-1"
		case rank > 1 && rank <= 3:
			status = "top-3"
		case rank > 3 && rank <= 5:
			status = "top-5"
		}
		t.Logf("%-7s rank=%d %q", status, rank, tc.query)
		if rank == 0 {
			candidates := searchHeadings(t, cfg, candidateConfigPath, tc.libraryID, tc.query, 20)
			candidateRank := 0
			for i, hit := range candidates {
				if benchmarkResultMatches(tc, hit) {
					candidateRank = i + 1
					break
				}
			}
			t.Logf("candidate diagnostic: relevant rank=%d among %d candidates", candidateRank, len(candidates))
		}
		for i, hit := range results {
			marker := "    "
			if i+1 == rank {
				marker = "-> "
			}
			t.Logf("      %s%d. [%s] %s", marker, i+1, hit.heading, hit.url)
		}
	}

	metrics := measureRetrievalBenchmark(liveBenchmarkCases, ranked)
	t.Logf("live benchmark: Top-1 %.1f%%, Top-3 %.1f%%, Top-5 %.1f%%, MRR %.3f, Recall@5 %.3f",
		metrics.Top1*100, metrics.Top3*100, metrics.Top5*100, metrics.MRR, metrics.RecallAt5)
	t.Logf("original live cases in Top-5: %d/%d", baselineTop5, baselineCases)

	if metrics.Top5 < 0.90 || metrics.Top3 < 0.80 {
		t.Errorf("live quality gate failed: Top-5 %.3f (want >= .90), Top-3 %.3f (want >= .80)",
			metrics.Top5, metrics.Top3)
	}
	// Preserve the improved original subset independently from new queries.
	// Direct exposure remains a known miss requiring further ranking work.
	const knownMisses = 1
	if baselineCases == 0 || baselineTop5 < baselineCases-knownMisses {
		t.Errorf("only %d of %d original live queries found their section in the top 5; want at least %d",
			baselineTop5, baselineCases, baselineCases-knownMisses)
	}
}

type headingHit struct {
	heading string
	url     string
	content string
}

// searchHeadings runs a library-scoped search and reads back the ranked
// headings the CLI printed.
func searchHeadings(t *testing.T, cfg config.Config, configPath, libraryID, query string, limit int) []headingHit {
	t.Helper()

	out, err := runRootWithConfig(t, cfg, configPath, "search", query,
		"--library", libraryID)
	if err != nil {
		t.Fatalf("search %q: %v", query, err)
	}

	// Results are separated by "---". Each block starts with "### <heading>"
	// followed by "Source: <url>"; only the block header counts, because a
	// chunk's own Markdown body can contain its own "### " lines.
	var hits []headingHit
	for _, block := range strings.Split(out, "\n---\n") {
		lines := strings.Split(block, "\n")

		hit := headingHit{}
		for i, line := range lines {
			line = strings.TrimSpace(line)

			if strings.HasPrefix(line, "### ") && hit.heading == "" {
				hit.heading = strings.TrimPrefix(line, "### ")
				continue
			}
			if strings.HasPrefix(line, "Source: ") {
				hit.url = strings.TrimPrefix(line, "Source: ")
				continue
			}
			if strings.HasPrefix(line, "Library: ") {
				hit.content = strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
				break
			}
			if strings.HasPrefix(line, "## ") {
				break
			}
		}
		if hit.heading != "" {
			hits = append(hits, hit)
		}
	}

	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}
