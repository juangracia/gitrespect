package metrics

import (
	"time"
)

// Churn holds code churn metrics for an author over a time window.
type Churn struct {
	WindowDays   int     `json:"window_days"`
	AddedLines   int     `json:"added_lines"`
	ChurnedLines int     `json:"churned_lines"`
	Ratio        float64 `json:"ratio"`
	// ReposCovered is how many repositories' line counts went into this ratio.
	ReposCovered int `json:"repos_covered,omitempty"`
}

// ComputeChurn calculates code churn for an author by comparing lines added
// in a prior window against lines deleted in the current period.
func ComputeChurn(repoPath, author string, since, until time.Time, window time.Duration, exclude []string) (Churn, error) {
	return ComputeChurnAcross([]string{repoPath}, []string{author}, since, until, window, exclude)
}

// ComputeChurnAcross calculates churn over every repository in paths, for an
// author reachable under any of the given addresses.
//
// The two line counts are summed across repositories and the ratio is taken
// once from those totals. Averaging each repository's ratio would let a repo
// where the author added four lines and deleted two count as heavily as a repo
// with forty thousand lines behind it.
//
// A repository git cannot read is skipped rather than aborting the run. If none
// could be read the result is an error, not a confident zero.
func ComputeChurnAcross(paths []string, authors []string, since, until time.Time, window time.Duration, exclude []string) (Churn, error) {
	priorStart := since.Add(-window)

	var (
		totalAdded   int
		totalDeleted int
		scanned      int
		contributed  int
		firstErr     error
	)
	for _, path := range dedupePaths(paths) {
		added, _, err := sumNumstat(path, authors, priorStart, since, exclude)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		_, deleted, err := sumNumstat(path, authors, since, until, exclude)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		scanned++
		if added > 0 || deleted > 0 {
			// A repository the author added nothing to contributed nothing to
			// this ratio, so counting it would overstate the coverage.
			contributed++
		}
		totalAdded += added
		totalDeleted += deleted
	}
	if scanned == 0 {
		return Churn{}, coverageErr(firstErr)
	}

	c := Churn{
		WindowDays:   int(window.Hours() / 24),
		AddedLines:   totalAdded,
		ChurnedLines: totalDeleted,
		ReposCovered: contributed,
	}
	if totalAdded > 0 {
		c.Ratio = float64(totalDeleted) / float64(totalAdded)
	}
	return c, nil
}
