package git

import (
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Contributor is one author seen in a scan, with how many commits they landed.
type Contributor struct {
	Email   string
	Name    string
	Commits int
}

// DefaultBotPatterns are the automation identities stripped from a contributor
// scan by default.
//
// Without them the top of a large scan is CI: GitLab mints a distinct
// group_<id>_bot_<token> or project_<id>_bot_<token> address for every project
// access token, so a busy group contributes dozens of one-off identities that
// crowd out the humans the list is meant to surface.
//
// Patterns are regular expressions matched case-insensitively against both the
// address and the display name, since GitHub apps carry the "[bot]" marker in
// the name as well as in the address. The list is exported so it can be shown
// to a user asking why an identity vanished from their report.
var DefaultBotPatterns = []string{
	`^group_\d+_bot_\w*@noreply\.gitlab\.com$`,
	`^project_\d+_bot_\w*@noreply\.gitlab\.com$`,
	`_bot_.*@noreply\.gitlab\.com`,
	`semantic-release-bot`,
	`dependabot`,
	`renovate(bot)?`,
	`\[bot\]`,
	`^actions@github\.com$`,
}

// TopContributors counts commits per author across several repositories and
// returns the n most active humans, most commits first.
//
// This replaces the manual ritual of running git log over every repo in a
// tree, counting addresses by hand and eyeballing the result to strip CI, which
// is how a team list for --team gets built today.
//
// A repository that git cannot read is skipped rather than aborting the scan,
// because one broken clone in a tree of hundreds should not cost the whole
// report. If every path fails the result would be a confident empty list, so
// that case returns an error instead. n <= 0 returns every contributor.
func TopContributors(paths []string, since, until time.Time, n int, extraExclude []string) ([]Contributor, error) {
	// Compile the exclusions before doing any work, so a typo in a user
	// pattern is reported immediately rather than after a long scan.
	if _, err := compileBotPatterns(extraExclude); err != nil {
		return nil, err
	}

	contribs, err := ScanContributors(paths, since, until)
	if err != nil {
		return nil, err
	}
	contribs, err = FilterBots(contribs, extraExclude)
	if err != nil {
		return nil, err
	}

	sortContributors(contribs)
	if n > 0 && len(contribs) > n {
		contribs = contribs[:n]
	}
	return contribs, nil
}

// ScanContributors counts commits per canonical author across paths, with no
// automation filtering applied.
//
// Callers that need to tell the user which identities were dropped scan once
// with this and apply FilterBots themselves. Deriving both lists from a single
// scan matters because the scan is the expensive step: it walks every commit in
// every repository, and asking TopContributors twice would double the cost of
// the slowest part of a run over a few hundred repos.
func ScanContributors(paths []string, since, until time.Time) ([]Contributor, error) {
	type tally struct {
		email   string
		name    string
		commits int
	}
	counts := make(map[string]*tally)
	failures := 0
	var lastErr error

	for _, p := range paths {
		args := LogArgs(p)
		args = append(args, "--use-mailmap")
		// %aE and %aN are the mailmap-mapped author fields. The lower-case
		// %ae and %an are not mapped, so a repo carrying a .mailmap would
		// still report each alias as its own contributor if they were used.
		args = append(args, "--pretty=format:%ae|%an")
		if !since.IsZero() {
			args = append(args, "--since="+TimeArg(since))
		}
		// A zero until would be formatted as year 1 and exclude every commit,
		// so an unset bound is left off entirely.
		if !until.IsZero() {
			args = append(args, "--until="+TimeArg(until))
		}

		out, err := exec.Command("git", args...).Output()
		if err != nil {
			failures++
			lastErr = fmt.Errorf("git log failed in %s: %w", p, err)
			continue
		}

		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			email, name, ok := strings.Cut(line, "|")
			if !ok {
				continue
			}
			email = strings.TrimSpace(email)
			name = strings.TrimSpace(name)
			if email == "" {
				continue
			}
			// Addresses are case-insensitive in practice, so the same person
			// writing Alice@corp.com on one machine must not split in two.
			// The first spelling seen is kept for display.
			key := strings.ToLower(email)
			t, ok := counts[key]
			if !ok {
				t = &tally{email: email, name: name}
				counts[key] = t
			}
			if t.name == "" {
				t.name = name
			}
			t.commits++
		}
	}

	if len(paths) > 0 && failures == len(paths) {
		return nil, fmt.Errorf("could not read any of the %d repositories: %w", len(paths), lastErr)
	}

	contribs := make([]Contributor, 0, len(counts))
	for _, t := range counts {
		contribs = append(contribs, Contributor{Email: t.email, Name: t.name, Commits: t.commits})
	}
	sortContributors(contribs)
	return contribs, nil
}

// FilterBots removes automation identities, matching DefaultBotPatterns plus
// any extra user-supplied regular expressions. It is separate from the scan so
// the exclusion rules can be exercised without building repositories.
func FilterBots(contribs []Contributor, extraExclude []string) ([]Contributor, error) {
	patterns, err := compileBotPatterns(extraExclude)
	if err != nil {
		return nil, err
	}

	kept := make([]Contributor, 0, len(contribs))
	for _, c := range contribs {
		if matchesAnyPattern(patterns, c.Email) || matchesAnyPattern(patterns, c.Name) {
			continue
		}
		kept = append(kept, c)
	}
	return kept, nil
}

// compileBotPatterns compiles the defaults plus extra. Compiling on every call
// rather than once at init keeps a caller that has appended to
// DefaultBotPatterns honest, and the cost is invisible next to running git.
//
// A pattern that does not compile is an error naming the pattern. Ignoring it
// would leave the user believing they had filtered an identity out while it
// stayed in every total.
func compileBotPatterns(extra []string) ([]*regexp.Regexp, error) {
	all := make([]string, 0, len(DefaultBotPatterns)+len(extra))
	all = append(all, DefaultBotPatterns...)
	all = append(all, extra...)

	compiled := make([]*regexp.Regexp, 0, len(all))
	for _, p := range all {
		if strings.TrimSpace(p) == "" {
			continue
		}
		// Prefixing the flag rather than requiring it in each pattern keeps
		// user-supplied exclusions case-insensitive too, which is what someone
		// typing an address expects.
		re, err := regexp.Compile("(?i)" + p)
		if err != nil {
			return nil, fmt.Errorf("invalid exclude pattern %q: %w", p, err)
		}
		compiled = append(compiled, re)
	}
	return compiled, nil
}

func matchesAnyPattern(patterns []*regexp.Regexp, s string) bool {
	if s == "" {
		return false
	}
	for _, re := range patterns {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// sortContributors orders by commits descending, then by address ascending so
// two people with the same count always come out in the same order.
func sortContributors(contribs []Contributor) {
	sort.Slice(contribs, func(i, j int) bool {
		if contribs[i].Commits != contribs[j].Commits {
			return contribs[i].Commits > contribs[j].Commits
		}
		return strings.ToLower(contribs[i].Email) < strings.ToLower(contribs[j].Email)
	})
}
