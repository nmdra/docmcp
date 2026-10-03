package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nmdra/docmcp/internal/search"
	"github.com/nmdra/docmcp/internal/source"
)

// The two input schemas are written out by hand rather than inferred from Go
// structs. Inference would tie the public contract to internal type changes, and
// the contract tests need the schema to be exactly these property sets — no more,
// no less. Hand-writing it also leaves room to accept undocumented argument
// aliases without loosening validation.
var (
	resolveInputSchema = objectSchema(
		property(argLibraryName, "the library, package, framework, CLI, or product name to resolve"),
		property(argQuery, "the user's question, used to rank matching libraries and versions"),
	)

	queryInputSchema = objectSchema(
		property(argLibraryID, "an exact library ID from resolve-library-id, such as /local/pi/0.99.2"),
		property(argQuery, "one documentation concept to retrieve, described clearly"),
	)
)

type propertySpec struct {
	name        string
	description string
}

func property(name, description string) propertySpec {
	return propertySpec{name: name, description: description}
}

// objectSchema builds a JSON Schema requiring every listed property.
func objectSchema(props ...propertySpec) map[string]any {
	properties := make(map[string]any, len(props))
	required := make([]any, 0, len(props))

	for _, p := range props {
		properties[p.name] = map[string]any{
			"type":        "string",
			"description": p.description,
		}
		required = append(required, p.name)
	}

	return map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   required,
		// additionalProperties stays false so a typo is reported rather than
		// silently ignored. Aliases are handled before validation.
		"additionalProperties": false,
	}
}

func (s *Server) handleResolve(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, query, err := readFields(req, argLibraryName, argQuery)
	if err != nil {
		return toolError(err)
	}

	matches, err := s.resolver.Resolve(ctx, name, query)
	if err != nil {
		return toolError(fmt.Errorf("resolve %q: %w", name, err))
	}

	return textResult(formatLibraries(matches)), nil
}

func (s *Server) handleQuery(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	libraryID, query, err := readFields(req, argLibraryID, argQuery)
	if err != nil {
		return toolError(err)
	}

	if _, err := source.ParseLibraryID(libraryID); err != nil {
		return toolError(fmt.Errorf(
			"%q is not a valid library ID; call resolve-library-id first: %w", libraryID, err))
	}

	text, err := s.searcher.Search(ctx, SearchRequest{LibraryID: libraryID, Query: query})
	if err != nil {
		return toolError(err)
	}

	return textResult(text), nil
}

// readFields pulls two required string arguments, applying aliases first.
//
// Aliases exist because models routinely send libraryID or userQuery. Accepting
// them costs one lookup; rejecting them costs the agent a round trip. They are
// never advertised, so the advertised contract stays canonical.
func readFields(req *mcp.CallToolRequest, names ...string) (string, string, error) {
	if len(names) != 2 {
		return "", "", fmt.Errorf("readFields needs exactly two field names")
	}

	args := rawArguments(req)

	out := make([]string, 0, 2)
	for _, name := range names {
		value := stringArgument(args, name)
		if value == "" {
			return "", "", fmt.Errorf("%s is required", name)
		}
		out = append(out, value)
	}
	return out[0], out[1], nil
}

// rawArguments decodes the opaque arguments object into a map.
func rawArguments(req *mcp.CallToolRequest) map[string]any {
	if req == nil || req.Params == nil || req.Params.Arguments == nil {
		return nil
	}

	raw, err := json.Marshal(req.Params.Arguments)
	if err != nil {
		return nil
	}

	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil
	}
	return args
}

// stringArgument reads a canonical argument, falling back to any alias that maps
// onto it. A canonical value always wins: an alias only fills a gap.
func stringArgument(args map[string]any, canonical string) string {
	if s := stringOf(args[canonical]); s != "" {
		return s
	}

	for alias := range aliasesFor(canonical) {
		if s := stringOf(args[alias]); s != "" {
			return s
		}
	}
	return ""
}

// aliasesFor returns the alias sets that apply to a canonical field. The two
// tools name the same concept differently — libraryName versus libraryId — so a
// model that sends the wrong one is still understood.
func aliasesFor(canonical string) map[string]string {
	merged := map[string]string{}
	for alias, target := range ArgumentAliases {
		merged[alias] = target
	}
	for alias, target := range NameAliases {
		if target == canonical {
			merged[alias] = target
		}
	}
	return merged
}

func stringOf(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

// toolError reports a failure as tool output rather than a protocol error.
//
// MCP guidance is explicit: a failed tool call should come back as content with
// isError set, so the model can read the message and correct itself. A
// protocol-level error is invisible to the model.
func toolError(err error) (*mcp.CallToolResult, error) {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}, nil
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

// formatLibraries renders resolve-library-id output as plain, citable text.
//
// Internal Chroma identifiers are never shown: a model echoing one back would be
// guessing at a detail it cannot depend on.
func formatLibraries(matches []search.Match) string {
	if len(matches) == 0 {
		return "No indexed libraries match that name.\n\n" +
			"Run `docmcp list` to see what is indexed, " +
			"or `docmcp add <url> --name <name>` to index one."
	}

	var b strings.Builder
	b.WriteString("Available Libraries:\n\n")

	for _, m := range matches {
		fmt.Fprintf(&b, "- Library ID: %s\n", m.LibraryID)
		fmt.Fprintf(&b, "  Name: %s\n", m.Name)
		if m.Description != "" {
			fmt.Fprintf(&b, "  Description: %s\n", m.Description)
		}
		fmt.Fprintf(&b, "  Indexed Chunks: %d\n", m.IndexedChunks)

		if others := siblingsOf(matches, m); len(others) > 0 {
			b.WriteString("  Versions:\n")
			for _, id := range others {
				fmt.Fprintf(&b, "    - %s\n", id)
			}
		}
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

// siblingsOf lists the other library IDs sharing a name, newest first, so a
// model can pick a version without a second call.
func siblingsOf(matches []search.Match, self search.Match) []string {
	var out []string
	for _, m := range matches {
		if m.Name == self.Name && m.LibraryID != self.LibraryID {
			out = append(out, m.LibraryID)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out
}
