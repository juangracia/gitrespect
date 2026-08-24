package report

import (
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/juangracia/gitrespect/internal/git"
	"github.com/juangracia/gitrespect/internal/metrics"
)

func TestBuildBreakdownHTMLMarksTheStrongestPeriod(t *testing.T) {
	rows := buildBreakdownHTML(sampleStats(), "monthly")
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}

	wantLabels := []string{"Jan 2025", "Feb 2025", "Mar 2025"}
	maxes := 0
	for i, r := range rows {
		if r.Label != wantLabels[i] {
			t.Errorf("row %d label = %q, want %q (oldest first)", i, r.Label, wantLabels[i])
		}
		if r.IsMax {
			maxes++
			// February nets 300, the highest of the three.
			if r.Label != "Feb 2025" {
				t.Errorf("marked %q as the strongest period, want Feb 2025", r.Label)
			}
		}
	}
	if maxes != 1 {
		t.Errorf("marked %d rows as the maximum, want exactly one", maxes)
	}
	if rows[0].Added != 350 || rows[0].Deleted != 100 || rows[0].Net != 250 {
		t.Errorf("January row = %+v", rows[0])
	}
}

// TestBuildBreakdownHTMLAllNegative pins what happens when nobody had a
// positive month: no row is dressed up as a high point.
func TestBuildBreakdownHTMLAllNegative(t *testing.T) {
	stats := git.RepoStats{Daily: map[string]git.DayStats{
		"2025-01-15": day("2025-01-15", 10, 100, 1),
		"2025-02-15": day("2025-02-15", 5, 50, 1),
	}}
	rows := buildBreakdownHTML(stats, "monthly")
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	for _, r := range rows {
		if r.IsMax {
			t.Errorf("row %q was marked the maximum even though every month was negative", r.Label)
		}
	}
}

func TestBuildBreakdownHTMLEmpty(t *testing.T) {
	if rows := buildBreakdownHTML(git.RepoStats{}, "monthly"); rows != nil {
		t.Errorf("empty stats produced %+v, want nil", rows)
	}
	if rows := buildBreakdownHTML(sampleStats(), "quarterly"); rows != nil {
		t.Errorf("unsupported granularity produced %+v, want nil", rows)
	}
}

func TestBreakdownHTMLTitle(t *testing.T) {
	tests := []struct{ in, want string }{
		{"monthly", "Monthly Breakdown"},
		{"weekly", "Weekly Breakdown"},
		{"daily", "Daily Breakdown"},
		{"", "Monthly Breakdown"},
	}
	for _, tt := range tests {
		if got := breakdownHTMLTitle(tt.in); got != tt.want {
			t.Errorf("breakdownHTMLTitle(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestChangeEmoji(t *testing.T) {
	tests := []struct {
		name string
		in   float64
		want string
	}{
		{"a drop is not called out", 0.5, ""},
		{"flat is not called out", 1, ""},
		{"just under doubling", 1.99, ""},
		{"doubling earns the chart", 2, "📈"},
		{"just under 5x", 4.99, "📈"},
		{"5x earns the rocket", 5, "🚀"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := changeEmoji(tt.in); got != tt.want {
				t.Errorf("changeEmoji(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestHTMLEntryPointsSurviveEmptyData is the cheap high-value guard: every
// template renders zero-value data without panicking, and nothing reaches the
// file with template directives left unexecuted.
func TestHTMLEntryPointsSurviveEmptyData(t *testing.T) {
	dir := t.TempDir()

	entries := []struct {
		name  string
		write func(path string) error
	}{
		{"HTML", func(p string) error {
			return HTML(git.RepoStats{}, p, "", "dark", metrics.Bundle{})
		}},
		{"HTML light theme", func(p string) error {
			return HTML(git.RepoStats{}, p, "monthly", "light", metrics.Bundle{})
		}},
		{"HTMLWithOptions chart requested but no data", func(p string) error {
			return HTMLWithOptions(git.RepoStats{}, p, "", "dark", metrics.Bundle{}, ChartOptions{Enabled: true})
		}},
		{"TeamHTML", func(p string) error {
			return TeamHTML(git.TeamStats{}, p, "dark", "", nil)
		}},
		{"TeamHTMLWithOptions chart requested but no data", func(p string) error {
			return TeamHTMLWithOptions(git.TeamStats{}, p, "light", "monthly", nil, ChartOptions{Enabled: true, Highlight: "nobody@example.com"})
		}},
		{"CompareHTML", func(p string) error {
			return CompareHTML(git.CompareStats{}, p, "dark")
		}},
		{"TeamCompareHTML", func(p string) error {
			return TeamCompareHTML(git.TeamCompareStats{}, p, "light")
		}},
	}

	for i, e := range entries {
		t.Run(e.name, func(t *testing.T) {
			path := filepath.Join(dir, strings.ReplaceAll(e.name, " ", "-")+string(rune('a'+i))+".html")
			if err := e.write(path); err != nil {
				t.Fatalf("%s: %v", e.name, err)
			}
			assertRenderedHTML(t, path)
		})
	}
}

func TestHTMLEntryPointsWithRealData(t *testing.T) {
	dir := t.TempDir()

	entries := []struct {
		name  string
		write func(path string) error
	}{
		{"HTML", func(p string) error {
			return HTML(sampleStats(), p, "monthly", "dark", fullBundle())
		}},
		{"TeamHTML", func(p string) error {
			return TeamHTML(teamStatsFixture(), p, "light", "weekly", map[string]metrics.Bundle{
				"ana@example.com": fullBundle(),
			})
		}},
		{"CompareHTML", func(p string) error {
			return CompareHTML(git.CompareStats{
				BeforeLabel: "2025-01:2025-03",
				AfterLabel:  "2025-04:2025-06",
				Before:      git.RepoStats{Net: 500, Since: mustTime("2025-01-01"), Until: mustTime("2025-03-31")},
				After:       git.RepoStats{Net: 1500, Since: mustTime("2025-04-01"), Until: mustTime("2025-06-30")},
			}, p, "dark")
		}},
		{"TeamCompareHTML", func(p string) error {
			return TeamCompareHTML(teamCompareFixture(), p, "dark")
		}},
	}

	for _, e := range entries {
		t.Run(e.name, func(t *testing.T) {
			path := filepath.Join(dir, e.name+".html")
			if err := e.write(path); err != nil {
				t.Fatalf("%s: %v", e.name, err)
			}
			assertRenderedHTML(t, path)
		})
	}
}

// TestTeamCompareHTMLShowsNoBaseline mirrors the JSON null-multiplier rule:
// a member with nothing to compare against must not be shown a ratio.
func TestTeamCompareHTMLShowsNoBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "team-compare.html")
	if err := TeamCompareHTML(teamCompareFixture(), path, "dark"); err != nil {
		t.Fatalf("TeamCompareHTML: %v", err)
	}
	body := assertRenderedHTML(t, path)

	for _, email := range []string{"veteran@example.com", "newjoiner@example.com", "leaver@example.com"} {
		if !strings.Contains(body, email) {
			t.Errorf("member %q is missing from the report", email)
		}
	}
	if !strings.Contains(body, "n/a") {
		t.Errorf("no member was shown as having no baseline:\n%s", body)
	}
}

func TestHTMLHasNoChartByDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.html")
	if err := HTML(sampleStats(), path, "monthly", "dark", metrics.Bundle{}); err != nil {
		t.Fatalf("HTML: %v", err)
	}
	body := assertRenderedHTML(t, path)

	if strings.Contains(body, "gr-chart") {
		t.Error("the default report shipped chart markup, so it is no longer lightweight")
	}
	// The breakdown table is still what carries the numbers.
	if !strings.Contains(body, "Monthly Breakdown") {
		t.Error("the breakdown table is missing")
	}
}

func TestTeamHTMLHasNoChartByDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "team.html")
	if err := TeamHTML(teamStatsFixture(), path, "dark", "monthly", nil); err != nil {
		t.Fatalf("TeamHTML: %v", err)
	}
	if strings.Contains(assertRenderedHTML(t, path), "gr-chart") {
		t.Error("the default team report shipped chart markup")
	}
}

func TestHTMLWithOptionsEmbedsTheChart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.html")
	err := HTMLWithOptions(sampleStats(), path, "monthly", "dark", metrics.Bundle{}, ChartOptions{Enabled: true})
	if err != nil {
		t.Fatalf("HTMLWithOptions: %v", err)
	}
	body := assertRenderedHTML(t, path)

	if !strings.Contains(body, "<svg viewBox=") {
		t.Fatalf("no SVG in the report:\n%s", body)
	}
	assertWellFormedSVGString(t, body)
	if !strings.Contains(body, "dev@example.com") {
		t.Error("the chart does not name the author whose lines it plots")
	}
	// Dark theme, so the dark palette step.
	if !strings.Contains(body, `class="gr-chart is-dark"`) {
		t.Error("the chart did not follow the report's dark theme")
	}
	// One series, so one line and no legend box.
	if got := strings.Count(body, `class="gr-chart-line"`); got != 1 {
		t.Errorf("drew %d lines, want 1", got)
	}
	// The breakdown table stays: it is the text relief for the light-mode
	// hues that sit under 3:1, and the accessible view of the same numbers.
	if !strings.Contains(body, "Monthly Breakdown") {
		t.Error("the chart replaced the breakdown table instead of joining it")
	}
}

func TestTeamHTMLWithOptionsEmbedsTheChart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "team.html")
	err := TeamHTMLWithOptions(teamStatsFixture(), path, "light", "monthly", nil, ChartOptions{Enabled: true})
	if err != nil {
		t.Fatalf("TeamHTMLWithOptions: %v", err)
	}
	body := assertRenderedHTML(t, path)

	assertWellFormedSVGString(t, body)
	if got := strings.Count(body, `class="gr-chart-line"`); got != 3 {
		t.Errorf("drew %d lines, want one per member", got)
	}
	if !strings.Contains(body, `<div class="gr-chart-legend">`) {
		t.Error("the team chart has no legend")
	}
	if strings.Contains(body, "is-dark") && strings.Contains(body, `class="gr-chart is-dark"`) {
		t.Error("the light report rendered a dark chart")
	}
}

func TestTeamHTMLWithOptionsHighlightTitlesTheComparison(t *testing.T) {
	dir := t.TempDir()

	matched := filepath.Join(dir, "highlight.html")
	err := TeamHTMLWithOptions(teamStatsFixture(), matched, "dark", "", nil,
		ChartOptions{Enabled: true, Highlight: "ana@example.com"})
	if err != nil {
		t.Fatalf("TeamHTMLWithOptions: %v", err)
	}
	body := assertRenderedHTML(t, matched)
	if !strings.Contains(body, "ana@example.com vs Team average") {
		t.Errorf("the chart title does not name the comparison:\n%s", body)
	}
	if got := strings.Count(body, `class="gr-chart-line is-dashed"`); got != 1 {
		t.Errorf("found %d dashed lines, want the team average", got)
	}

	// A highlight naming nobody falls back to every member, and the title
	// must not claim a comparison the chart does not show.
	missed := filepath.Join(dir, "no-highlight.html")
	err = TeamHTMLWithOptions(teamStatsFixture(), missed, "dark", "", nil,
		ChartOptions{Enabled: true, Highlight: "ghost@example.com"})
	if err != nil {
		t.Fatalf("TeamHTMLWithOptions: %v", err)
	}
	body = assertRenderedHTML(t, missed)
	if strings.Contains(body, " vs ") {
		t.Errorf("the title claims a comparison that was not drawn:\n%s", body)
	}
	if got := strings.Count(body, `class="gr-chart-line"`); got != 3 {
		t.Errorf("drew %d plain lines, want one per member", got)
	}
}

// TestHTMLDefaultsFilename covers the empty-filename branch, which writes into
// the working directory.
func TestHTMLDefaultsFilename(t *testing.T) {
	dir := t.TempDir()
	restore := chdir(t, dir)
	defer restore()

	if err := HTML(sampleStats(), "", "", "dark", metrics.Bundle{}); err != nil {
		t.Fatalf("HTML: %v", err)
	}
	assertRenderedHTML(t, filepath.Join(dir, "gitrespect-report.html"))

	if err := TeamHTML(teamStatsFixture(), "", "dark", "", nil); err != nil {
		t.Fatalf("TeamHTML: %v", err)
	}
	assertRenderedHTML(t, filepath.Join(dir, "gitrespect-team.html"))

	if err := CompareHTML(git.CompareStats{}, "", "dark"); err != nil {
		t.Fatalf("CompareHTML: %v", err)
	}
	assertRenderedHTML(t, filepath.Join(dir, "gitrespect-compare.html"))

	if err := TeamCompareHTML(git.TeamCompareStats{}, "", "dark"); err != nil {
		t.Fatalf("TeamCompareHTML: %v", err)
	}
	assertRenderedHTML(t, filepath.Join(dir, "gitrespect-team-compare.html"))
}

// assertRenderedHTML reads a generated report and checks it is a finished
// document: no unexecuted template directives, and nothing loaded off the
// network, since the report has to work as a single file.
func assertRenderedHTML(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	body := string(raw)

	if len(body) == 0 {
		t.Fatalf("%s is empty", path)
	}
	for _, directive := range []string{"{{", "}}"} {
		if strings.Contains(body, directive) {
			t.Errorf("%s contains an unrendered template directive %q", path, directive)
		}
	}
	if !strings.Contains(body, "<!DOCTYPE html>") || !strings.Contains(body, "</html>") {
		t.Errorf("%s is not a complete document", path)
	}
	// The report has to work as a single file, so nothing may be fetched at
	// view time. The one external URL is the project link in the footer,
	// which is a link rather than a load.
	for _, remote := range []string{"<script src=", "<link rel=\"stylesheet\"", "src=\"//", "@import url(", "url(http"} {
		if strings.Contains(body, remote) {
			t.Errorf("%s pulls a remote resource: found %q", path, remote)
		}
	}
	return body
}

func assertWellFormedSVGString(t *testing.T, body string) {
	t.Helper()
	assertWellFormedSVG(t, template.HTML(body))
}

func fullBundle() metrics.Bundle {
	return metrics.Bundle{
		Baseline: &metrics.Baseline{
			WindowStart:     mustTime("2024-10-01"),
			WindowEnd:       mustTime("2024-12-31"),
			LOCPerDay:       12,
			PeriodLOCPerDay: 18,
			PercentDelta:    50,
		},
		CommitSize: &metrics.CommitSizeDistribution{Total: 10},
		Cadence:    &metrics.Cadence{MainBranch: "main", MedianDaysBetween: 1.5, Samples: 9},
		LeadTime:   &metrics.LeadTime{MainBranch: "main", MedianDays: 2.5, Samples: 4},
		Churn:      &metrics.Churn{AddedLines: 500, Ratio: 0.2, WindowDays: 30},
	}
}

func mustTime(date string) time.Time {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		panic(err)
	}
	return t
}

func chdir(t *testing.T, dir string) func() {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := os.Chdir(prev); err != nil {
			t.Fatal(err)
		}
	}
}
