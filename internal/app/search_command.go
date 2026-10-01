package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/docmcp/docmcp/internal/search"
)

// newSearchCommand runs a retrieval from the terminal.
//
// It shares the search engine with the MCP server on purpose: a second
// implementation would be a second set of answers, and users would reasonably
// expect the CLI and an agent to see the same thing.
func newSearchCommand(factory runtimeFactory) *cobra.Command {
	d := &deps{factory: factory}

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search an indexed library",
		Long: "Search one indexed library and print the matching documentation sections.\n\n" +
			"The query should describe a single documentation concept clearly. " +
			"Retrieval options are not exposed: the internal budget is tuned, not chosen.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			libraryID, _ := cmd.Flags().GetString("library")

			query := strings.TrimSpace(strings.Join(args, " "))
			if query == "" {
				return fmt.Errorf("a query is required")
			}
			if strings.TrimSpace(libraryID) == "" {
				return fmt.Errorf("--library is required: pass an ID such as /local/pi/0.99.2")
			}

			ctx := cmd.Context()

			rt, err := d.open(ctx, cmd)
			if err != nil {
				return err
			}
			defer rt.Close()

			engine, err := rt.SearchEngine()
			if err != nil {
				return err
			}

			// Resolving here means a short name works too, which is friendlier
			// than making the user copy an ID out of `docmcp list`.
			resolved, err := resolveLibraryID(ctx, rt, libraryID, query)
			if err != nil {
				return err
			}

			// A well-formed ID that was never indexed would otherwise return an
			// empty answer, which reads like "no such documentation" rather than
			// "you asked for a library that does not exist".
			if err := rt.RequireIndexed(ctx, resolved); err != nil {
				return err
			}

			results, err := engine.Search(ctx, search.Request{
				LibraryID: resolved,
				Query:     query,
			})
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if len(results) == 0 {
				fmt.Fprintf(out, "No matching documentation in %s.\n", resolved)
				return nil
			}

			fmt.Fprintf(out, "%s — %d result(s)\n\n", resolved, len(results))
			fmt.Fprint(out, search.FormatAll(results))
			return nil
		},
	}

	cmd.Flags().String("library", "", "library ID or name to search (required)")

	return cmd
}

// resolveLibraryID accepts an exact library ID, or a name to rank against the
// indexed libraries.
func resolveLibraryID(ctx context.Context, rt *Runtime, libraryID, query string) (string, error) {
	if strings.HasPrefix(libraryID, "/") {
		return libraryID, nil
	}

	libraries, err := rt.Sources.List(ctx)
	if err != nil {
		return "", fmt.Errorf("list libraries: %w", err)
	}
	if len(libraries) == 0 {
		return "", fmt.Errorf("no libraries are indexed yet; run `docmcp add`")
	}

	resolver := rt.Resolver(ctx, libraries)
	matches, err := resolver.Resolve(ctx, libraryID, query)
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("library %q is not indexed; run `docmcp list`", libraryID)
	}
	return matches[0].LibraryID, nil
}

// Resolve is used by the MCP handler, which needs matches rather than one ID.
func (rt *Runtime) Resolve(ctx context.Context, libraryName, query string) ([]search.Match, error) {
	libraries, err := rt.Sources.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list libraries: %w", err)
	}
	return rt.Resolver(ctx, libraries).Resolve(ctx, libraryName, query)
}
