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

// liveBenchmarkCases map a real question to the pi.dev section that answers it.
// wantURL is the page; wantHeading must appear in a top-3 result for a pass.
var liveBenchmarkCases = []struct {
	query      string
	wantURL    string
	wantSubstr string
}{
	{
		query:      "How are MCP codemode tools discovered?",
		wantURL:    "https://pi.dev/docs/latest/mcp",
		wantSubstr: "Control tool exposure",
	},
	{
		query:      "How do I add a default tool?",
		wantURL:    "https://pi.dev/docs/latest/settings",
		wantSubstr: "Tools",
	},
	{
		query:      "How is OAuth configured for an MCP server?",
		wantURL:    "https://pi.dev/docs/latest/mcp",
		wantSubstr: "OAuth",
	},
	{
		query:      "How are MCP tools hidden from the model?",
		wantURL:    "https://pi.dev/docs/latest/mcp",
		wantSubstr: "exposure",
	},
	{
		// The `direct` row of the exposure table lives in the "Control tool
		// exposure" section, so this query must surface that section.
		query:      "How does direct MCP exposure work?",
		wantURL:    "https://pi.dev/docs/latest/mcp",
		wantSubstr: "Control tool exposure",
	},
	{
		query:      "How do I use slash commands in the editor?",
		wantURL:    "https://pi.dev/docs/latest/slash-commands",
		wantSubstr: "",
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
	// The synthetic benchmark uses the fake embedder for speed. This one has to
	// use the real local model: a fake embedder's ranking says nothing about
	// whether a real documentation site is findable, and would report a
	// retrieval failure that is really a provider artefact.

	// A config file with no embedding section at all selects the built-in local
	// model, which is what a real user gets.
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("[crawler]\nmax_pages = 400\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	start := time.Now()
	if out, err := runRootWithConfig(t, cfg, configPath, "add", "https://pi.dev/docs/latest/",
		"--name", "pi", "--version", "current"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	t.Logf("crawl+index took %s", time.Since(start).Round(time.Millisecond))

	top1, top3, top5 := 0, 0, 0
	for _, tc := range liveBenchmarkCases {
		results := searchHeadings(t, cfg, configPath, "pi", tc.query, 5)

		rank := 0
		for i, h := range results {
			headingOK := tc.wantSubstr == "" ||
				strings.Contains(strings.ToLower(h.heading), strings.ToLower(tc.wantSubstr))
			if strings.Contains(h.url, tc.wantURL) && headingOK {
				rank = i + 1
				break
			}
		}

		switch {
		case rank == 1:
			top1++
		case rank >= 2 && rank <= 3:
			top3++
		case rank >= 4 && rank <= 5:
			top5++
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
		for i, h := range results {
			marker := "    "
			if i+1 == rank {
				marker = "-> "
			}
			t.Logf("      %s%d. [%s] %s", marker, i+1, h.heading, h.url)
		}
	}

	n := len(liveBenchmarkCases)
	t.Logf("live benchmark: top-1 %d/%d, top-3 %d/%d, top-5 %d/%d",
		top1, n, top1+top3, n, top1+top3+top5, n)

	// A regression floor, not a quality claim. The synthetic benchmark holds the
	// line at Recall@5 1.0; this one only has to notice if real-world chunking
	// stops being findable at all.
	//
	// The floor is n-2 because two queries are known to miss, recorded rather
	// than tuned away:
	//
	//   - "How does direct MCP exposure work?" reaches the right page but ranks
	//     a neighbouring section first. The exposure table is in "Control tool
	//     exposure"; the stronger lexical pull is "add servers from extensions".
	//   - "How do I use slash commands in the editor?" misses entirely. The
	//     page's descriptive text is a short intro chunk, and the per-command
	//     tables dominate the page, so a question about using slash commands
	//     does not lexically resemble the intro. Rephrasing to "slash commands
	//     available in the current session" finds it at rank 1.
	//
	// Both are retrieval-quality findings, not indexing faults: the content is
	// present and reachable. Fixing them means changing ranking, which needs its
	// own justification against this benchmark.
	const knownMisses = 2
	if top1+top3+top5 < n-knownMisses {
		t.Errorf("only %d of %d live queries found their section in the top 5 (floor allows %d known misses)",
			top1+top3+top5, n, knownMisses)
	}
}

type headingHit struct {
	heading string
	url     string
}

// searchHeadings runs a library-scoped search and reads back the ranked
// headings the CLI printed.
func searchHeadings(t *testing.T, cfg config.Config, configPath, lib, query string, limit int) []headingHit {
	t.Helper()

	out, err := runRootWithConfig(t, cfg, configPath, "search", query,
		"--library", "/local/"+lib+"/current")
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
				break
			}
			if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "Library: ") {
				// The chunk body has started; nothing after this is metadata.
				break
			}
			_ = i
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
