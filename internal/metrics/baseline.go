package metrics

import (
	"time"

	"github.com/juangracia/gitrespect/internal/git"
)

// Baseline holds a personal productivity baseline derived from prior commit history.
type Baseline struct {
	WindowStart         time.Time `json:"window_start"`
	WindowEnd           time.Time `json:"window_end"`
	WorkingDays         int       `json:"working_days"`
	LOCPerDay           float64   `json:"loc_per_day"`
	InsufficientHistory bool      `json:"insufficient_history"`
	PeriodLOCPerDay     float64   `json:"period_loc_per_day"`
	PercentDelta        float64   `json:"percent_delta"`
}

// ComputeBaseline runs git.Analyze on the window [periodStart - window, periodStart)
// and returns the resulting net LOC/day. If the actual commit activity span in
// the window is under 30 days, marks InsufficientHistory.
func ComputeBaseline(repoPath, author string, periodStart time.Time, window time.Duration, exclude []string) (Baseline, error) {
	b := Baseline{
		WindowStart: periodStart.Add(-window),
		WindowEnd:   periodStart,
	}
	stats, err := git.Analyze(repoPath, author, b.WindowStart, b.WindowEnd, exclude)
	if err != nil {
		return b, err
	}
	if stats.FirstCommit.IsZero() || stats.LastCommit.IsZero() {
		b.InsufficientHistory = true
		return b, nil
	}
	activitySpanDays := int(stats.LastCommit.Sub(stats.FirstCommit).Hours() / 24)
	if activitySpanDays < 30 {
		b.InsufficientHistory = true
		return b, nil
	}
	b.WorkingDays = git.WorkingDays(b.WindowStart, b.WindowEnd)
	if b.WorkingDays > 0 {
		b.LOCPerDay = float64(stats.Net) / float64(b.WorkingDays)
	}
	return b, nil
}

// SetPeriod records the current period's LOC/day and computes PercentDelta
// relative to the baseline. No-op if InsufficientHistory or LOCPerDay is zero.
func (b *Baseline) SetPeriod(periodLOCPerDay float64) {
	b.PeriodLOCPerDay = periodLOCPerDay
	if b.InsufficientHistory || b.LOCPerDay == 0 {
		return
	}
	b.PercentDelta = (periodLOCPerDay - b.LOCPerDay) / b.LOCPerDay * 100
}
