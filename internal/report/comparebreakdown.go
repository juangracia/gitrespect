package report

import (
	"fmt"

	"github.com/juangracia/gitrespect/internal/git"
)

// CompareTerminalWithBreakdown prints a period comparison, optionally followed
// by a breakdown of each period.
//
// The two periods are broken down SEPARATELY rather than as one continuous
// series. They can be different lengths and need not be adjacent, so a single
// spanning series would either invent empty buckets for the gap between them or
// silently imply the periods are contiguous when they are not. Two labelled
// tables answer "when inside each period did the work happen" without making
// either claim.
//
// An empty granularity reproduces the plain comparison exactly.
func CompareTerminalWithBreakdown(c git.CompareStats, granularity string) error {
	if err := CompareTerminal(c); err != nil {
		return err
	}
	if granularity == "" {
		return nil
	}

	printLabelledBreakdown(c.Before, c.BeforeLabel, "Before", granularity)
	printLabelledBreakdown(c.After, c.AfterLabel, "After", granularity)
	return nil
}

// TeamCompareTerminalWithBreakdown prints a team period comparison, optionally
// followed by a team-wide breakdown of each period.
//
// Team mode gets the same treatment as a single author for the same reason: the
// question after "did the team's output change" is "when did it change", and
// answering it for one person but not for a team would make the flag mean
// different things depending on who you asked about.
func TeamCompareTerminalWithBreakdown(c git.TeamCompareStats, granularity string) error {
	if err := TeamCompareTerminal(c); err != nil {
		return err
	}
	if granularity == "" {
		return nil
	}

	printLabelledBreakdown(teamAsRepoStats(c.Before), c.BeforeLabel, "Before", granularity)
	printLabelledBreakdown(teamAsRepoStats(c.After), c.AfterLabel, "After", granularity)
	return nil
}

// teamAsRepoStats adapts a team's aggregated buckets to the shape the shared
// breakdown renderer takes. The team totals are already summed across members,
// so this is a view rather than a conversion.
func teamAsRepoStats(t git.TeamStats) git.RepoStats {
	return git.RepoStats{Monthly: t.Monthly, Daily: t.Daily}
}

// printLabelledBreakdown heads one period's table with which side of the
// comparison it is, since two identically shaped tables in sequence are
// otherwise impossible to tell apart.
func printLabelledBreakdown(stats git.RepoStats, label, side, granularity string) {
	heading := side
	if label != "" {
		heading = fmt.Sprintf("%s (%s)", side, label)
	}
	fmt.Printf("  %s%s%s\n", colorBold, heading, colorReset)

	if len(git.Breakdown(stats, granularity)) == 0 {
		fmt.Printf("  %sno commits in this period%s\n\n", colorDim, colorReset)
		return
	}
	printBreakdown(stats, granularity)
}
