package metrics

import (
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/juangracia/gitrespect/internal/git"
)

// Cadence measures how frequently an author commits to the main branch.
type Cadence struct {
	MedianDaysBetween float64 `json:"median_days_between"`
	Samples           int     `json:"samples"`
	MainBranch        string  `json:"main_branch"`
	// ReposCovered is how many repositories contributed commits to this
	// timeline. Repositories with no main-like branch are not counted, since
	// there was nothing in them for this metric to look at.
	ReposCovered int `json:"repos_covered,omitempty"`
}

// ComputeCadence returns the median number of days between the author's
// commits on the main branch within [since, until].
func ComputeCadence(repoPath, author string, since, until time.Time) (Cadence, error) {
	return ComputeCadenceAcross([]string{repoPath}, []string{author}, since, until)
}

// ComputeCadenceAcross returns the median gap between the author's commits
// across every repository in paths, for an author reachable under any of the
// given addresses.
//
// The commits are merged into one timeline before the gaps are measured, rather
// than each repository's gaps being measured separately and the results piled
// together. Cadence describes a person's rhythm, and someone who commits to a
// different repo each day is committing daily; measuring within each repo
// separately would call that a three day cadence. It also keeps a scan of
// twenty repos from being dominated by the nineteen that are touched once a
// year, each of which would otherwise contribute a gap of months.
//
// Sorting is what makes the merge safe: commits arrive per repository in that
// repository's order, and the pooled slice is ordered by time before any gap is
// taken.
//
// A repository git cannot read is skipped rather than aborting the run. If none
// could be read the result is an error, not a confident zero. A repository with
// no main-like branch is not a failure: it was read, it simply has nothing to
// measure, and it is left out of ReposCovered.
func ComputeCadenceAcross(paths []string, authors []string, since, until time.Time) (Cadence, error) {
	var (
		timestamps  []int64
		branches    []string
		scanned     int
		contributed int
		firstErr    error
	)
	for _, path := range dedupePaths(paths) {
		branch := detectMainBranch(path)
		if branch == "" {
			if isReadableRepo(path) {
				scanned++
			} else if firstErr == nil {
				firstErr = fmt.Errorf("not a git repository: %s", path)
			}
			continue
		}
		ts, err := cadenceTimestamps(path, branch, authors, since, until)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		scanned++
		branches = append(branches, branch)
		if len(ts) > 0 {
			// Only a repo that actually yielded commits counts as covered. A
			// repo the author never touched was examined, not covered, and
			// counting it would overstate how much history the median rests on.
			contributed++
		}
		timestamps = append(timestamps, ts...)
	}
	if scanned == 0 {
		return Cadence{}, coverageErr(firstErr)
	}

	c := Cadence{
		MainBranch:   joinBranches(branches),
		ReposCovered: contributed,
	}
	if len(timestamps) < 2 {
		return c, nil
	}

	sort.Slice(timestamps, func(i, j int) bool { return timestamps[i] < timestamps[j] })

	intervals := make([]float64, 0, len(timestamps)-1)
	for i := 1; i < len(timestamps); i++ {
		intervals = append(intervals, float64(timestamps[i]-timestamps[i-1])/86400.0)
	}

	c.Samples = len(intervals)
	c.MedianDaysBetween = median(intervals)
	return c, nil
}

// cadenceTimestamps returns the commit times, in unix seconds, of the authors'
// non-merge commits on branch. Merges are excluded because landing a branch is
// not a separate act of committing and would show up as a zero length gap.
func cadenceTimestamps(repoPath, branch string, authors []string, since, until time.Time) ([]int64, error) {
	args := []string{"-C", repoPath, "log", branch}
	args = append(args, git.AuthorArgsMulti(authors)...)
	args = append(args,
		"--since="+git.TimeArg(since),
		"--until="+git.TimeArg(until),
		"--no-merges",
		"--format=%ct",
	)
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}

	var timestamps []int64
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		ts, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			continue
		}
		timestamps = append(timestamps, ts)
	}
	return timestamps, nil
}
