package metrics

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// LeadTime holds the result of a lead time analysis across merge commits.
type LeadTime struct {
	MedianDays float64 `json:"median_days"`
	Samples    int     `json:"samples"`
	MainBranch string  `json:"main_branch"`
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
		"--since=" + since.Format("2006-01-02"),
		"--until=" + until.Format("2006-01-02"),
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

	return LeadTime{
		MedianDays: median(days),
		Samples:    len(days),
		MainBranch: main,
	}, nil
}
