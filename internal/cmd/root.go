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
	allAuthors      bool
	topN            int
	excludeAuthors  []string
	rosterPath      string
	aliasSpecs      []string
	chart           bool
	highlight       string
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
	rootCmd.Flags().BoolVar(&allAuthors, "all-authors", false, "Analyze every author's commits, unfiltered (whole repo or org)")
	rootCmd.Flags().IntVar(&topN, "top", 0, "Auto-discover the top N contributors by commits and run team mode on them")
	rootCmd.Flags().StringSliceVar(&excludeAuthors, "exclude-authors", nil, "Extra regexes excluded from --top, on top of the built-in bot filter")
	rootCmd.Flags().StringVar(&rosterPath, "roster", "", "Roster file mapping a canonical name to that person's email addresses")
	rootCmd.Flags().StringArrayVar(&aliasSpecs, "alias", nil, "Inline identity: 'Name=a@x.com,b@x.com' (repeatable)")
	rootCmd.Flags().BoolVar(&chart, "chart", false, "Include a trend chart in the HTML report (needs --breakdown)")
	rootCmd.Flags().StringVar(&highlight, "highlight", "", "In team mode, emphasise this member against the team average in the chart")
}

// checkSelectionConflicts rejects combinations that pick contributors in two
// different ways at once.
//
// Each of these flags answers the question "whose commits am I counting", and
// silently letting one win would produce a confident report about the wrong
// people. Failing here costs a re-run; guessing costs a wrong number that
// nobody catches.
func checkSelectionConflicts() error {
	selectors := []struct {
		on   bool
		name string
	}{
		{author != "", "--author"},
		{len(team) > 0, "--team"},
		{allAuthors, "--all-authors"},
		{topN > 0, "--top"},
	}
	var on []string
	for _, s := range selectors {
		if s.on {
			on = append(on, s.name)
		}
	}
	if len(on) > 1 {
		return fmt.Errorf("%s are mutually exclusive: each selects who to count, so pass exactly one",
			strings.Join(on, " and "))
	}
	if topN < 0 {
		return fmt.Errorf("invalid --top %d: expected a positive number of contributors", topN)
	}
	if len(excludeAuthors) > 0 && topN == 0 {
		return fmt.Errorf("--exclude-authors only applies to --top, which is not set")
	}
	if highlight != "" && len(team) == 0 && topN == 0 {
		return fmt.Errorf("--highlight names a team member, so it needs --team or --top")
	}
	return nil
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

// checkTeamConflicts rejects flags that team mode would otherwise ignore in
// silence, so a user who thinks they are filtering actually is.
func checkTeamConflicts(author string) error {
	if author != "" {
		return fmt.Errorf("--author and --team are mutually exclusive: pass one or the other")
	}
	return nil
}

// resolveAuthor returns the explicit --author, or falls back to the repo's
// configured user.email. An empty author would match every commit in the
// repository, so report the whole repo as one person's work is refused.
func resolveAuthor(explicit, repoPath string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	email, err := git.GetDefaultAuthor(repoPath)
	if err != nil || strings.TrimSpace(email) == "" {
		return "", fmt.Errorf("could not determine author: git config user.email is unset; pass --author")
	}
	return email, nil
}

// warnUnusedChart tells the user when --chart cannot take effect. The chart is
// drawn from breakdown buckets and only appears in HTML, so asking for one
// without either is a mistake worth naming rather than a silent no-op.
func warnUnusedChart(output string, chart bool, breakdown string) {
	if !chart {
		return
	}
	if output != "html" {
		fmt.Fprintln(os.Stderr, "note: --chart only applies to --output html; ignoring it")
		return
	}
	if breakdown == "" {
		fmt.Fprintln(os.Stderr, "note: --chart needs --breakdown to have periods to plot; ignoring it")
	}
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
	paths = dedupeRepos(resolvedPaths)

	if len(paths) == 0 {
		return fmt.Errorf("no git repositories found")
	}

	// Parse dates
	var sinceTime, untilTime time.Time
	var err error

	if err := validateOutputFlags(breakdown, output, theme); err != nil {
		return err
	}
	if err := checkSelectionConflicts(); err != nil {
		return err
	}
	if err := git.ValidateExcludePatterns(exclude); err != nil {
		return err
	}
	warnUnusedFile(output, file)
	warnUnusedChart(output, chart, breakdown)

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

	roster, err := buildRoster(rosterPath, aliasSpecs)
	if err != nil {
		return err
	}

	// Team mode, either named explicitly or discovered with --top.
	if len(team) > 0 || topN > 0 {
		members := expandTeam(team, roster)
		if topN > 0 {
			members, err = discoverTeam(paths, sinceTime, untilTime, topN, excludeAuthors, roster)
			if err != nil {
				return err
			}
		}
		return runTeamAnalysis(paths, members, sinceTime, untilTime)
	}

	identity, err := resolveIdentity(paths[0], roster)
	if err != nil {
		return err
	}

	// Analyze repositories
	var allStats []git.RepoStats
	for _, path := range paths {
		stats, err := git.AnalyzeIdentity(path, identity, sinceTime, untilTime, exclude)
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

	// Opt-in metrics pool their samples across every repository that was
	// analysed, so a distribution or a median describes the same body of work
	// the headline totals do. Computing them from a single "primary" repo made
	// the numbers look like they covered everything when they did not.
	analysed := analysedPaths(allStats, paths[0])
	bundle := computeOptInMetrics(analysed, identity.Emails, sinceTime, untilTime, selection, cWindow, exclude)
	bundle.LegacyBenchmark = legacyBenchmark

	if !legacyBenchmark {
		baseline, err := metrics.ComputeBaselineAcross(analysed, identity.Emails, sinceTime, bWindow, exclude)
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
		return report.HTMLWithOptions(combined, file, breakdown, theme, bundle,
			report.ChartOptions{Enabled: chart})
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
func buildTeamStats(paths []string, members []git.Identity, since, until time.Time) (git.TeamStats, map[string][]git.RepoStats) {
	teamStats := git.TeamStats{
		Since:   since,
		Until:   until,
		Members: make(map[string]git.RepoStats),
	}
	perMember := make(map[string][]git.RepoStats)
	var memberCombined []git.RepoStats

	for _, member := range members {
		// One git log per repo carrying every address this person uses. Running
		// one pass per address and adding the results would double-count any
		// commit that two of their patterns both match.
		var memberStats []git.RepoStats
		for _, path := range paths {
			stats, err := git.AnalyzeIdentity(path, member, since, until, exclude)
			if err != nil {
				continue
			}
			memberStats = append(memberStats, stats)
		}

		if len(memberStats) == 0 {
			continue
		}

		label := member.Label()
		combined := git.CombineStats(memberStats)
		combined.Author = label
		teamStats.Members[label] = combined
		teamStats.TotalAdded += combined.Added
		teamStats.TotalDeleted += combined.Deleted
		teamStats.TotalNet += combined.Net
		teamStats.TotalCommits += combined.Commits
		memberCombined = append(memberCombined, combined)
		perMember[label] = memberStats
	}

	agg := git.CombineStats(memberCombined)
	teamStats.Monthly = agg.Monthly
	teamStats.Daily = agg.Daily
	return teamStats, perMember
}

func runTeamAnalysis(paths []string, members []git.Identity, sinceTime, untilTime time.Time) error {
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

	if highlight != "" {
		if _, ok := teamStats.Members[resolveHighlight(highlight, members)]; !ok {
			return fmt.Errorf("--highlight %q is not one of the analyzed team members", highlight)
		}
	}

	// Per-member opt-in metrics, pooled across every repo that member touched
	// rather than drawn from whichever single repo they committed to most.
	bundles := make(map[string]metrics.Bundle)
	if selection.Any() {
		byLabel := make(map[string]git.Identity, len(members))
		for _, m := range members {
			byLabel[m.Label()] = m
		}
		for label, memberStats := range perMember {
			bundles[label] = computeOptInMetrics(
				analysedPaths(memberStats, paths[0]), byLabel[label].Emails,
				sinceTime, untilTime, selection, cWindow, exclude)
		}
	}

	// --per-repo in team mode reports one row per repository with every
	// member's contribution folded in, rather than one table per member, which
	// stops being readable past a handful of repos.
	var rollups []git.RepoRollup
	if perRepo {
		rollups = git.RollupByRepo(perMember)
	}

	chartOpts := report.ChartOptions{
		Enabled:   chart,
		Highlight: resolveHighlight(highlight, members),
	}

	// Generate output
	switch output {
	case "json":
		return report.TeamJSONWithRepos(teamStats, file, breakdown, bundles, rollups)
	case "html":
		return report.TeamHTMLWithOptions(teamStats, file, theme, breakdown, bundles, chartOpts)
	default:
		return report.TeamTerminalWithRepos(teamStats, breakdown, bundles, rollups)
	}
}

// resolveHighlight maps whatever the user typed onto the label the team report
// actually keys members by, so --highlight accepts an address even when the
// roster renamed that person.
func resolveHighlight(token string, members []git.Identity) string {
	if token == "" {
		return ""
	}
	for _, m := range members {
		if strings.EqualFold(m.Label(), token) {
			return m.Label()
		}
		for _, e := range m.Emails {
			if strings.EqualFold(e, token) {
				return m.Label()
			}
		}
	}
	return token
}

// analysedPaths lists the repositories that actually produced stats, so opt-in
// metrics are pooled over exactly the repos the totals were drawn from and the
// reported coverage count is honest.
func analysedPaths(stats []git.RepoStats, fallback string) []string {
	paths := make([]string, 0, len(stats))
	for _, s := range stats {
		if s.Path != "" {
			paths = append(paths, s.Path)
		}
	}
	if len(paths) == 0 {
		return []string{fallback}
	}
	return paths
}

// computeOptInMetrics computes the selected opt-in metrics for one person,
// pooling samples across every repository given.
//
// Each metric is best-effort: a failure leaves that field nil rather than
// aborting the whole report, since a missing lead time is no reason to withhold
// a commit-size distribution.
func computeOptInMetrics(paths []string, authors []string, since, until time.Time, sel metrics.Selection, cWindow time.Duration, exclude []string) metrics.Bundle {
	bundle := metrics.Bundle{Selection: sel}
	if sel.CommitSize {
		if d, err := metrics.ComputeCommitSizeAcross(paths, authors, since, until, exclude); err == nil {
			bundle.CommitSize = &d
		}
	}
	if sel.Cadence {
		if c, err := metrics.ComputeCadenceAcross(paths, authors, since, until); err == nil {
			bundle.Cadence = &c
		}
	}
	if sel.LeadTime {
		if lt, err := metrics.ComputeLeadTimeAcross(paths, authors, since, until); err == nil {
			bundle.LeadTime = &lt
		}
	}
	if sel.Churn {
		if ch, err := metrics.ComputeChurnAcross(paths, authors, since, until, cWindow, exclude); err == nil {
			bundle.Churn = &ch
		}
	}
	return bundle
}
