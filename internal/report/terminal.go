package report

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/juangracia/gitrespect/internal/benchmark"
	"github.com/juangracia/gitrespect/internal/git"
)

const (
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
	colorCyan   = "\033[36m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
)

func Terminal(stats git.RepoStats, breakdown string) error {
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

	// Daily average
	fmt.Printf("  %sDaily avg:%s %.0f lines/day (%d working days)\n",
		colorDim, colorReset, locPerDay, workingDays)
	fmt.Println()

	// Industry comparison
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
	fmt.Println()

	// Monthly breakdown if requested
	if breakdown == "monthly" && len(stats.Monthly) > 0 {
		printMonthlyBreakdown(stats)
	}

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

	fmt.Printf("  %sPeriod%s           %sNet Lines%s   %sDays%s    %sPer Day%s\n",
		colorDim, colorReset, colorDim, colorReset, colorDim, colorReset, colorDim, colorReset)
	fmt.Println("  " + strings.Repeat("─", 44))

	fmt.Printf("  %-16s %s%-11s%s %-6d  %s%.0f%s\n",
		comparison.BeforeLabel,
		colorDim, formatNumber(comparison.Before.Net), colorReset,
		beforeDays, colorDim, beforePerDay, colorReset)

	fmt.Printf("  %-16s %s%-11s%s %-6d  %s%.0f%s\n",
		comparison.AfterLabel,
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

func printMonthlyBreakdown(stats git.RepoStats) {
	fmt.Printf("  %sMonthly Breakdown:%s\n", colorDim, colorReset)
	fmt.Println("  " + strings.Repeat("─", 44))
	fmt.Printf("  %sMonth%s       %sAdded%s     %sDeleted%s   %sNet%s\n",
		colorDim, colorReset, colorDim, colorReset, colorDim, colorReset, colorDim, colorReset)
	fmt.Println("  " + strings.Repeat("─", 44))

	// Sort months
	var months []string
	for m := range stats.Monthly {
		months = append(months, m)
	}
	sort.Strings(months)

	for _, m := range months {
		ms := stats.Monthly[m]
		monthName := getMonthName(ms.Month)
		netColor := colorCyan
		if ms.Net < 0 {
			netColor = colorYellow
		}
		fmt.Printf("  %-11s %-9s %-9s %s%-9s%s\n",
			monthName+" "+fmt.Sprintf("%d", ms.Year),
			formatNumber(ms.Added),
			formatNumber(ms.Deleted),
			netColor, formatNumber(ms.Net), colorReset)
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
