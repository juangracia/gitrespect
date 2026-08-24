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
