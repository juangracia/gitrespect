package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/juangracia/gitrespect/internal/git"
	"github.com/juangracia/gitrespect/internal/metrics"
	"github.com/juangracia/gitrespect/internal/report"
	"github.com/spf13/cobra"
)

var (
	author          string
	team            []string
	since           string
	until           string
	breakdown       string
	output          string
	file            string
	year            int
	theme           string
	recursive       bool
	perRepo         bool
	exclude         []string
	metricsFlag     string
	baselineWindow  string
	churnWindow     string
	legacyBenchmark bool
)

var rootCmd = &cobra.Command{
	Use:   "gitrespect [paths...]",
	Short: "Respect your git work with real metrics",
	Long: `gitrespect analyzes git repositories and provides developer productivity metrics.

Run in any git repository to see your contribution statistics including
lines added, deleted, net changes, and a comparison against your own prior
output (personal baseline). Add --metrics for commit size, integration
cadence, lead time, and churn.`,
	Args: cobra.ArbitraryArgs,
	RunE: runAnalyze,
}

func init() {
	rootCmd.Flags().StringVarP(&author, "author", "a", "", "Filter by author email (default: git config user.email)")
	rootCmd.Flags().StringSliceVarP(&team, "team", "t", nil, "Team mode: analyze multiple authors (comma-separated emails)")
	rootCmd.Flags().StringVarP(&since, "since", "s", "30 days ago", "Start date (YYYY-MM-DD or relative like '30 days ago')")
	rootCmd.Flags().StringVarP(&until, "until", "u", "", "End date (default: now)")
	rootCmd.Flags().StringVarP(&breakdown, "breakdown", "b", "", "Show breakdown: monthly, weekly, or daily")
	rootCmd.Flags().StringVarP(&output, "output", "o", "terminal", "Output format: terminal, json, or html")
	rootCmd.Flags().StringVarP(&file, "file", "f", "", "Output file path (for html/json)")
	rootCmd.Flags().IntVar(&year, "year", 0, "Filter by year (e.g., --year=2025)")
	rootCmd.Flags().StringVar(&theme, "theme", "dark", "HTML theme: dark or light")
	rootCmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "Scan subdirectories for git repositories")
	rootCmd.Flags().BoolVar(&perRepo, "per-repo", false, "Show breakdown by repository when analyzing multiple repos")
	rootCmd.Flags().StringSliceVarP(&exclude, "exclude", "e", nil, "Exclude files matching glob patterns (e.g., -e 'vendor/*' -e '*.generated.go')")
	rootCmd.Flags().StringVar(&metricsFlag, "metrics", "", "Opt-in metrics: comma list of churn,lead-time,commit-size,cadence, or 'all'")
	rootCmd.Flags().StringVar(&baselineWindow, "baseline-window", "90d", "Personal baseline window (e.g. 30d, 90d, 6m, 1y)")
	rootCmd.Flags().StringVar(&churnWindow, "churn-window", "30d", "Churn detection window")
	rootCmd.Flags().BoolVar(&legacyBenchmark, "legacy-benchmark", false, "Show deprecated Senior/Avg/Junior comparison instead of personal baseline")
}

func parseWindow(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) < 2 {
		return 0, fmt.Errorf("invalid window %q (examples: 30d, 90d, 6m, 1y)", raw)
	}
	unit := raw[len(raw)-1]
	n, err := strconv.Atoi(raw[:len(raw)-1])
	if err != nil {
		return 0, fmt.Errorf("invalid window %q: %w", raw, err)
	}
	switch unit {
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	case 'w':
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	case 'm':
		return time.Duration(n) * 30 * 24 * time.Hour, nil
	case 'y':
		return time.Duration(n) * 365 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown unit %q in %q (use d/w/m/y)", string(unit), raw)
	}
}

func Execute() error {
	// Runtime failures (bad repo, bad dates) are not usage errors, so don't
	// reprint the whole help screen after the message.
	rootCmd.SilenceUsage = true
	return rootCmd.Execute()
}

// validateOutputFlags rejects unsupported enum values up front, so a typo
// fails loudly instead of being silently ignored.
func validateOutputFlags(breakdown, output, theme string) error {
	if breakdown != "" && !git.ValidGranularity(breakdown) {
		return fmt.Errorf("invalid --breakdown %q: expected one of %s",
			breakdown, strings.Join(git.Granularities, ", "))
	}
	switch output {
	case "", "terminal", "json", "html":
	default:
		return fmt.Errorf("invalid --output %q: expected one of terminal, json, html", output)
	}
	switch theme {
	case "", "dark", "light":
	default:
		return fmt.Errorf("invalid --theme %q: expected one of dark, light", theme)
	}
	return nil
}

// warnUnusedFile tells the user when --file cannot take effect, rather than
// silently writing nothing.
func warnUnusedFile(output, file string) {
	if file != "" && output != "json" && output != "html" {
		fmt.Fprintf(os.Stderr,
			"note: --file is only used with --output json or html; ignoring %q\n", file)
	}
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
	var resolvedPaths []string
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return fmt.Errorf("invalid path %s: %w", p, err)
		}

		if recursive {
			// Find git repos in subdirectories
			repos, err := git.FindRepos(abs)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to scan %s: %v\n", abs, err)
				continue
			}
			resolvedPaths = append(resolvedPaths, repos...)
		} else {
			resolvedPaths = append(resolvedPaths, abs)
		}
	}
	paths = resolvedPaths

	if len(paths) == 0 {
		return fmt.Errorf("no git repositories found")
	}

	// Parse dates
	var sinceTime, untilTime time.Time
	var err error

	if err := validateOutputFlags(breakdown, output, theme); err != nil {
		return err
	}
	warnUnusedFile(output, file)

	if cmd.Flags().Changed("year") && year <= 0 {
		return fmt.Errorf("invalid --year %d: expected a four-digit year like 2025", year)
	}

	if year > 0 {
		sinceTime = time.Date(year, 1, 1, 0, 0, 0, 0, time.Local)
		untilTime = time.Date(year, 12, 31, 23, 59, 59, 0, time.Local)
		if sinceTime.After(time.Now()) {
			return fmt.Errorf("--year %d is in the future", year)
		}
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
			// ParseDateEnd so that --until 2025-03-05 covers all of 5 March
			// rather than stopping at midnight and dropping that day.
			untilTime, err = git.ParseDateEnd(until)
			if err != nil {
				return fmt.Errorf("invalid --until date: %w", err)
			}
		}
	}

	if !untilTime.After(sinceTime) {
		return fmt.Errorf("--since (%s) must be before --until (%s)",
			sinceTime.Format("2006-01-02"), untilTime.Format("2006-01-02"))
	}

	// Check if team mode is enabled
	if len(team) > 0 {
		return runTeamAnalysis(paths, team, sinceTime, untilTime)
	}

	// Get author if not specified
	authorEmail := author
	if authorEmail == "" {
		authorEmail, _ = git.GetDefaultAuthor(paths[0])
	}

	// Analyze repositories
	var allStats []git.RepoStats
	for _, path := range paths {
		stats, err := git.Analyze(path, authorEmail, sinceTime, untilTime, exclude)
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

	// Parse metric options
	selection, err := metrics.ParseSelection(metricsFlag)
	if err != nil {
		return err
	}
	bWindow, err := parseWindow(baselineWindow)
	if err != nil {
		return fmt.Errorf("invalid --baseline-window: %w", err)
	}
	cWindow, err := parseWindow(churnWindow)
	if err != nil {
		return fmt.Errorf("invalid --churn-window: %w", err)
	}

	// Pick the repo with the most author commits as the primary for opt-in metrics.
	primaryPath := primaryRepo(allStats, paths[0])
	bundle := computeOptInMetrics(primaryPath, authorEmail, sinceTime, untilTime, selection, cWindow, exclude)
	bundle.LegacyBenchmark = legacyBenchmark

	if !legacyBenchmark {
		baseline, err := metrics.ComputeBaseline(primaryPath, authorEmail, sinceTime, bWindow, exclude)
		if err == nil {
			wd := git.WorkingDays(sinceTime, untilTime)
			var locPerDay float64
			if wd > 0 {
				locPerDay = float64(combined.Net) / float64(wd)
			}
			baseline.SetPeriod(locPerDay)
			bundle.Baseline = &baseline
		}
	}

	// Generate output
	switch output {
	case "json":
		return report.JSON(combined, file, breakdown, bundle)
	case "html":
		return report.HTML(combined, file, breakdown, theme, bundle)
	default:
		if perRepo && len(allStats) > 1 {
			return report.TerminalWithRepos(combined, allStats, breakdown, bundle)
		}
		return report.Terminal(combined, breakdown, bundle)
	}
}

// buildTeamStats aggregates each member's stats across the given paths for a
// single period. It also returns the per-member, per-repo stats so callers can
// pick a primary repo for opt-in metrics.
func buildTeamStats(paths, members []string, since, until time.Time) (git.TeamStats, map[string][]git.RepoStats) {
	teamStats := git.TeamStats{
		Since:   since,
		Until:   until,
		Members: make(map[string]git.RepoStats),
	}
	perMember := make(map[string][]git.RepoStats)
	var memberCombined []git.RepoStats

	for _, member := range members {
		var memberStats []git.RepoStats
		for _, path := range paths {
			stats, err := git.Analyze(path, member, since, until, exclude)
			if err != nil {
				continue
			}
			memberStats = append(memberStats, stats)
		}

		if len(memberStats) == 0 {
			continue
		}

		combined := git.CombineStats(memberStats)
		teamStats.Members[member] = combined
		teamStats.TotalAdded += combined.Added
		teamStats.TotalDeleted += combined.Deleted
		teamStats.TotalNet += combined.Net
		teamStats.TotalCommits += combined.Commits
		memberCombined = append(memberCombined, combined)
		perMember[member] = memberStats
	}

	agg := git.CombineStats(memberCombined)
	teamStats.Monthly = agg.Monthly
	teamStats.Daily = agg.Daily
	return teamStats, perMember
}

func runTeamAnalysis(paths []string, members []string, sinceTime, untilTime time.Time) error {
	selection, err := metrics.ParseSelection(metricsFlag)
	if err != nil {
		return err
	}
	cWindow, err := parseWindow(churnWindow)
	if err != nil {
		return fmt.Errorf("invalid --churn-window: %w", err)
	}

	teamStats, perMember := buildTeamStats(paths, members, sinceTime, untilTime)

	if len(teamStats.Members) == 0 {
		return fmt.Errorf("no team members could be analyzed")
	}

	// Per-member opt-in metrics, computed on each member's primary repo.
	bundles := make(map[string]metrics.Bundle)
	if selection.Any() {
		for member, memberStats := range perMember {
			primary := primaryRepo(memberStats, paths[0])
			bundles[member] = computeOptInMetrics(primary, member, sinceTime, untilTime, selection, cWindow, exclude)
		}
	}

	// Generate output
	switch output {
	case "json":
		return report.TeamJSON(teamStats, file, breakdown, bundles)
	case "html":
		return report.TeamHTML(teamStats, file, theme, breakdown, bundles)
	default:
		return report.TeamTerminal(teamStats, breakdown, bundles)
	}
}

// primaryRepo returns the path of the repo with the most commits in stats,
// falling back to the given path when stats is empty.
func primaryRepo(stats []git.RepoStats, fallback string) string {
	primary := fallback
	maxCommits := -1
	for _, s := range stats {
		if s.Commits > maxCommits {
			maxCommits = s.Commits
			primary = s.Path
		}
	}
	return primary
}

// computeOptInMetrics computes the opt-in metrics selected on the given repo for
// one author. Each metric is best-effort: a failure leaves that field nil rather
// than aborting the whole report.
func computeOptInMetrics(primaryPath, author string, since, until time.Time, sel metrics.Selection, cWindow time.Duration, exclude []string) metrics.Bundle {
	bundle := metrics.Bundle{Selection: sel}
	if sel.CommitSize {
		if d, err := metrics.ComputeCommitSize(primaryPath, author, since, until, exclude); err == nil {
			bundle.CommitSize = &d
		}
	}
	if sel.Cadence {
		if c, err := metrics.ComputeCadence(primaryPath, author, since, until); err == nil {
			bundle.Cadence = &c
		}
	}
	if sel.LeadTime {
		if lt, err := metrics.ComputeLeadTime(primaryPath, author, since, until); err == nil {
			bundle.LeadTime = &lt
		}
	}
	if sel.Churn {
		if ch, err := metrics.ComputeChurn(primaryPath, author, since, until, cWindow, exclude); err == nil {
			bundle.Churn = &ch
		}
	}
	return bundle
}
