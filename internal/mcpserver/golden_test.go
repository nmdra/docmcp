package mcpserver_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nmdra/docmcp/internal/mcpserver"
	"github.com/nmdra/docmcp/internal/search"
	"github.com/nmdra/docmcp/internal/store"
)

// Goldens are reviewed literals. Tests never create or update them.
func readMCPGolden(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "mcp", name))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	return data
}

func assertMCPTextGolden(t *testing.T, name, got string) {
	t.Helper()
	if want := string(readMCPGolden(t, name)); got != want {
		t.Fatalf("output differs from %s\nwant:\n%q\ngot:\n%q", name, want, got)
	}
}

func queryGoldenResults() []search.Result {
	return []search.Result{
		{
			Chunk: store.Chunk{
				ID: "internal-chunk-1", LibraryID: "/local/pi/0.99.2",
				URL: "https://pi.dev/docs/latest/mcp", Title: "Pi documentation",
				HeadingPath: "MCP > Control tool exposure",
				Content:     "  MCP servers using codemode exposure discover tools lazily.\n\nUse `exposure: codemode` to enable this behavior.  \n",
			},
			Score: 0.125,
		},
		{
			Chunk: store.Chunk{
				ID: "internal-chunk-2", LibraryID: "/local/pi/0.99.2",
				URL: "https://pi.dev/docs/latest/settings", Title: "Pi settings",
				Content: "Configure enabled tools in settings.\n\n```json\n{\"enabled\": true}\n```\n",
			},
			Score: 0.375,
		},
		{
			Chunk: store.Chunk{
				ID: "internal-chunk-3", LibraryID: "/local/pi/0.99.2",
				URL:     "https://pi.dev/docs/latest/commands",
				Content: "Use `/reload` to reload extensions.\n",
			},
			Score: 0.5,
		},
	}
}

func callQueryGolden(t *testing.T, results []search.Result) string {
	t.Helper()
	// The search seam uses the real public renderer on fixed results. This
	// freezes both formatting and MCP passthrough without a store or model.
	session := newTestClientWithOptions(t, mcpserver.Options{
		Version: "v0.1.0-test", Resolve: staticResolver(),
		Search: searcherFunc(func(_ context.Context, req mcpserver.SearchRequest) (string, error) {
			if req.LibraryID != "/local/pi/0.99.2" || req.Query != "tool exposure" {
				t.Errorf("unexpected search request: %+v", req)
			}
			return search.FormatAll(results), nil
		}),
	})
	got, err := callTool(t, session, "query-docs", map[string]any{
		"libraryId": "/local/pi/0.99.2", "query": "tool exposure",
	})
	if err != nil {
		t.Fatalf("call query-docs: %v", err)
	}
	return got
}

func TestQueryDocs_FormatSingleResult(t *testing.T) {
	assertMCPTextGolden(t, "query_single.golden", callQueryGolden(t, queryGoldenResults()[:1]))
}

func TestQueryDocs_FormatMultipleResults(t *testing.T) {
	// Also freezes title and URL fallback headings, result order, separators,
	// code fences, and omission of nonzero scores and internal chunk IDs.
	assertMCPTextGolden(t, "query_multiple.golden", callQueryGolden(t, queryGoldenResults()))
}

func TestResolveLibraryID_FormatLibraries(t *testing.T) {
	got, err := callTool(t, newTestClient(t), "resolve-library-id", map[string]any{
		"libraryName": "Pi", "query": "tool exposure",
	})
	if err != nil {
		t.Fatalf("call resolve-library-id: %v", err)
	}
	// Preserve the current labels, counts, version siblings, and blank lines.
	assertMCPTextGolden(t, "resolve_libraries.golden", got)
}

func TestResolveLibraryID_FormatNoResults(t *testing.T) {
	got, err := callTool(t, newTestClient(t), "resolve-library-id", map[string]any{
		"libraryName": "Missing", "query": "tool exposure",
	})
	if err != nil {
		t.Fatalf("call resolve-library-id: %v", err)
	}
	assertMCPTextGolden(t, "resolve_empty.golden", got)
}

func TestMCPServer_PublicContractGolden(t *testing.T) {
	toolsByName := listTools(t, newTestClient(t))
	tools := make([]*mcp.Tool, 0, len(toolsByName))
	for _, tool := range toolsByName {
		tools = append(tools, tool)
	}
	slices.SortFunc(tools, func(a, b *mcp.Tool) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
	gotJSON, err := json.Marshal(tools)
	if err != nil {
		t.Fatalf("marshal tools: %v", err)
	}
	// Compare JSON values, not SDK struct field order or indentation. The
	// complete advertised tools are frozen, including any newly added fields.
	var got, want any
	if err := json.Unmarshal(gotJSON, &got); err != nil {
		t.Fatalf("decode tools: %v", err)
	}
	if err := json.Unmarshal(readMCPGolden(t, "tools.golden.json"), &want); err != nil {
		t.Fatalf("decode contract golden: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		formatted, _ := json.MarshalIndent(got, "", "  ")
		t.Fatalf("public tools differ from tools.golden.json:\n%s", formatted)
	}
}
