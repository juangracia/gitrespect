package report

import (
	"html/template"
	"os"
	"path/filepath"
	"regexp"
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

// sevenFigureStats is the shape this release exists to report on: a year of
// team output, where every headline number is seven digits.
func sevenFigureStats() git.RepoStats {
	return git.RepoStats{
		Author:  "dev@example.com",
		Since:   mustTime("2025-01-01"),
		Until:   mustTime("2025-12-31"),
		Added:   1481000,
		Deleted: 246433,
		Net:     1234567,
		Commits: 12045,
		Daily: map[string]git.DayStats{
			"2025-03-15": day("2025-03-15", 740500, 123216, 6000),
			"2025-09-15": day("2025-09-15", 740500, 123217, 6045),
		},
	}
}

// TestHTMLGroupsSevenFigureNumbers is the readability fix: the HTML report
// used to print a bare 1234567 in a 28px stat card.
func TestHTMLGroupsSevenFigureNumbers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.html")
	if err := HTML(sevenFigureStats(), path, "monthly", "dark", metrics.Bundle{}); err != nil {
		t.Fatalf("HTML: %v", err)
	}
	body := assertRenderedHTML(t, path)

	for _, want := range []string{
		`<div class="stat-value added">+1,481,000</div>`,
		`<div class="stat-value deleted">-246,433</div>`,
		`<div class="stat-value net">1,234,567</div>`,
		`<div class="stat-value">12,045</div>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing grouped stat card %q", want)
		}
	}
	assertNoUngroupedNumbers(t, path, body)
}

func TestTeamHTMLGroupsSevenFigureNumbers(t *testing.T) {
	stats := git.TeamStats{
		Since:        mustTime("2025-01-01"),
		Until:        mustTime("2025-12-31"),
		TotalAdded:   2481000,
		TotalDeleted: 1246433,
		TotalNet:     1234567,
		TotalCommits: 24090,
		Members: map[string]git.RepoStats{
			"ana@example.com": {Added: 1481000, Deleted: 246433, Net: 1234567, Commits: 12045},
		},
		Daily: sevenFigureStats().Daily,
	}

	path := filepath.Join(t.TempDir(), "team.html")
	if err := TeamHTML(stats, path, "light", "monthly", nil); err != nil {
		t.Fatalf("TeamHTML: %v", err)
	}
	body := assertRenderedHTML(t, path)

	for _, want := range []string{
		`<div class="stat-value net">1,234,567</div>`,
		`<div class="stat-value">24,090</div>`,
		`<td>+1,481,000</td>`,
		`<td>1,234,567</td>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing grouped value %q", want)
		}
	}
	assertNoUngroupedNumbers(t, path, body)
}

func TestCompareHTMLGroupsNumbers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "compare.html")
	c := git.CompareStats{
		BeforeLabel: "2024",
		AfterLabel:  "2025",
		Before:      git.RepoStats{Net: 1234567, Since: mustTime("2024-01-01"), Until: mustTime("2024-12-31")},
		After:       git.RepoStats{Net: 2345678, Since: mustTime("2025-01-01"), Until: mustTime("2025-12-31")},
	}
	if err := CompareHTML(c, path, "dark"); err != nil {
		t.Fatalf("CompareHTML: %v", err)
	}
	body := assertRenderedHTML(t, path)
	for _, want := range []string{"1,234,567", "2,345,678"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing grouped value %q", want)
		}
	}
	assertNoUngroupedNumbers(t, path, body)
}

// TestTeamCompareHTMLGroupsNegativeNumbers covers the sign path, since net
// lines go negative whenever deletions outweigh additions.
func TestTeamCompareHTMLGroupsNegativeNumbers(t *testing.T) {
	c := teamCompareFixture()
	before := c.Before.Members["leaver@example.com"]
	before.Net = -1234567
	c.Before.Members["leaver@example.com"] = before

	path := filepath.Join(t.TempDir(), "team-compare.html")
	if err := TeamCompareHTML(c, path, "dark"); err != nil {
		t.Fatalf("TeamCompareHTML: %v", err)
	}
	body := assertRenderedHTML(t, path)
	if !strings.Contains(body, "-1,234,567") {
		t.Errorf("a negative seven-figure net was not grouped:\n%s", body)
	}
	assertNoUngroupedNumbers(t, path, body)
}

// assertNoUngroupedNumbers is the guard against a headline figure being added
// later without the grouping function.
//
// It is scoped to the elements that carry counts, not to the whole document:
// a period label is legitimately "2025", and inline CSS and SVG geometry are
// full of bare numbers that must stay bare.
func assertNoUngroupedNumbers(t *testing.T, path, body string) {
	t.Helper()
	for _, m := range ungroupedFigure.FindAllString(body, -1) {
		t.Errorf("%s renders an ungrouped figure: %s", path, m)
	}
}

var ungroupedFigure = regexp.MustCompile(
	`<div class="(?:stat-value[^"]*|daily-stat|period-value[^"]*|period-perday)">[+-]?\d{4,}\b`)

// TestUngroupedFigureGuardWorks checks the guard above can actually fail. A
// regression detector that matches nothing passes every test for free.
func TestUngroupedFigureGuardWorks(t *testing.T) {
	shouldFlag := []string{
		`<div class="stat-value net">1234567</div>`,
		`<div class="stat-value added">+1481000</div>`,
		`<div class="stat-value deleted">-246433</div>`,
		`<div class="daily-stat">4748</div>`,
		`<div class="period-value after">2345678</div>`,
	}
	for _, s := range shouldFlag {
		if !ungroupedFigure.MatchString(s) {
			t.Errorf("the guard would not catch %s", s)
		}
	}

	shouldPass := []string{
		`<div class="stat-value net">1,234,567</div>`,
		`<div class="daily-stat">4,748</div>`,
		`<div class="stat-value">999</div>`,
		// A period label really is a bare four digit number.
		`<div class="period-label">2025</div>`,
		`<div class="period">Jan 1, 2025 — Dec 31, 2025</div>`,
	}
	for _, s := range shouldPass {
		if ungroupedFigure.MatchString(s) {
			t.Errorf("the guard wrongly flags %s", s)
		}
	}
}

// TestChartNoteExplainsAMissingChart covers the lead's rule that a chart the
// report cannot draw is explained rather than silently dropped.
func TestChartNoteExplainsAMissingChart(t *testing.T) {
	dir := t.TempDir()

	// Asked for a chart but gave no granularity: nothing says what a point
	// would cover.
	noBreakdown := filepath.Join(dir, "no-breakdown.html")
	err := HTMLWithOptions(sampleStats(), noBreakdown, "", "dark", metrics.Bundle{}, ChartOptions{Enabled: true})
	if err != nil {
		t.Fatalf("HTMLWithOptions: %v", err)
	}
	body := assertRenderedHTML(t, noBreakdown)
	if !strings.Contains(body, "needs --breakdown") {
		t.Errorf("the report does not explain the missing chart:\n%s", body)
	}
	if strings.Contains(body, "<svg viewBox=") {
		t.Error("a chart was drawn without a granularity to draw it at")
	}

	// A valid granularity with nothing in range gets a different reason.
	noData := filepath.Join(dir, "no-data.html")
	err = HTMLWithOptions(git.RepoStats{}, noData, "weekly", "dark", metrics.Bundle{}, ChartOptions{Enabled: true})
	if err != nil {
		t.Fatalf("HTMLWithOptions: %v", err)
	}
	body = assertRenderedHTML(t, noData)
	if !strings.Contains(body, "no weekly period in this range has any activity") {
		t.Errorf("the report does not explain the empty chart:\n%s", body)
	}

	// A chart that renders carries no note at all.
	fine := filepath.Join(dir, "fine.html")
	err = HTMLWithOptions(sampleStats(), fine, "monthly", "dark", metrics.Bundle{}, ChartOptions{Enabled: true})
	if err != nil {
		t.Fatalf("HTMLWithOptions: %v", err)
	}
	body = assertRenderedHTML(t, fine)
	if strings.Contains(body, "Chart not shown") {
		t.Error("a rendered chart still carried a not-shown note")
	}

	// And a report that never asked for a chart says nothing either way.
	silent := filepath.Join(dir, "silent.html")
	if err := HTML(sampleStats(), silent, "", "dark", metrics.Bundle{}); err != nil {
		t.Fatalf("HTML: %v", err)
	}
	if strings.Contains(assertRenderedHTML(t, silent), "Chart not shown") {
		t.Error("a report that never asked for a chart explained its absence")
	}
}

func TestChartUnavailableNote(t *testing.T) {
	tests := []struct {
		name        string
		rendered    bool
		granularity string
		want        string
	}{
		{"rendered charts say nothing", true, "monthly", ""},
		{"rendered without a granularity still says nothing", true, "", ""},
		{"no granularity", false, "", "Chart not shown: a trend chart needs --breakdown to say what a point covers."},
		{"unknown granularity", false, "hourly", "Chart not shown: a trend chart needs --breakdown to say what a point covers."},
		{"no activity", false, "daily", "Chart not shown: no daily period in this range has any activity to plot."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := chartUnavailableNote(tt.rendered, tt.granularity); got != tt.want {
				t.Errorf("chartUnavailableNote(%v, %q) = %q, want %q", tt.rendered, tt.granularity, got, tt.want)
			}
		})
	}
}

// TestHTMLChartFollowsBreakdown is the end-to-end version: the chart in the
// file plots the same granularity as the table beside it.
func TestHTMLChartFollowsBreakdown(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		granularity string
		wantTitle   string
		wantTable   string
		wantPoints  int
	}{
		{"monthly", "Net Lines by Month", "Monthly Breakdown", 3},
		{"weekly", "Net Lines by Week", "Weekly Breakdown", 4},
		{"daily", "Net Lines by Day", "Daily Breakdown", 5},
	}

	for _, tt := range cases {
		t.Run(tt.granularity, func(t *testing.T) {
			path := filepath.Join(dir, tt.granularity+".html")
			err := HTMLWithOptions(sampleStats(), path, tt.granularity, "dark", metrics.Bundle{}, ChartOptions{Enabled: true})
			if err != nil {
				t.Fatalf("HTMLWithOptions: %v", err)
			}
			body := assertRenderedHTML(t, path)
			assertWellFormedSVGString(t, body)

			if !strings.Contains(body, tt.wantTitle) {
				t.Errorf("chart title does not name the granularity, want %q", tt.wantTitle)
			}
			if !strings.Contains(body, tt.wantTable) {
				t.Errorf("breakdown table heading %q is missing", tt.wantTable)
			}
			// One hover band per period means the chart has as many points as
			// the table has rows.
			if got := strings.Count(body, `class="gr-chart-hit"`); got != tt.wantPoints {
				t.Errorf("chart plots %d periods, table has %d rows", got, tt.wantPoints)
			}
		})
	}
}

func TestTeamHTMLChartFollowsBreakdown(t *testing.T) {
	dir := t.TempDir()
	for _, granularity := range []string{"monthly", "weekly", "daily"} {
		t.Run(granularity, func(t *testing.T) {
			path := filepath.Join(dir, granularity+".html")
			err := TeamHTMLWithOptions(teamStatsFixture(), path, "dark", granularity, nil, ChartOptions{Enabled: true})
			if err != nil {
				t.Fatalf("TeamHTMLWithOptions: %v", err)
			}
			body := assertRenderedHTML(t, path)
			assertWellFormedSVGString(t, body)

			if !strings.Contains(body, chartGranularityTitle(granularity)) {
				t.Errorf("chart title does not name %q", granularity)
			}
			if got := strings.Count(body, `class="gr-chart-line"`); got != 3 {
				t.Errorf("drew %d lines, want one per member", got)
			}
		})
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
	err := TeamHTMLWithOptions(teamStatsFixture(), matched, "dark", "monthly", nil,
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
	err = TeamHTMLWithOptions(teamStatsFixture(), missed, "dark", "monthly", nil,
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
