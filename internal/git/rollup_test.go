package git

import (
	"reflect"
	"testing"
)

// repoStat builds a per-member row the way Analyze would, with Net derived.
func repoStat(path, author string, added, deleted, commits int) RepoStats {
	return RepoStats{
		Path:    path,
		Author:  author,
		Added:   added,
		Deleted: deleted,
		Net:     added - deleted,
		Commits: commits,
	}
}

// teamFixture is the shape a real team report has: uneven participation, a
// member absent from a repository entirely, a member present but idle, and a
// repository whose net is negative because it was mostly deletion.
//
//	alpha  Alice +500/-100, Bob +100/-20                  net  480
//	beta   Alice +200/-50,  Bob +10/-900,  Carol +40/-10  net -710
//	gamma  Alice idle,      Bob +50/-50,   Carol +70/-20  net   50
func teamFixture() map[string][]RepoStats {
	return map[string][]RepoStats{
		"Alice": {
			repoStat("/r/alpha", "Alice", 500, 100, 10),
			repoStat("/r/beta", "Alice", 200, 50, 5),
			// Present in the input but never touched: must not become a row.
			repoStat("/r/gamma", "Alice", 0, 0, 0),
		},
		"Bob": {
			repoStat("/r/alpha", "Bob", 100, 20, 3),
			repoStat("/r/beta", "Bob", 10, 900, 2),
			// A pure refactor: nets zero, but four commits of real work.
			repoStat("/r/gamma", "Bob", 50, 50, 4),
		},
		"Carol": {
			// Absent from alpha entirely, not merely zeroed.
			repoStat("/r/beta", "Carol", 40, 10, 1),
			repoStat("/r/gamma", "Carol", 70, 20, 2),
		},
	}
}

func TestRollupByRepoAggregatesAcrossMembers(t *testing.T) {
	got := RollupByRepo(teamFixture())

	if len(got) != 3 {
		t.Fatalf("got %d rollups, want 3: %+v", len(got), got)
	}

	// Ordered by net descending: alpha 480, gamma 50, beta -710.
	wantOrder := []string{"/r/alpha", "/r/gamma", "/r/beta"}
	for i, want := range wantOrder {
		if got[i].Path != want {
			t.Errorf("rollup %d = %q, want %q (order by net descending)", i, got[i].Path, want)
		}
	}

	alpha := got[0]
	if alpha.Added != 600 || alpha.Deleted != 120 || alpha.Net != 480 || alpha.Commits != 13 {
		t.Errorf("alpha = +%d/-%d net %d over %d commits, want +600/-120 net 480 over 13",
			alpha.Added, alpha.Deleted, alpha.Net, alpha.Commits)
	}

	// A repository that deleted far more than it added must report a negative
	// net rather than being clamped or dropped.
	beta := got[2]
	if beta.Net != -710 {
		t.Errorf("beta net = %d, want -710", beta.Net)
	}
	if beta.Added != 250 || beta.Deleted != 960 || beta.Commits != 8 {
		t.Errorf("beta = +%d/-%d over %d commits, want +250/-960 over 8",
			beta.Added, beta.Deleted, beta.Commits)
	}

	gamma := got[1]
	if gamma.Added != 120 || gamma.Deleted != 70 || gamma.Net != 50 || gamma.Commits != 6 {
		t.Errorf("gamma = +%d/-%d net %d over %d commits, want +120/-70 net 50 over 6",
			gamma.Added, gamma.Deleted, gamma.Net, gamma.Commits)
	}
}

func TestRollupByRepoOrdersContributorsByNet(t *testing.T) {
	got := RollupByRepo(teamFixture())

	want := map[string][]string{
		"/r/alpha": {"Alice", "Bob"},          // 400, 80
		"/r/beta":  {"Alice", "Carol", "Bob"}, // 150, 30, -890
		"/r/gamma": {"Carol", "Bob"},          // 50, 0
	}
	for _, r := range got {
		var authors []string
		for _, c := range r.Contributors {
			authors = append(authors, c.Author)
		}
		if !reflect.DeepEqual(authors, want[r.Path]) {
			t.Errorf("%s contributors = %v, want %v", r.Path, authors, want[r.Path])
		}
	}
}

// The motivating case: one person on a few hundred repositories touches a
// handful of them. The other rows must not reach the table at all.
func TestRollupByRepoDropsUntouchedRepos(t *testing.T) {
	perMember := map[string][]RepoStats{
		"Alice": {repoStat("/r/worked-on", "Alice", 10, 2, 1)},
		"Bob":   {},
	}
	for i := 0; i < 240; i++ {
		perMember["Alice"] = append(perMember["Alice"], repoStat("/r/untouched", "Alice", 0, 0, 0))
		perMember["Bob"] = append(perMember["Bob"], repoStat("/r/untouched", "Bob", 0, 0, 0))
	}

	got := RollupByRepo(perMember)

	if len(got) != 1 {
		t.Fatalf("got %d rollups, want only the repo that was worked on: %+v", len(got), got)
	}
	if got[0].Path != "/r/worked-on" {
		t.Errorf("path = %q, want /r/worked-on", got[0].Path)
	}
}

// Net alone cannot decide whether a row is empty. A refactor that deletes as
// much as it adds nets zero and is real work.
func TestRollupByRepoKeepsZeroNetWorkWithCommits(t *testing.T) {
	got := RollupByRepo(map[string][]RepoStats{
		"Alice": {repoStat("/r/refactor", "Alice", 400, 400, 7)},
	})

	if len(got) != 1 {
		t.Fatalf("got %d rollups, want the refactor kept: %+v", len(got), got)
	}
	if got[0].Net != 0 {
		t.Errorf("net = %d, want 0", got[0].Net)
	}
	if got[0].Commits != 7 {
		t.Errorf("commits = %d, want 7", got[0].Commits)
	}
	if len(got[0].Contributors) != 1 {
		t.Errorf("contributors = %d, want 1", len(got[0].Contributors))
	}
}

func TestRollupByRepoTieBreaksDeterministically(t *testing.T) {
	// Two repos with the same net, and inside one of them two members with the
	// same net, so both tie-breaks are exercised at once.
	got := RollupByRepo(map[string][]RepoStats{
		"Zoe": {
			repoStat("/r/zebra", "Zoe", 100, 0, 1),
			repoStat("/r/apple", "Zoe", 50, 0, 1),
		},
		"Ana": {
			repoStat("/r/apple", "Ana", 50, 0, 1),
		},
	})

	if len(got) != 2 {
		t.Fatalf("got %d rollups, want 2: %+v", len(got), got)
	}
	// Both repos net 100, so path ascending decides.
	if got[0].Path != "/r/apple" || got[1].Path != "/r/zebra" {
		t.Errorf("order = %q, %q, want /r/apple then /r/zebra", got[0].Path, got[1].Path)
	}
	// Both contributors net 50, so author ascending decides.
	if got[0].Contributors[0].Author != "Ana" || got[0].Contributors[1].Author != "Zoe" {
		t.Errorf("contributors = %q, %q, want Ana then Zoe",
			got[0].Contributors[0].Author, got[0].Contributors[1].Author)
	}
}

// The input is a map, so without a total order the same team would print in a
// different order on every run.
//
// Comparing repeated runs catches that only by luck, since map iteration may
// happen to repeat an order, so the documented ordering is asserted directly as
// well. The repeated runs stay because they also catch nondeterminism that is
// not an ordering bug.
func TestRollupByRepoIsStableAcrossRuns(t *testing.T) {
	first := RollupByRepo(teamFixture())
	assertRollupOrder(t, first)

	for i := 0; i < 50; i++ {
		got := RollupByRepo(teamFixture())
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d differed from the first:\n got %+v\nwant %+v", i, got, first)
		}
	}
}

// assertRollupOrder checks the total order RollupByRepo documents: repositories
// by net descending then path ascending, contributors by net descending then
// author ascending.
func assertRollupOrder(t *testing.T, rollups []RepoRollup) {
	t.Helper()
	for i := 1; i < len(rollups); i++ {
		prev, cur := rollups[i-1], rollups[i]
		if cur.Net > prev.Net {
			t.Errorf("rollup %d (%s, net %d) sorts after %s (net %d)", i, cur.Path, cur.Net, prev.Path, prev.Net)
		}
		if cur.Net == prev.Net && cur.Path < prev.Path {
			t.Errorf("rollups tied at net %d are not in path order: %q after %q", cur.Net, cur.Path, prev.Path)
		}
	}
	for _, r := range rollups {
		for i := 1; i < len(r.Contributors); i++ {
			prev, cur := r.Contributors[i-1], r.Contributors[i]
			if cur.Net > prev.Net {
				t.Errorf("%s: %s (net %d) sorts after %s (net %d)", r.Path, cur.Author, cur.Net, prev.Author, prev.Net)
			}
			if cur.Net == prev.Net && cur.Author < prev.Author {
				t.Errorf("%s: contributors tied at net %d are not in author order: %q after %q",
					r.Path, cur.Net, cur.Author, prev.Author)
			}
		}
	}
}

// A caller that keys the map by label and leaves Author unset would otherwise
// print a table of blank names.
func TestRollupByRepoFillsAuthorFromMapKey(t *testing.T) {
	got := RollupByRepo(map[string][]RepoStats{
		"Juan Gracia": {repoStat("/r/one", "", 10, 1, 2)},
	})

	if len(got) != 1 || len(got[0].Contributors) != 1 {
		t.Fatalf("got %+v, want one rollup with one contributor", got)
	}
	if got[0].Contributors[0].Author != "Juan Gracia" {
		t.Errorf("author = %q, want the map key", got[0].Contributors[0].Author)
	}
}

// An explicit author on the row wins over the map key, since AnalyzeIdentity
// already sets it to the canonical name.
func TestRollupByRepoKeepsExplicitAuthor(t *testing.T) {
	got := RollupByRepo(map[string][]RepoStats{
		"juan@corp.com": {repoStat("/r/one", "Juan Gracia", 10, 1, 2)},
	})

	if got[0].Contributors[0].Author != "Juan Gracia" {
		t.Errorf("author = %q, want the row's own author", got[0].Contributors[0].Author)
	}
}

func TestRollupByRepoEmptyInput(t *testing.T) {
	for _, in := range []map[string][]RepoStats{nil, {}, {"Alice": nil}, {"Alice": {}}} {
		got := RollupByRepo(in)
		if got == nil {
			t.Errorf("RollupByRepo(%v) returned nil, want an empty slice", in)
		}
		if len(got) != 0 {
			t.Errorf("RollupByRepo(%v) = %+v, want empty", in, got)
		}
	}
}

// Callers reuse the stats they pass in for other reports, so the rollup must
// not rewrite them in place.
func TestRollupByRepoDoesNotMutateInput(t *testing.T) {
	in := map[string][]RepoStats{
		"Juan Gracia": {{Path: "/r/one", Added: 10, Deleted: 1, Commits: 2}},
	}
	RollupByRepo(in)

	row := in["Juan Gracia"][0]
	if row.Author != "" {
		t.Errorf("input Author was rewritten to %q", row.Author)
	}
	if row.Net != 0 {
		t.Errorf("input Net was rewritten to %d", row.Net)
	}
}

// Net is derived rather than trusted, so a row whose Net was never set still
// rolls up correctly instead of reporting zero.
func TestRollupByRepoDerivesNet(t *testing.T) {
	got := RollupByRepo(map[string][]RepoStats{
		"Alice": {{Path: "/r/one", Author: "Alice", Added: 30, Deleted: 12, Commits: 1}},
	})

	if got[0].Net != 18 {
		t.Errorf("rollup net = %d, want 18", got[0].Net)
	}
	if got[0].Contributors[0].Net != 18 {
		t.Errorf("contributor net = %d, want 18", got[0].Contributors[0].Net)
	}
}
