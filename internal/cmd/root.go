package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/juangracia/gitrespect/internal/git"
	"github.com/juangracia/gitrespect/internal/report"
	"github.com/spf13/cobra"
)

var (
	author    string
	since     string
	until     string
	breakdown string
	output    string
	file      string
	year      int
)

var rootCmd = &cobra.Command{
	Use:   "gitrespect [paths...]",
	Short: "Respect your git work with real metrics",
	Long: `gitrespect analyzes git repositories and provides developer productivity metrics.

Run in any git repository to see your contribution statistics including
lines added, deleted, net changes, and comparisons to industry benchmarks.`,
	Args: cobra.ArbitraryArgs,
	RunE: runAnalyze,
}

func init() {
	rootCmd.Flags().StringVarP(&author, "author", "a", "", "Filter by author email (default: git config user.email)")
	rootCmd.Flags().StringVarP(&since, "since", "s", "30 days ago", "Start date (YYYY-MM-DD or relative like '30 days ago')")
	rootCmd.Flags().StringVarP(&until, "until", "u", "", "End date (default: now)")
	rootCmd.Flags().StringVarP(&breakdown, "breakdown", "b", "", "Show breakdown: monthly, weekly, or daily")
	rootCmd.Flags().StringVarP(&output, "output", "o", "terminal", "Output format: terminal, json, or html")
	rootCmd.Flags().StringVarP(&file, "file", "f", "", "Output file path (for html/json)")
	rootCmd.Flags().IntVar(&year, "year", 0, "Filter by year (e.g., --year=2025)")
}

func Execute() error {
	return rootCmd.Execute()
}

func runAnalyze(cmd *cobra.Command, args []string) error {
	paths := args
	if len(paths) == 0 {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
		paths = []string{cwd}
	}

	// Resolve paths to absolute
	for i, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return fmt.Errorf("invalid path %s: %w", p, err)
		}
		paths[i] = abs
	}

	// Parse dates
	var sinceTime, untilTime time.Time
	var err error

	if year > 0 {
		sinceTime = time.Date(year, 1, 1, 0, 0, 0, 0, time.Local)
		untilTime = time.Date(year, 12, 31, 23, 59, 59, 0, time.Local)
		if untilTime.After(time.Now()) {
			untilTime = time.Now()
		}
	} else {
		sinceTime, err = git.ParseDate(since)
		if err != nil {
			return fmt.Errorf("invalid --since date: %w", err)
		}

		if until == "" {
			untilTime = time.Now()
		} else {
			untilTime, err = git.ParseDate(until)
			if err != nil {
				return fmt.Errorf("invalid --until date: %w", err)
			}
		}
	}

	// Get author if not specified
	authorEmail := author
	if authorEmail == "" {
		authorEmail, _ = git.GetDefaultAuthor(paths[0])
	}

	// Analyze repositories
	var allStats []git.RepoStats
	for _, path := range paths {
		stats, err := git.Analyze(path, authorEmail, sinceTime, untilTime)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to analyze %s: %v\n", path, err)
			continue
		}
		allStats = append(allStats, stats)
	}

	if len(allStats) == 0 {
		return fmt.Errorf("no repositories could be analyzed")
	}

	// Aggregate stats
	combined := git.CombineStats(allStats)

	// Generate output
	switch output {
	case "json":
		return report.JSON(combined, file, breakdown)
	case "html":
		return report.HTML(combined, file, breakdown)
	default:
		return report.Terminal(combined, breakdown)
	}
}
