package metrics

import (
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"
)

const testAuthor = "Test User <test@example.com>"

var testAuthors = []string{"test@example.com"}

// day returns base offset by n whole days, so the expected medians in these
// tests are exact rather than approximate.
func day(base time.Time, n float64) time.Time {
	return base.Add(time.Duration(n * float64(24*time.Hour)))
}

func closeTo(got, want float64) bool { return math.Abs(got-want) < 0.01 }

// mergeBranch creates a branch, commits on it at start, and merges it back into
// main at merged. Lead time for the merge is exactly merged minus start.
func (r *testRepo) mergeBranch(name, author string, start, merged time.Time) {
	r.t.Helper()
	run(r.t, r.path, "git", "checkout", "-q", "-b", name)
	r.writeFile(name+".txt", name+"\n")
	r.commit("work on "+name, author, start)
	run(r.t, r.path, "git", "checkout", "-q", "main")

	stamp := merged.Format(time.RFC3339)
	env := []string{
		"GIT_AUTHOR_DATE=" + stamp,
		"GIT_COMMITTER_DATE=" + stamp,
		"GIT_AUTHOR_NAME=" + parseName(author),
		"GIT_AUTHOR_EMAIL=" + parseEmail(author),
		"GIT_COMMITTER_NAME=" + parseName(author),
		"GIT_COMMITTER_EMAIL=" + parseEmail(author),
	}
	runEnv(r.t, env, "git", "-C", r.path, "merge", "-q", "--no-ff", "-m", "Merge "+name, name)
}

// TestCadenceAcrossPoolsCommitsNotMedians is the regression test for the bug
// this API exists to fix: two repositories with deliberately different rhythms
// must produce a pooled figure that is neither repository's own, and in
// particular is not the average of the two.
func TestCadenceAcrossPoolsCommitsNotMedians(t *testing.T) {
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	// Gaps of 1, 1, 8 days. Median 1.
	fast := newTestRepo(t)
	for i, off := range []float64{0, 1, 2, 10} {
		fast.writeFile("f.txt", line(i))
		fast.commit("c", testAuthor, day(base, off))
	}

	// Gaps of 4, 9, 9 days, in a stretch that does not overlap the first repo.
	slow := newTestRepo(t)
	for i, off := range []float64{100, 104, 113, 122} {
		slow.writeFile("f.txt", line(i))
		slow.commit("c", testAuthor, day(base, off))
	}

	since := day(base, -1)
	until := day(base, 130)

	fastOnly, err := ComputeCadence(fast.path, "test@example.com", since, until)
	if err != nil {
		t.Fatalf("ComputeCadence(fast): %v", err)
	}
	if !closeTo(fastOnly.MedianDaysBetween, 1) {
		t.Fatalf("fast repo median = %v, want 1", fastOnly.MedianDaysBetween)
	}

	slowOnly, err := ComputeCadence(slow.path, "test@example.com", since, until)
	if err != nil {
		t.Fatalf("ComputeCadence(slow): %v", err)
	}
	if !closeTo(slowOnly.MedianDaysBetween, 9) {
		t.Fatalf("slow repo median = %v, want 9", slowOnly.MedianDaysBetween)
	}

	got, err := ComputeCadenceAcross([]string{fast.path, slow.path}, testAuthors, since, until)
	if err != nil {
		t.Fatalf("ComputeCadenceAcross: %v", err)
	}

	// Pooled gaps are 1, 1, 8, 90 (the bridge between the two stretches), 4, 9,
	// 9. Sorted that is 1, 1, 4, 8, 9, 9, 90, so the median is 8.
	if !closeTo(got.MedianDaysBetween, 8) {
		t.Errorf("pooled median = %v, want 8", got.MedianDaysBetween)
	}
	// The trap: averaging the two per-repo medians would give 5.
	meanOfMedians := (fastOnly.MedianDaysBetween + slowOnly.MedianDaysBetween) / 2
	if closeTo(got.MedianDaysBetween, meanOfMedians) {
		t.Errorf("pooled median = %v, which is the mean of the per-repo medians; medians must not be averaged", got.MedianDaysBetween)
	}
	if closeTo(got.MedianDaysBetween, fastOnly.MedianDaysBetween) || closeTo(got.MedianDaysBetween, slowOnly.MedianDaysBetween) {
		t.Errorf("pooled median = %v, want a value drawn from both repos, not one of them", got.MedianDaysBetween)
	}
	if got.Samples != 7 {
		t.Errorf("Samples = %d, want 7", got.Samples)
	}
	if got.ReposCovered != 2 {
		t.Errorf("ReposCovered = %d, want 2", got.ReposCovered)
	}
	if got.MainBranch != "main" {
		t.Errorf("MainBranch = %q, want main", got.MainBranch)
	}
}

// TestLeadTimeAcrossPoolsSamples checks the same trap for lead time, whose
// samples come from merge commits.
func TestLeadTimeAcrossPoolsSamples(t *testing.T) {
	base := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

	// Lead times of 1, 1 and 8 days. Median 1.
	quick := newTestRepo(t)
	quick.writeFile("README.md", "init\n")
	quick.commit("init", testAuthor, base)
	quick.mergeBranch("one", testAuthor, day(base, 10), day(base, 11))
	quick.mergeBranch("two", testAuthor, day(base, 12), day(base, 13))
	quick.mergeBranch("three", testAuthor, day(base, 14), day(base, 22))

	// Lead times of 4, 9 and 9 days. Median 9.
	slow := newTestRepo(t)
	slow.writeFile("README.md", "init\n")
	slow.commit("init", testAuthor, base)
	slow.mergeBranch("one", testAuthor, day(base, 10), day(base, 14))
	slow.mergeBranch("two", testAuthor, day(base, 16), day(base, 25))
	slow.mergeBranch("three", testAuthor, day(base, 26), day(base, 35))

	since := day(base, -1)
	until := day(base, 40)

	quickOnly, err := ComputeLeadTime(quick.path, "test@example.com", since, until)
	if err != nil {
		t.Fatalf("ComputeLeadTime(quick): %v", err)
	}
	if !closeTo(quickOnly.MedianDays, 1) || quickOnly.Samples != 3 {
		t.Fatalf("quick repo = %.2f days over %d samples, want 1 over 3", quickOnly.MedianDays, quickOnly.Samples)
	}

	slowOnly, err := ComputeLeadTime(slow.path, "test@example.com", since, until)
	if err != nil {
		t.Fatalf("ComputeLeadTime(slow): %v", err)
	}
	if !closeTo(slowOnly.MedianDays, 9) || slowOnly.Samples != 3 {
		t.Fatalf("slow repo = %.2f days over %d samples, want 9 over 3", slowOnly.MedianDays, slowOnly.Samples)
	}

	got, err := ComputeLeadTimeAcross([]string{quick.path, slow.path}, testAuthors, since, until)
	if err != nil {
		t.Fatalf("ComputeLeadTimeAcross: %v", err)
	}

	// Pooled samples 1, 1, 8, 4, 9, 9 sort to 1, 1, 4, 8, 9, 9, median 6.
	if !closeTo(got.MedianDays, 6) {
		t.Errorf("pooled median = %v, want 6", got.MedianDays)
	}
	meanOfMedians := (quickOnly.MedianDays + slowOnly.MedianDays) / 2
	if closeTo(got.MedianDays, meanOfMedians) {
		t.Errorf("pooled median = %v, which is the mean of the per-repo medians; medians must not be averaged", got.MedianDays)
	}
	if closeTo(got.MedianDays, quickOnly.MedianDays) || closeTo(got.MedianDays, slowOnly.MedianDays) {
		t.Errorf("pooled median = %v, want a value drawn from both repos, not one of them", got.MedianDays)
	}
	if got.Samples != 6 {
		t.Errorf("Samples = %d, want 6", got.Samples)
	}
	if got.Method != LeadTimeMerge {
		t.Errorf("Method = %q, want %q", got.Method, LeadTimeMerge)
	}
	if got.ReposCovered != 2 {
		t.Errorf("ReposCovered = %d, want 2", got.ReposCovered)
	}
}

// TestLeadTimeAcrossDoesNotMixMethods checks that a repo with merge commits and
// a repo without are not blended into one median, since the two measurement
// methods are not comparable.
func TestLeadTimeAcrossDoesNotMixMethods(t *testing.T) {
	base := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

	merged := newTestRepo(t)
	merged.writeFile("README.md", "init\n")
	merged.commit("init", testAuthor, base)
	merged.mergeBranch("one", testAuthor, day(base, 10), day(base, 12))

	// Straight-line history: no merges, so nothing this method can measure.
	linear := newTestRepo(t)
	for i, off := range []float64{1, 2, 3, 4} {
		linear.writeFile("f.txt", line(i))
		linear.commit("c", testAuthor, day(base, off))
	}

	got, err := ComputeLeadTimeAcross([]string{merged.path, linear.path}, testAuthors, day(base, -1), day(base, 40))
	if err != nil {
		t.Fatalf("ComputeLeadTimeAcross: %v", err)
	}
	if got.Method != LeadTimeMerge {
		t.Errorf("Method = %q, want %q: one repo has merges, so the merge method wins for the whole run", got.Method, LeadTimeMerge)
	}
	if got.Samples != 1 {
		t.Errorf("Samples = %d, want 1: only the merging repo can contribute merge samples", got.Samples)
	}
	if !closeTo(got.MedianDays, 2) {
		t.Errorf("MedianDays = %v, want 2", got.MedianDays)
	}
}

func TestCommitSizeAcrossSumsBuckets(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Three micro commits and one small one.
	small := newTestRepo(t)
	for i, off := range []float64{0, 1, 2} {
		small.writeFile("tiny.txt", line(i))
		small.commit("micro", testAuthor, day(base, off))
	}
	small.writeFile("small.txt", lines(20))
	small.commit("small", testAuthor, day(base, 3))

	// Three large commits and one medium one.
	big := newTestRepo(t)
	for i, off := range []float64{0, 1, 2} {
		// fmt rather than line(), which appends a newline for use as file
		// CONTENT. Embedded in a name it produced "biga\n.txt", which Unix
		// accepts and Windows rejects, so this passed everywhere but CI.
		big.writeFile(fmt.Sprintf("big%d.txt", i), lines(600))
		big.commit("large", testAuthor, day(base, off))
	}
	big.writeFile("medium.txt", lines(150))
	big.commit("medium", testAuthor, day(base, 3))

	since := day(base, -1)
	until := day(base, 10)

	smallOnly, err := ComputeCommitSize(small.path, testAuthor, since, until, nil)
	if err != nil {
		t.Fatalf("ComputeCommitSize(small): %v", err)
	}
	bigOnly, err := ComputeCommitSize(big.path, testAuthor, since, until, nil)
	if err != nil {
		t.Fatalf("ComputeCommitSize(big): %v", err)
	}

	got, err := ComputeCommitSizeAcross([]string{small.path, big.path}, testAuthors, since, until, nil)
	if err != nil {
		t.Fatalf("ComputeCommitSizeAcross: %v", err)
	}

	want := [4]int{3, 1, 1, 3}
	if got.Counts != want {
		t.Errorf("Counts = %v, want %v", got.Counts, want)
	}
	if got.Total != 8 {
		t.Errorf("Total = %d, want 8", got.Total)
	}
	if got.ReposCovered != 2 {
		t.Errorf("ReposCovered = %d, want 2", got.ReposCovered)
	}
	// The share of micro commits describes both repos, not whichever one the
	// old code happened to pick as primary.
	if closeTo(got.Percent(BucketMicro), smallOnly.Percent(BucketMicro)) ||
		closeTo(got.Percent(BucketMicro), bigOnly.Percent(BucketMicro)) {
		t.Errorf("pooled micro share = %.1f%%, matches one repo alone (%.1f%% / %.1f%%)",
			got.Percent(BucketMicro), smallOnly.Percent(BucketMicro), bigOnly.Percent(BucketMicro))
	}
	if !closeTo(got.Percent(BucketMicro), 37.5) {
		t.Errorf("pooled micro share = %.2f%%, want 37.5%%", got.Percent(BucketMicro))
	}
}

// TestChurnAcrossPoolsLinesNotRatios checks that the churn ratio is taken once
// from the pooled line counts. Averaging the two repos' ratios would let a repo
// with a handful of lines weigh as much as one with thousands.
func TestChurnAcrossPoolsLinesNotRatios(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	since := day(base, 5)
	until := day(base, 15)
	window := 30 * 24 * time.Hour

	// 100 lines added before the period, 10 of them deleted during it.
	bulk := newTestRepo(t)
	bulk.writeFile("a.txt", churnLines(100))
	bulk.commit("write", testAuthor, base)
	bulk.writeFile("a.txt", churnLines(90))
	bulk.commit("trim", testAuthor, day(base, 10))

	// 10 lines added before the period, 5 of them deleted during it.
	tiny := newTestRepo(t)
	tiny.writeFile("b.txt", churnLines(10))
	tiny.commit("write", testAuthor, base)
	tiny.writeFile("b.txt", churnLines(5))
	tiny.commit("trim", testAuthor, day(base, 10))

	bulkOnly, err := ComputeChurn(bulk.path, "test@example.com", since, until, window, nil)
	if err != nil {
		t.Fatalf("ComputeChurn(bulk): %v", err)
	}
	tinyOnly, err := ComputeChurn(tiny.path, "test@example.com", since, until, window, nil)
	if err != nil {
		t.Fatalf("ComputeChurn(tiny): %v", err)
	}
	if !closeTo(bulkOnly.Ratio, 0.1) || !closeTo(tinyOnly.Ratio, 0.5) {
		t.Fatalf("per-repo ratios = %.3f and %.3f, want 0.1 and 0.5", bulkOnly.Ratio, tinyOnly.Ratio)
	}

	got, err := ComputeChurnAcross([]string{bulk.path, tiny.path}, testAuthors, since, until, window, nil)
	if err != nil {
		t.Fatalf("ComputeChurnAcross: %v", err)
	}
	if got.AddedLines != 110 {
		t.Errorf("AddedLines = %d, want 110", got.AddedLines)
	}
	if got.ChurnedLines != 15 {
		t.Errorf("ChurnedLines = %d, want 15", got.ChurnedLines)
	}
	if !closeTo(got.Ratio, 15.0/110.0) {
		t.Errorf("Ratio = %.4f, want %.4f", got.Ratio, 15.0/110.0)
	}
	meanOfRatios := (bulkOnly.Ratio + tinyOnly.Ratio) / 2
	if closeTo(got.Ratio, meanOfRatios) {
		t.Errorf("Ratio = %.4f, which is the mean of the per-repo ratios", got.Ratio)
	}
	if got.ReposCovered != 2 {
		t.Errorf("ReposCovered = %d, want 2", got.ReposCovered)
	}
}

// TestBaselineAcrossSumsLinesOverSharedWorkingDays checks that the working days
// come from the calendar once, not once per repository.
func TestBaselineAcrossSumsLinesOverSharedWorkingDays(t *testing.T) {
	periodStart := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	window := 90 * 24 * time.Hour

	// Two commits 50 days apart, so the activity span clears the 30 day floor.
	big := newTestRepo(t)
	big.writeFile("a.txt", churnLines(100))
	big.commit("first", testAuthor, day(periodStart, -85))
	big.writeFile("b.txt", churnLines(200))
	big.commit("second", testAuthor, day(periodStart, -35))

	small := newTestRepo(t)
	small.writeFile("a.txt", churnLines(10))
	small.commit("first", testAuthor, day(periodStart, -85))
	small.writeFile("b.txt", churnLines(20))
	small.commit("second", testAuthor, day(periodStart, -35))

	bigOnly, err := ComputeBaseline(big.path, "test@example.com", periodStart, window, nil)
	if err != nil {
		t.Fatalf("ComputeBaseline(big): %v", err)
	}
	smallOnly, err := ComputeBaseline(small.path, "test@example.com", periodStart, window, nil)
	if err != nil {
		t.Fatalf("ComputeBaseline(small): %v", err)
	}
	if bigOnly.InsufficientHistory || smallOnly.InsufficientHistory {
		t.Fatalf("per-repo baselines flagged insufficient: big=%v small=%v",
			bigOnly.InsufficientHistory, smallOnly.InsufficientHistory)
	}

	got, err := ComputeBaselineAcross([]string{big.path, small.path}, testAuthors, periodStart, window, nil)
	if err != nil {
		t.Fatalf("ComputeBaselineAcross: %v", err)
	}
	if got.WorkingDays != bigOnly.WorkingDays {
		t.Errorf("WorkingDays = %d, want %d: the calendar is shared, not per repo", got.WorkingDays, bigOnly.WorkingDays)
	}
	if !closeTo(got.LOCPerDay, bigOnly.LOCPerDay+smallOnly.LOCPerDay) {
		t.Errorf("LOCPerDay = %v, want %v", got.LOCPerDay, bigOnly.LOCPerDay+smallOnly.LOCPerDay)
	}
	if closeTo(got.LOCPerDay, bigOnly.LOCPerDay) {
		t.Errorf("LOCPerDay = %v, which is the big repo alone", got.LOCPerDay)
	}
	if got.ReposCovered != 2 {
		t.Errorf("ReposCovered = %d, want 2", got.ReposCovered)
	}
}

// TestAcrossSpanningRepoUnionOfActivity checks the InsufficientHistory rule
// uses the union of activity. Neither repo on its own shows 30 days of work,
// but the person was working the whole time.
func TestBaselineAcrossUsesUnionOfActivity(t *testing.T) {
	periodStart := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	window := 90 * 24 * time.Hour

	early := newTestRepo(t)
	early.writeFile("a.txt", churnLines(50))
	early.commit("first", testAuthor, day(periodStart, -80))
	early.writeFile("b.txt", churnLines(50))
	early.commit("second", testAuthor, day(periodStart, -75))

	late := newTestRepo(t)
	late.writeFile("a.txt", churnLines(50))
	late.commit("first", testAuthor, day(periodStart, -20))
	late.writeFile("b.txt", churnLines(50))
	late.commit("second", testAuthor, day(periodStart, -15))

	for name, path := range map[string]string{"early": early.path, "late": late.path} {
		b, err := ComputeBaseline(path, "test@example.com", periodStart, window, nil)
		if err != nil {
			t.Fatalf("ComputeBaseline(%s): %v", name, err)
		}
		if !b.InsufficientHistory {
			t.Fatalf("%s repo should be insufficient on its own", name)
		}
	}

	got, err := ComputeBaselineAcross([]string{early.path, late.path}, testAuthors, periodStart, window, nil)
	if err != nil {
		t.Fatalf("ComputeBaselineAcross: %v", err)
	}
	if got.InsufficientHistory {
		t.Error("pooled activity spans 65 days, should not be flagged insufficient")
	}
	if got.LOCPerDay <= 0 {
		t.Errorf("LOCPerDay = %v, want > 0", got.LOCPerDay)
	}
}

// TestAcrossSkipsUnreadableRepos checks that one bad path costs the user that
// repo and nothing else, and that the coverage count says so.
func TestAcrossSkipsUnreadableRepos(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	good := newTestRepo(t)
	for i, off := range []float64{0, 1, 2} {
		good.writeFile("f.txt", line(i))
		good.commit("c", testAuthor, day(base, off))
	}
	// A plain directory, not a repository.
	bad := t.TempDir()

	since := day(base, -1)
	until := day(base, 10)

	alone, err := ComputeCommitSize(good.path, testAuthor, since, until, nil)
	if err != nil {
		t.Fatalf("ComputeCommitSize: %v", err)
	}

	got, err := ComputeCommitSizeAcross([]string{good.path, bad}, testAuthors, since, until, nil)
	if err != nil {
		t.Fatalf("ComputeCommitSizeAcross: %v", err)
	}
	if got.Total != alone.Total {
		t.Errorf("Total = %d, want %d: the readable repo should still be counted", got.Total, alone.Total)
	}
	if got.ReposCovered != 1 {
		t.Errorf("ReposCovered = %d, want 1: coverage must not claim the repo that failed", got.ReposCovered)
	}

	c, err := ComputeCadenceAcross([]string{good.path, bad}, testAuthors, since, until)
	if err != nil {
		t.Fatalf("ComputeCadenceAcross: %v", err)
	}
	if c.ReposCovered != 1 {
		t.Errorf("cadence ReposCovered = %d, want 1", c.ReposCovered)
	}
}

// TestAcrossErrorsWhenNothingReadable checks that a run where every repository
// failed reports an error instead of a confident zero.
func TestAcrossErrorsWhenNothingReadable(t *testing.T) {
	bad := []string{t.TempDir(), t.TempDir()}
	since := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)

	if _, err := ComputeCommitSizeAcross(bad, testAuthors, since, until, nil); err == nil {
		t.Error("ComputeCommitSizeAcross: want error, got nil")
	}
	if _, err := ComputeCadenceAcross(bad, testAuthors, since, until); err == nil {
		t.Error("ComputeCadenceAcross: want error, got nil")
	}
	if _, err := ComputeLeadTimeAcross(bad, testAuthors, since, until); err == nil {
		t.Error("ComputeLeadTimeAcross: want error, got nil")
	}
	if _, err := ComputeChurnAcross(bad, testAuthors, since, until, 30*24*time.Hour, nil); err == nil {
		t.Error("ComputeChurnAcross: want error, got nil")
	}
	if _, err := ComputeBaselineAcross(bad, testAuthors, until, 30*24*time.Hour, nil); err == nil {
		t.Error("ComputeBaselineAcross: want error, got nil")
	}
}

// TestAcrossDedupesRepeatedPaths checks that a repo named twice, which is what
// a positional path plus a -r scan produces, is not weighed twice.
func TestAcrossDedupesRepeatedPaths(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	repo := newTestRepo(t)
	for i, off := range []float64{0, 1, 2} {
		repo.writeFile("f.txt", line(i))
		repo.commit("c", testAuthor, day(base, off))
	}

	since := day(base, -1)
	until := day(base, 10)

	once, err := ComputeCommitSizeAcross([]string{repo.path}, testAuthors, since, until, nil)
	if err != nil {
		t.Fatalf("ComputeCommitSizeAcross: %v", err)
	}
	twice, err := ComputeCommitSizeAcross(
		[]string{repo.path, filepath.Join(repo.path, ".")}, testAuthors, since, until, nil)
	if err != nil {
		t.Fatalf("ComputeCommitSizeAcross: %v", err)
	}
	if twice.Total != once.Total {
		t.Errorf("Total = %d, want %d: the same repo listed twice must not count twice", twice.Total, once.Total)
	}
	if twice.ReposCovered != 1 {
		t.Errorf("ReposCovered = %d, want 1", twice.ReposCovered)
	}
}

// TestAcrossMatchesSeveralAddresses checks that one person's split identities
// are pooled as one contributor.
func TestAcrossMatchesSeveralAddresses(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	work := newTestRepo(t)
	work.writeFile("a.txt", lines(20))
	work.commit("work laptop", "Dev <dev@corp.com>", base)

	home := newTestRepo(t)
	home.writeFile("b.txt", lines(20))
	home.commit("home laptop", "Dev <dev@personal.net>", day(base, 1))

	since := day(base, -1)
	until := day(base, 10)

	got, err := ComputeCommitSizeAcross(
		[]string{work.path, home.path},
		[]string{"dev@corp.com", "dev@personal.net"},
		since, until, nil)
	if err != nil {
		t.Fatalf("ComputeCommitSizeAcross: %v", err)
	}
	if got.Total != 2 {
		t.Errorf("Total = %d, want 2: both addresses belong to the same person", got.Total)
	}
}

// TestMainBranchNamesAcrossRepos checks the reported branch name when the
// repositories do not agree on one.
func TestMainBranchNamesAcrossRepos(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	onMain := newTestRepo(t)
	for i, off := range []float64{0, 1} {
		onMain.writeFile("f.txt", line(i))
		onMain.commit("c", testAuthor, day(base, off))
	}

	onMaster := newTestRepo(t)
	for i, off := range []float64{0, 1} {
		onMaster.writeFile("f.txt", line(i))
		onMaster.commit("c", testAuthor, day(base, off))
	}
	run(t, onMaster.path, "git", "branch", "-M", "master")

	got, err := ComputeCadenceAcross([]string{onMain.path, onMaster.path}, testAuthors, day(base, -1), day(base, 10))
	if err != nil {
		t.Fatalf("ComputeCadenceAcross: %v", err)
	}
	if got.MainBranch != "main/master" {
		t.Errorf("MainBranch = %q, want %q", got.MainBranch, "main/master")
	}
}
