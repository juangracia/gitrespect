package metrics

import (
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Cadence measures how frequently an author commits to the main branch.
type Cadence struct {
	MedianDaysBetween float64 `json:"median_days_between"`
	Samples           int     `json:"samples"`
	MainBranch        string  `json:"main_branch"`
}

// ComputeCadence returns the median number of days between the author's
// commits on the main branch within [since, until].
func ComputeCadence(repoPath, author string, since, until time.Time) (Cadence, error) {
	var c Cadence
	branch := detectMainBranch(repoPath)
	if branch == "" {
		return c, nil
	}
	c.MainBranch = branch

	// git --since is exclusive (commits on that calendar day are excluded), so
	// subtract one day to include commits that fall on the since date itself.
	sinceStr := since.AddDate(0, 0, -1).Format("2006-01-02")
	args := []string{
		"-C", repoPath, "log", branch,
		"--author=" + author,
		"--since=" + sinceStr,
		"--until=" + until.Format("2006-01-02"),
		"--no-merges",
		"--format=%ct",
	}
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return c, fmt.Errorf("git log: %w", err)
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

	if len(timestamps) < 2 {
		return c, nil
	}

	sort.Slice(timestamps, func(i, j int) bool { return timestamps[i] < timestamps[j] })

	intervals := make([]float64, 0, len(timestamps)-1)
	for i := 1; i < len(timestamps); i++ {
		delta := float64(timestamps[i]-timestamps[i-1]) / 86400.0
		intervals = append(intervals, delta)
	}

	c.Samples = len(intervals)
	c.MedianDaysBetween = median(intervals)
	return c, nil
}
