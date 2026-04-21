package metrics

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// SizeBucket categorizes a commit by total lines changed.
type SizeBucket int

const (
	BucketMicro  SizeBucket = iota // <10 LOC total change
	BucketSmall                    // 10-99
	BucketMedium                   // 100-499
	BucketLarge                    // 500+
)

// CommitSizeDistribution holds counts of commits per size bucket.
type CommitSizeDistribution struct {
	Counts [4]int `json:"counts"`
	Total  int    `json:"total"`
}

// Percent returns the percentage of commits in bucket b. Returns 0 if Total is 0.
func (d CommitSizeDistribution) Percent(b SizeBucket) float64 {
	if d.Total == 0 {
		return 0
	}
	return float64(d.Counts[b]) * 100 / float64(d.Total)
}

// ComputeCommitSize analyzes the size distribution of commits in repoPath for the
// given author and date window. Binary files and files matching exclude patterns
// are ignored.
func ComputeCommitSize(repoPath, author string, since, until time.Time, exclude []string) (CommitSizeDistribution, error) {
	args := []string{
		"-C", repoPath, "log",
		"--author=" + author,
		"--since=" + since.Format("2006-01-02"),
		"--until=" + until.Format("2006-01-02"),
		"--pretty=format:COMMIT %H",
		"--numstat",
	}
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return CommitSizeDistribution{}, fmt.Errorf("git log: %w", err)
	}

	var dist CommitSizeDistribution
	inCommit := false
	commitTotal := 0

	flush := func() {
		if !inCommit {
			return
		}
		var b SizeBucket
		switch {
		case commitTotal < 10:
			b = BucketMicro
		case commitTotal < 100:
			b = BucketSmall
		case commitTotal < 500:
			b = BucketMedium
		default:
			b = BucketLarge
		}
		dist.Counts[b]++
		dist.Total++
		inCommit = false
		commitTotal = 0
	}

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "COMMIT ") {
			flush()
			inCommit = true
			commitTotal = 0
			continue
		}
		if !inCommit || line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		// Binary files show "-" for added/deleted counts.
		if fields[0] == "-" || fields[1] == "-" {
			continue
		}
		filename := strings.Join(fields[2:], " ")
		if shouldExcludeFile(filename, exclude) {
			continue
		}
		a, err1 := strconv.Atoi(fields[0])
		d, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		commitTotal += a + d
	}
	flush()

	return dist, nil
}
