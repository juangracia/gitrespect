package report

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/juangracia/gitrespect/internal/benchmark"
	"github.com/juangracia/gitrespect/internal/git"
	"github.com/juangracia/gitrespect/internal/metrics"
)

type JSONReport struct {
	Author     string             `json:"author"`
	Period     PeriodInfo         `json:"period"`
	Summary    SummaryStats       `json:"summary"`
	Daily      DailyStats         `json:"daily"`
	Benchmarks []BenchmarkResult  `json:"benchmarks,omitempty"`
	Metrics    *MetricsPayload    `json:"metrics,omitempty"`
	Monthly    []MonthlyJSONStats `json:"monthly,omitempty"`
	Breakdown  *BreakdownJSON     `json:"breakdown,omitempty"`
}

// BreakdownJSON carries whichever --breakdown granularity was requested.
// The legacy "monthly" field is still emitted for monthly so existing
// consumers keep working.
type BreakdownJSON struct {
	Granularity string          `json:"granularity"`
	Periods     []PeriodRowJSON `json:"periods"`
}

type PeriodRowJSON struct {
	Label   string `json:"label"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
	Net     int    `json:"net"`
	Commits int    `json:"commits"`
}

// buildBreakdown converts stats into the generic breakdown payload.
func buildBreakdown(stats git.RepoStats, granularity string) *BreakdownJSON {
	rows := git.Breakdown(stats, granularity)
	if len(rows) == 0 {
		return nil
	}
	out := &BreakdownJSON{Granularity: granularity, Periods: make([]PeriodRowJSON, 0, len(rows))}
	for _, r := range rows {
		out.Periods = append(out.Periods, PeriodRowJSON{
			Label:   r.Label,
			Added:   r.Added,
			Deleted: r.Deleted,
			Net:     r.Net,
			Commits: r.Commits,
		})
	}
	return out
}

type MetricsPayload struct {
	Baseline   *metrics.Baseline               `json:"baseline,omitempty"`
	CommitSize *metrics.CommitSizeDistribution `json:"commit_size,omitempty"`
	Cadence    *metrics.Cadence                `json:"cadence,omitempty"`
	LeadTime   *metrics.LeadTime               `json:"lead_time,omitempty"`
	Churn      *metrics.Churn                  `json:"churn,omitempty"`
}

type PeriodInfo struct {
	Since string `json:"since"`
	Until string `json:"until"`
	Days  int    `json:"working_days"`
}

type SummaryStats struct {
	Added        int `json:"added"`
	Deleted      int `json:"deleted"`
	Net          int `json:"net"`
	Commits      int `json:"commits"`
	FilesChanged int `json:"files_changed"`
}

type DailyStats struct {
	Added   float64 `json:"added"`
	Deleted float64 `json:"deleted"`
	Net     float64 `json:"net"`
}

type BenchmarkResult struct {
	Label      string  `json:"label"`
	Benchmark  int     `json:"benchmark_loc_per_day"`
	Multiplier float64 `json:"multiplier"`
}

type MonthlyJSONStats struct {
	Month   string `json:"month"`
	Year    int    `json:"year"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
	Net     int    `json:"net"`
	Commits int    `json:"commits"`
}

type CompareJSONReport struct {
	Before     PeriodStats `json:"before"`
	After      PeriodStats `json:"after"`
	Multiplier float64     `json:"productivity_multiplier"`
	Change     string      `json:"change_description"`
}

type PeriodStats struct {
	Label       string  `json:"label"`
	Net         int     `json:"net"`
	WorkingDays int     `json:"working_days"`
	PerDay      float64 `json:"per_day"`
}

type TeamCompareJSONReport struct {
	Before     PeriodStats               `json:"before"`
	After      PeriodStats               `json:"after"`
	Multiplier float64                   `json:"productivity_multiplier"`
	Change     string                    `json:"change_description"`
	Members    []MemberCompareJSONReport `json:"members"`
}

type MemberCompareJSONReport struct {
	Email  string      `json:"email"`
	Before PeriodStats `json:"before"`
	After  PeriodStats `json:"after"`
	// Multiplier is null when the member has no positive "before" output to
	// compare against, rather than reporting a meaningless ratio.
	Multiplier *float64 `json:"productivity_multiplier"`
}

func JSON(stats git.RepoStats, filename string, breakdown string, bundle metrics.Bundle) error {
	workingDays := git.WorkingDays(stats.Since, stats.Until)
	locPerDay := float64(stats.Net) / float64(workingDays)

	report := JSONReport{
		Author: stats.Author,
		Period: PeriodInfo{
			Since: stats.Since.Format("2006-01-02"),
			Until: stats.Until.Format("2006-01-02"),
			Days:  workingDays,
		},
		Summary: SummaryStats{
			Added:        stats.Added,
			Deleted:      stats.Deleted,
			Net:          stats.Net,
			Commits:      stats.Commits,
			FilesChanged: stats.FilesChanged,
		},
		Daily: DailyStats{
			Added:   float64(stats.Added) / float64(workingDays),
			Deleted: float64(stats.Deleted) / float64(workingDays),
			Net:     locPerDay,
		},
	}

	// Legacy benchmarks only when explicitly requested
	if bundle.LegacyBenchmark {
		comparisons := benchmark.Compare(locPerDay)
		for _, c := range comparisons {
			report.Benchmarks = append(report.Benchmarks, BenchmarkResult{
				Label:      c.Label,
				Benchmark:  c.Benchmark,
				Multiplier: c.Multiplier,
			})
		}
	}

	// New metrics payload
	if bundle.Baseline != nil || bundle.CommitSize != nil || bundle.Cadence != nil || bundle.LeadTime != nil || bundle.Churn != nil {
		report.Metrics = &MetricsPayload{
			Baseline:   bundle.Baseline,
			CommitSize: bundle.CommitSize,
			Cadence:    bundle.Cadence,
			LeadTime:   bundle.LeadTime,
			Churn:      bundle.Churn,
		}
	}

	// Add monthly if requested
	if breakdown == "monthly" && len(stats.Monthly) > 0 {
		var months []string
		for m := range stats.Monthly {
			months = append(months, m)
		}
		sort.Strings(months)

		for _, m := range months {
			ms := stats.Monthly[m]
			report.Monthly = append(report.Monthly, MonthlyJSONStats{
				Month:   getMonthName(ms.Month),
				Year:    ms.Year,
				Added:   ms.Added,
				Deleted: ms.Deleted,
				Net:     ms.Net,
				Commits: ms.Commits,
			})
		}
	}

	if breakdown != "" {
		report.Breakdown = buildBreakdown(stats, breakdown)
	}

	return writeJSON(report, filename)
}

type TeamJSONReport struct {
	Period  PeriodInfo         `json:"period"`
	Totals  TeamTotals         `json:"totals"`
	Members []MemberStats      `json:"members"`
	Monthly []MonthlyJSONStats `json:"monthly,omitempty"`
}

type TeamTotals struct {
	Added   int     `json:"added"`
	Deleted int     `json:"deleted"`
	Net     int     `json:"net"`
	Commits int     `json:"commits"`
	PerDay  float64 `json:"per_day"`
}

type MemberStats struct {
	Email   string          `json:"email"`
	Added   int             `json:"added"`
	Deleted int             `json:"deleted"`
	Net     int             `json:"net"`
	Commits int             `json:"commits"`
	PerDay  float64         `json:"per_day"`
	Metrics *MetricsPayload `json:"metrics,omitempty"`
}

func TeamJSON(stats git.TeamStats, filename string, breakdown string, bundles map[string]metrics.Bundle) error {
	workingDays := git.WorkingDays(stats.Since, stats.Until)

	report := TeamJSONReport{
		Period: PeriodInfo{
			Since: stats.Since.Format("2006-01-02"),
			Until: stats.Until.Format("2006-01-02"),
			Days:  workingDays,
		},
		Totals: TeamTotals{
			Added:   stats.TotalAdded,
			Deleted: stats.TotalDeleted,
			Net:     stats.TotalNet,
			Commits: stats.TotalCommits,
			PerDay:  float64(stats.TotalNet) / float64(workingDays),
		},
	}

	// Add member stats sorted by net lines
	type memberEntry struct {
		email string
		stats git.RepoStats
	}
	var members []memberEntry
	for email, ms := range stats.Members {
		members = append(members, memberEntry{email, ms})
	}
	sort.Slice(members, func(i, j int) bool {
		return members[i].stats.Net > members[j].stats.Net
	})

	for _, m := range members {
		ms := MemberStats{
			Email:   m.email,
			Added:   m.stats.Added,
			Deleted: m.stats.Deleted,
			Net:     m.stats.Net,
			Commits: m.stats.Commits,
			PerDay:  float64(m.stats.Net) / float64(workingDays),
		}
		if b, ok := bundles[m.email]; ok {
			if b.CommitSize != nil || b.Cadence != nil || b.LeadTime != nil || b.Churn != nil {
				ms.Metrics = &MetricsPayload{
					CommitSize: b.CommitSize,
					Cadence:    b.Cadence,
					LeadTime:   b.LeadTime,
					Churn:      b.Churn,
				}
			}
		}
		report.Members = append(report.Members, ms)
	}

	// Team-wide monthly breakdown
	if breakdown == "monthly" && len(stats.Monthly) > 0 {
		var months []string
		for m := range stats.Monthly {
			months = append(months, m)
		}
		sort.Strings(months)
		for _, m := range months {
			mo := stats.Monthly[m]
			report.Monthly = append(report.Monthly, MonthlyJSONStats{
				Month:   getMonthName(mo.Month),
				Year:    mo.Year,
				Added:   mo.Added,
				Deleted: mo.Deleted,
				Net:     mo.Net,
				Commits: mo.Commits,
			})
		}
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	if filename != "" {
		err = os.WriteFile(filename, data, 0644)
		if err != nil {
			return fmt.Errorf("failed to write file: %w", err)
		}
		fmt.Printf("✓ Report saved to %s\n", filename)
	} else {
		fmt.Println(string(data))
	}

	return nil
}

// TeamCompareJSON writes a before/after comparison for a whole team.
func TeamCompareJSON(c git.TeamCompareStats, filename string) error {
	beforeDays := git.WorkingDays(c.Before.Since, c.Before.Until)
	afterDays := git.WorkingDays(c.After.Since, c.After.Until)

	beforePerDay := float64(c.Before.TotalNet) / float64(beforeDays)
	afterPerDay := float64(c.After.TotalNet) / float64(afterDays)
	multiplier := benchmark.CalculateMultiplier(beforePerDay, afterPerDay)

	report := TeamCompareJSONReport{
		Before: PeriodStats{
			Label:       c.BeforeLabel,
			Net:         c.Before.TotalNet,
			WorkingDays: beforeDays,
			PerDay:      beforePerDay,
		},
		After: PeriodStats{
			Label:       c.AfterLabel,
			Net:         c.After.TotalNet,
			WorkingDays: afterDays,
			PerDay:      afterPerDay,
		},
		Multiplier: multiplier,
		Change:     fmt.Sprintf("%.1fx productivity change", multiplier),
		Members:    []MemberCompareJSONReport{},
	}

	for _, email := range c.MemberEmails() {
		before := c.Before.Members[email]
		after := c.After.Members[email]
		bPerDay := float64(before.Net) / float64(beforeDays)
		aPerDay := float64(after.Net) / float64(afterDays)

		row := MemberCompareJSONReport{
			Email:  email,
			Before: PeriodStats{Label: c.BeforeLabel, Net: before.Net, WorkingDays: beforeDays, PerDay: bPerDay},
			After:  PeriodStats{Label: c.AfterLabel, Net: after.Net, WorkingDays: afterDays, PerDay: aPerDay},
		}
		if before.Net > 0 {
			m := benchmark.CalculateMultiplier(bPerDay, aPerDay)
			row.Multiplier = &m
		}
		report.Members = append(report.Members, row)
	}

	return writeJSON(report, filename)
}

// writeJSON marshals v and either writes it to filename or prints it.
func writeJSON(v any, filename string) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	if filename != "" {
		if err := os.WriteFile(filename, data, 0644); err != nil {
			return fmt.Errorf("failed to write file: %w", err)
		}
		fmt.Printf("✓ Report saved to %s\n", filename)
		return nil
	}
	fmt.Println(string(data))
	return nil
}

func CompareJSON(comparison git.CompareStats, filename string) error {
	beforeDays := git.WorkingDays(comparison.Before.Since, comparison.Before.Until)
	afterDays := git.WorkingDays(comparison.After.Since, comparison.After.Until)

	beforePerDay := float64(comparison.Before.Net) / float64(beforeDays)
	afterPerDay := float64(comparison.After.Net) / float64(afterDays)

	multiplier := benchmark.CalculateMultiplier(beforePerDay, afterPerDay)

	report := CompareJSONReport{
		Before: PeriodStats{
			Label:       comparison.BeforeLabel,
			Net:         comparison.Before.Net,
			WorkingDays: beforeDays,
			PerDay:      beforePerDay,
		},
		After: PeriodStats{
			Label:       comparison.AfterLabel,
			Net:         comparison.After.Net,
			WorkingDays: afterDays,
			PerDay:      afterPerDay,
		},
		Multiplier: multiplier,
		Change:     fmt.Sprintf("%.1fx productivity change", multiplier),
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	if filename != "" {
		err = os.WriteFile(filename, data, 0644)
		if err != nil {
			return fmt.Errorf("failed to write file: %w", err)
		}
		fmt.Printf("✓ Report saved to %s\n", filename)
	} else {
		fmt.Println(string(data))
	}

	return nil
}
