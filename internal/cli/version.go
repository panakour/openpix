package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Set via goreleaser ldflags.
//
//nolint:gochecknoglobals
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// Version returns the human-readable build version string.
func Version() string { return fmt.Sprintf("%s (%s, built %s)", version, commit, date) }

func newVersionCmd() *cobra.Command {
	// nolint: exhaustruct
	return &cobra.Command{
		Use:   "version",
		Short: "Print detailed version information",
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Printf("openpix %s\n", version)
			cmd.Printf("  commit:   %s\n", commit)
			cmd.Printf("  built:    %s\n", date)
			cmd.Printf("  platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
			cmd.Printf("  go:       %s\n", runtime.Version())
		},
	}
}
