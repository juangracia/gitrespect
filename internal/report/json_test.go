package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/juangracia/gitrespect/internal/git"
	"github.com/juangracia/gitrespect/internal/metrics"
)

func day(date string, added, deleted, commits int) git.DayStats {
	return git.DayStats{Date: date, Added: added, Deleted: deleted, Net: added - deleted, Commits: commits}
}

// sampleStats spans three calendar months so every breakdown granularity has
// something to group.
func sampleStats() git.RepoStats {
	return git.RepoStats{
		Path:    "/tmp/repo",
		Author:  "dev@example.com",
		Since:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Until:   time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC),
		Added:   900,
		Deleted: 300,
		Net:     600,
		Commits: 9,
		Daily: map[string]git.DayStats{
			// Jan 15 and Jan 16 fall in the same week; Jan 20 starts the next.
			"2025-01-15": day("2025-01-15", 100, 20, 2),
			"2025-01-16": day("2025-01-16", 50, 10, 1),
			"2025-01-20": day("2025-01-20", 200, 70, 3),
			"2025-02-03": day("2025-02-03", 400, 100, 2),
			"2025-03-10": day("2025-03-10", 150, 100, 1),
		},
		Monthly: map[string]git.MonthStats{
			"2025-01": {Year: 2025, Month: 1, Added: 350, Deleted: 100, Net: 250, Commits: 6},
			"2025-02": {Year: 2025, Month: 2, Added: 400, Deleted: 100, Net: 300, Commits: 2},
			"2025-03": {Year: 2025, Month: 3, Added: 150, Deleted: 100, Net: 50, Commits: 1},
		},
	}
}

func TestBuildBreakdownMonthly(t *testing.T) {
	got := buildBreakdown(sampleStats(), "monthly")
	if got == nil {
		t.Fatal("buildBreakdown returned nil for populated stats")
	}
	if got.Granularity != "monthly" {
		t.Errorf("granularity = %q, want %q", got.Granularity, "monthly")
	}
	wantLabels := []string{"Jan 2025", "Feb 2025", "Mar 2025"}
	if len(got.Periods) != len(wantLabels) {
		t.Fatalf("got %d periods, want %d: %+v", len(got.Periods), len(wantLabels), got.Periods)
	}
	for i, want := range wantLabels {
		if got.Periods[i].Label != want {
			t.Errorf("period %d label = %q, want %q (rows must be oldest first)", i, got.Periods[i].Label, want)
		}
	}

	jan := got.Periods[0]
	if jan.Added != 350 || jan.Deleted != 100 || jan.Net != 250 || jan.Commits != 6 {
		t.Errorf("January = %+v, want added 350 deleted 100 net 250 commits 6", jan)
	}
}

func TestBuildBreakdownWeeklyAndDaily(t *testing.T) {
	weekly := buildBreakdown(sampleStats(), "weekly")
	if weekly == nil {
		t.Fatal("weekly breakdown was nil")
	}
	// Jan 15 (Wed) and Jan 16 (Thu) share the week starting Mon Jan 13.
	if got := weekly.Periods[0].Label; got != "Week of Jan 13 2025" {
		t.Errorf("first weekly label = %q, want %q", got, "Week of Jan 13 2025")
	}
	if got := weekly.Periods[0].Added; got != 150 {
		t.Errorf("first week added = %d, want 150 (Jan 15 and Jan 16 combined)", got)
	}
	if len(weekly.Periods) != 4 {
		t.Errorf("got %d weeks, want 4: %+v", len(weekly.Periods), weekly.Periods)
	}

	daily := buildBreakdown(sampleStats(), "daily")
	if daily == nil {
		t.Fatal("daily breakdown was nil")
	}
	if len(daily.Periods) != 5 {
		t.Fatalf("got %d days, want 5", len(daily.Periods))
	}
	if got := daily.Periods[0].Label; got != "Jan 15 2025" {
		t.Errorf("first daily label = %q, want %q", got, "Jan 15 2025")
	}
	for i := 1; i < len(daily.Periods); i++ {
		if daily.Periods[i-1].Label == daily.Periods[i].Label {
			t.Errorf("duplicate day label %q", daily.Periods[i].Label)
		}
	}
}

func TestBuildBreakdownEmptyAndInvalid(t *testing.T) {
	if got := buildBreakdown(git.RepoStats{}, "monthly"); got != nil {
		t.Errorf("empty stats produced %+v, want nil so the section is left out", got)
	}
	if got := buildBreakdown(sampleStats(), "hourly"); got != nil {
		t.Errorf("unsupported granularity produced %+v, want nil", got)
	}
	if got := buildBreakdown(sampleStats(), ""); got != nil {
		t.Errorf("empty granularity produced %+v, want nil", got)
	}
}

func TestJSONWritesReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	if err := JSON(sampleStats(), path, "monthly", metrics.Bundle{}); err != nil {
		t.Fatalf("JSON: %v", err)
	}

	var report JSONReport
	readJSONFile(t, path, &report)

	if report.Author != "dev@example.com" {
		t.Errorf("author = %q", report.Author)
	}
	if report.Summary.Net != 600 {
		t.Errorf("summary net = %d, want 600", report.Summary.Net)
	}
	if report.Breakdown == nil || len(report.Breakdown.Periods) != 3 {
		t.Fatalf("breakdown = %+v, want 3 periods", report.Breakdown)
	}
	// The legacy monthly field stays populated for existing consumers.
	if len(report.Monthly) != 3 {
		t.Errorf("legacy monthly = %+v, want 3 entries", report.Monthly)
	}
	if report.Monthly[0].Month != "Jan" || report.Monthly[0].Year != 2025 {
		t.Errorf("first monthly entry = %+v, want Jan 2025", report.Monthly[0])
	}
	if len(report.Benchmarks) != 0 {
		t.Errorf("benchmarks = %+v, want none without LegacyBenchmark", report.Benchmarks)
	}
}

func TestJSONLegacyBenchmarksOptIn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	if err := JSON(sampleStats(), path, "", metrics.Bundle{LegacyBenchmark: true}); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	var report JSONReport
	readJSONFile(t, path, &report)
	if len(report.Benchmarks) != 3 {
		t.Errorf("benchmarks = %+v, want 3", report.Benchmarks)
	}
	if report.Breakdown != nil {
		t.Errorf("breakdown = %+v, want nil when none was requested", report.Breakdown)
	}
}

func teamCompareFixture() git.TeamCompareStats {
	period := func(members map[string]git.RepoStats, since, until time.Time) git.TeamStats {
		total := 0
		for _, m := range members {
			total += m.Net
		}
		return git.TeamStats{Since: since, Until: until, Members: members, TotalNet: total}
	}
	beforeStart := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	beforeEnd := time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC)
	afterStart := time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)
	afterEnd := time.Date(2025, 6, 30, 0, 0, 0, 0, time.UTC)

	return git.TeamCompareStats{
		BeforeLabel: "2025-01:2025-03",
		AfterLabel:  "2025-04:2025-06",
		Before: period(map[string]git.RepoStats{
			"veteran@example.com":   {Net: 1000, Added: 1400, Deleted: 400, Commits: 40},
			"newjoiner@example.com": {},
			"leaver@example.com":    {Net: -50, Added: 10, Deleted: 60, Commits: 2},
		}, beforeStart, beforeEnd),
		After: period(map[string]git.RepoStats{
			"veteran@example.com":   {Net: 3000, Added: 3600, Deleted: 600, Commits: 90},
			"newjoiner@example.com": {Net: 2000, Added: 2200, Deleted: 200, Commits: 30},
			"leaver@example.com":    {},
		}, afterStart, afterEnd),
	}
}

// TestTeamCompareJSONNullMultiplier pins the correctness rule: a member with
// no output in the before period has no baseline to multiply, and the report
// must say so with null rather than a "0" that reads as "got 0x worse".
func TestTeamCompareJSONNullMultiplier(t *testing.T) {
	path := filepath.Join(t.TempDir(), "team-compare.json")
	if err := TeamCompareJSON(teamCompareFixture(), path); err != nil {
		t.Fatalf("TeamCompareJSON: %v", err)
	}

	var report TeamCompareJSONReport
	readJSONFile(t, path, &report)

	byEmail := map[string]MemberCompareJSONReport{}
	for _, m := range report.Members {
		byEmail[m.Email] = m
	}
	if len(byEmail) != 3 {
		t.Fatalf("got %d members, want 3", len(byEmail))
	}

	if m := byEmail["newjoiner@example.com"]; m.Multiplier != nil {
		t.Errorf("newjoiner multiplier = %v, want null: they had no before output to multiply", *m.Multiplier)
	}
	// Negative net is also no baseline, for the same reason.
	if m := byEmail["leaver@example.com"]; m.Multiplier != nil {
		t.Errorf("leaver multiplier = %v, want null for a negative before period", *m.Multiplier)
	}
	m := byEmail["veteran@example.com"]
	if m.Multiplier == nil {
		t.Fatal("veteran multiplier is null, want a real ratio")
	}
	if *m.Multiplier <= 1 {
		t.Errorf("veteran multiplier = %v, want above 1 after tripling output", *m.Multiplier)
	}

	// Serialised shape matters as much as the parsed value: consumers read
	// the raw field.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"productivity_multiplier": null`) {
		t.Errorf("no null multiplier in the emitted JSON:\n%s", raw)
	}
}

func TestTeamCompareJSONMembersAreSorted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "team-compare.json")
	if err := TeamCompareJSON(teamCompareFixture(), path); err != nil {
		t.Fatalf("TeamCompareJSON: %v", err)
	}
	var report TeamCompareJSONReport
	readJSONFile(t, path, &report)

	for i := 1; i < len(report.Members); i++ {
		if report.Members[i-1].Email > report.Members[i].Email {
			t.Errorf("members out of order at %d: %q before %q",
				i, report.Members[i-1].Email, report.Members[i].Email)
		}
	}
	if report.Before.Label != "2025-01:2025-03" || report.After.Label != "2025-04:2025-06" {
		t.Errorf("period labels = %q / %q", report.Before.Label, report.After.Label)
	}
}

func TestTeamJSONWritesMembers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "team.json")
	stats := teamStatsFixture()
	if err := TeamJSON(stats, path, "monthly", nil); err != nil {
		t.Fatalf("TeamJSON: %v", err)
	}

	var report TeamJSONReport
	readJSONFile(t, path, &report)

	if len(report.Members) != len(stats.Members) {
		t.Fatalf("got %d members, want %d", len(report.Members), len(stats.Members))
	}
	// Members are ranked by net output, strongest first.
	for i := 1; i < len(report.Members); i++ {
		if report.Members[i-1].Net < report.Members[i].Net {
			t.Errorf("members not sorted by net at %d: %d then %d",
				i, report.Members[i-1].Net, report.Members[i].Net)
		}
	}
	if len(report.Monthly) == 0 {
		t.Error("team monthly breakdown is empty")
	}
}

func TestCompareJSONWritesReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "compare.json")
	c := git.CompareStats{
		BeforeLabel: "2025-01:2025-03",
		AfterLabel:  "2025-04:2025-06",
		Before: git.RepoStats{
			Net:   500,
			Since: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			Until: time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC),
		},
		After: git.RepoStats{
			Net:   1500,
			Since: time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC),
			Until: time.Date(2025, 6, 30, 0, 0, 0, 0, time.UTC),
		},
	}
	if err := CompareJSON(c, path); err != nil {
		t.Fatalf("CompareJSON: %v", err)
	}

	var report CompareJSONReport
	readJSONFile(t, path, &report)
	if report.Multiplier <= 1 {
		t.Errorf("multiplier = %v, want above 1 after tripling output", report.Multiplier)
	}
	if !strings.Contains(report.Change, "productivity change") {
		t.Errorf("change description = %q", report.Change)
	}
}

func readJSONFile(t *testing.T, path string, into any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("parsing %s: %v\n%s", path, err, raw)
	}
}
