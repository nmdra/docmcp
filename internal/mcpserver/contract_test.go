package mcpserver_test

import (
	"context"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/docmcp/docmcp/internal/mcpserver"
)

// newTestClient connects a real MCP client to the server over an in-memory
// transport. Every test here goes through the protocol, not through the handler
// functions: the contract is what a model actually sees.
func newTestClient(t *testing.T) *mcp.ClientSession {
	t.Helper()

	return newTestClientWithOptions(t, mcpserver.Options{
		Version: "v0.1.0-test",
		Resolve: staticResolver(),
		Search:  staticSearcher(),
	})
}

func newTestClientWithOptions(t *testing.T, opts mcpserver.Options) *mcp.ClientSession {
	t.Helper()

	server := mcpserver.New(opts)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	t.Cleanup(func() { serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() { clientSession.Close() })

	return clientSession
}

func listTools(t *testing.T, session *mcp.ClientSession) map[string]*mcp.Tool {
	t.Helper()

	result, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	out := map[string]*mcp.Tool{}
	for _, tool := range result.Tools {
		out[tool.Name] = tool
	}
	return out
}

// TestMCPServer_AdvertisesExactlyTwoTools is the central constraint of the whole
// design. A third tool changes the surface a model was trained to expect.
func TestMCPServer_AdvertisesExactlyTwoTools(t *testing.T) {
	tools := listTools(t, newTestClient(t))

	want := []string{"query-docs", "resolve-library-id"}

	if len(tools) != len(want) {
		names := make([]string, 0, len(tools))
		for name := range tools {
			names = append(names, name)
		}
		slices.Sort(names)
		t.Fatalf("server advertises %d tools %v, want exactly %v", len(tools), names, want)
	}

	for _, name := range want {
		if _, ok := tools[name]; !ok {
			t.Errorf("missing tool %q", name)
		}
	}
}

func TestMCPServer_HasNoWriteTools(t *testing.T) {
	tools := listTools(t, newTestClient(t))

	// Mutation belongs to the CLI. A write tool over MCP would make the server
	// unsafe and non-deterministic.
	forbidden := []string{
		"add-library", "remove-library", "sync-library",
		"crawl", "reindex", "add", "sync", "remove", "search",
	}

	for _, name := range forbidden {
		if _, ok := tools[name]; ok {
			t.Errorf("server exposes %q, want mutation to stay in the CLI", name)
		}
	}
}

// TestResolveLibraryIDSchema freezes the property set. Both fields are required:
// an optional query would make resolution ambiguous, and an optional name would
// let a model call it with nothing to go on.
func TestResolveLibraryIDSchema(t *testing.T) {
	tools := listTools(t, newTestClient(t))

	tool, ok := tools["resolve-library-id"]
	if !ok {
		t.Fatal("resolve-library-id is not advertised")
	}

	assertExactProperties(t, "resolve-library-id", tool.InputSchema,
		[]string{"libraryName", "query"})
}

func TestQueryDocsSchema(t *testing.T) {
	tools := listTools(t, newTestClient(t))

	tool, ok := tools["query-docs"]
	if !ok {
		t.Fatal("query-docs is not advertised")
	}

	assertExactProperties(t, "query-docs", tool.InputSchema,
		[]string{"libraryId", "query"})
}

// TestSchemasRejectRetrievalKnobs is the guard that keeps retrieval internal.
// Every one of these fields would let a model tune ranking it cannot evaluate.
func TestSchemasRejectRetrievalKnobs(t *testing.T) {
	tools := listTools(t, newTestClient(t))

	forbidden := []string{
		"limit", "page", "topic", "mode", "source", "version",
		"topK", "top_k", "threshold", "score", "namespace",
		"filters", "include", "exclude",
	}

	for name, tool := range tools {
		props := propertiesOf(t, name, tool.InputSchema)
		for _, bad := range forbidden {
			if _, present := props[bad]; present {
				t.Errorf("%s advertises %q; retrieval internals must stay internal", name, bad)
			}
		}
	}
}

// assertExactProperties checks the property set and that all of them are
// required. An extra property is as much a contract break as a missing one.
func assertExactProperties(t *testing.T, toolName string, schema any, want []string) {
	t.Helper()

	props := propertiesOf(t, toolName, schema)

	if len(props) != len(want) {
		names := make([]string, 0, len(props))
		for name := range props {
			names = append(names, name)
		}
		slices.Sort(names)

		sorted := slices.Clone(want)
		slices.Sort(sorted)

		t.Fatalf("%s properties = %v, want exactly %v", toolName, names, sorted)
	}

	for _, name := range want {
		prop, present := props[name]
		if !present {
			t.Fatalf("%s is missing required property %q", toolName, name)
		}
		if !prop.required {
			t.Errorf("%s.%s is optional; every field must be required", toolName, name)
		}
		if prop.typ != "string" {
			t.Errorf("%s.%s type = %q, want string", toolName, name, prop.typ)
		}
	}
}

type property struct {
	typ      string
	required bool
}

func propertiesOf(t *testing.T, toolName string, schema any) map[string]property {
	t.Helper()

	object, ok := schema.(map[string]any)
	if !ok {
		t.Fatalf("%s InputSchema has type %T, want a JSON object", toolName, schema)
	}

	rawProps, present := object["properties"]
	if !present {
		t.Fatalf("%s InputSchema has no properties", toolName)
	}

	propMap, ok := rawProps.(map[string]any)
	if !ok {
		t.Fatalf("%s properties has type %T, want an object", toolName, rawProps)
	}

	required := map[string]bool{}
	if rawRequired, present := object["required"]; present {
		list, ok := rawRequired.([]any)
		if !ok {
			t.Fatalf("%s required has type %T, want an array", toolName, rawRequired)
		}
		for _, item := range list {
			if name, ok := item.(string); ok {
				required[name] = true
			}
		}
	}

	out := map[string]property{}
	for name, raw := range propMap {
		definition, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		out[name] = property{
			typ:      stringField(definition, "type"),
			required: required[name],
		}
	}
	return out
}

func stringField(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// TestToolAnnotations asserts the read-only contract. openWorldHint is false
// because a query reads the local index and never the network.
func TestToolAnnotations(t *testing.T) {
	tools := listTools(t, newTestClient(t))

	for name, tool := range tools {
		annotations := tool.Annotations
		if annotations == nil {
			t.Errorf("%s has no annotations", name)
			continue
		}

		if !annotations.ReadOnlyHint {
			t.Errorf("%s ReadOnlyHint = false, want true", name)
		}
		if !annotations.IdempotentHint {
			t.Errorf("%s IdempotentHint = false, want true", name)
		}
		if annotations.DestructiveHint == nil || *annotations.DestructiveHint {
			t.Errorf("%s DestructiveHint should be explicitly false", name)
		}
		if annotations.OpenWorldHint == nil || *annotations.OpenWorldHint {
			t.Errorf("%s OpenWorldHint should be explicitly false: a query reads only the local index", name)
		}
	}
}

func TestToolDescriptions_GuideTheAgent(t *testing.T) {
	tools := listTools(t, newTestClient(t))

	want := map[string][]string{
		"resolve-library-id": {"library", "query-docs", "resolve"},
		"query-docs":         {"documentation", "resolve-library-id"},
	}

	for name, phrases := range want {
		tool, ok := tools[name]
		if !ok {
			t.Errorf("%s is not advertised", name)
			continue
		}
		if tool.Description == "" {
			t.Errorf("%s has no description; a model cannot pick it correctly", name)
			continue
		}

		lower := tool.Description
		for _, phrase := range phrases {
			if !containsFold(lower, phrase) {
				t.Errorf("%s description does not mention %q:\n%s", name, phrase, tool.Description)
			}
		}
	}
}

func TestServerInstructions_SteerUsage(t *testing.T) {
	server := mcpserver.New(mcpserver.Options{
		Version: "v0.1.0-test",
		Resolve: staticResolver(),
		Search:  staticSearcher(),
	})

	instructions := server.Instructions()
	if instructions == "" {
		t.Fatal("server has no instructions; a model would not know when to use it")
	}

	for _, want := range []string{"resolve-library-id", "query-docs", "indexed"} {
		if !containsFold(instructions, want) {
			t.Errorf("server instructions do not mention %q:\n%s", want, instructions)
		}
	}
}

func containsFold(haystack, needle string) bool {
	return len(needle) == 0 || indexFold(haystack, needle) >= 0
}

func indexFold(haystack, needle string) int {
	lower := make([]rune, 0, len(haystack))
	for _, r := range haystack {
		lower = append(lower, toLower(r))
	}
	target := make([]rune, 0, len(needle))
	for _, r := range needle {
		target = append(target, toLower(r))
	}

	for i := 0; i+len(target) <= len(lower); i++ {
		match := true
		for j := range target {
			if lower[i+j] != target[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func toLower(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + 32
	}
	return r
}

var _ = context.Background
