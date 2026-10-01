package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/docmcp/docmcp/internal/ingest"
	"github.com/docmcp/docmcp/internal/source"
	"github.com/docmcp/docmcp/internal/store"
)

// Runtime is built once per command run and torn down on the way out.
type deps struct {
	factory runtimeFactory
}

func (d *deps) open(ctx context.Context, cmd *cobra.Command) (*Runtime, error) {
	cfg, err := configFor(cmd)
	if err != nil {
		return nil, err
	}
	return d.factory(ctx, cfg)
}

func newAddCommand(factory runtimeFactory) *cobra.Command {
	d := &deps{factory: factory}

	cmd := &cobra.Command{
		Use:   "add <url>",
		Short: "Index a documentation site as a new library",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rawURL := strings.TrimSpace(args[0])
			if rawURL == "" {
				return fmt.Errorf("a documentation URL is required")
			}

			name, _ := cmd.Flags().GetString("name")
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("--name is required: it is the short name an agent will ask for")
			}
			version, _ := cmd.Flags().GetString("version")
			description, _ := cmd.Flags().GetString("description")
			includes, _ := cmd.Flags().GetStringSlice("include")
			excludes, _ := cmd.Flags().GetStringSlice("exclude")

			normalized, err := normalizeBaseURL(rawURL)
			if err != nil {
				return err
			}

			libraryID, err := source.NewLibraryID(name, version)
			if err != nil {
				return fmt.Errorf("--name and --version: %w", err)
			}

			ctx := cmd.Context()
			rt, err := d.open(ctx, cmd)
			if err != nil {
				return err
			}
			defer rt.Close()

			src := source.Source{
				LibraryID:   libraryID,
				Name:        name,
				Description: description,
				BaseURL:     normalized,
				Version:     version,
				Includes:    includes,
				Excludes:    excludes,
			}

			id, err := rt.Sources.Add(ctx, src)
			if err != nil {
				if errors.Is(err, source.ErrSourceExists) {
					return fmt.Errorf(
						"library %s already exists; run `docmcp sync %s` to update it",
						libraryID, name)
				}
				return fmt.Errorf("register library: %w", err)
			}
			src.ID = id

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Discovering documentation...\n")

			report, err := rt.Ingestor.Ingest(ctx, src)
			if err == nil && report.PagesFetched == 0 {
				// Zero pages means the crawl matched nothing: a typo'd path, a
				// site that 404s, a content type we cannot read, or include
				// rules that exclude everything. Reporting success here leaves
				// `list` showing a library with 0 pages that an agent will then
				// try to query. Fail instead, and roll the registration back so
				// no broken entry survives.
				err = fmt.Errorf(
					"the crawl produced no pages; check the URL, that the site serves HTML, " +
						"and any --include/--exclude rules")
			}
			if err != nil {
				// Roll the registration back: a library with no pages indexed
				// would show up in `list` as a broken entry.
				if delErr := rt.Sources.Delete(ctx, id); delErr != nil {
					return fmt.Errorf("index %s: %w (rollback also failed: %w)", libraryID, err, delErr)
				}
				return fmt.Errorf("index %s: %w", libraryID, err)
			}

			markSynced(ctx, rt, src)
			writeIngestReport(out, libraryID, report)
			return nil
		},
	}

	cmd.Flags().String("name", "", "short library name agents will resolve (required)")
	cmd.Flags().String("version", "", "documentation version")
	cmd.Flags().String("description", "", "one-line description shown by resolve-library-id")
	cmd.Flags().StringSlice("include", nil, "path glob to include, repeatable (default: everything under the URL path)")
	cmd.Flags().StringSlice("exclude", nil, "path glob to exclude, repeatable (exclude always wins)")

	return cmd
}

func newSyncCommand(factory runtimeFactory) *cobra.Command {
	d := &deps{factory: factory}

	return &cobra.Command{
		Use:   "sync <name>",
		Short: "Re-index a library, embedding only what changed",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			rt, err := d.open(ctx, cmd)
			if err != nil {
				return err
			}
			defer rt.Close()

			src, err := findSource(ctx, rt, args[0])
			if err != nil {
				return err
			}

			report, err := rt.Ingestor.Ingest(ctx, src)
			if err != nil {
				return fmt.Errorf("sync %s: %w", src.LibraryID, err)
			}
			// Sync does not roll the registration back: the library already
			// existed and its indexed chunks are still there. An empty crawl is
			// worth saying out loud — it usually means the site moved or the
			// rules now exclude everything — but it is not a failure, and
			// discarding a working index over one bad crawl would be worse.
			if report.PagesFetched == 0 {
				kept := 0
				if n, err := rt.Store.CountChunks(ctx, src.LibraryID); err == nil {
					kept = n
				}
				fmt.Fprintf(cmd.ErrOrStderr(),
					"warning: the crawl produced no pages; %s keeps its %d indexed chunks\n",
					src.LibraryID, kept)
			}

			markSynced(ctx, rt, src)
			writeIngestReport(cmd.OutOrStdout(), src.LibraryID, report)
			return nil
		},
	}
}

func newListCommand(factory runtimeFactory) *cobra.Command {
	d := &deps{factory: factory}

	return &cobra.Command{
		Use:   "list",
		Short: "List indexed libraries",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			rt, err := d.open(ctx, cmd)
			if err != nil {
				return err
			}
			defer rt.Close()

			list, err := rt.Sources.List(ctx)
			if err != nil {
				return fmt.Errorf("list libraries: %w", err)
			}

			out := cmd.OutOrStdout()
			if len(list) == 0 {
				fmt.Fprintln(out, "No libraries indexed yet.")
				fmt.Fprintln(out, "Add one with: docmcp add <url> --name <name>")
				return nil
			}

			rows := make([][]string, 0, len(list)+1)
			rows = append(rows, []string{"LIBRARY", "VERSION", "PAGES", "CHUNKS", "LAST SYNC"})

			for _, s := range list {
				pages, chunks, err := countLibrary(ctx, rt, s)
				if err != nil {
					return err
				}

				version := s.Version
				if version == "" {
					version = "-"
				}

				lastSync := "never"
				if s.SyncedAt != nil {
					lastSync = s.SyncedAt.Format("2006-01-02")
				}

				rows = append(rows, []string{
					s.LibraryID, version,
					fmt.Sprint(pages), fmt.Sprint(chunks), lastSync,
				})
			}

			writeTable(out, rows)
			return nil
		},
	}
}

func newInfoCommand(factory runtimeFactory) *cobra.Command {
	d := &deps{factory: factory}

	return &cobra.Command{
		Use:   "info <library-id>",
		Short: "Show detail for one library",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := source.ParseLibraryID(args[0]); err != nil {
				return err
			}

			ctx := cmd.Context()
			rt, err := d.open(ctx, cmd)
			if err != nil {
				return err
			}
			defer rt.Close()

			src, err := findSourceByLibraryID(ctx, rt, args[0])
			if err != nil {
				return err
			}

			pages, chunks, err := countLibrary(ctx, rt, src)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Library:     %s\n", src.LibraryID)
			if src.Description != "" {
				fmt.Fprintf(out, "Description: %s\n", src.Description)
			}
			fmt.Fprintf(out, "URL:         %s\n", src.BaseURL)
			fmt.Fprintf(out, "Pages:       %d\n", pages)
			fmt.Fprintf(out, "Chunks:      %d\n", chunks)
			fmt.Fprintf(out, "Added:       %s\n", src.CreatedAt.Format("2006-01-02"))

			if src.SyncedAt != nil {
				fmt.Fprintf(out, "Last sync:   %s\n", src.SyncedAt.Format("2006-01-02"))
			} else {
				fmt.Fprintf(out, "Last sync:   never\n")
			}
			if len(src.Includes) > 0 {
				fmt.Fprintf(out, "Includes:    %s\n", strings.Join(src.Includes, " "))
			}
			if len(src.Excludes) > 0 {
				fmt.Fprintf(out, "Excludes:    %s\n", strings.Join(src.Excludes, " "))
			}

			return nil
		},
	}
}

func newRemoveCommand(factory runtimeFactory) *cobra.Command {
	d := &deps{factory: factory}

	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a library and its indexed chunks",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			rt, err := d.open(ctx, cmd)
			if err != nil {
				return err
			}
			defer rt.Close()

			src, err := findSource(ctx, rt, args[0])
			if err != nil {
				return err
			}

			if err := rt.Ingestor.Remove(ctx, src.ID); err != nil {
				return fmt.Errorf("remove chunks for %s: %w", src.LibraryID, err)
			}
			if err := rt.Sources.Delete(ctx, src.ID); err != nil {
				return fmt.Errorf("remove library %s: %w", src.LibraryID, err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Removed %s\n", src.LibraryID)
			return nil
		},
	}
}

// writeIngestReport prints the run summary. The counts are the point of sync: a
// user needs to see that nothing was re-embedded.
func writeIngestReport(out io.Writer, libraryID string, r ingest.Report) {
	fmt.Fprintf(out, "\nLibrary: %s\n\n", libraryID)
	fmt.Fprintf(out, "Pages:\n")
	fmt.Fprintf(out, "  discovered: %d\n", r.PagesDiscovered)
	fmt.Fprintf(out, "  fetched:    %d\n", r.PagesFetched)
	fmt.Fprintf(out, "  skipped:    %d\n", r.PagesSkipped)
	fmt.Fprintf(out, "\nChunks:\n")
	fmt.Fprintf(out, "  unchanged:  %d\n", r.ChunksUnchanged)
	fmt.Fprintf(out, "  updated:    %d\n", r.ChunksUpdated)
	fmt.Fprintf(out, "  added:      %d\n", r.ChunksAdded)
	fmt.Fprintf(out, "  removed:    %d\n", r.ChunksRemoved)
	fmt.Fprintf(out, "  indexed:    %d\n", r.ChunksAdded+r.ChunksUpdated)
}

func writeTable(out io.Writer, rows [][]string) {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, row := range rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	w.Flush()
}

// findSource resolves a CLI argument, which may be a short name or a full
// library ID.
func findSource(ctx context.Context, rt *Runtime, name string) (source.Source, error) {
	if strings.HasPrefix(name, "/") {
		return findSourceByLibraryID(ctx, rt, name)
	}

	list, err := rt.Sources.List(ctx)
	if err != nil {
		return source.Source{}, fmt.Errorf("list libraries: %w", err)
	}

	slug := source.Slug(name)
	var matches []source.Source
	for _, s := range list {
		if source.Slug(s.Name) == slug {
			matches = append(matches, s)
		}
	}

	switch len(matches) {
	case 0:
		return source.Source{}, fmt.Errorf("library %q is not indexed; run `docmcp list`", name)
	case 1:
		return matches[0], nil
	default:
		ids := make([]string, 0, len(matches))
		for _, s := range matches {
			ids = append(ids, s.LibraryID)
		}
		return source.Source{}, fmt.Errorf(
			"%q matches several versions: %s; use a full library ID",
			name, strings.Join(ids, ", "))
	}
}

func findSourceByLibraryID(ctx context.Context, rt *Runtime, libraryID string) (source.Source, error) {
	list, err := rt.Sources.List(ctx)
	if err != nil {
		return source.Source{}, fmt.Errorf("list libraries: %w", err)
	}

	for _, s := range list {
		if s.LibraryID == libraryID {
			return s, nil
		}
	}
	return source.Source{}, fmt.Errorf("library %s is not indexed; run `docmcp list`", libraryID)
}

// countLibrary derives page and chunk counts for one library. Pages come from the
// distinct source URLs in the index, so a page with no chunks still counts.
func countLibrary(ctx context.Context, rt *Runtime, src source.Source) (pages, chunks int, err error) {
	chunks, err = rt.Store.CountChunks(ctx, src.LibraryID)
	if err != nil {
		return 0, 0, fmt.Errorf("count chunks for %s: %w", src.LibraryID, err)
	}

	all, err := rt.Store.ListChunks(ctx, store.ListFilter{LibraryID: src.LibraryID})
	if err != nil {
		return 0, 0, fmt.Errorf("list chunks for %s: %w", src.LibraryID, err)
	}

	seen := map[string]bool{}
	for _, c := range all {
		if c.URL != "" {
			seen[c.URL] = true
		}
	}
	return len(seen), chunks, nil
}

func markSynced(ctx context.Context, rt *Runtime, src source.Source) {
	now := time.Now().UTC()
	src.SyncedAt = &now

	// A failure here is not worth failing the run: the index is already correct.
	_ = rt.Sources.Update(ctx, src)
}

// newReindexCommand rebuilds a library's vectors from scratch.
//
// It exists because an index is tied to the embedding model that wrote it.
// Changing provider, model, or version means the stored vectors no longer belong
// together, so the only safe repair is to discard them and re-ingest. That means
// clearing the recorded identity first: without that, the ingest would refuse
// for exactly the reason the user ran this command.
func newReindexCommand(factory runtimeFactory) *cobra.Command {
	d := &deps{factory: factory}

	return &cobra.Command{
		Use:   "reindex <name>",
		Short: "Rebuild a library's vectors, for example after changing the embedding model",
		Long: "Discard a library's indexed chunks and re-ingest it from scratch.\n\n" +
			"Use this after changing embedding.provider or embedding.model. " +
			"Ordinary updates should use `docmcp sync`, which embeds only what changed.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			rt, err := d.open(ctx, cmd)
			if err != nil {
				return err
			}
			defer rt.Close()

			src, err := findSource(ctx, rt, args[0])
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()

			if err := rt.Ingestor.Remove(ctx, src.ID); err != nil {
				return fmt.Errorf("clear chunks for %s: %w", src.LibraryID, err)
			}
			if err := rt.Store.SetIdentity(ctx, ""); err != nil {
				return fmt.Errorf("clear index identity: %w", err)
			}

			fmt.Fprintf(out, "Rebuilding %s...\n", src.LibraryID)

			report, err := rt.Ingestor.Ingest(ctx, src)
			if err != nil {
				return fmt.Errorf("reindex %s: %w", src.LibraryID, err)
			}

			markSynced(ctx, rt, src)
			writeIngestReport(out, src.LibraryID, report)
			return nil
		},
	}
}
