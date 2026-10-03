package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/nmdra/docmcp/internal/mcpserver"
	"github.com/nmdra/docmcp/internal/search"
)

// newServeCommand runs the MCP server over stdio.
//
// The stream discipline here is the whole point of the command: stdout carries
// JSON-RPC framing and nothing else, and every log line goes to stderr. One
// stray log line on stdout corrupts the protocol and the client drops the
// connection, which is why the logger is constructed with stderr explicitly
// rather than relying on the default.
func newServeCommand(factory runtimeFactory, version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the local index over MCP on stdio",
		Long: "Serve indexed documentation to MCP clients on stdio.\n\n" +
			"Configuration:\n" +
			"  {\n" +
			"    \"mcpServers\": {\n" +
			"      \"docmcp\": { \"command\": \"docmcp\", \"args\": [\"serve\"] }\n" +
			"    }\n" +
			"  }\n\n" +
			"Only resolve-library-id and query-docs are exposed. Indexing and sync " +
			"stay in the CLI.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd.Context(), cmd, factory, version)
		},
	}

	cmd.Flags().String("log-level", "error",
		"log verbosity: error, warn, info, or debug (logs always go to stderr)")

	return cmd
}

func runServe(ctx context.Context, cmd *cobra.Command, factory runtimeFactory, version string) error {
	logLevel, _ := cmd.Flags().GetString("log-level")
	logger := newStderrLogger(cmd.ErrOrStderr(), logLevel)

	cfg, err := configFor(cmd)
	if err != nil {
		return err
	}

	rt, err := factory(ctx, cfg)
	if err != nil {
		return err
	}
	defer rt.Close()

	engine, err := rt.SearchEngine()
	if err != nil {
		return err
	}

	server := mcpserver.New(mcpserver.Options{
		Version: version,
		Resolve: &runtimeResolver{rt: rt},
		Search:  &runtimeSearcher{rt: rt, engine: engine, logger: logger},
	})

	// stdin and stdout are the MCP transport; stderr stays free for logs.
	session, err := server.Connect(ctx, &mcp.StdioTransport{}, nil)
	if err != nil {
		return fmt.Errorf("serve over stdio: %w", err)
	}
	defer session.Close()

	logger.Info("docmcp serving over stdio")

	<-ctx.Done()
	return nil
}

// runtimeResolver adapts the runtime to the MCP resolver.
type runtimeResolver struct {
	rt *Runtime
}

func (r *runtimeResolver) Resolve(ctx context.Context, libraryName, query string) ([]search.Match, error) {
	return r.rt.Resolve(ctx, libraryName, query)
}

// runtimeSearcher adapts the search engine to the MCP searcher, formatting
// results once so the CLI and an agent see identical text.
type runtimeSearcher struct {
	rt     *Runtime
	engine *search.Engine
	logger *slog.Logger
}

func (s *runtimeSearcher) Search(ctx context.Context, req mcpserver.SearchRequest) (string, error) {
	if err := s.rt.RequireIndexed(ctx, req.LibraryID); err != nil {
		return "", err
	}

	results, err := s.engine.Search(ctx, search.Request{
		LibraryID: req.LibraryID,
		Query:     req.Query,
	})
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return fmt.Sprintf(
			"No indexed documentation in %s matched that question.\n\n"+
				"The library is indexed, but this query found nothing. "+
				"Try one clear concept, or a different wording.",
			req.LibraryID), nil
	}

	return search.FormatAll(results), nil
}

// newStderrLogger builds a logger that can never write to stdout.
func newStderrLogger(w interface{ Write([]byte) (int, error) }, level string) *slog.Logger {
	var lvl slog.Level

	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		lvl = slog.LevelDebug
	case "info":
		lvl = slog.LevelInfo
	case "warn":
		lvl = slog.LevelWarn
	default:
		lvl = slog.LevelError
	}

	// slog.NewTextHandler with this writer, not slog.Default: the default handler
	// writes to stderr but a future change could redirect it.
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: lvl}))
}
