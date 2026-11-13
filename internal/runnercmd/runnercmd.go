// Package runnercmd implements the tgp-runner command line.
package runnercmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/version"
)

// Execute runs the CLI and returns the process exit code.
func Execute(args []string) int {
	root := &cobra.Command{
		Use:           "tgp-runner",
		Short:         "Run tgp CI jobs in Docker containers",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version.Version,
	}
	root.SetVersionTemplate(version.String("tgp-runner") + "\n")
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		Run:   func(cmd *cobra.Command, _ []string) { fmt.Fprintln(cmd.OutOrStdout(), version.String("tgp-runner")) },
	})
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}
