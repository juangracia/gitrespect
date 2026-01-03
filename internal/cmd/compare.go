package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/juangracia/gitrespect/internal/git"
	"github.com/juangracia/gitrespect/internal/report"
	"github.com/spf13/cobra"
)

var (
	beforePeriod string
	afterPeriod  string
)

var compareCmd = &cobra.Command{
	Use:   "compare [paths...]",
	Short: "Compare productivity between two time periods",
	Long: `Compare your productivity metrics between two time periods.

Useful for measuring the impact of tooling changes, AI adoption, or other
workflow improvements.

Example:
  gitrespect compare --before=2025-01:2025-07 --after=2025-08:2025-12`,
	RunE: runCompare,
}

func init() {
	compareCmd.Flags().StringVar(&beforePeriod, "before", "", "Before period (YYYY-MM:YYYY-MM)")
	compareCmd.Flags().StringVar(&afterPeriod, "after", "", "After period (YYYY-MM:YYYY-MM)")
	compareCmd.Flags().StringVarP(&author, "author", "a", "", "Filter by author email")
	compareCmd.Flags().StringVarP(&output, "output", "o", "terminal", "Output format: terminal, json, or html")
	compareCmd.Flags().StringVarP(&file, "file", "f", "", "Output file path")

	compareCmd.MarkFlagRequired("before")
	compareCmd.MarkFlagRequired("after")

	rootCmd.AddCommand(compareCmd)
}

func parsePeriod(period string) (time.Time, time.Time, error) {
	parts := strings.Split(period, ":")
	if len(parts) != 2 {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid period format, expected YYYY-MM:YYYY-MM")
	}

	start, err := time.Parse("2006-01", parts[0])
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid start date: %w", err)
	}

	end, err := time.Parse("2006-01", parts[1])
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid end date: %w", err)
	}

	// End should be last day of the month
	end = end.AddDate(0, 1, 0).Add(-time.Second)

	return start, end, nil
}

func runCompare(cmd *cobra.Command, args []string) error {
	paths := args
	if len(paths) == 0 {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
		paths = []string{cwd}
	}

	for i, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return fmt.Errorf("invalid path %s: %w", p, err)
		}
		paths[i] = abs
	}

	beforeStart, beforeEnd, err := parsePeriod(beforePeriod)
	if err != nil {
		return fmt.Errorf("invalid --before: %w", err)
	}

	afterStart, afterEnd, err := parsePeriod(afterPeriod)
	if err != nil {
		return fmt.Errorf("invalid --after: %w", err)
	}

	authorEmail := author
	if authorEmail == "" {
		authorEmail, _ = git.GetDefaultAuthor(paths[0])
	}

	// Analyze both periods
	var beforeStats, afterStats []git.RepoStats

	for _, path := range paths {
		bStats, err := git.Analyze(path, authorEmail, beforeStart, beforeEnd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to analyze %s: %v\n", path, err)
			continue
		}
		beforeStats = append(beforeStats, bStats)

		aStats, err := git.Analyze(path, authorEmail, afterStart, afterEnd)
		if err != nil {
			continue
		}
		afterStats = append(afterStats, aStats)
	}

	if len(beforeStats) == 0 || len(afterStats) == 0 {
		return fmt.Errorf("could not analyze repositories for both periods")
	}

	beforeCombined := git.CombineStats(beforeStats)
	afterCombined := git.CombineStats(afterStats)

	comparison := git.CompareStats{
		Before:      beforeCombined,
		After:       afterCombined,
		BeforeLabel: beforePeriod,
		AfterLabel:  afterPeriod,
	}

	switch output {
	case "json":
		return report.CompareJSON(comparison, file)
	case "html":
		return report.CompareHTML(comparison, file)
	default:
		return report.CompareTerminal(comparison)
	}
}
