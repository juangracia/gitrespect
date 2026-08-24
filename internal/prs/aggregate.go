package prs

import (
	"sort"
	"strings"
	"time"
)

// maxUnmatchedReported caps the "nobody claimed these accounts" list so a
// large group does not bury the actual report under hundreds of handles.
const maxUnmatchedReported = 10

// daysPerMonth normalizes counts from windows of different length. Merge
// request volume is naturally a per month rate, unlike lines of code, which
// the git side normalizes per working day.
const daysPerMonth = 30.0

// Aggregate turns raw provider output into a Result. It is pure: no network,
// no clock and no filesystem, so bucketing, identity matching and lead time
// are all testable on their own.
func Aggregate(f Fetched, opts Options) (Result, error) {
	// Matching and filtering are separate questions. A --team says who to
	// count, so an account it does not claim is dropped. A bare --roster only
	// says who an account belongs to, so an account it does not claim keeps
	// its own row rather than disappearing from the group total.
	identities, filtering := opts.identities()
	matcher, err := NewMatcherFor(identities, opts.Mappings)
	if err != nil {
		return Result{}, err
	}

	res := Result{
		Provider:    opts.Provider,
		Scope:       opts.Scope,
		Since:       opts.Since,
		Until:       opts.Until,
		Granularity: opts.Granularity,
		Filtered:    filtering,
		Truncated:   f.Truncated,
		Note:        f.Note,
		Requests:    f.Requests,
	}

	type bucket struct {
		label    string
		handles  map[string]bool
		opened   int
		merged   int
		periods  map[string]*PeriodCount
		leadDays []float64
	}

	order := []string{}
	buckets := map[string]*bucket{}
	newBucket := func(key, label string) *bucket {
		b := &bucket{label: label, handles: map[string]bool{}, periods: map[string]*PeriodCount{}}
		buckets[key] = b
		order = append(order, key)
		return b
	}

	// Known identities are seeded even when they opened nothing: a team member
	// or roster entry with zero merge requests in the window is a real answer,
	// not a missing row.
	for _, label := range matcher.Labels() {
		newBucket(label, label)
	}

	teamPeriods := map[string]*PeriodCount{}
	var teamLeadDays []float64
	unmatched := map[string]int{}

	for _, mr := range f.Items {
		if mr.CreatedAt.IsZero() {
			continue
		}

		var key, label string
		switch matched, ok := matcher.Match(mr); {
		case ok:
			key, label = matched, matched
		case filtering:
			unmatched[mr.authorLabel()]++
			res.UnmatchedTotal++
			continue
		default:
			// Nothing claimed this account and nothing was asked to, so it is
			// its own identity. Group on the username when there is one, since
			// display names change and emails are usually absent.
			label = mr.authorLabel()
			if mr.AuthorUser != "" {
				label = mr.AuthorUser
			}
			key = strings.ToLower(label)
		}

		b, ok := buckets[key]
		if !ok {
			b = newBucket(key, label)
		}
		if h := mr.authorLabel(); h != "(unknown)" {
			b.handles[h] = true
		}

		merged := !mr.MergedAt.IsZero()
		b.opened++
		res.Opened++
		if merged {
			b.merged++
			res.Merged++
		}

		if opts.Granularity != "" {
			pk, plabel, start := periodKey(mr.CreatedAt, opts.Granularity)
			addPeriod(teamPeriods, pk, plabel, start, merged)
			addPeriod(b.periods, pk, plabel, start, merged)
		}

		if merged && mr.MergedAt.After(mr.CreatedAt) {
			days := mr.MergedAt.Sub(mr.CreatedAt).Hours() / 24
			b.leadDays = append(b.leadDays, days)
			teamLeadDays = append(teamLeadDays, days)
		}
	}

	res.Periods = sortPeriods(teamPeriods)
	res.LeadTime = summarizeLeadTime(teamLeadDays)

	for _, key := range order {
		b := buckets[key]
		as := AuthorStats{
			Identity: b.label,
			Handles:  sortedHandles(b.handles),
			Opened:   b.opened,
			Merged:   b.merged,
			LeadTime: summarizeLeadTime(b.leadDays),
		}
		// Align every author on the team wide buckets, zeros included, so the
		// per member breakdown lines up column for column.
		if opts.Granularity != "" {
			as.Periods = alignPeriods(res.Periods, b.periods)
		}
		res.Authors = append(res.Authors, as)
	}
	sort.SliceStable(res.Authors, func(i, j int) bool {
		if res.Authors[i].Opened != res.Authors[j].Opened {
			return res.Authors[i].Opened > res.Authors[j].Opened
		}
		return res.Authors[i].Identity < res.Authors[j].Identity
	})

	// An explicit --team is always shown in full: the user named those people
	// and a missing row would read as "they opened nothing".
	res.AuthorsTotal = len(res.Authors)
	if opts.Top > 0 && !filtering && len(res.Authors) > opts.Top {
		res.Authors = res.Authors[:opts.Top]
	}

	res.UnmatchedAccounts = len(unmatched)
	res.Unmatched = topUnmatched(unmatched)
	return res, nil
}

func addPeriod(into map[string]*PeriodCount, key, label string, start time.Time, merged bool) {
	p, ok := into[key]
	if !ok {
		p = &PeriodCount{Key: key, Label: label, Start: start}
		into[key] = p
	}
	p.Opened++
	if merged {
		p.Merged++
	}
}

func sortPeriods(in map[string]*PeriodCount) []PeriodCount {
	out := make([]PeriodCount, 0, len(in))
	for _, p := range in {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// alignPeriods projects one author's buckets onto the team wide set, filling
// gaps with zeros.
func alignPeriods(reference []PeriodCount, own map[string]*PeriodCount) []PeriodCount {
	if len(reference) == 0 {
		return nil
	}
	out := make([]PeriodCount, 0, len(reference))
	for _, ref := range reference {
		row := PeriodCount{Key: ref.Key, Label: ref.Label, Start: ref.Start}
		if p, ok := own[ref.Key]; ok {
			row.Opened, row.Merged = p.Opened, p.Merged
		}
		out = append(out, row)
	}
	return out
}

// periodKey buckets a timestamp, using the same keys and labels as the git
// side's breakdown so the two reports can sit next to each other.
func periodKey(t time.Time, granularity string) (key, label string, start time.Time) {
	t = t.Local()
	switch granularity {
	case "weekly":
		// Anchor each week on its Monday so the label names a real date.
		monday := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()).
			AddDate(0, 0, -weekdayOffset(t))
		return monday.Format("2006-01-02"), "Week of " + monday.Format("Jan 2 2006"), monday
	case "daily":
		day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
		return day.Format("2006-01-02"), day.Format("Jan 2 2006"), day
	default:
		month := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
		return month.Format("2006-01"), month.Format("Jan 2006"), month
	}
}

// weekdayOffset returns days elapsed since Monday for t.
func weekdayOffset(t time.Time) int {
	return (int(t.Weekday()) + 6) % 7
}

// summarizeLeadTime returns nil rather than a zeroed struct when there is no
// signal, so reports can distinguish "no merged merge requests" from "merged
// instantly".
func summarizeLeadTime(days []float64) *LeadTimeStats {
	if len(days) == 0 {
		return nil
	}
	sorted := make([]float64, len(days))
	copy(sorted, days)
	sort.Float64s(sorted)

	var sum float64
	for _, d := range sorted {
		sum += d
	}
	return &LeadTimeStats{
		Samples:    len(sorted),
		MedianDays: percentile(sorted, 0.5),
		P75Days:    percentile(sorted, 0.75),
		MeanDays:   sum / float64(len(sorted)),
	}
}

// percentile does a nearest rank lookup on an already sorted slice, except for
// the median of an even sample, which is averaged the way readers expect.
func percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if p == 0.5 && n%2 == 0 {
		return (sorted[n/2-1] + sorted[n/2]) / 2
	}
	idx := int(p * float64(n))
	if idx >= n {
		idx = n - 1
	}
	return sorted[idx]
}

func topUnmatched(counts map[string]int) []UnmatchedAuthor {
	if len(counts) == 0 {
		return nil
	}
	out := make([]UnmatchedAuthor, 0, len(counts))
	for handle, n := range counts {
		out = append(out, UnmatchedAuthor{Handle: handle, Opened: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Opened != out[j].Opened {
			return out[i].Opened > out[j].Opened
		}
		return out[i].Handle < out[j].Handle
	})
	if len(out) > maxUnmatchedReported {
		out = out[:maxUnmatchedReported]
	}
	return out
}

// Days is the length of the reporting window in calendar days.
func (r Result) Days() float64 {
	d := r.Until.Sub(r.Since).Hours() / 24
	if d <= 0 {
		return 0
	}
	return d
}

// OpenedPerMonth normalizes volume so two windows of different length are
// comparable.
func (r Result) OpenedPerMonth() float64 {
	days := r.Days()
	if days == 0 {
		return 0
	}
	return float64(r.Opened) / days * daysPerMonth
}

// Author returns one identity's stats by label.
func (r Result) Author(identity string) (AuthorStats, bool) {
	for _, a := range r.Authors {
		if a.Identity == identity {
			return a, true
		}
	}
	return AuthorStats{}, false
}

// AuthorComparison is one person's before and after volume.
type AuthorComparison struct {
	Identity   string  `json:"identity"`
	Before     int     `json:"before"`
	After      int     `json:"after"`
	Multiplier float64 `json:"multiplier"`
	// Undefined is set when Before is zero, where a multiplier has no meaning.
	// Reporting 0.0x for "went from nothing to twenty" would be backwards.
	Undefined bool `json:"undefined,omitempty"`
}

// Comparison is the before/after answer for merge request volume, the same
// shape the lines of code comparison already produces.
type Comparison struct {
	Before      Result `json:"before"`
	After       Result `json:"after"`
	BeforeLabel string `json:"before_label"`
	AfterLabel  string `json:"after_label"`

	OpenedMultiplier float64 `json:"opened_multiplier"`
	OpenedUndefined  bool    `json:"opened_undefined,omitempty"`

	BeforePerMonth   float64 `json:"before_per_month"`
	AfterPerMonth    float64 `json:"after_per_month"`
	PerMonthMultiple float64 `json:"per_month_multiplier"`
	PerMonthUndefine bool    `json:"per_month_undefined,omitempty"`

	Authors []AuthorComparison `json:"authors,omitempty"`
}

// CompareResults produces the Nx style before/after multiplier for merge
// request volume. It works on two Results, so it does not care whether they
// came from the same provider call, two calls, or a test fixture.
func CompareResults(before, after Result) Comparison {
	c := Comparison{Before: before, After: after}
	c.OpenedMultiplier, c.OpenedUndefined = ratio(float64(before.Opened), float64(after.Opened))
	c.BeforePerMonth = before.OpenedPerMonth()
	c.AfterPerMonth = after.OpenedPerMonth()
	c.PerMonthMultiple, c.PerMonthUndefine = ratio(c.BeforePerMonth, c.AfterPerMonth)

	seen := map[string]bool{}
	var identities []string
	for _, list := range [][]AuthorStats{before.Authors, after.Authors} {
		for _, a := range list {
			if !seen[a.Identity] {
				seen[a.Identity] = true
				identities = append(identities, a.Identity)
			}
		}
	}
	for _, id := range identities {
		b, _ := before.Author(id)
		a, _ := after.Author(id)
		mult, undef := ratio(float64(b.Opened), float64(a.Opened))
		c.Authors = append(c.Authors, AuthorComparison{
			Identity:   id,
			Before:     b.Opened,
			After:      a.Opened,
			Multiplier: mult,
			Undefined:  undef,
		})
	}
	sort.SliceStable(c.Authors, func(i, j int) bool {
		if c.Authors[i].After != c.Authors[j].After {
			return c.Authors[i].After > c.Authors[j].After
		}
		return c.Authors[i].Identity < c.Authors[j].Identity
	})
	return c
}

// ratio returns after/before, flagging the case where before is zero and the
// multiplier is meaningless rather than quietly returning 0.
func ratio(before, after float64) (float64, bool) {
	if before <= 0 {
		return 0, true
	}
	return after / before, false
}
