package metrics

import (
	"time"

	"github.com/juangracia/gitrespect/internal/git"
)

// minBaselineSpanDays is how much real activity a window must contain before
// its LOC/day is treated as a baseline. A window whose commits all fall in one
// week describes that week, not a normal rate of work.
const minBaselineSpanDays = 30

// Baseline holds a personal productivity baseline derived from prior commit history.
type Baseline struct {
	WindowStart         time.Time `json:"window_start"`
	WindowEnd           time.Time `json:"window_end"`
	WorkingDays         int       `json:"working_days"`
	LOCPerDay           float64   `json:"loc_per_day"`
	InsufficientHistory bool      `json:"insufficient_history"`
	PeriodLOCPerDay     float64   `json:"period_loc_per_day"`
	PercentDelta        float64   `json:"percent_delta"`
	// ReposCovered is how many repositories' history the baseline was drawn
	// from. A baseline built on one repo and compared against a period total
	// covering five is not a comparison at all, so the report needs to be able
	// to say which it has.
	ReposCovered int `json:"repos_covered,omitempty"`
}

// ComputeBaseline derives the author's net LOC/day over the window
// [periodStart - window, periodStart). If the actual commit activity span in
// the window is under 30 days, marks InsufficientHistory.
func ComputeBaseline(repoPath, author string, periodStart time.Time, window time.Duration, exclude []string) (Baseline, error) {
	return ComputeBaselineAcross([]string{repoPath}, []string{author}, periodStart, window, exclude)
}

// ComputeBaselineAcross derives one baseline from every repository in paths,
// for an author reachable under any of the given addresses.
//
// Net lines are summed across repositories, and the rate is taken once by
// dividing that sum by the window's working days. The working days belong to
// the calendar, not to any repository, so they are counted once no matter how
// many repos contributed; dividing per repo and adding the rates would inflate
// the baseline by roughly the number of repositories.
//
// The activity span that decides InsufficientHistory is the union across
// repositories: earliest commit anywhere to latest commit anywhere. Someone
// working steadily but rotating between repos has a continuous history even
// though no single repo shows one.
//
// A repository git cannot read is skipped rather than aborting the run. If none
// could be read the result is an error, not a confident zero.
func ComputeBaselineAcross(paths []string, authors []string, periodStart time.Time, window time.Duration, exclude []string) (Baseline, error) {
	b := Baseline{
		WindowStart: periodStart.Add(-window),
		WindowEnd:   periodStart,
	}

	var (
		perRepo  []git.RepoStats
		firstErr error
	)
	for _, path := range dedupePaths(paths) {
		// AnalyzeMulti rather than one Analyze per address: git ORs repeated
		// --author patterns, so a commit matched by two of them is counted once
		// here and twice by a loop, and overlapping patterns are the norm for
		// the merged identities this exists for.
		stats, err := git.AnalyzeMulti(path, authors, b.WindowStart, b.WindowEnd, exclude)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		perRepo = append(perRepo, stats)
	}
	if len(perRepo) == 0 {
		return b, coverageErr(firstErr)
	}
	b.ReposCovered = len(perRepo)

	// CombineStats already sums the lines and takes the earliest first commit
	// and latest last commit, which is the union of activity this needs.
	combined := git.CombineStats(perRepo)
	if combined.FirstCommit.IsZero() || combined.LastCommit.IsZero() {
		b.InsufficientHistory = true
		return b, nil
	}
	if int(combined.LastCommit.Sub(combined.FirstCommit).Hours()/24) < minBaselineSpanDays {
		b.InsufficientHistory = true
		return b, nil
	}

	b.WorkingDays = git.WorkingDays(b.WindowStart, b.WindowEnd)
	if b.WorkingDays > 0 {
		b.LOCPerDay = float64(combined.Net) / float64(b.WorkingDays)
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
