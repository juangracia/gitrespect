package metrics

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/juangracia/gitrespect/internal/git"
)

// Lead time measurement methods.
const (
	// LeadTimeMerge measures from the first commit on a feature branch to the
	// merge commit that landed it. Requires real merge commits.
	LeadTimeMerge = "merge-commits"
	// LeadTimeAuthored measures committer date minus author date for commits
	// on main. Rebase and patch workflows preserve the author date, so this
	// recovers lead time where no merge commit exists. Squash merges rewrite
	// both dates, so they yield no samples here.
	LeadTimeAuthored = "authored-to-landed"
)

// LeadTime holds the result of a lead time analysis.
type LeadTime struct {
	MedianDays float64 `json:"median_days"`
	Samples    int     `json:"samples"`
	MainBranch string  `json:"main_branch"`
	// Method records how lead time was derived, since the two approaches are
	// not directly comparable.
	Method string `json:"method,omitempty"`
	// ReposCovered is how many repositories were examined for lead time.
	// Repositories with no main-like branch are not counted, since there was
	// nothing in them to measure.
	ReposCovered int `json:"repos_covered,omitempty"`
}

// ComputeLeadTime calculates the median lead time (in days) for merge commits
// authored by the given author on the main branch within the specified window.
func ComputeLeadTime(repoPath, author string, since, until time.Time) (LeadTime, error) {
	return ComputeLeadTimeAcross([]string{repoPath}, []string{author}, since, until)
}

// ComputeLeadTimeAcross calculates one median lead time over every repository
// in paths, for an author reachable under any of the given addresses.
//
// Pooling here cannot be a plain concatenation, because lead time is measured
// two ways and the two are not comparable: a merge-commit sample is how long a
// branch lived, while an authored-to-landed sample is how long a patch waited
// between being written and reaching main. Mixing them would produce a median
// of two different quantities.
//
// So the method is chosen once, for the whole run, and only samples of that one
// method are pooled. Every repository is asked for merge-commit samples first;
// if any repository anywhere yields one, the merge-commit median is taken over
// all of them and repositories without merges simply contribute nothing. Only
// when no repository has a single merge commit does the run fall back to
// authored-to-landed, pooled the same way. That mirrors what the single
// repository path has always done, one level up.
//
// The minimum sample guard applies to the pooled set rather than per
// repository, because the pooled median is the number being reported and the
// guard is about how much evidence stands behind it.
//
// A repository git cannot read is skipped rather than aborting the run. If none
// could be read the result is an error, not a confident zero. A repository with
// no main-like branch is not a failure: it was read, it simply has nothing to
// measure, and it is left out of ReposCovered.
func ComputeLeadTimeAcross(paths []string, authors []string, since, until time.Time) (LeadTime, error) {
	type target struct{ path, branch string }

	var (
		targets  []target
		scanned  int
		firstErr error
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
		targets = append(targets, target{path, branch})
	}

	var (
		mergeDays []float64
		branches  []string
		measured  []target
	)
	for _, t := range targets {
		days, err := mergeLeadTimes(t.path, t.branch, authors, since, until)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		scanned++
		measured = append(measured, t)
		branches = append(branches, t.branch)
		mergeDays = append(mergeDays, days...)
	}
	if scanned == 0 {
		return LeadTime{}, coverageErr(firstErr)
	}

	lt := LeadTime{
		MainBranch:   joinBranches(branches),
		ReposCovered: len(measured),
	}
	if len(mergeDays) > 0 {
		lt.MedianDays = median(mergeDays)
		lt.Samples = len(mergeDays)
		lt.Method = LeadTimeMerge
		return lt, nil
	}

	// No merge commits anywhere in range. Fall back to author-to-landed, which
	// still works for rebase and patch-based workflows.
	var authoredDays []float64
	for _, t := range measured {
		// A failure here is not fatal: the repo was read well enough to answer
		// the merge question, and the run still has the other repos' samples.
		days, err := authoredLeadTimes(t.path, t.branch, authors, since, until)
		if err != nil {
			continue
		}
		authoredDays = append(authoredDays, days...)
	}
	if len(authoredDays) < minAuthoredSamples {
		return lt, nil
	}
	lt.MedianDays = median(authoredDays)
	lt.Samples = len(authoredDays)
	lt.Method = LeadTimeAuthored
	return lt, nil
}

// mergeLeadTimes returns one sample per merge commit on branch: the days
// between the oldest commit unique to the merged branch and the merge itself.
func mergeLeadTimes(repoPath, branch string, authors []string, since, until time.Time) ([]float64, error) {
	args := []string{"-C", repoPath, "log", branch, "--merges", "--first-parent"}
	args = append(args, git.AuthorArgsMulti(authors)...)
	args = append(args,
		"--since="+git.TimeArg(since),
		"--until="+git.TimeArg(until),
		"--format=%H %P %ct",
	)
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}

	var days []float64
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		tokens := strings.Fields(line)
		// tokens: [mergeSHA, parent1, parent2, ..., commitTimestamp]
		if len(tokens) < 4 {
			// need at least merge SHA + 2 parents + timestamp
			continue
		}
		mergeCTStr := tokens[len(tokens)-1]
		parents := tokens[1 : len(tokens)-1]
		if len(parents) < 2 {
			continue
		}
		p1 := parents[0]
		p2 := parents[1]

		mergeCT, err := strconv.ParseInt(mergeCTStr, 10, 64)
		if err != nil {
			continue
		}

		// Find the oldest commit unique to the feature branch (p2 side).
		branchOut, err := exec.Command("git", "-C", repoPath, "log",
			p1+".."+p2, "--format=%ct", "--reverse").Output()
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimSpace(string(branchOut)), "\n")
		if len(lines) == 0 || lines[0] == "" {
			continue
		}
		oldestCT, err := strconv.ParseInt(strings.TrimSpace(lines[0]), 10, 64)
		if err != nil {
			continue
		}

		leadDays := float64(mergeCT-oldestCT) / 86400.0
		if leadDays < 0 {
			leadDays = 0
		}
		days = append(days, leadDays)
	}
	return days, nil
}

// minAuthoredSamples is the number of rewritten commits required before an
// authored-to-landed median is reported. A single cherry-pick of old work
// would otherwise become the entire sample.
const minAuthoredSamples = 3

// authoredLeadTimes derives lead time from the gap between when a commit was
// authored and when it landed on main. Only commits whose committer date is
// later than their author date carry a signal; a commit made directly on main
// has identical dates.
//
// That discriminator detects a rewritten committer date, which a rebase or
// patch workflow produces but so do cherry-picks, amends and whole-history
// rewrites such as filter-repo. The guard applied here keeps the worst of those
// out: samples longer than the analysis window are discarded, since work that
// took longer than the period asked about did not flow through it. The other
// guard, requiring several samples before a median is reported, belongs to the
// caller because it applies to the pooled set.
func authoredLeadTimes(repoPath, branch string, authors []string, since, until time.Time) ([]float64, error) {
	args := []string{"-C", repoPath, "log", branch, "--no-merges"}
	args = append(args, git.AuthorArgsMulti(authors)...)
	args = append(args,
		"--since="+git.TimeArg(since),
		"--until="+git.TimeArg(until),
		"--format=%at %ct",
	)
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}

	windowDays := until.Sub(since).Hours() / 24

	var days []float64
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		at, err1 := strconv.ParseInt(fields[0], 10, 64)
		ct, err2 := strconv.ParseInt(fields[1], 10, 64)
		if err1 != nil || err2 != nil || ct <= at {
			continue
		}
		delta := float64(ct-at) / 86400.0
		// A cherry-pick or history rewrite shows a gap far larger than the
		// period under analysis. That is not this period's lead time.
		if windowDays > 0 && delta > windowDays {
			continue
		}
		days = append(days, delta)
	}
	return days, nil
}
