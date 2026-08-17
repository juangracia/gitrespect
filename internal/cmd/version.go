package cmd

import (
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

var (
	// Version is set by goreleaser at build time
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func init() {
	fillVersionFromBuildInfo()
	rootCmd.AddCommand(versionCmd)
}

// fillVersionFromBuildInfo recovers version details for builds that goreleaser
// did not stamp, which is what `go install ...@latest` produces. Without this
// an installed release binary reports itself as "dev".
func fillVersionFromBuildInfo() {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	var revision, buildTime string
	fromVCS := false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision, fromVCS = s.Value, true
		case "vcs.time":
			buildTime = s.Value
		}
	}
	Version, Commit, Date = resolveVersion(
		Version, Commit, Date, info.Main.Version, revision, buildTime, fromVCS)
}

// resolveVersion decides what to report given the ldflags-stamped values and
// what the Go build info carries.
//
// Only trust the module version when the binary came from the module proxy. A
// build from a local checkout derives a version from the nearest tag, which
// would claim the last release rather than admitting it is an untagged working
// copy.
func resolveVersion(version, commit, date, moduleVersion, revision, buildTime string, fromVCS bool) (string, string, string) {
	if version == "dev" && !fromVCS {
		// "(devel)" already means an unreleased build, which "dev" conveys.
		if moduleVersion != "" && moduleVersion != "(devel)" {
			version = strings.TrimPrefix(moduleVersion, "v")
		}
	}
	if commit == "none" && revision != "" {
		commit = revision
		if len(commit) > 7 {
			commit = commit[:7]
		}
		if date == "unknown" {
			date = buildTime
		}
	}
	return version, commit, date
}

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
