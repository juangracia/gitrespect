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
	compareCmd.Flags().StringSliceVarP(&team, "team", "t", nil, "Team mode: compare multiple authors (comma-separated emails)")
	compareCmd.Flags().StringVarP(&output, "output", "o", "terminal", "Output format: terminal, json, or html")
	compareCmd.Flags().StringVarP(&file, "file", "f", "", "Output file path")
	compareCmd.Flags().StringVar(&theme, "theme", "dark", "HTML theme: dark or light")
	compareCmd.Flags().StringSliceVarP(&exclude, "exclude", "e", nil, "Exclude files matching glob patterns")

	compareCmd.MarkFlagRequired("before")
	compareCmd.MarkFlagRequired("after")

	rootCmd.AddCommand(compareCmd)
}

func parsePeriod(period string) (time.Time, time.Time, error) {
	parts := strings.Split(period, ":")
	if len(parts) != 2 {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid period format %q, expected YYYY-MM:YYYY-MM", period)
	}

	// Reuse the shared parsers so periods resolve in local time and the end
	// bound covers the whole final month or day.
	start, err := git.ParseDate(parts[0])
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid start date %q: %w", parts[0], err)
	}

	end, err := git.ParseDateEnd(parts[1])
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid end date %q: %w", parts[1], err)
	}

	if !end.After(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("period %q ends before it starts", period)
	}

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

	if err := validateOutputFlags("", output, theme); err != nil {
		return err
	}
	if err := git.ValidateExcludePatterns(exclude); err != nil {
		return err
	}

	beforeStart, beforeEnd, err := parsePeriod(beforePeriod)
	if err != nil {
		return fmt.Errorf("invalid --before: %w", err)
	}

	afterStart, afterEnd, err := parsePeriod(afterPeriod)
	if err != nil {
		return fmt.Errorf("invalid --after: %w", err)
	}

	if len(team) > 0 {
		if err := checkTeamConflicts(author); err != nil {
			return err
		}
		beforeTeam, _ := buildTeamStats(paths, team, beforeStart, beforeEnd)
		afterTeam, _ := buildTeamStats(paths, team, afterStart, afterEnd)
		if len(beforeTeam.Members) == 0 && len(afterTeam.Members) == 0 {
			return fmt.Errorf("no team members could be analyzed")
		}
		comparison := git.TeamCompareStats{
			Before:      beforeTeam,
			After:       afterTeam,
			BeforeLabel: beforePeriod,
			AfterLabel:  afterPeriod,
		}
		switch output {
		case "json":
			return report.TeamCompareJSON(comparison, file)
		case "html":
			return report.TeamCompareHTML(comparison, file, theme)
		default:
			return report.TeamCompareTerminal(comparison)
		}
	}

	authorEmail, err := resolveAuthor(author, paths[0])
	if err != nil {
		return err
	}

	// Analyze both periods
	var beforeStats, afterStats []git.RepoStats

	for _, path := range paths {
		bStats, err := git.Analyze(path, authorEmail, beforeStart, beforeEnd, exclude)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to analyze %s: %v\n", path, err)
			continue
		}
		beforeStats = append(beforeStats, bStats)

		aStats, err := git.Analyze(path, authorEmail, afterStart, afterEnd, exclude)
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
		return report.CompareHTML(comparison, file, theme)
	default:
		return report.CompareTerminal(comparison)
	}
}
