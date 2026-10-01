package mcpserver_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/docmcp/docmcp/internal/mcpserver"
	"github.com/docmcp/docmcp/internal/search"
)

func staticResolver() mcpserver.Resolver {
	return resolverFunc(func(_ context.Context, name, query string) ([]search.Match, error) {
		if name == "Missing" {
			return nil, nil
		}
		return []search.Match{
			{
				LibraryID:    "/local/pi",
				Name:         "Pi",
				Description:  "Pi coding agent documentation",
				IndexedPages: 61,
			},
			{
				LibraryID:    "/local/pi/0.99.2",
				Name:         "Pi",
				Version:      "0.99.2",
				Description:  "Pi coding agent documentation",
				IndexedPages: 61,
			},
			{
				LibraryID:    "/local/pi-sdk",
				Name:         "Pi SDK",
				Description:  "Pi SDK reference",
				IndexedPages: 24,
			},
		}, nil
	})
}

func staticSearcher() mcpserver.Searcher {
	return searcherFunc(func(_ context.Context, req mcpserver.SearchRequest) (string, error) {
		if req.LibraryID == "/local/missing/1" {
			return "", mcpserver.ErrLibraryNotFound
		}
		if req.Query == "" {
			return "", mcpserver.ErrEmptyQuery
		}
		return "### MCP > Control tool exposure\n\n" +
			"Source: https://pi.dev/docs/latest/mcp\n" +
			"Library: " + req.LibraryID + "\n\n" +
			"MCP servers using codemode exposure discover tools lazily.\n", nil
	})
}

// callTool invokes a tool and returns the joined text content.
func callTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) (string, error) {
	t.Helper()

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		return "", err
	}

	var text strings.Builder
	for _, content := range result.Content {
		if tc, ok := content.(*mcp.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}
	return text.String(), nil
}

func TestResolveLibraryIDTool_ReturnsLibraries(t *testing.T) {
	session := newTestClient(t)

	got, err := callTool(t, session, "resolve-library-id", map[string]any{
		"libraryName": "Pi",
		"query":       "codemode exposure",
	})
	if err != nil {
		t.Fatalf("callTool: %v", err)
	}

	for _, want := range []string{"Available Libraries", "/local/pi", "/local/pi/0.99.2"} {
		if !strings.Contains(got, want) {
			t.Errorf("resolve output missing %q:\n%s", want, got)
		}
	}
}

func TestResolveLibraryIDTool_ShowsDescriptions(t *testing.T) {
	session := newTestClient(t)

	got, err := callTool(t, session, "resolve-library-id", map[string]any{
		"libraryName": "Pi",
		"query":       "exposure",
	})
	if err != nil {
		t.Fatalf("callTool: %v", err)
	}

	if !strings.Contains(got, "Pi coding agent documentation") {
		t.Errorf("resolve output missing descriptions:\n%s", got)
	}
	if !strings.Contains(got, "Indexed Pages") && !strings.Contains(got, "Pages") {
		t.Errorf("resolve output missing page counts:\n%s", got)
	}
}

func TestResolveLibraryIDTool_MissingQuery(t *testing.T) {
	session := newTestClient(t)

	_, err := callTool(t, session, "resolve-library-id", map[string]any{"libraryName": "Pi"})
	if err == nil {
		t.Error("resolve without a query succeeded, want error")
	}
}

func TestResolveLibraryIDTool_MissingLibraryName(t *testing.T) {
	session := newTestClient(t)

	_, err := callTool(t, session, "resolve-library-id", map[string]any{"query": "exposure"})
	if err == nil {
		t.Error("resolve without a library name succeeded, want error")
	}
}

func TestResolveLibraryIDTool_EmptyLibraryName(t *testing.T) {
	session := newTestClient(t)

	_, err := callTool(t, session, "resolve-library-id", map[string]any{
		"libraryName": "   ",
		"query":       "exposure",
	})
	if err == nil {
		t.Error("resolve with a blank library name succeeded, want error")
	}
}

func TestResolveLibraryIDTool_NoResults(t *testing.T) {
	session := newTestClient(t)

	got, err := callTool(t, session, "resolve-library-id", map[string]any{
		"libraryName": "Missing",
		"query":       "anything",
	})
	if err != nil {
		t.Fatalf("callTool: %v", err)
	}

	// An empty result must say so; blank output reads like a bug.
	if strings.TrimSpace(got) == "" {
		t.Error("resolve with no matches returned empty output")
	}
	if !strings.Contains(strings.ToLower(got), "no ") {
		t.Errorf("resolve with no matches should say so:\n%s", got)
	}
}

func TestResolveLibraryIDTool_MultipleVersions(t *testing.T) {
	session := newTestClient(t)

	got, err := callTool(t, session, "resolve-library-id", map[string]any{
		"libraryName": "Pi",
		"query":       "exposure",
	})
	if err != nil {
		t.Fatalf("callTool: %v", err)
	}

	if !strings.Contains(got, "/local/pi-sdk") {
		t.Errorf("resolve output should list every candidate:\n%s", got)
	}
}

func TestResolveLibraryIDTool_DoesNotExposeInternalIDs(t *testing.T) {
	session := newTestClient(t)

	got, err := callTool(t, session, "resolve-library-id", map[string]any{
		"libraryName": "Pi",
		"query":       "exposure",
	})
	if err != nil {
		t.Fatalf("callTool: %v", err)
	}

	lower := strings.ToLower(got)
	for _, leak := range []string{"chroma", "collection", "docmcp_chunks", "vector"} {
		if strings.Contains(lower, leak) {
			t.Errorf("resolve output leaks internal detail %q:\n%s", leak, got)
		}
	}
}

func TestQueryDocsTool_ReturnsDocumentation(t *testing.T) {
	session := newTestClient(t)

	got, err := callTool(t, session, "query-docs", map[string]any{
		"libraryId": "/local/pi/0.99.2",
		"query":     "how does codemode exposure work",
	})
	if err != nil {
		t.Fatalf("callTool: %v", err)
	}

	for _, want := range []string{"###", "Source: https://pi.dev/docs/latest/mcp", "/local/pi/0.99.2"} {
		if !strings.Contains(got, want) {
			t.Errorf("query output missing %q:\n%s", want, got)
		}
	}
}

func TestQueryDocsTool_InvalidLibraryID(t *testing.T) {
	session := newTestClient(t)

	_, err := callTool(t, session, "query-docs", map[string]any{
		"libraryId": "not-a-library-id",
		"query":     "exposure",
	})
	if err == nil {
		t.Error("query with a malformed library ID succeeded, want error")
	}
}

func TestQueryDocsTool_UnknownLibrary(t *testing.T) {
	session := newTestClient(t)

	_, err := callTool(t, session, "query-docs", map[string]any{
		"libraryId": "/local/missing/1",
		"query":     "exposure",
	})
	if err == nil {
		t.Error("query on an unknown library succeeded, want error")
	}
}

func TestQueryDocsTool_MissingQuery(t *testing.T) {
	session := newTestClient(t)

	_, err := callTool(t, session, "query-docs", map[string]any{"libraryId": "/local/pi"})
	if err == nil {
		t.Error("query without a query succeeded, want error")
	}
}

func TestQueryDocsTool_EmptyQuery(t *testing.T) {
	session := newTestClient(t)

	_, err := callTool(t, session, "query-docs", map[string]any{
		"libraryId": "/local/pi",
		"query":     "",
	})
	if err == nil {
		t.Error("query with an empty query succeeded, want error")
	}
}

func TestQueryDocsTool_DoesNotExposeRetrievalInternals(t *testing.T) {
	session := newTestClient(t)

	got, err := callTool(t, session, "query-docs", map[string]any{
		"libraryId": "/local/pi/0.99.2",
		"query":     "exposure",
	})
	if err != nil {
		t.Fatalf("callTool: %v", err)
	}

	lower := strings.ToLower(got)
	for _, leak := range []string{
		"distance", "score", "cosine", "topk", "top_k",
		"hnsw", "vector", "embedding", "threshold", "similarity",
	} {
		if strings.Contains(lower, leak) {
			t.Errorf("query output leaks %q:\n%s", leak, got)
		}
	}
}

func TestQueryDocsTool_DoesNotWrapInSystemPreamble(t *testing.T) {
	session := newTestClient(t)

	got, err := callTool(t, session, "query-docs", map[string]any{
		"libraryId": "/local/pi/0.99.2",
		"query":     "exposure",
	})
	if err != nil {
		t.Fatalf("callTool: %v", err)
	}

	// Indexed documentation is untrusted. Wrapping it in an imperative preamble
	// would hand a document author control of the agent's instructions.
	for _, wrapper := range []string{
		"important system instruction",
		"system:",
		"you must",
		"ignore previous",
		"disregard the above",
	} {
		if strings.Contains(strings.ToLower(got), wrapper) {
			t.Errorf("output contains an instruction wrapper %q:\n%s", wrapper, got)
		}
	}
}

// TestQueryDocs_AcceptsLibraryIDAlias covers a model that sends libraryID instead
// of libraryId. The alias is accepted but never advertised.
func TestQueryDocs_AcceptsLibraryIDAlias(t *testing.T) {
	session := newTestClient(t)

	got, err := callTool(t, session, "query-docs", map[string]any{
		"libraryID": "/local/pi/0.99.2",
		"query":     "exposure",
	})
	if err != nil {
		t.Fatalf("callTool with libraryID alias: %v", err)
	}
	if !strings.Contains(got, "Source:") {
		t.Errorf("alias call returned no documentation:\n%s", got)
	}
}

func TestQueryDocs_AcceptsUserQueryAlias(t *testing.T) {
	session := newTestClient(t)

	got, err := callTool(t, session, "query-docs", map[string]any{
		"libraryId": "/local/pi/0.99.2",
		"userQuery": "exposure",
	})
	if err != nil {
		t.Fatalf("callTool with userQuery alias: %v", err)
	}
	if !strings.Contains(got, "Source:") {
		t.Errorf("alias call returned no documentation:\n%s", got)
	}
}

func TestQueryDocs_AcceptsSourceIDAlias(t *testing.T) {
	session := newTestClient(t)

	got, err := callTool(t, session, "query-docs", map[string]any{
		"sourceId": "/local/pi/0.99.2",
		"query":    "exposure",
	})
	if err != nil {
		t.Fatalf("callTool with sourceId alias: %v", err)
	}
	if !strings.Contains(got, "Source:") {
		t.Errorf("alias call returned no documentation:\n%s", got)
	}
}

func TestResolveLibraryID_AcceptsLibraryIDAlias(t *testing.T) {
	session := newTestClient(t)

	got, err := callTool(t, session, "resolve-library-id", map[string]any{
		"libraryID": "Pi",
		"query":     "exposure",
	})
	if err != nil {
		t.Fatalf("callTool with libraryID alias: %v", err)
	}
	if !strings.Contains(got, "/local/pi") {
		t.Errorf("alias call returned no libraries:\n%s", got)
	}
}

// TestAliasesAreNotAdvertised is the other half of alias support: a model must
// only learn the canonical names.
func TestAliasesAreNotAdvertised(t *testing.T) {
	tools := listTools(t, newTestClient(t))

	for name, tool := range tools {
		props := propertiesOf(t, name, tool.InputSchema)
		for alias := range mcpserver.ArgumentAliases {
			if _, present := props[alias]; present {
				t.Errorf("%s advertises alias %q; aliases must stay hidden", name, alias)
			}
		}
	}
}

func TestQueryDocs_IsOffline(t *testing.T) {
	// A query must touch only the local index. This asserts the searcher is the
	// only dependency, so no network path can sneak in.
	var calls int
	var mu sync.Mutex

	searcher := searcherFunc(func(_ context.Context, req mcpserver.SearchRequest) (string, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return "Source: https://example.test\n\nLocal content.", nil
	})

	server := mcpserver.New(mcpserver.Options{
		Version: "v0.1.0-test",
		Resolve: staticResolver(),
		Search:  searcher,
	})

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "1"}, nil)
	clientSession, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer clientSession.Close()

	if _, err := callTool(t, clientSession, "query-docs", map[string]any{
		"libraryId": "/local/pi",
		"query":     "anything",
	}); err != nil {
		t.Fatalf("callTool: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Errorf("searcher called %d times, want exactly 1 local lookup", calls)
	}
}
