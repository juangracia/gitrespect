package report

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/juangracia/gitrespect/internal/benchmark"
	"github.com/juangracia/gitrespect/internal/git"
	"github.com/juangracia/gitrespect/internal/metrics"
)

// ANSI styles, blanked when the output is not an interactive terminal so
// piping or redirecting produces clean text instead of escape codes.
var (
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
	colorCyan   = "\033[36m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
)

func init() {
	if !colorEnabled() {
		colorReset, colorBold, colorDim = "", "", ""
		colorCyan, colorGreen, colorYellow = "", "", ""
	}
}

// colorEnabled reports whether to emit ANSI styling. It honours the NO_COLOR
// convention (https://no-color.org) and otherwise requires a character device
// on stdout.
func colorEnabled() bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func Terminal(stats git.RepoStats, breakdown string, bundle metrics.Bundle) error {
	// Use full date range for daily average (not just active commit span)
	workingDays := git.WorkingDays(stats.Since, stats.Until)
	locPerDay := float64(stats.Net) / float64(workingDays)

	// Header
	repoName := filepath.Base(stats.Path)
	if strings.Contains(stats.Path, "repositories") {
		repoName = stats.Path
	}

	dateRange := fmt.Sprintf("%s to %s", stats.Since.Format("Jan 2 2006"), stats.Until.Format("Jan 2 2006"))

	fmt.Println()
	fmt.Printf("%s%s gitrespect%s - %s\n", colorBold, colorCyan, colorReset, stats.Author)
	fmt.Printf("%s%s (%s)%s\n", colorDim, repoName, dateRange, colorReset)
	fmt.Println(strings.Repeat("─", 50))
	fmt.Println()

	// Main stats
	fmt.Printf("  %sAdded%s       %sDeleted%s     %sNet%s         %sCommits%s\n",
		colorDim, colorReset, colorDim, colorReset, colorDim, colorReset, colorDim, colorReset)
	fmt.Println("  " + strings.Repeat("─", 44))
	fmt.Printf("  %s%-11s%s %-11s %s%-11s%s %-8d\n",
		colorGreen, formatNumber(stats.Added), colorReset,
		formatNumber(stats.Deleted),
		colorCyan, formatNumber(stats.Net), colorReset,
		stats.Commits)
	fmt.Println()

	// Daily average - show actual activity period if different from date range
	if !stats.FirstCommit.IsZero() && !stats.LastCommit.IsZero() {
		activityRange := fmt.Sprintf("%s to %s", stats.FirstCommit.Format("Jan 2 2006"), stats.LastCommit.Format("Jan 2 2006"))
		fmt.Printf("  %sDaily avg:%s %s lines/day (%d working days)\n",
			colorDim, colorReset, formatRate(locPerDay), workingDays)
		fmt.Printf("  %sActivity:%s  %s\n", colorDim, colorReset, activityRange)
	} else {
		fmt.Printf("  %sDaily avg:%s %s lines/day (%d working days)\n",
			colorDim, colorReset, formatRate(locPerDay), workingDays)
	}
	fmt.Println()

	// Baseline comparison (default) or legacy Senior/Avg/Junior (opt-in)
	if bundle.LegacyBenchmark {
		if workingDays >= 21 {
			comparisons := benchmark.Compare(locPerDay)
			fmt.Printf("  %svs Industry:%s\n", colorDim, colorReset)
			for i, c := range comparisons {
				prefix := "├──"
				if i == len(comparisons)-1 {
					prefix = "└──"
				}
				bar := renderBar(c.Multiplier, 20)
				fmt.Printf("  %s %s (%d/day): %s%.1fx%s %s\n",
					prefix, c.Label, c.Benchmark, colorYellow, c.Multiplier, colorReset, bar)
			}
		} else {
			fmt.Printf("  %sPace:%s %.0f lines/day\n", colorDim, colorReset, locPerDay)
			fmt.Printf("  %s(Industry comparison requires 30+ days of activity)%s\n", colorDim, colorReset)
		}
	} else if bundle.Baseline != nil {
		b := bundle.Baseline
		windowDays := int(b.WindowEnd.Sub(b.WindowStart).Hours() / 24)
		fmt.Printf("  %sBaseline (%dd prior):%s\n", colorDim, windowDays, colorReset)
		if b.InsufficientHistory {
			fmt.Printf("  └── %sinsufficient prior history%s\n", colorDim, colorReset)
		} else {
			sign := "+"
			arrow := "↑"
			color := colorGreen
			if b.PercentDelta < 0 {
				sign = ""
				arrow = "↓"
				color = colorYellow
			}
			fmt.Printf("  └── Your normal: %.0f lines/day → this period: %.0f (%s%s%.0f%%%s %s)\n",
				b.LOCPerDay, b.PeriodLOCPerDay, color, sign, b.PercentDelta, colorReset, arrow)
		}
	}
	fmt.Println()

	// Opt-in metrics
	renderMetrics(bundle)

	// Monthly breakdown if requested
	if breakdown != "" {
		printBreakdown(stats, breakdown)
	}

	return nil
}

func renderMetrics(b metrics.Bundle) {
	if b.CommitSize != nil && b.CommitSize.Total > 0 {
		d := b.CommitSize
		fmt.Printf("  %sCommit size distribution:%s\n", colorDim, colorReset)
		rows := []struct {
			label  string
			bucket metrics.SizeBucket
		}{
			{"Micro (<10)", metrics.BucketMicro},
			{"Small (10-99)", metrics.BucketSmall},
			{"Medium (100-499)", metrics.BucketMedium},
			{"Large (500+)", metrics.BucketLarge},
		}
		for i, row := range rows {
			prefix := "├──"
			if i == len(rows)-1 {
				prefix = "└──"
			}
			pct := d.Percent(row.bucket)
			bar := renderBar(pct/10, 20)
			fmt.Printf("  %s %-18s %3.0f%%  %s\n", prefix, row.label+":", pct, bar)
		}
		fmt.Println()
	}
	if b.Cadence != nil {
		c := b.Cadence
		fmt.Printf("  %sIntegration cadence:%s\n", colorDim, colorReset)
		switch {
		case c.MainBranch == "":
			fmt.Printf("  └── %sno main branch detected%s\n", colorDim, colorReset)
		case c.Samples < 1:
			fmt.Printf("  └── %sinsufficient data (need 2+ commits on %s)%s\n", colorDim, c.MainBranch, colorReset)
		case c.MedianDaysBetween < 0.1:
			fmt.Printf("  └── Multiple commits most active days on %s (%d gaps)\n", c.MainBranch, c.Samples)
		default:
			fmt.Printf("  └── Median %.1f days between commits to %s (%d gaps)\n", c.MedianDaysBetween, c.MainBranch, c.Samples)
		}
		fmt.Println()
	}
	if b.LeadTime != nil {
		lt := b.LeadTime
		fmt.Printf("  %sLead time (branch → main):%s\n", colorDim, colorReset)
		switch {
		case lt.MainBranch == "":
			fmt.Printf("  └── %sno main branch detected%s\n", colorDim, colorReset)
		case lt.Samples == 0:
			fmt.Printf("  └── %sno signal: no merge commits, and too few commits landed later than they were authored%s\n", colorDim, colorReset)
		case lt.Method == metrics.LeadTimeAuthored:
			fmt.Printf("  └── Median %.1f days (%d %s, authored → landed)\n",
				lt.MedianDays, lt.Samples, pluralize(lt.Samples, "commit"))
		default:
			fmt.Printf("  └── Median %.1f days (%d %s analyzed)\n",
				lt.MedianDays, lt.Samples, pluralize(lt.Samples, "merge"))
		}
		fmt.Println()
	}
	if b.Churn != nil {
		c := b.Churn
		fmt.Printf("  %sChurn rate:%s\n", colorDim, colorReset)
		if c.AddedLines == 0 {
			fmt.Printf("  └── %sno lines added in the %dd window before this period%s\n", colorDim, c.WindowDays, colorReset)
		} else {
			fmt.Printf("  └── %.0f%% of added lines rewritten within %d days\n", c.Ratio*100, c.WindowDays)
		}
		fmt.Println()
	}
}

func TerminalWithRepos(combined git.RepoStats, repos []git.RepoStats, breakdown string, bundle metrics.Bundle) error {
	// Print combined stats first
	if err := Terminal(combined, breakdown, bundle); err != nil {
		return err
	}

	// Print per-repo breakdown
	fmt.Printf("  %sRepository Breakdown:%s\n", colorBold, colorReset)
	fmt.Println("  " + strings.Repeat("─", 56))
	fmt.Printf("  %sRepository%s                          %sNet%s       %sCommits%s\n",
		colorDim, colorReset, colorDim, colorReset, colorDim, colorReset)
	fmt.Println("  " + strings.Repeat("─", 56))

	// Sort repos by net lines descending
	sorted := make([]git.RepoStats, len(repos))
	copy(sorted, repos)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Net > sorted[j].Net
	})

	for _, repo := range sorted {
		if repo.Commits == 0 {
			continue // Skip repos with no commits
		}
		repoName := filepath.Base(repo.Path)
		if len(repoName) > 34 {
			repoName = repoName[:31] + "..."
		}
		netColor := colorCyan
		if repo.Net < 0 {
			netColor = colorYellow
		}
		fmt.Printf("  %-36s %s%-10s%s %-8d\n",
			repoName,
			netColor, formatNumber(repo.Net), colorReset,
			repo.Commits)
	}
	fmt.Println()

	return nil
}

func CompareTerminal(comparison git.CompareStats) error {
	beforeDays := git.WorkingDays(comparison.Before.Since, comparison.Before.Until)
	afterDays := git.WorkingDays(comparison.After.Since, comparison.After.Until)

	beforePerDay := float64(comparison.Before.Net) / float64(beforeDays)
	afterPerDay := float64(comparison.After.Net) / float64(afterDays)

	multiplier := benchmark.CalculateMultiplier(beforePerDay, afterPerDay)

	fmt.Println()
	fmt.Printf("%s%s gitrespect%s - Period Comparison\n", colorBold, colorCyan, colorReset)
	fmt.Println(strings.Repeat("─", 50))
	fmt.Println()

	periodWidth := periodLabelWidth(comparison.BeforeLabel, comparison.AfterLabel)
	fmt.Printf("  %s%-*s%s %sNet Lines%s   %sDays%s    %sPer Day%s\n",
		colorDim, periodWidth, "Period", colorReset,
		colorDim, colorReset, colorDim, colorReset, colorDim, colorReset)
	fmt.Println("  " + strings.Repeat("─", periodWidth+28))

	fmt.Printf("  %-*s %s%-11s%s %-6d  %s%.0f%s\n",
		periodWidth, comparison.BeforeLabel,
		colorDim, formatNumber(comparison.Before.Net), colorReset,
		beforeDays, colorDim, beforePerDay, colorReset)

	fmt.Printf("  %-*s %s%-11s%s %-6d  %s%.0f%s\n",
		periodWidth, comparison.AfterLabel,
		colorCyan, formatNumber(comparison.After.Net), colorReset,
		afterDays, colorCyan, afterPerDay, colorReset)

	fmt.Println()

	changeSign := "+"
	changeColor := colorGreen
	if multiplier < 1 {
		changeSign = ""
		changeColor = colorYellow
	}

	fmt.Printf("  %sChange:%s %s%s%.1fx productivity %s%s\n",
		colorDim, colorReset, changeColor, changeSign, multiplier,
		getChangeEmoji(multiplier), colorReset)
	fmt.Println()

	return nil
}

// formatRate renders a per-day rate without rounding a small but real number
// down to a flat "0", which reads as "shipped nothing".
func formatRate(v float64) string {
	abs := v
	if abs < 0 {
		abs = -abs
	}
	switch {
	case abs == 0:
		return "0"
	case abs < 0.1:
		return fmt.Sprintf("%.2f", v)
	case abs < 10:
		return fmt.Sprintf("%.1f", v)
	default:
		return fmt.Sprintf("%.0f", v)
	}
}

// pluralize returns word or its plural depending on n.
func pluralize(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// periodLabelWidth sizes the period column so long labels
// ("2025-01:2025-06") do not push the following columns out of alignment.
func periodLabelWidth(labels ...string) int {
	width := len("Period")
	for _, l := range labels {
		if len(l) > width {
			width = len(l)
		}
	}
	return width
}

// TeamCompareTerminal renders a before/after comparison for a whole team,
// with the team total first and then each member's own change.
func TeamCompareTerminal(c git.TeamCompareStats) error {
	beforeDays := git.WorkingDays(c.Before.Since, c.Before.Until)
	afterDays := git.WorkingDays(c.After.Since, c.After.Until)

	beforePerDay := float64(c.Before.TotalNet) / float64(beforeDays)
	afterPerDay := float64(c.After.TotalNet) / float64(afterDays)
	multiplier := benchmark.CalculateMultiplier(beforePerDay, afterPerDay)

	fmt.Println()
	fmt.Printf("%s%s gitrespect%s - Team Period Comparison\n", colorBold, colorCyan, colorReset)
	fmt.Println(strings.Repeat("─", 66))
	fmt.Println()

	periodWidth := periodLabelWidth(c.BeforeLabel, c.AfterLabel)
	fmt.Printf("  %s%-*s%s %sNet Lines%s   %sDays%s    %sPer Day%s\n",
		colorDim, periodWidth, "Period", colorReset,
		colorDim, colorReset, colorDim, colorReset, colorDim, colorReset)
	fmt.Println("  " + strings.Repeat("─", periodWidth+28))
	fmt.Printf("  %-*s %s%-11s%s %-6d  %s%.0f%s\n",
		periodWidth, c.BeforeLabel, colorDim, formatNumber(c.Before.TotalNet), colorReset,
		beforeDays, colorDim, beforePerDay, colorReset)
	fmt.Printf("  %-*s %s%-11s%s %-6d  %s%.0f%s\n",
		periodWidth, c.AfterLabel, colorCyan, formatNumber(c.After.TotalNet), colorReset,
		afterDays, colorCyan, afterPerDay, colorReset)
	fmt.Println()

	changeSign := "+"
	changeColor := colorGreen
	if multiplier < 1 {
		changeSign = ""
		changeColor = colorYellow
	}
	fmt.Printf("  %sTeam change:%s %s%s%.1fx productivity %s%s\n",
		colorDim, colorReset, changeColor, changeSign, multiplier,
		getChangeEmoji(multiplier), colorReset)
	fmt.Println()

	emails := c.MemberEmails()
	if len(emails) == 0 {
		return nil
	}

	labelWidth := 20
	for _, e := range emails {
		if len(e) > labelWidth {
			labelWidth = len(e)
		}
	}

	fmt.Printf("  %sPer Member%s\n", colorBold, colorReset)
	fmt.Printf("  %s%-*s%s %sBefore%s     %sAfter%s      %sChange%s\n",
		colorDim, labelWidth, "Contributor", colorReset,
		colorDim, colorReset, colorDim, colorReset, colorDim, colorReset)
	fmt.Println("  " + strings.Repeat("─", labelWidth+34))

	for _, email := range emails {
		before := c.Before.Members[email]
		after := c.After.Members[email]
		bPerDay := float64(before.Net) / float64(beforeDays)
		aPerDay := float64(after.Net) / float64(afterDays)
		m := benchmark.CalculateMultiplier(bPerDay, aPerDay)

		mColor := colorGreen
		sign := "+"
		if m < 1 {
			mColor = colorYellow
			sign = ""
		}
		change := fmt.Sprintf("%s%.1fx", sign, m)
		// A member with no "before" output has no baseline to multiply.
		if before.Net <= 0 {
			mColor = colorDim
			change = "n/a"
		}

		fmt.Printf("  %-*s %-10s %-10s %s%s%s\n",
			labelWidth, email,
			formatNumber(before.Net),
			formatNumber(after.Net),
			mColor, change, colorReset)
	}
	fmt.Println()

	return nil
}

// breakdownTitle maps a granularity to its section heading.
func breakdownTitle(granularity string) string {
	switch granularity {
	case "weekly":
		return "Weekly Breakdown"
	case "daily":
		return "Daily Breakdown"
	default:
		return "Monthly Breakdown"
	}
}

func printBreakdown(stats git.RepoStats, granularity string) {
	rows := git.Breakdown(stats, granularity)
	if len(rows) == 0 {
		return
	}

	// Weekly and daily labels ("Week of Jan 13 2025") are wider than monthly.
	labelWidth := 11
	for _, r := range rows {
		if len(r.Label) > labelWidth {
			labelWidth = len(r.Label)
		}
	}
	width := labelWidth + 33

	fmt.Printf("  %s%s:%s\n", colorDim, breakdownTitle(granularity), colorReset)
	fmt.Println("  " + strings.Repeat("─", width))
	fmt.Printf("  %s%-*s%s %sAdded%s     %sDeleted%s   %sNet%s\n",
		colorDim, labelWidth, "Period", colorReset,
		colorDim, colorReset, colorDim, colorReset, colorDim, colorReset)
	fmt.Println("  " + strings.Repeat("─", width))

	for _, r := range rows {
		netColor := colorCyan
		if r.Net < 0 {
			netColor = colorYellow
		}
		fmt.Printf("  %-*s %-9s %-9s %s%-9s%s\n",
			labelWidth, r.Label,
			formatNumber(r.Added),
			formatNumber(r.Deleted),
			netColor, formatNumber(r.Net), colorReset)
	}
	fmt.Println()
}

func renderBar(value float64, width int) string {
	filled := int(value * float64(width) / 10) // Scale to max 10x
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	return colorCyan + strings.Repeat("█", filled) + colorDim + strings.Repeat("░", width-filled) + colorReset
}

func formatNumber(n int) string {
	if n < 0 {
		return fmt.Sprintf("-%s", formatNumberAbs(-n))
	}
	return formatNumberAbs(n)
}

func formatNumberAbs(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%d,%03d", n/1000, n%1000)
}

func getMonthName(m int) string {
	months := []string{"", "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	if m >= 1 && m <= 12 {
		return months[m]
	}
	return "???"
}

func getChangeEmoji(multiplier float64) string {
	if multiplier >= 5 {
		return " 🚀"
	}
	if multiplier >= 2 {
		return " 📈"
	}
	if multiplier >= 1 {
		return ""
	}
	return " 📉"
}

func TeamTerminal(stats git.TeamStats, breakdown string, bundles map[string]metrics.Bundle) error {
	// Use the requested period, not the active commit span, so team and
	// single-author daily averages are computed the same way.
	workingDays := git.WorkingDays(stats.Since, stats.Until)
	locPerDay := float64(stats.TotalNet) / float64(workingDays)

	dateRange := fmt.Sprintf("%s to %s", stats.Since.Format("Jan 2 2006"), stats.Until.Format("Jan 2 2006"))

	fmt.Println()
	fmt.Printf("%s%s gitrespect%s - Team Report\n", colorBold, colorCyan, colorReset)
	fmt.Printf("%s%s%s\n", colorDim, dateRange, colorReset)
	fmt.Println(strings.Repeat("─", 60))
	fmt.Println()

	// Team totals
	fmt.Printf("  %sTeam Totals%s\n", colorBold, colorReset)
	fmt.Printf("  %sAdded%s       %sDeleted%s     %sNet%s         %sCommits%s\n",
		colorDim, colorReset, colorDim, colorReset, colorDim, colorReset, colorDim, colorReset)
	fmt.Println("  " + strings.Repeat("─", 44))
	fmt.Printf("  %s%-11s%s %-11s %s%-11s%s %-8d\n",
		colorGreen, formatNumber(stats.TotalAdded), colorReset,
		formatNumber(stats.TotalDeleted),
		colorCyan, formatNumber(stats.TotalNet), colorReset,
		stats.TotalCommits)
	fmt.Println()

	fmt.Printf("  %sTeam daily avg:%s %s lines/day (%d working days)\n",
		colorDim, colorReset, formatRate(locPerDay), workingDays)
	fmt.Println()

	// Member breakdown - sort by net lines descending
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

	fmt.Printf("  %sTeam Members%s\n", colorBold, colorReset)
	fmt.Printf("  %sContributor%s                         %sNet%s       %sCommits%s  %s/day%s\n",
		colorDim, colorReset, colorDim, colorReset, colorDim, colorReset, colorDim, colorReset)
	fmt.Println("  " + strings.Repeat("─", 56))

	for _, m := range members {
		memberDaily := float64(m.stats.Net) / float64(workingDays)
		// Truncate email if too long
		email := m.email
		if len(email) > 32 {
			email = email[:29] + "..."
		}
		fmt.Printf("  %-34s %s%-10s%s %-8d %.0f\n",
			email,
			colorCyan, formatNumber(m.stats.Net), colorReset,
			m.stats.Commits,
			memberDaily)
	}
	fmt.Println()

	// Per-member opt-in metrics (only when --metrics was requested)
	for _, m := range members {
		b, ok := bundles[m.email]
		if !ok || !hasAnyMetric(b) {
			continue
		}
		fmt.Printf("  %s● %s%s\n", colorBold, m.email, colorReset)
		renderMetrics(b)
	}

	// Team-wide breakdown
	if breakdown != "" {
		printBreakdown(git.RepoStats{Monthly: stats.Monthly, Daily: stats.Daily}, breakdown)
	}

	return nil
}

// hasAnyMetric reports whether the bundle carries at least one opt-in metric.
func hasAnyMetric(b metrics.Bundle) bool {
	return b.CommitSize != nil || b.Cadence != nil || b.LeadTime != nil || b.Churn != nil
}
