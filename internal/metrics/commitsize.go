package metrics

import (
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
	// ReposCovered is how many repositories' histories went into these counts.
	// The report needs it to avoid presenting one repo's habits as if they
	// described every repo in the run.
	ReposCovered int `json:"repos_covered,omitempty"`
}

// Percent returns the percentage of commits in bucket b. Returns 0 if Total is 0.
func (d CommitSizeDistribution) Percent(b SizeBucket) float64 {
	if d.Total == 0 {
		return 0
	}
	return float64(d.Counts[b]) * 100 / float64(d.Total)
}

// bucketFor places a commit by its total lines changed.
func bucketFor(total int) SizeBucket {
	switch {
	case total < 10:
		return BucketMicro
	case total < 100:
		return BucketSmall
	case total < 500:
		return BucketMedium
	default:
		return BucketLarge
	}
}

// ComputeCommitSize analyzes the size distribution of commits in repoPath for the
// given author and date window. Binary files and files matching exclude patterns
// are ignored.
func ComputeCommitSize(repoPath, author string, since, until time.Time, exclude []string) (CommitSizeDistribution, error) {
	return ComputeCommitSizeAcross([]string{repoPath}, []string{author}, since, until, exclude)
}

// ComputeCommitSizeAcross pools the commits of every repository in paths into a
// single distribution, for an author reachable under any of the given
// addresses.
//
// Counts add up across repositories with no statistical subtlety: each commit
// belongs to exactly one bucket wherever it was made. The reason this is not
// simply a caller-side loop is ReposCovered, which has to reflect the repos
// that were actually read rather than the repos that were asked for.
//
// A repository git cannot read is skipped rather than aborting the run, since
// one unreadable repo in a -r scan should not cost the user every other repo's
// numbers. If none could be read the result is an error, not an empty
// distribution.
func ComputeCommitSizeAcross(paths []string, authors []string, since, until time.Time, exclude []string) (CommitSizeDistribution, error) {
	var (
		dist     CommitSizeDistribution
		scanned  int
		firstErr error
	)
	for _, path := range dedupePaths(paths) {
		records, err := scanNumstat(path, authors, since, until, exclude)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		scanned++
		for _, r := range records {
			dist.Counts[bucketFor(r.Total())]++
			dist.Total++
		}
	}
	if scanned == 0 {
		return CommitSizeDistribution{}, coverageErr(firstErr)
	}
	dist.ReposCovered = scanned
	return dist, nil
}
