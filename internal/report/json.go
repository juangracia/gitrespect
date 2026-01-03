package report

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/juangracia/gitrespect/internal/benchmark"
	"github.com/juangracia/gitrespect/internal/git"
)

type JSONReport struct {
	Author       string               `json:"author"`
	Period       PeriodInfo           `json:"period"`
	Summary      SummaryStats         `json:"summary"`
	Daily        DailyStats           `json:"daily"`
	Benchmarks   []BenchmarkResult    `json:"benchmarks"`
	Monthly      []MonthlyJSONStats   `json:"monthly,omitempty"`
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

func JSON(stats git.RepoStats, filename string, breakdown string) error {
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

	// Add benchmarks
	comparisons := benchmark.Compare(locPerDay)
	for _, c := range comparisons {
		report.Benchmarks = append(report.Benchmarks, BenchmarkResult{
			Label:      c.Label,
			Benchmark:  c.Benchmark,
			Multiplier: c.Multiplier,
		})
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

type TeamJSONReport struct {
	Period  PeriodInfo           `json:"period"`
	Totals  TeamTotals           `json:"totals"`
	Members []MemberStats        `json:"members"`
}

type TeamTotals struct {
	Added   int     `json:"added"`
	Deleted int     `json:"deleted"`
	Net     int     `json:"net"`
	Commits int     `json:"commits"`
	PerDay  float64 `json:"per_day"`
}

type MemberStats struct {
	Email   string  `json:"email"`
	Added   int     `json:"added"`
	Deleted int     `json:"deleted"`
	Net     int     `json:"net"`
	Commits int     `json:"commits"`
	PerDay  float64 `json:"per_day"`
}

func TeamJSON(stats git.TeamStats, filename string) error {
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
		report.Members = append(report.Members, MemberStats{
			Email:   m.email,
			Added:   m.stats.Added,
			Deleted: m.stats.Deleted,
			Net:     m.stats.Net,
			Commits: m.stats.Commits,
			PerDay:  float64(m.stats.Net) / float64(workingDays),
		})
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
