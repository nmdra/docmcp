package app

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/docmcp/docmcp/internal/config"
)

// runtimeFactory builds the application for one command run. Commands depend on
// this rather than on a Runtime, so a test can supply a fake embedder.
type runtimeFactory func(ctx context.Context, cfg config.Config) (*Runtime, error)

// globalFlags are the run-wide overrides. They exist so a command never depends
// on the user's real config or index.
type globalFlags struct {
	configPath string
	dataDir    string
}

func (g *globalFlags) register(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVar(&g.configPath, "config", "",
		"path to config.toml (default: the per-user config location)")
	cmd.PersistentFlags().StringVar(&g.dataDir, "data-dir", "",
		"directory holding sources.json and the vector index")
}

// configFor resolves the effective configuration for a run: defaults, then the
// config file, then the global flag overrides.
func configFor(cmd *cobra.Command) (config.Config, error) {
	global, _ := cmd.Root().PersistentFlags().GetString("config")
	dataDir, _ := cmd.Root().PersistentFlags().GetString("data-dir")

	path := global
	if path == "" {
		path = config.DefaultPath()
	}

	cfg, err := config.LoadWith(path, config.Options{DataPath: dataDir})
	if err != nil {
		return config.Config{}, err
	}
	return cfg, nil
}

// NewRootCommand builds the docmcp CLI. The version is injected so the binary's
// build-stamped version is the only thing main.go contributes.
//
// factory defaults to the real runtime; tests pass their own to avoid needing a
// model or a real index.
func NewRootCommand(version string, factory ...runtimeFactory) *cobra.Command {
	build := NewRuntime
	if len(factory) > 0 && factory[0] != nil {
		build = factory[0]
	}

	var global globalFlags

	root := &cobra.Command{
		Use:           "docmcp",
		Short:         "Index documentation sites and serve them over MCP",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	global.register(root)

	root.AddCommand(
		newAddCommand(build),
		newSyncCommand(build),
		newListCommand(build),
		newInfoCommand(build),
		newRemoveCommand(build),
		newReindexCommand(build),
		newSearchCommand(build),
		newServeCommand(build, version),
		newVersionCommand(version),
	)

	return root
}

func newVersionCommand(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the docmcp version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), version)
			return err
		},
	}
}

// normalizeBaseURL validates and canonicalizes the URL a user gave for a
// library. The path prefix is meaningful: it bounds the whole crawl.
func normalizeBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("--url: %w", err)
	}

	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return "", fmt.Errorf(
			"unsupported URL scheme %q; pass an http or https documentation URL", parsed.Scheme)
	}

	if parsed.Host == "" {
		return "", fmt.Errorf("%q has no host", raw)
	}

	// Strip the fragment and any query: neither identifies a documentation tree.
	parsed.Fragment = ""
	parsed.RawFragment = ""
	parsed.RawQuery = ""
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)

	if !strings.HasPrefix(parsed.Path, "/") {
		parsed.Path = "/" + parsed.Path
	}
	if parsed.Path == "/" {
		// A bare host would bound nothing; make the scope explicit instead of
		// crawling the whole site silently.
		return "", fmt.Errorf(
			"%q has no path; point at a documentation path such as %s/docs/",
			raw, parsed.Scheme+"://"+parsed.Host)
	}

	return parsed.String(), nil
}
