package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- date parsing -----------------------------------------------------------

func TestParseDateAbsoluteFormsResolveToStartOfUnit(t *testing.T) {
	tests := []struct {
		in   string
		want time.Time
	}{
		{"2025-03-04", time.Date(2025, 3, 4, 0, 0, 0, 0, time.Local)},
		{"2025-03", time.Date(2025, 3, 1, 0, 0, 0, 0, time.Local)},
		{"2025", time.Date(2025, 1, 1, 0, 0, 0, 0, time.Local)},
	}
	for _, tc := range tests {
		got, err := ParseDate(tc.in)
		if err != nil {
			t.Fatalf("ParseDate(%q): %v", tc.in, err)
		}
		if !got.Equal(tc.want) {
			t.Errorf("ParseDate(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseDateEndCoversWholeUnit(t *testing.T) {
	tests := []struct {
		in           string
		wantAfter    time.Time // must be strictly after this instant
		wantNotAfter time.Time // must not be after this instant
	}{
		{
			in:           "2025-03-04",
			wantAfter:    time.Date(2025, 3, 4, 23, 59, 59, 0, time.Local),
			wantNotAfter: time.Date(2025, 3, 5, 0, 0, 0, 0, time.Local),
		},
		{
			in:           "2025-02",
			wantAfter:    time.Date(2025, 2, 28, 23, 59, 59, 0, time.Local),
			wantNotAfter: time.Date(2025, 3, 1, 0, 0, 0, 0, time.Local),
		},
		{
			in:           "2025",
			wantAfter:    time.Date(2025, 12, 31, 23, 59, 59, 0, time.Local),
			wantNotAfter: time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local),
		},
	}
	for _, tc := range tests {
		got, err := ParseDateEnd(tc.in)
		if err != nil {
			t.Fatalf("ParseDateEnd(%q): %v", tc.in, err)
		}
		if !got.After(tc.wantAfter) {
			t.Errorf("ParseDateEnd(%q) = %v, want after %v", tc.in, got, tc.wantAfter)
		}
		if got.After(tc.wantNotAfter) {
			t.Errorf("ParseDateEnd(%q) = %v, want not after %v", tc.in, got, tc.wantNotAfter)
		}
	}
}

func TestParseDateRelative(t *testing.T) {
	got, err := ParseDate("30 days ago")
	if err != nil {
		t.Fatalf("ParseDate: %v", err)
	}
	want := time.Now().AddDate(0, 0, -30)
	if diff := got.Sub(want); diff > time.Minute || diff < -time.Minute {
		t.Errorf("ParseDate(30 days ago) = %v, want ~%v", got, want)
	}
}

func TestParseDateRejectsGarbage(t *testing.T) {
	if _, err := ParseDate("not-a-date"); err == nil {
		t.Error("ParseDate(\"not-a-date\") succeeded, want error")
	}
}

// TimeArg must carry an explicit offset. A bare YYYY-MM-DD is re-interpreted
// by git's approxidate parser and silently drops commits at day boundaries.
func TestTimeArgIncludesZoneOffset(t *testing.T) {
	got := TimeArg(time.Date(2025, 3, 4, 0, 0, 0, 0, time.UTC))
	if got != "2025-03-04T00:00:00Z" {
		t.Errorf("TimeArg = %q, want RFC3339", got)
	}
	if !strings.Contains(TimeArg(time.Now()), "T") {
		t.Error("TimeArg dropped the time component")
	}
}

// --- commit header parsing --------------------------------------------------

func TestParseCommitHeader(t *testing.T) {
	sha := strings.Repeat("a1b2c3d4", 5) // 40 hex chars
	if date, ok := parseCommitHeader(sha + "|2025-03-04"); !ok || date != "2025-03-04" {
		t.Errorf("parseCommitHeader(valid) = %q,%v", date, ok)
	}
	// A numstat line for a file whose name contains a pipe must not be
	// mistaken for a commit header.
	if _, ok := parseCommitHeader("10\t2\tsrc/we|rd.go"); ok {
		t.Error("parseCommitHeader matched a numstat line containing a pipe")
	}
	if _, ok := parseCommitHeader("short|2025-03-04"); ok {
		t.Error("parseCommitHeader matched a too-short sha")
	}
	if _, ok := parseCommitHeader("nothexnothexnothexnothexnothexnothexnoth|2025-03-04"); ok {
		t.Error("parseCommitHeader matched a non-hex sha")
	}
}

// --- breakdown --------------------------------------------------------------

func TestBreakdownGroupsByGranularity(t *testing.T) {
	stats := RepoStats{Daily: map[string]DayStats{
		// Mon 13 Jan and Wed 15 Jan fall in the same week.
		"2025-01-13": {Date: "2025-01-13", Added: 10, Deleted: 1, Commits: 1},
		"2025-01-15": {Date: "2025-01-15", Added: 20, Deleted: 2, Commits: 2},
		"2025-02-03": {Date: "2025-02-03", Added: 5, Deleted: 0, Commits: 1},
	}}

	daily := Breakdown(stats, "daily")
	if len(daily) != 3 {
		t.Fatalf("daily rows = %d, want 3", len(daily))
	}
	if daily[0].Label != "Jan 13 2025" {
		t.Errorf("daily[0].Label = %q", daily[0].Label)
	}

	weekly := Breakdown(stats, "weekly")
	if len(weekly) != 2 {
		t.Fatalf("weekly rows = %d, want 2", len(weekly))
	}
	if weekly[0].Added != 30 || weekly[0].Commits != 3 {
		t.Errorf("weekly[0] = %+v, want the two January days merged", weekly[0])
	}
	if weekly[0].Label != "Week of Jan 13 2025" {
		t.Errorf("weekly[0].Label = %q, want Monday anchor", weekly[0].Label)
	}

	monthly := Breakdown(stats, "monthly")
	if len(monthly) != 2 {
		t.Fatalf("monthly rows = %d, want 2", len(monthly))
	}
	if monthly[0].Net != 27 {
		t.Errorf("monthly[0].Net = %d, want 27", monthly[0].Net)
	}

	if Breakdown(stats, "hourly") != nil {
		t.Error("Breakdown accepted an unsupported granularity")
	}
}

func TestValidGranularity(t *testing.T) {
	for _, g := range []string{"monthly", "weekly", "daily"} {
		if !ValidGranularity(g) {
			t.Errorf("ValidGranularity(%q) = false", g)
		}
	}
	if ValidGranularity("yearly") {
		t.Error("ValidGranularity(\"yearly\") = true")
	}
}

// --- Analyze ----------------------------------------------------------------

type repoFixture struct {
	t    *testing.T
	path string
}

func newRepo(t *testing.T) *repoFixture {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "Test")
	gitRun(t, dir, "config", "commit.gpgsign", "false")
	return &repoFixture{t: t, path: dir}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// commitLines writes n lines to name and commits it at ts.
func (r *repoFixture) commitLines(name string, n int, email string, ts time.Time) {
	r.t.Helper()
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString("line\n")
	}
	if err := os.WriteFile(filepath.Join(r.path, name), []byte(b.String()), 0644); err != nil {
		r.t.Fatalf("write: %v", err)
	}
	gitRun(r.t, r.path, "add", "-A")
	cmd := exec.Command("git", "-C", r.path, "commit", "-q", "--no-gpg-sign", "-m", "c "+name)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL="+email,
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL="+email,
		"GIT_AUTHOR_DATE="+ts.Format(time.RFC3339),
		"GIT_COMMITTER_DATE="+ts.Format(time.RFC3339),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		r.t.Fatalf("commit: %v\n%s", err, out)
	}
}

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 10, 0, 0, 0, time.UTC)
}

// A single-day window must include commits made during that day. Passing a
// bare date to git silently excluded them, which made every bounded report
// wrong at its edges.
func TestAnalyzeIncludesCommitsOnTheUntilDay(t *testing.T) {
	r := newRepo(t)
	r.commitLines("a.txt", 10, "alice@example.com", day(2025, 3, 4))

	since := time.Date(2025, 3, 4, 0, 0, 0, 0, time.Local)
	until, err := ParseDateEnd("2025-03-04")
	if err != nil {
		t.Fatal(err)
	}

	stats, err := Analyze(r.path, "alice@example.com", since, until, nil)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if stats.Commits != 1 || stats.Added != 10 {
		t.Errorf("got commits=%d added=%d, want 1 and 10", stats.Commits, stats.Added)
	}
}

func TestAnalyzeCountsCommitsNotFiles(t *testing.T) {
	r := newRepo(t)
	// One commit touching two files must count as one commit everywhere.
	if err := os.WriteFile(filepath.Join(r.path, "one.txt"), []byte("a\nb\n"), 0644); err != nil {
		t.Fatal(err)
	}
	r.commitLines("two.txt", 3, "alice@example.com", day(2025, 5, 6))

	stats, err := Analyze(r.path, "alice@example.com",
		time.Date(2025, 1, 1, 0, 0, 0, 0, time.Local),
		time.Date(2025, 12, 31, 23, 59, 59, 0, time.Local), nil)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if stats.Commits != 1 {
		t.Errorf("Commits = %d, want 1", stats.Commits)
	}
	if stats.FilesChanged != 2 {
		t.Errorf("FilesChanged = %d, want 2", stats.FilesChanged)
	}
	for k, m := range stats.Monthly {
		if m.Commits != 1 {
			t.Errorf("Monthly[%s].Commits = %d, want 1 (files must not inflate it)", k, m.Commits)
		}
	}
	for k, d := range stats.Daily {
		if d.Commits != 1 {
			t.Errorf("Daily[%s].Commits = %d, want 1", k, d.Commits)
		}
	}
}

func TestAnalyzeExcludePatterns(t *testing.T) {
	r := newRepo(t)
	if err := os.MkdirAll(filepath.Join(r.path, "vendor"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.path, "vendor", "lib.go"),
		[]byte(strings.Repeat("x\n", 100)), 0644); err != nil {
		t.Fatal(err)
	}
	r.commitLines("main.go", 10, "alice@example.com", day(2025, 8, 1))

	since := time.Date(2025, 1, 1, 0, 0, 0, 0, time.Local)
	until := time.Date(2025, 12, 31, 23, 59, 59, 0, time.Local)

	all, err := Analyze(r.path, "alice@example.com", since, until, nil)
	if err != nil {
		t.Fatal(err)
	}
	if all.Added != 110 {
		t.Errorf("Added without exclude = %d, want 110", all.Added)
	}

	filtered, err := Analyze(r.path, "alice@example.com", since, until, []string{"vendor/*"})
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Added != 10 {
		t.Errorf("Added with exclude = %d, want 10", filtered.Added)
	}
}

func TestAnalyzeErrorsOnNonRepo(t *testing.T) {
	if _, err := Analyze(t.TempDir(), "alice@example.com",
		time.Now().AddDate(0, 0, -1), time.Now(), nil); err == nil {
		t.Error("Analyze on a non-repo succeeded, want error")
	}
}

func TestCombineStatsMergesDailyAndMonthly(t *testing.T) {
	a := RepoStats{
		Added: 10, Deleted: 1, Commits: 1,
		Monthly: map[string]MonthStats{"2025-01": {Year: 2025, Month: 1, Added: 10, Deleted: 1, Commits: 1}},
		Daily:   map[string]DayStats{"2025-01-05": {Date: "2025-01-05", Added: 10, Deleted: 1, Commits: 1}},
	}
	b := RepoStats{
		Added: 5, Deleted: 0, Commits: 2,
		Monthly: map[string]MonthStats{"2025-01": {Year: 2025, Month: 1, Added: 5, Commits: 2}},
		Daily:   map[string]DayStats{"2025-01-05": {Date: "2025-01-05", Added: 5, Commits: 2}},
	}

	got := CombineStats([]RepoStats{a, b})
	if got.Added != 15 || got.Commits != 3 {
		t.Errorf("combined = added %d commits %d, want 15 and 3", got.Added, got.Commits)
	}
	if m := got.Monthly["2025-01"]; m.Added != 15 || m.Commits != 3 || m.Net != 14 {
		t.Errorf("monthly merge = %+v", m)
	}
	if d := got.Daily["2025-01-05"]; d.Added != 15 || d.Commits != 3 || d.Net != 14 {
		t.Errorf("daily merge = %+v", d)
	}
}

func TestWorkingDaysNeverZero(t *testing.T) {
	// A zero-length range must not produce a divide-by-zero downstream.
	now := time.Now()
	if got := WorkingDays(now, now); got < 1 {
		t.Errorf("WorkingDays(same, same) = %d, want >= 1", got)
	}
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := WorkingDays(start, start.AddDate(0, 0, 7)); got != 5 {
		t.Errorf("WorkingDays(one week) = %d, want 5", got)
	}
}
