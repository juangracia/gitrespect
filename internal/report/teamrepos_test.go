package report

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/juangracia/gitrespect/internal/git"
)

// captureStdout runs fn with os.Stdout redirected and returns what was printed.
// The terminal reporters write straight to stdout, so this is the only way to
// assert on what a user actually sees.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	fn()

	os.Stdout = orig
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return <-done
}

func sampleRollups() []git.RepoRollup {
	return []git.RepoRollup{
		{
			Path: "/src/payments-group/payments-api", Added: 600, Deleted: 120, Net: 480, Commits: 13,
			Contributors: []git.RepoStats{
				{Path: "/src/payments-group/payments-api", Author: "Alice", Added: 500, Deleted: 100, Net: 400, Commits: 10},
				{Path: "/src/payments-group/payments-api", Author: "Bob", Added: 100, Deleted: 20, Net: 80, Commits: 3},
			},
		},
		{
			Path: "/src/widgets", Added: 120, Deleted: 70, Net: 50, Commits: 6,
			Contributors: []git.RepoStats{
				{Path: "/src/widgets", Author: "Carol", Added: 120, Deleted: 70, Net: 50, Commits: 6},
			},
		},
	}
}

// A report without --per-repo must omit the key entirely rather than emit an
// empty array, which a consumer reads as "no repositories".
func TestBuildRepoRollupJSONNilInNilOut(t *testing.T) {
	if got := buildRepoRollupJSON(nil, 20); got != nil {
		t.Errorf("buildRepoRollupJSON(nil) = %v, want nil so the JSON key is omitted", got)
	}
	if got := buildRepoRollupJSON([]git.RepoRollup{}, 20); got != nil {
		t.Errorf("buildRepoRollupJSON(empty) = %v, want nil", got)
	}
}

func TestBuildRepoRollupJSONCarriesEveryField(t *testing.T) {
	got := buildRepoRollupJSON(sampleRollups(), 20)

	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2", len(got))
	}
	first := got[0]
	if first.Path != "/src/payments-group/payments-api" {
		t.Errorf("Path = %q", first.Path)
	}
	// Name is the basename, so a nested layout does not print the whole path.
	if first.Name != "payments-api" {
		t.Errorf("Name = %q, want the basename %q", first.Name, "payments-api")
	}
	if first.Added != 600 || first.Deleted != 120 || first.Net != 480 || first.Commits != 13 {
		t.Errorf("totals = +%d/-%d net %d over %d, want +600/-120 net 480 over 13",
			first.Added, first.Deleted, first.Net, first.Commits)
	}
	if len(first.Contributors) != 2 {
		t.Fatalf("contributors = %d, want 2: per-person attribution is the point of this block", len(first.Contributors))
	}
	if first.Contributors[0].Author != "Alice" || first.Contributors[0].Net != 400 {
		t.Errorf("first contributor = %+v, want Alice at net 400", first.Contributors[0])
	}
}

// PerDay is net lines over the WORKING DAYS in the period. Dividing by anything
// else (commits, calendar days) silently rescales every repository's rate.
func TestBuildRepoRollupJSONPerDayDividesByWorkingDays(t *testing.T) {
	got := buildRepoRollupJSON(sampleRollups(), 20)

	if want := 480.0 / 20.0; got[0].PerDay != want {
		t.Errorf("PerDay = %v, want %v (net 480 over 20 working days)", got[0].PerDay, want)
	}
	if want := 50.0 / 20.0; got[1].PerDay != want {
		t.Errorf("PerDay = %v, want %v", got[1].PerDay, want)
	}
}

// A zero working-day window must not divide by zero or invent a rate.
func TestBuildRepoRollupJSONPerDayIsZeroForAnEmptyWindow(t *testing.T) {
	got := buildRepoRollupJSON(sampleRollups(), 0)
	if got[0].PerDay != 0 {
		t.Errorf("PerDay = %v, want 0 when the window has no working days", got[0].PerDay)
	}
}

// Repository names in a nested layout share a prefix and differ at the end, so
// the tail must survive truncation or two repos print identically.
func TestTruncateMiddleKeepsBothEnds(t *testing.T) {
	got := truncateMiddle("tag-api-group-tag-api-helm-chart", 20)

	if len(got) > 20 {
		t.Fatalf("truncateMiddle = %q, %d chars, want at most 20", got, len(got))
	}
	if !strings.Contains(got, "...") {
		t.Fatalf("truncateMiddle = %q, want an ellipsis marking the cut", got)
	}
	if !strings.HasPrefix(got, "tag-") {
		t.Errorf("truncateMiddle = %q, want the head kept", got)
	}
	if !strings.HasSuffix(got, "chart") {
		t.Errorf("truncateMiddle = %q, want the tail kept: nested repos differ at the end", got)
	}
}

func TestTruncateMiddleLeavesShortNamesAlone(t *testing.T) {
	if got := truncateMiddle("widgets", 20); got != "widgets" {
		t.Errorf("truncateMiddle(%q) = %q, want it unchanged", "widgets", got)
	}
	if got := truncateMiddle("exactly-twenty-chars", 20); got != "exactly-twenty-chars" {
		t.Errorf("a name exactly at the width was truncated: %q", got)
	}
}

// Two names that differ only in their tail must stay distinguishable.
func TestTruncateMiddleKeepsSimilarNamesDistinct(t *testing.T) {
	a := truncateMiddle("platform-services-group-billing-api", 20)
	b := truncateMiddle("platform-services-group-billing-web", 20)
	if a == b {
		t.Errorf("both names truncated to %q; the table cannot tell them apart", a)
	}
}

func TestRepoNameWidthFitsTheLongestNameWithinBounds(t *testing.T) {
	short := []git.RepoRollup{{Path: "/src/a"}, {Path: "/src/bb"}}
	if got := repoNameWidth(short); got != 20 {
		t.Errorf("repoNameWidth(short) = %d, want the floor of 20", got)
	}

	mid := []git.RepoRollup{{Path: "/src/a-twenty-five-char-name"}}
	if got := repoNameWidth(mid); got != len("a-twenty-five-char-name") {
		t.Errorf("repoNameWidth = %d, want the longest name %d", got, len("a-twenty-five-char-name"))
	}

	long := []git.RepoRollup{{Path: "/src/" + strings.Repeat("x", 90)}}
	if got := repoNameWidth(long); got != 40 {
		t.Errorf("repoNameWidth(long) = %d, want the ceiling of 40 so one outlier does not widen the table", got)
	}
}

func TestTotalRollupNetSumsEveryRepo(t *testing.T) {
	if got := totalRollupNet(sampleRollups()); got != 530 {
		t.Errorf("totalRollupNet = %d, want 530 (480 + 50)", got)
	}
	if got := totalRollupNet(nil); got != 0 {
		t.Errorf("totalRollupNet(nil) = %d, want 0", got)
	}
}

func teamStatsForRepos() git.TeamStats {
	return git.TeamStats{
		Since:        time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Until:        time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		Members:      map[string]git.RepoStats{"Alice": {Author: "Alice", Added: 600, Deleted: 120, Net: 480, Commits: 13}},
		TotalAdded:   720,
		TotalDeleted: 190,
		TotalNet:     530,
		TotalCommits: 19,
		Monthly:      map[string]git.MonthStats{},
		Daily:        map[string]git.DayStats{},
	}
}

// The by-repository table is the headline of --per-repo: it must name every
// repository, its net lines and how many people touched it.
func TestTeamTerminalWithReposPrintsEveryRepository(t *testing.T) {
	rollups := sampleRollups()

	out := captureStdout(t, func() {
		if err := TeamTerminalWithRepos(teamStatsForRepos(), "", nil, rollups); err != nil {
			t.Errorf("TeamTerminalWithRepos: %v", err)
		}
	})

	if !strings.Contains(out, "By Repository") {
		t.Error("output has no By Repository section")
	}
	for _, name := range []string{"payments-api", "widgets"} {
		if !strings.Contains(out, name) {
			t.Errorf("output does not name repository %q:\n%s", name, out)
		}
	}
	// Net lines are grouped in thousands, so assert the rendered spelling.
	if !strings.Contains(out, "480") {
		t.Error("output does not show the payments-api net of 480")
	}
	if !strings.Contains(out, "530") {
		t.Error("output does not show the 530 net-line total across repositories")
	}
	if !strings.Contains(out, "2 repositories") {
		t.Errorf("output does not state the repository count:\n%s", out)
	}
}

// Without --per-repo there are no rollups, and the table must not appear at all
// rather than printing an empty frame.
func TestTeamTerminalWithReposOmitsTheTableWhenThereAreNoRollups(t *testing.T) {
	out := captureStdout(t, func() {
		if err := TeamTerminalWithRepos(teamStatsForRepos(), "", nil, nil); err != nil {
			t.Errorf("TeamTerminalWithRepos: %v", err)
		}
	})

	if strings.Contains(out, "By Repository") {
		t.Errorf("empty rollups still printed the By Repository table:\n%s", out)
	}
}
