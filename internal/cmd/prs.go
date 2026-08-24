package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/juangracia/gitrespect/internal/git"
	"github.com/juangracia/gitrespect/internal/prs"
	"github.com/spf13/cobra"
)

// Flags shared by prsCmd and by compareCmd's --data=prs path. Only one command
// runs per invocation, so sharing is safe today, but a third consumer would be
// reading whatever the other two last defaulted: give it its own vars rather
// than adding to this block.
var (
	prsProvider string
	prsGroup    string
	prsOrg      string
	prsMap      []string
	prsToken    string
	prsAPIURL   string
)

// Flags used only by prsCmd. The analyze command's equivalents live in
// root.go; these are separate so the two commands cannot default over each
// other.
var (
	prsAuthor    string
	prsTeam      []string
	prsSince     string
	prsUntil     string
	prsYear      int
	prsBreakdown string
	prsOutput    string
	prsFile      string
	prsTop       int
	prsTheme     string
)

var prsCmd = &cobra.Command{
	Use:   "prs",
	Short: "Count merge requests / pull requests per person from the review platform",
	Long: `Report merge request (GitLab) or pull request (GitHub) activity per person.

Local git history cannot answer "how many MRs did X open per month versus the
team", because a merge request lives on the review platform, not in the object
database. This command asks the platform directly and shapes the answer like
the git based reports: a total, a per contributor table, and an optional
breakdown.

Authentication, tried in this order:
  1. --token, or the GITLAB_TOKEN / GITHUB_TOKEN environment variable.
  2. The locally authenticated CLI (glab or gh), which needs no token at all.
Prefer the environment variable over --token: a token on the command line is
visible to anyone who can list processes.

With no --author or --team the report covers everyone who opened a merge
request in the window, which is what makes a team average meaningful.

Examples:
  gitrespect prs --provider gitlab --group bunn-digital/web -t "a@x.com,b@x.com" --year=2025 -b monthly
  gitrespect prs --provider github --org my-org -a me@x.com --year=2025
  gitrespect prs --group bunn-digital/web --since 2025-01-01 -o html -f mrs.html`,
	Args: cobra.NoArgs,
	RunE: runPRs,
}

func init() {
	prsCmd.Flags().StringVar(&prsProvider, "provider", prs.ProviderGitLab, "Review platform: gitlab or github")
	prsCmd.Flags().StringVar(&prsGroup, "group", "", "GitLab group path or id (covers every project underneath)")
	prsCmd.Flags().StringVar(&prsOrg, "org", "", "GitHub organization")
	prsCmd.Flags().StringVarP(&prsAuthor, "author", "a", "", "Filter by a single identity (email, username or display name)")
	prsCmd.Flags().StringSliceVarP(&prsTeam, "team", "t", nil, "Team mode: report multiple identities (comma-separated)")
	prsCmd.Flags().StringArrayVar(&prsMap, "map", nil, "Pin a platform account to an identity: --map you@corp.com=handle (repeatable)")
	prsCmd.Flags().StringVar(&rosterPath, "roster", "", "Roster file mapping a canonical name to that person's email addresses")
	prsCmd.Flags().StringArrayVar(&aliasSpecs, "alias", nil, "Inline identity: 'Name=a@x.com,b@x.com' (repeatable)")
	prsCmd.Flags().StringVarP(&prsSince, "since", "s", "30 days ago", "Start date (YYYY-MM-DD or relative like '30 days ago')")
	prsCmd.Flags().StringVarP(&prsUntil, "until", "u", "", "End date (default: now)")
	prsCmd.Flags().IntVar(&prsYear, "year", 0, "Filter by year (e.g., --year=2025)")
	prsCmd.Flags().IntVar(&prsTop, "top", 0, "Show only the top N contributors (default: everyone; ignored with --author/--team)")
	prsCmd.Flags().StringVarP(&prsBreakdown, "breakdown", "b", "", "Show breakdown: monthly, weekly, or daily")
	prsCmd.Flags().StringVarP(&prsOutput, "output", "o", "terminal", "Output format: terminal, json, or html")
	prsCmd.Flags().StringVarP(&prsFile, "file", "f", "", "Output file path (for html/json)")
	prsCmd.Flags().StringVar(&prsTheme, "theme", "dark", "HTML theme: dark or light")
	prsCmd.Flags().StringVar(&prsToken, "token", "", "API token (prefer GITLAB_TOKEN / GITHUB_TOKEN)")
	prsCmd.Flags().StringVar(&prsAPIURL, "api-url", "", "API root for a self-hosted instance (e.g. https://gitlab.corp.com)")

	rootCmd.AddCommand(prsCmd)
}

func runPRs(cmd *cobra.Command, args []string) error {
	if err := validateOutputFlags(prsBreakdown, prsOutput, prsTheme); err != nil {
		return err
	}
	warnUnusedFile(prsOutput, prsFile)

	scope, err := resolvePRScope(prsProvider, prsGroup, prsOrg)
	if err != nil {
		return err
	}

	roster, err := buildRoster(rosterPath, aliasSpecs)
	if err != nil {
		return err
	}

	people, grouping, err := resolvePRIdentities(prsAuthor, prsTeam, roster)
	if err != nil {
		return err
	}

	since, until, err := resolvePRWindow(cmd)
	if err != nil {
		return err
	}

	opts := prs.Options{
		Provider:    prsProvider,
		Scope:       scope,
		People:      people,
		Roster:      grouping,
		Mappings:    prsMap,
		Since:       since,
		Until:       until,
		Granularity: prsBreakdown,
		Top:         prsTop,
		Token:       prsToken,
		BaseURL:     prsAPIURL,
	}

	result, err := prs.Fetch(cmd.Context(), opts)
	if err != nil {
		return err
	}

	switch prsOutput {
	case "json":
		return prs.RenderJSON(result, prsFile)
	case "html":
		return prs.RenderHTML(result, prsFile, prsTheme)
	default:
		return prs.RenderTerminal(result)
	}
}

// resolvePRScope maps the provider onto its scope flag and rejects the other
// one, so a --org typo against gitlab fails loudly instead of querying nothing.
func resolvePRScope(provider, group, org string) (string, error) {
	group, org = strings.TrimSpace(group), strings.TrimSpace(org)
	switch provider {
	case prs.ProviderGitLab:
		if org != "" {
			return "", fmt.Errorf("--org is a github flag: use --group with --provider gitlab")
		}
		if group == "" {
			return "", fmt.Errorf("--group is required (for example --group bunn-digital/web)")
		}
		return group, nil
	case prs.ProviderGitHub:
		if group != "" {
			return "", fmt.Errorf("--group is a gitlab flag: use --org with --provider github")
		}
		if org == "" {
			return "", fmt.Errorf("--org is required (for example --org my-org)")
		}
		return org, nil
	default:
		return "", fmt.Errorf("invalid --provider %q: expected one of %s",
			provider, strings.Join(prs.Providers, ", "))
	}
}

// prsPeople converts git identities into the prs package's grouped form, so a
// person the roster knows under three commit addresses stays one row in the
// merge request table instead of splitting into three.
func prsPeople(ids []git.Identity) []prs.Person {
	out := make([]prs.Person, 0, len(ids))
	for _, id := range ids {
		// Label is the canonical name, matching what the git report now
		// prints, so the two artefacts in a release name the same human the
		// same way.
		out = append(out, prs.Person{Label: id.Label(), Keys: id.Emails})
	}
	return out
}

// resolvePRIdentities turns --author/--team/--roster/--alias into the two sets
// prs.Options distinguishes.
//
// people both filters and groups: it is the answer to "who am I counting".
// grouping only groups: a roster describes who an account belongs to, not
// which accounts to count, so on its own it folds recognised accounts under
// their canonical names and leaves the rest of the group visible. Passing
// --author and --team together is rejected rather than silently preferring one.
func resolvePRIdentities(author string, team []string, roster git.Roster) (people, grouping []prs.Person, err error) {
	switch {
	case len(team) > 0:
		if err := checkTeamConflicts(author); err != nil {
			return nil, nil, err
		}
		return prsPeople(expandTeam(team, roster)), nil, nil
	case strings.TrimSpace(author) != "":
		return prsPeople([]git.Identity{expandIdentity(author, roster)}), nil, nil
	default:
		// No filter means every account in the scope, which is the only way a
		// "versus the team" comparison has a denominator.
		return nil, prsPeople(roster), nil
	}
}

// resolvePRWindow parses the date flags the same way the analyze command does,
// so --year and --since/--until behave identically across the CLI.
func resolvePRWindow(cmd *cobra.Command) (time.Time, time.Time, error) {
	if cmd.Flags().Changed("year") && prsYear <= 0 {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid --year %d: expected a four-digit year like 2025", prsYear)
	}

	if prsYear > 0 {
		since := time.Date(prsYear, 1, 1, 0, 0, 0, 0, time.Local)
		until := time.Date(prsYear, 12, 31, 23, 59, 59, 0, time.Local)
		if since.After(time.Now()) {
			return time.Time{}, time.Time{}, fmt.Errorf("--year %d is in the future", prsYear)
		}
		if until.After(time.Now()) {
			until = time.Now()
		}
		if cmd.Flags().Changed("since") || cmd.Flags().Changed("until") {
			fmt.Fprintln(os.Stderr, "note: --year overrides --since/--until")
		}
		return since, until, nil
	}

	since, err := git.ParseDate(prsSince)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid --since date: %w", err)
	}
	until := time.Now()
	if prsUntil != "" {
		// ParseDateEnd so that --until 2025-03-05 covers all of 5 March rather
		// than stopping at midnight and dropping that day.
		until, err = git.ParseDateEnd(prsUntil)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid --until date: %w", err)
		}
	}
	if !until.After(since) {
		return time.Time{}, time.Time{}, fmt.Errorf("--since (%s) must be before --until (%s)",
			since.Format("2006-01-02"), until.Format("2006-01-02"))
	}
	return since, until, nil
}
