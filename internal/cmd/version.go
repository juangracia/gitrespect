package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	// Version is set by goreleaser at build time
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("gitrespect %s\n", Version)
		if Commit != "none" {
			fmt.Printf("  commit: %s\n", Commit)
			fmt.Printf("  built:  %s\n", Date)
		}
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
