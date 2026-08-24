package report

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/juangracia/gitrespect/internal/git"
	"github.com/juangracia/gitrespect/internal/metrics"
)

// RepoRollupJSON is one repository's contribution to a team total, with the
// per-contributor split that a reader needs to attribute it.
type RepoRollupJSON struct {
	Path         string                `json:"path"`
	Name         string                `json:"name"`
	Added        int                   `json:"added"`
	Deleted      int                   `json:"deleted"`
	Net          int                   `json:"net"`
	Commits      int                   `json:"commits"`
	PerDay       float64               `json:"per_day"`
	Contributors []RepoContributorJSON `json:"contributors"`
}

// RepoContributorJSON is one person's share of one repository.
type RepoContributorJSON struct {
	Author  string `json:"author"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
	Net     int    `json:"net"`
	Commits int    `json:"commits"`
}

// buildRepoRollupJSON converts the git-layer rollups into their serialisable
// form. Nil in, nil out, so a report without --per-repo omits the key entirely
// rather than emitting an empty array that looks like "no repositories".
func buildRepoRollupJSON(rollups []git.RepoRollup, workingDays int) []RepoRollupJSON {
	if len(rollups) == 0 {
		return nil
	}
	out := make([]RepoRollupJSON, 0, len(rollups))
	for _, r := range rollups {
		row := RepoRollupJSON{
			Path:    r.Path,
			Name:    filepath.Base(r.Path),
			Added:   r.Added,
			Deleted: r.Deleted,
			Net:     r.Net,
			Commits: r.Commits,
		}
		if workingDays > 0 {
			row.PerDay = float64(r.Net) / float64(workingDays)
		}
		for _, c := range r.Contributors {
			row.Contributors = append(row.Contributors, RepoContributorJSON{
				Author:  c.Author,
				Added:   c.Added,
				Deleted: c.Deleted,
				Net:     c.Net,
				Commits: c.Commits,
			})
		}
		out = append(out, row)
	}
	return out
}

// TeamTerminalWithRepos prints the team report followed by a by-repository
// table.
//
// The table aggregates every member's work per repository rather than printing
// one table per member. Across the couple of hundred repositories this was
// built for, per-member tables are unreadable, and the question a reader
// actually has is "which repositories did this team's output land in".
// Per-contributor attribution is still available, in the JSON output.
func TeamTerminalWithRepos(stats git.TeamStats, breakdown string, bundles map[string]metrics.Bundle, rollups []git.RepoRollup) error {
	if err := TeamTerminal(stats, breakdown, bundles); err != nil {
		return err
	}
	if len(rollups) == 0 {
		return nil
	}

	workingDays := git.WorkingDays(stats.Since, stats.Until)
	nameWidth := repoNameWidth(rollups)

	fmt.Printf("  %sBy Repository%s %s(%d)%s\n", colorBold, colorReset, colorDim, len(rollups), colorReset)
	fmt.Printf("  %s%-*s%s  %sNet%s        %sCommits%s  %sPeople%s\n",
		colorDim, nameWidth, "Repository", colorReset,
		colorDim, colorReset, colorDim, colorReset, colorDim, colorReset)
	fmt.Println("  " + strings.Repeat("─", nameWidth+30))

	for _, r := range rollups {
		fmt.Printf("  %-*s  %s%-10s%s %-8d %d\n",
			nameWidth, truncateMiddle(filepath.Base(r.Path), nameWidth),
			colorCyan, formatNumber(r.Net), colorReset,
			r.Commits, len(r.Contributors))
	}
	fmt.Println()

	if workingDays > 0 {
		fmt.Printf("  %s%d repositories, %s net lines total%s\n",
			colorDim, len(rollups), formatNumber(totalRollupNet(rollups)), colorReset)
		fmt.Println()
	}
	return nil
}

func totalRollupNet(rollups []git.RepoRollup) int {
	total := 0
	for _, r := range rollups {
		total += r.Net
	}
	return total
}

// repoNameWidth sizes the name column to the longest repository name actually
// present, within bounds, so short lists stay tight and long names are not all
// truncated to fit one outlier.
func repoNameWidth(rollups []git.RepoRollup) int {
	const min, max = 20, 40
	width := min
	for _, r := range rollups {
		if n := len(filepath.Base(r.Path)); n > width {
			width = n
		}
	}
	if width > max {
		width = max
	}
	return width
}

// truncateMiddle shortens a name from the middle, because repository names in
// a nested layout often share a prefix and differ only at the end.
func truncateMiddle(s string, width int) string {
	if len(s) <= width || width < 5 {
		return s
	}
	keep := width - 3
	head := (keep + 1) / 2
	tail := keep - head
	return s[:head] + "..." + s[len(s)-tail:]
}
