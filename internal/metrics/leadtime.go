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
}

// ComputeLeadTime calculates the median lead time (in days) for merge commits
// authored by the given author on the main branch within the specified window.
func ComputeLeadTime(repoPath, author string, since, until time.Time) (LeadTime, error) {
	main := detectMainBranch(repoPath)
	if main == "" {
		return LeadTime{MainBranch: ""}, nil
	}

	args := []string{
		"-C", repoPath,
		"log", main,
		"--merges",
		"--first-parent",
		"--author=" + author,
		"--since=" + git.TimeArg(since),
		"--until=" + git.TimeArg(until),
		"--format=%H %P %ct",
	}
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return LeadTime{}, fmt.Errorf("git log: %w", err)
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

	if len(days) > 0 {
		return LeadTime{
			MedianDays: median(days),
			Samples:    len(days),
			MainBranch: main,
			Method:     LeadTimeMerge,
		}, nil
	}

	// No merge commits in range. Fall back to author-to-landed, which still
	// works for rebase and patch-based workflows.
	return authoredLeadTime(repoPath, author, main, since, until)
}

// authoredLeadTime derives lead time from the gap between when a commit was
// authored and when it landed on main. Only commits whose committer date is
// later than their author date carry a signal; a commit made directly on main
// has identical dates and is correctly excluded rather than counted as zero.
func authoredLeadTime(repoPath, author, main string, since, until time.Time) (LeadTime, error) {
	args := []string{
		"-C", repoPath,
		"log", main,
		"--no-merges",
		"--author=" + author,
		"--since=" + git.TimeArg(since),
		"--until=" + git.TimeArg(until),
		"--format=%at %ct",
	}
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return LeadTime{MainBranch: main}, fmt.Errorf("git log: %w", err)
	}

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
		days = append(days, float64(ct-at)/86400.0)
	}

	if len(days) == 0 {
		return LeadTime{MainBranch: main}, nil
	}
	return LeadTime{
		MedianDays: median(days),
		Samples:    len(days),
		MainBranch: main,
		Method:     LeadTimeAuthored,
	}, nil
}
