package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/juangracia/gitrespect/internal/git"
	"github.com/juangracia/gitrespect/internal/prs"
	"github.com/juangracia/gitrespect/internal/report"
	"github.com/spf13/cobra"
)

var (
	beforePeriod string
	afterPeriod  string
	compareData  string
)

// compareDataSources are the values --data accepts.
const (
	dataLines = "lines"
	dataPRs   = "prs"
)

var compareCmd = &cobra.Command{
	Use:   "compare [paths...]",
	Short: "Compare productivity between two time periods",
	Long: `Compare your productivity metrics between two time periods.

Useful for measuring the impact of tooling changes, AI adoption, or other
workflow improvements.

Whose commits: pass -a for one person, -t for a team, or --all-authors for
everything in the repository. With none of those it uses the repository's
configured user.email. --roster and --alias merge one person's several
addresses, exactly as they do for the main command.

-b/--breakdown adds each period's trend. The two periods are broken down
SEPARATELY, since they can differ in length and need not be adjacent.

--data=prs compares merge request volume instead of lines of code, giving the
same Nx multiplier for review throughput. It queries the platform rather than
local git, so it needs --group (GitLab) or --org (GitHub), and a token from
--token or GITLAB_TOKEN/GITHUB_TOKEN, or a locally authenticated glab/gh.

Examples:
  gitrespect compare --before=2025-01:2025-07 --after=2025-08:2025-12
  gitrespect compare -t a@x.com,b@x.com --before=2025-01:2025-06 --after=2025-07:2025-12 -b monthly
  gitrespect compare --all-authors --before=2025-01:2025-06 --after=2025-07:2025-12
  gitrespect compare --data=prs --group my-group/web --before=2025-01:2025-06 --after=2025-07:2025-12`,
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
	compareCmd.Flags().StringVarP(&breakdown, "breakdown", "b", "", "Show breakdown per period: monthly, weekly, or daily")
	compareCmd.Flags().BoolVar(&allAuthors, "all-authors", false, "Compare every author's commits, unfiltered")
	compareCmd.Flags().StringVar(&rosterPath, "roster", "", "Roster file mapping a canonical name to that person's email addresses")
	compareCmd.Flags().StringArrayVar(&aliasSpecs, "alias", nil, "Inline identity: 'Name=a@x.com,b@x.com' (repeatable)")
	compareCmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "Scan subdirectories for git repositories")

	// Comparing merge request volume answers the same "did this change help"
	// question as lines of code, and for a review-heavy team it is the more
	// honest signal, so it shares this command rather than growing its own.
	compareCmd.Flags().StringVar(&compareData, "data", dataLines, "What to compare: lines or prs")
	compareCmd.Flags().StringVar(&prsProvider, "provider", prs.ProviderGitLab, "With --data=prs: review platform, gitlab or github")
	compareCmd.Flags().StringVar(&prsGroup, "group", "", "With --data=prs: GitLab group path or id")
	compareCmd.Flags().StringVar(&prsOrg, "org", "", "With --data=prs: GitHub organization")
	compareCmd.Flags().StringArrayVar(&prsMap, "map", nil, "With --data=prs: pin a platform account to an identity (repeatable)")
	compareCmd.Flags().StringVar(&prsToken, "token", "", "With --data=prs: API token (prefer GITLAB_TOKEN / GITHUB_TOKEN)")
	compareCmd.Flags().StringVar(&prsAPIURL, "api-url", "", "With --data=prs: API root for a self-hosted instance")

	compareCmd.MarkFlagRequired("before")
	compareCmd.MarkFlagRequired("after")

	rootCmd.AddCommand(compareCmd)
}

// runComparePRs answers the same before/after question as the line based
// comparison, but against merge request volume.
//
// This is the case the git based compare cannot reach: a merge request lives on
// the review platform, so quantifying "we opened Nx more MRs after adopting X"
// previously meant building a separate API client and duplicating the
// multiplier arithmetic by hand.
func runComparePRs(cmd *cobra.Command, before, after prs.Window) error {
	scope, err := resolvePRScope(prsProvider, prsGroup, prsOrg)
	if err != nil {
		return err
	}
	roster, err := buildRoster(rosterPath, aliasSpecs)
	if err != nil {
		return err
	}
	people, grouping, err := resolvePRIdentities(author, team, roster)
	if err != nil {
		return err
	}
	if breakdown != "" {
		fmt.Fprintln(os.Stderr, "note: --breakdown is not applied to --data=prs; the comparison reports per-period totals")
	}

	comparison, err := prs.FetchComparison(cmd.Context(), prs.Options{
		Provider: prsProvider,
		Scope:    scope,
		People:   people,
		Roster:   grouping,
		Mappings: prsMap,
		Token:    prsToken,
		BaseURL:  prsAPIURL,
	}, before, after)
	if err != nil {
		return err
	}

	switch output {
	case "json":
		return prs.RenderComparisonJSON(comparison, file)
	case "html":
		return fmt.Errorf("--output html is not supported for --data=prs yet: use terminal or json")
	default:
		return prs.RenderComparisonTerminal(comparison)
	}
}

// warnBreakdownUnsupported says so when --breakdown cannot be rendered, rather
// than accepting the flag and quietly producing a report without it.
//
// The HTML comparison has no breakdown section yet. Silence would make a
// documented flag look broken, which is exactly the failure this release set out
// to remove elsewhere.
func warnBreakdownUnsupported(output string) {
	if breakdown != "" && output == "html" {
		fmt.Fprintln(os.Stderr,
			"note: --breakdown is not rendered in the HTML comparison yet; use --output terminal or json for the per-period trend")
	}
}

// checkCompareDataFlags rejects review-platform flags on a lines comparison.
//
// Every one of them is documented as "With --data=prs", so accepting and
// discarding them hands back a plausible lines report to someone who meant to
// query a platform. --token is the worst of the set: silently ignoring a
// credential the user deliberately supplied is never the right default.
func checkCompareDataFlags(cmd *cobra.Command) error {
	if compareData == dataPRs {
		return nil
	}
	var set []string
	for _, name := range []string{"provider", "group", "org", "map", "token", "api-url"} {
		if cmd.Flags().Changed(name) {
			set = append(set, "--"+name)
		}
	}
	if len(set) > 0 {
		return fmt.Errorf("%s only apply to --data=prs, but --data is %q: add --data=prs, or drop %s",
			strings.Join(set, ", "), compareData, strings.Join(set, ", "))
	}
	return nil
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

	var resolved []string
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return fmt.Errorf("invalid path %s: %w", p, err)
		}
		if recursive {
			repos, err := git.FindRepos(abs)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to scan %s: %v\n", abs, err)
				continue
			}
			resolved = append(resolved, repos...)
			continue
		}
		resolved = append(resolved, abs)
	}
	paths = dedupeRepos(resolved)
	if len(paths) == 0 {
		return fmt.Errorf("no git repositories found")
	}

	if err := validateOutputFlags(breakdown, output, theme); err != nil {
		return err
	}
	if err := checkSelectionConflicts(cmd); err != nil {
		return err
	}
	if err := checkCompareDataFlags(cmd); err != nil {
		return err
	}
	if err := git.ValidateExcludePatterns(exclude); err != nil {
		return err
	}
	warnUnusedFile(output, file)

	beforeStart, beforeEnd, err := parsePeriod(beforePeriod)
	if err != nil {
		return fmt.Errorf("invalid --before: %w", err)
	}

	afterStart, afterEnd, err := parsePeriod(afterPeriod)
	if err != nil {
		return fmt.Errorf("invalid --after: %w", err)
	}

	switch compareData {
	case dataLines:
	case dataPRs:
		return runComparePRs(cmd,
			prs.Window{Label: beforePeriod, Since: beforeStart, Until: beforeEnd},
			prs.Window{Label: afterPeriod, Since: afterStart, Until: afterEnd})
	default:
		return fmt.Errorf("invalid --data %q: expected %s or %s", compareData, dataLines, dataPRs)
	}

	roster, err := buildRoster(rosterPath, aliasSpecs)
	if err != nil {
		return err
	}

	if len(team) > 0 {
		members := expandTeam(team, roster)
		beforeTeam, _ := buildTeamStats(paths, members, beforeStart, beforeEnd)
		afterTeam, _ := buildTeamStats(paths, members, afterStart, afterEnd)
		if len(beforeTeam.Members) == 0 && len(afterTeam.Members) == 0 {
			return fmt.Errorf("no team members could be analyzed")
		}
		comparison := git.TeamCompareStats{
			Before:      beforeTeam,
			After:       afterTeam,
			BeforeLabel: beforePeriod,
			AfterLabel:  afterPeriod,
		}
		warnBreakdownUnsupported(output)
		switch output {
		case "json":
			return report.TeamCompareJSONWithBreakdown(comparison, file, breakdown)
		case "html":
			return report.TeamCompareHTML(comparison, file, theme)
		default:
			return report.TeamCompareTerminalWithBreakdown(comparison, breakdown)
		}
	}

	identity, err := resolveIdentity(paths[0], roster)
	if err != nil {
		return err
	}

	// Analyze both periods
	var beforeStats, afterStats []git.RepoStats

	for _, path := range paths {
		bStats, err := git.AnalyzeIdentity(path, identity, beforeStart, beforeEnd, exclude)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to analyze %s: %v\n", path, err)
			continue
		}
		beforeStats = append(beforeStats, bStats)

		aStats, err := git.AnalyzeIdentity(path, identity, afterStart, afterEnd, exclude)
		if err != nil {
			// Dropping a repo from only one side compares unequal repo sets and
			// reports the difference as a productivity change, so this cannot
			// stay silent the way it used to be.
			fmt.Fprintf(os.Stderr,
				"Warning: %s analyzed for the before period but not the after period (%v); "+
					"the multiplier below compares unequal repository sets\n", path, err)
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

	warnBreakdownUnsupported(output)
	switch output {
	case "json":
		return report.CompareJSONWithBreakdown(comparison, file, breakdown)
	case "html":
		return report.CompareHTML(comparison, file, theme)
	default:
		return report.CompareTerminalWithBreakdown(comparison, breakdown)
	}
}
