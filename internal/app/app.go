package app

import (
	"github.com/spf13/cobra"
)

// NewRootCommand builds the docmcp CLI. version is injected so the binary's
// build-stamped version is the only thing main.go contributes.
func NewRootCommand(version string) *cobra.Command {
	root := &cobra.Command{
		Use:           "docmcp",
		Short:         "Index documentation sites and serve them over MCP",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(newVersionCommand(version))

	return root
}

func newVersionCommand(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the docmcp version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := cmd.OutOrStdout().Write([]byte(version + "\n"))
			return err
		},
	}
}
