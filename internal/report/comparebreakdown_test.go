package report

import (
	"strings"
	"testing"
	"time"

	"github.com/juangracia/gitrespect/internal/git"
)

// compareWithDailyData builds a comparison whose two periods land in different
// months, so a breakdown of each period is visibly distinct.
func compareWithDailyData() git.CompareStats {
	before := git.RepoStats{
		Path:   "/src/widgets",
		Author: "Alice",
		Since:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Until:  time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC),
		Added:  150, Deleted: 30, Net: 120, Commits: 3,
		FirstCommit: time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC),
		LastCommit:  time.Date(2025, 1, 22, 0, 0, 0, 0, time.UTC),
		Monthly: map[string]git.MonthStats{
			"2025-01": {Added: 150, Deleted: 30, Net: 120, Commits: 3},
		},
		Daily: map[string]git.DayStats{
			"2025-01-15": {Added: 100, Deleted: 20, Net: 80, Commits: 2},
			"2025-01-22": {Added: 50, Deleted: 10, Net: 40, Commits: 1},
		},
	}
	after := git.RepoStats{
		Path:   "/src/widgets",
		Author: "Alice",
		Since:  time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC),
		Until:  time.Date(2025, 8, 1, 0, 0, 0, 0, time.UTC),
		Added:  600, Deleted: 100, Net: 500, Commits: 9,
		FirstCommit: time.Date(2025, 7, 3, 0, 0, 0, 0, time.UTC),
		LastCommit:  time.Date(2025, 7, 28, 0, 0, 0, 0, time.UTC),
		Monthly: map[string]git.MonthStats{
			"2025-07": {Added: 600, Deleted: 100, Net: 500, Commits: 9},
		},
		Daily: map[string]git.DayStats{
			"2025-07-03": {Added: 300, Deleted: 50, Net: 250, Commits: 5},
			"2025-07-28": {Added: 300, Deleted: 50, Net: 250, Commits: 4},
		},
	}
	return git.CompareStats{
		Before: before, After: after,
		BeforeLabel: "pre-AI", AfterLabel: "post-AI",
	}
}

// An empty granularity must reproduce the plain comparison exactly: --breakdown
// is opt-in, and its absence must not change the report a user already had.
func TestCompareTerminalWithBreakdownEmptyGranularityMatchesPlain(t *testing.T) {
	c := compareWithDailyData()

	plain := captureStdout(t, func() {
		if err := CompareTerminal(c); err != nil {
			t.Errorf("CompareTerminal: %v", err)
		}
	})
	withEmpty := captureStdout(t, func() {
		if err := CompareTerminalWithBreakdown(c, ""); err != nil {
			t.Errorf("CompareTerminalWithBreakdown: %v", err)
		}
	})

	if plain != withEmpty {
		t.Errorf("empty granularity changed the output.\nplain:\n%s\nwith empty granularity:\n%s", plain, withEmpty)
	}
}

// The point of --breakdown on compare is that each period gets its own table.
// A granularity that is accepted but ignored is the failure this guards.
func TestCompareTerminalWithBreakdownAddsAPerPeriodTable(t *testing.T) {
	c := compareWithDailyData()

	plain := captureStdout(t, func() {
		if err := CompareTerminal(c); err != nil {
			t.Errorf("CompareTerminal: %v", err)
		}
	})
	monthly := captureStdout(t, func() {
		if err := CompareTerminalWithBreakdown(c, "monthly"); err != nil {
			t.Errorf("CompareTerminalWithBreakdown: %v", err)
		}
	})

	if len(monthly) <= len(plain) {
		t.Fatalf("--breakdown monthly added nothing to the report:\n%s", monthly)
	}
	// Each period is broken down separately, so both months must appear.
	if !strings.Contains(monthly, "Jan 2025") {
		t.Errorf("output has no January row for the before period:\n%s", monthly)
	}
	if !strings.Contains(monthly, "Jul 2025") {
		t.Errorf("output has no July row for the after period:\n%s", monthly)
	}
}

// Two identically shaped tables in sequence are impossible to tell apart, so
// each must be headed with which side of the comparison it is.
func TestCompareTerminalWithBreakdownLabelsBothPeriods(t *testing.T) {
	out := captureStdout(t, func() {
		if err := CompareTerminalWithBreakdown(compareWithDailyData(), "monthly"); err != nil {
			t.Errorf("CompareTerminalWithBreakdown: %v", err)
		}
	})

	// Assert the composed heading, not the bare label: CompareTerminal already
	// prints "pre-AI" and "post-AI" on its own, so checking for those alone
	// would pass even if the breakdown headings lost them entirely.
	if !strings.Contains(out, "Before (pre-AI)") {
		t.Errorf("output does not head the first table with %q:\n%s", "Before (pre-AI)", out)
	}
	if !strings.Contains(out, "After (post-AI)") {
		t.Errorf("output does not head the second table with %q:\n%s", "After (post-AI)", out)
	}
	if strings.Index(out, "Before (pre-AI)") > strings.Index(out, "After (post-AI)") {
		t.Error("the After table is printed before the Before table")
	}
}

// A period with no commits must say so rather than print an empty table, which
// reads as a rendering failure.
func TestCompareTerminalWithBreakdownReportsAnEmptyPeriod(t *testing.T) {
	c := compareWithDailyData()
	c.Before.Daily = map[string]git.DayStats{}
	c.Before.Monthly = map[string]git.MonthStats{}
	c.Before.Added, c.Before.Deleted, c.Before.Net, c.Before.Commits = 0, 0, 0, 0

	out := captureStdout(t, func() {
		if err := CompareTerminalWithBreakdown(c, "monthly"); err != nil {
			t.Errorf("CompareTerminalWithBreakdown: %v", err)
		}
	})

	if !strings.Contains(out, "no commits in this period") {
		t.Errorf("an empty before period did not say so:\n%s", out)
	}
	// The after period still has data and must still be broken down.
	if !strings.Contains(out, "Jul 2025") {
		t.Errorf("the populated after period lost its breakdown:\n%s", out)
	}
}

// Weekly and daily must actually group differently; accepting the flag and
// always grouping by month would pass a test that only checked monthly.
func TestCompareTerminalWithBreakdownHonoursEachGranularity(t *testing.T) {
	c := compareWithDailyData()

	monthly := captureStdout(t, func() { _ = CompareTerminalWithBreakdown(c, "monthly") })
	weekly := captureStdout(t, func() { _ = CompareTerminalWithBreakdown(c, "weekly") })
	daily := captureStdout(t, func() { _ = CompareTerminalWithBreakdown(c, "daily") })

	if monthly == weekly || weekly == daily || monthly == daily {
		t.Error("two granularities produced identical output; the flag is not reaching the grouping")
	}
	if !strings.Contains(weekly, "Week of") {
		t.Errorf("weekly output has no week rows:\n%s", weekly)
	}
	// The before period has two commits on distinct days, so daily must show
	// both of them where monthly showed one row.
	if !strings.Contains(daily, "Jan 15 2025") || !strings.Contains(daily, "Jan 22 2025") {
		t.Errorf("daily output does not show both commit days:\n%s", daily)
	}
}

// An unrecognised granularity must not silently print an empty table.
func TestCompareTerminalWithBreakdownRejectsAnUnknownGranularity(t *testing.T) {
	out := captureStdout(t, func() {
		if err := CompareTerminalWithBreakdown(compareWithDailyData(), "fortnightly"); err != nil {
			t.Errorf("CompareTerminalWithBreakdown: %v", err)
		}
	})

	if !strings.Contains(out, "no commits in this period") {
		t.Errorf("an unknown granularity produced neither rows nor a message:\n%s", out)
	}
}
