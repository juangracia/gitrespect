package metrics

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// rewrittenCommit lands a commit whose committer date is later than its author
// date, which is what a rebase or patch-based workflow produces and what the
// authored-to-landed fallback measures. A commit made directly on main has
// identical dates and carries no signal.
func rewrittenCommit(t *testing.T, repo *testRepo, file, msg, author string, authored, landed time.Time) {
	t.Helper()
	p := filepath.Join(repo.path, file)
	if err := os.WriteFile(p, []byte(msg+"\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", file, err)
	}
	run(t, repo.path, "git", "add", file)

	env := append(os.Environ(),
		"GIT_AUTHOR_DATE="+authored.Format(time.RFC3339),
		"GIT_COMMITTER_DATE="+landed.Format(time.RFC3339),
		"GIT_AUTHOR_NAME="+parseName(author),
		"GIT_AUTHOR_EMAIL="+parseEmail(author),
		"GIT_COMMITTER_NAME="+parseName(author),
		"GIT_COMMITTER_EMAIL="+parseEmail(author),
	)
	cmd := exec.Command("git", "-C", repo.path, "commit", "-q", "-m", msg)
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit %s: %v\n%s", msg, err, out)
	}
}

// With no merge commits anywhere, the pooled lead time falls back to
// authored-to-landed. This is the path a rebase or patch workflow takes, and it
// had no multi-repo test at all.
func TestLeadTimeAcrossFallsBackToAuthoredWhenNoMerges(t *testing.T) {
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	r := newTestRepo(t)
	// Authored-to-landed gaps of 2, 4 and 6 days. Pooled median 4.
	rewrittenCommit(t, r, "a.txt", "a", testAuthor, day(base, 1), day(base, 3))
	rewrittenCommit(t, r, "b.txt", "b", testAuthor, day(base, 5), day(base, 9))
	rewrittenCommit(t, r, "c.txt", "c", testAuthor, day(base, 10), day(base, 16))

	got, err := ComputeLeadTimeAcross([]string{r.path}, testAuthors, day(base, -1), day(base, 40))
	if err != nil {
		t.Fatalf("ComputeLeadTimeAcross: %v", err)
	}

	if got.Method != LeadTimeAuthored {
		t.Fatalf("Method = %v, want the authored fallback when there are no merges", got.Method)
	}
	if got.Samples != 3 {
		t.Errorf("Samples = %d, want 3", got.Samples)
	}
	if !closeTo(got.MedianDays, 4) {
		t.Errorf("MedianDays = %v, want 4", got.MedianDays)
	}
}

// The pooled authored median must be taken over the RAW samples from every
// repository, not over each repository's own median. This is the same bug the
// merge path was fixed for; the fallback path had no test holding it.
func TestLeadTimeAcrossAuthoredPoolsSamplesNotMedians(t *testing.T) {
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	// One sample of 10 days. Own median 10.
	slow := newTestRepo(t)
	rewrittenCommit(t, slow, "s.txt", "s", testAuthor, day(base, 1), day(base, 11))

	// Five samples of 2 days. Own median 2.
	fast := newTestRepo(t)
	for i, off := range []float64{1, 3, 5, 7, 9} {
		rewrittenCommit(t, fast, "f.txt", line(i), testAuthor, day(base, off), day(base, off+2))
	}

	since, until := day(base, -1), day(base, 40)

	slowOnly, err := ComputeLeadTimeAcross([]string{slow.path}, testAuthors, since, until)
	if err != nil {
		t.Fatalf("ComputeLeadTimeAcross(slow): %v", err)
	}
	fastOnly, err := ComputeLeadTimeAcross([]string{fast.path}, testAuthors, since, until)
	if err != nil {
		t.Fatalf("ComputeLeadTimeAcross(fast): %v", err)
	}
	if !closeTo(fastOnly.MedianDays, 2) {
		t.Fatalf("fast repo median = %v, want 2", fastOnly.MedianDays)
	}

	pooled, err := ComputeLeadTimeAcross([]string{slow.path, fast.path}, testAuthors, since, until)
	if err != nil {
		t.Fatalf("ComputeLeadTimeAcross(both): %v", err)
	}

	if pooled.Method != LeadTimeAuthored {
		t.Fatalf("Method = %v, want the authored fallback", pooled.Method)
	}
	if pooled.Samples != 6 {
		t.Errorf("Samples = %d, want 6: every repository's raw samples are pooled", pooled.Samples)
	}
	// Pooled samples sorted: 2,2,2,2,2,10 -> median 2.
	if !closeTo(pooled.MedianDays, 2) {
		t.Errorf("pooled median = %v, want 2", pooled.MedianDays)
	}
	// Averaging the per-repo medians would give (10+2)/2 = 6.
	meanOfMedians := (slowOnly.MedianDays + fastOnly.MedianDays) / 2
	if closeTo(pooled.MedianDays, meanOfMedians) {
		t.Errorf("pooled median = %v, which is the mean of the per-repo medians; medians must not be averaged",
			pooled.MedianDays)
	}
}

// A single cherry-pick of old work must not become the entire sample. The guard
// applies to the POOLED set, so two repositories with one sample each are still
// below it.
func TestLeadTimeAcrossAuthoredNeedsSeveralPooledSamples(t *testing.T) {
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	a := newTestRepo(t)
	rewrittenCommit(t, a, "a.txt", "a", testAuthor, day(base, 1), day(base, 4))
	b := newTestRepo(t)
	rewrittenCommit(t, b, "b.txt", "b", testAuthor, day(base, 2), day(base, 9))

	got, err := ComputeLeadTimeAcross([]string{a.path, b.path}, testAuthors, day(base, -1), day(base, 40))
	if err != nil {
		t.Fatalf("ComputeLeadTimeAcross: %v", err)
	}

	if got.Samples != 0 || got.MedianDays != 0 {
		t.Errorf("got %v days over %d samples, want nothing reported below %d pooled samples",
			got.MedianDays, got.Samples, minAuthoredSamples)
	}
	if got.Method == LeadTimeAuthored {
		t.Error("Method claims an authored median that was not reported")
	}
	// The repositories were still read, so coverage must be honest about it.
	if got.ReposCovered != 2 {
		t.Errorf("ReposCovered = %d, want 2", got.ReposCovered)
	}
}

// Commits made directly on main have identical author and committer dates and
// carry no lead-time signal, so they must not become a pile of zero samples.
func TestLeadTimeAcrossAuthoredIgnoresCommitsMadeDirectlyOnMain(t *testing.T) {
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	r := newTestRepo(t)
	for i, off := range []float64{1, 2, 3, 4} {
		r.writeFile("d.txt", line(i))
		r.commit("direct", testAuthor, day(base, off))
	}

	got, err := ComputeLeadTimeAcross([]string{r.path}, testAuthors, day(base, -1), day(base, 40))
	if err != nil {
		t.Fatalf("ComputeLeadTimeAcross: %v", err)
	}

	if got.Samples != 0 {
		t.Errorf("Samples = %d, want 0: same-date commits carry no lead time", got.Samples)
	}
	if got.MedianDays != 0 {
		t.Errorf("MedianDays = %v, want 0", got.MedianDays)
	}
}

// A merge commit anywhere in range wins: the merge median is the better signal
// and must not be diluted by the authored fallback.
func TestLeadTimeAcrossPrefersMergesOverTheAuthoredFallback(t *testing.T) {
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	merged := newTestRepo(t)
	merged.writeFile("README.md", "init\n")
	merged.commit("init", testAuthor, base)
	merged.mergeBranch("one", testAuthor, day(base, 2), day(base, 5))

	rebased := newTestRepo(t)
	for i, off := range []float64{1, 3, 5} {
		rewrittenCommit(t, rebased, "r.txt", line(i), testAuthor, day(base, off), day(base, off+8))
	}

	got, err := ComputeLeadTimeAcross([]string{merged.path, rebased.path}, testAuthors,
		day(base, -1), day(base, 40))
	if err != nil {
		t.Fatalf("ComputeLeadTimeAcross: %v", err)
	}

	if got.Method != LeadTimeMerge {
		t.Fatalf("Method = %v, want the merge method when merges exist anywhere", got.Method)
	}
	if !closeTo(got.MedianDays, 3) {
		t.Errorf("MedianDays = %v, want 3 from the one merge", got.MedianDays)
	}
	if got.Samples != 1 {
		t.Errorf("Samples = %d, want 1: the authored samples must not be mixed in", got.Samples)
	}
}
