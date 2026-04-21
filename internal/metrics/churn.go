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
}

// ComputeChurn calculates code churn for an author by comparing lines added
// in a prior window against lines deleted in the current period.
func ComputeChurn(repoPath, author string, since, until time.Time, window time.Duration, exclude []string) (Churn, error) {
	priorStart := since.Add(-window)

	added, _, err := sumNumstat(repoPath, author, priorStart, since, exclude)
	if err != nil {
		return Churn{}, err
	}

	_, deleted, err := sumNumstat(repoPath, author, since, until, exclude)
	if err != nil {
		return Churn{}, err
	}

	c := Churn{
		WindowDays:   int(window.Hours() / 24),
		AddedLines:   added,
		ChurnedLines: deleted,
	}
	if added > 0 {
		c.Ratio = float64(deleted) / float64(added)
	}
	return c, nil
}
