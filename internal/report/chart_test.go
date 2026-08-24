package report

import (
	"encoding/xml"
	"fmt"
	"html/template"
	"io"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/juangracia/gitrespect/internal/git"
)

// months builds a monthly bucket map from "2006-01" keys to net lines.
func months(vals map[string]int) map[string]git.MonthStats {
	out := make(map[string]git.MonthStats, len(vals))
	for k, net := range vals {
		y, _ := strconv.Atoi(k[:4])
		m, _ := strconv.Atoi(k[5:7])
		added, deleted := net, 0
		if net < 0 {
			added, deleted = 0, -net
		}
		out[k] = git.MonthStats{Year: y, Month: m, Added: added, Deleted: deleted, Net: net, Commits: 1}
	}
	return out
}

// netDays places each month's net output on the 15th of that month. The chart
// and the breakdown table are both derived from the daily buckets, so a
// fixture has to supply those and not only the monthly rollup.
func netDays(vals map[string]int) map[string]git.DayStats {
	out := make(map[string]git.DayStats, len(vals))
	for k, net := range vals {
		date := k + "-15"
		if net < 0 {
			out[date] = day(date, 0, -net, 1)
			continue
		}
		out[date] = day(date, net, 0, 1)
	}
	return out
}

// memberStats builds one contributor's stats from a net-per-month map,
// populating both the daily buckets everything is derived from and the monthly
// rollup the JSON report still emits.
func memberStats(netByMonth map[string]int) git.RepoStats {
	net := 0
	for _, v := range netByMonth {
		net += v
	}
	return git.RepoStats{
		Net:     net,
		Monthly: months(netByMonth),
		Daily:   netDays(netByMonth),
	}
}

// teamStatsFixture has one member active every month, one who skipped a month
// entirely, and one who netted negative. That mix exercises the union of
// period keys and the negative branch of the y scale.
func teamStatsFixture() git.TeamStats {
	return git.TeamStats{
		Since:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Until:    time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC),
		TotalNet: 1300,
		Members: map[string]git.RepoStats{
			"ana@example.com": memberStats(map[string]int{"2025-01": 300, "2025-02": 250, "2025-03": 350}),
			"bo@example.com":  memberStats(map[string]int{"2025-01": 100, "2025-03": 400}),
			"cy@example.com":  memberStats(map[string]int{"2025-02": -100}),
		},
		Monthly: months(map[string]int{"2025-01": 400, "2025-02": 150, "2025-03": 750}),
		Daily: map[string]git.DayStats{
			"2025-01-15": day("2025-01-15", 420, 20, 4),
			"2025-02-10": day("2025-02-10", 250, 100, 3),
			"2025-03-05": day("2025-03-05", 800, 50, 5),
		},
	}
}

func TestNiceTicks(t *testing.T) {
	tests := []struct {
		name             string
		min, max         float64
		wantLo, wantHi   float64
		wantStep         float64
		wantStepAtLeast1 bool
	}{
		{name: "plain positive range", min: 0, max: 900, wantLo: 0, wantHi: 1000, wantStep: 200},
		{name: "range crossing zero", min: -100, max: 400, wantLo: -100, wantHi: 400, wantStep: 100},
		{name: "all values identical", min: 500, max: 500, wantLo: 500, wantHi: 501, wantStep: 1},
		{name: "all values zero", min: 0, max: 0, wantLo: 0, wantHi: 1, wantStep: 1},
		{name: "entirely negative", min: -900, max: 0, wantLo: -1000, wantHi: 0, wantStep: 200},
		{name: "reversed input is tolerated", min: 400, max: -100, wantLo: -100, wantHi: 400, wantStep: 100},
		{name: "millions", min: 0, max: 4200000, wantLo: 0, wantHi: 5000000, wantStep: 1000000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lo, hi, step := niceTicks(tt.min, tt.max, chartTickTarget)
			if lo != tt.wantLo || hi != tt.wantHi || step != tt.wantStep {
				t.Errorf("niceTicks(%v, %v) = (%v, %v, %v), want (%v, %v, %v)",
					tt.min, tt.max, lo, hi, step, tt.wantLo, tt.wantHi, tt.wantStep)
			}
			if step < 1 {
				t.Errorf("step %v is below one line, which names nothing", step)
			}
			if hi <= lo {
				t.Errorf("degenerate axis: lo %v hi %v", lo, hi)
			}
		})
	}
}

func TestNiceTicksRejectsNonFiniteInput(t *testing.T) {
	for _, tt := range []struct{ min, max float64 }{
		{math.NaN(), 100},
		{0, math.NaN()},
		{math.Inf(-1), math.Inf(1)},
		{0, math.Inf(1)},
	} {
		lo, hi, step := niceTicks(tt.min, tt.max, chartTickTarget)
		for _, v := range []float64{lo, hi, step} {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				t.Errorf("niceTicks(%v, %v) leaked a non-finite value: (%v, %v, %v)", tt.min, tt.max, lo, hi, step)
			}
		}
		if hi <= lo || step <= 0 {
			t.Errorf("niceTicks(%v, %v) = (%v, %v, %v), want a usable axis", tt.min, tt.max, lo, hi, step)
		}
	}
}

func TestFormatChartNumber(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{0, "0"},
		{-0.4, "0"},
		{999, "999"},
		{1000, "1,000"},
		{1234567, "1,234,567"},
		{-1234567, "-1,234,567"},
		{249.6, "250"},
		{math.NaN(), "0"},
		{math.Inf(1), "0"},
	}
	for _, tt := range tests {
		if got := formatRoundedNumber(tt.in); got != tt.want {
			t.Errorf("formatRoundedNumber(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatAxisNumber(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{0, "0"},
		{500, "500"},
		{9999, "9,999"},
		{10000, "10k"},
		{-10000, "-10k"},
		{250000, "250k"},
		{1500000, "1.5M"},
		{2000000, "2M"},
		{-2000000, "-2M"},
	}
	for _, tt := range tests {
		if got := formatAxisNumber(tt.in); got != tt.want {
			t.Errorf("formatAxisNumber(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestShortSeriesLabel(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"ana@example.com", "ana"},
		{"Team average", "Team average"},
		{"", ""},
		{"@leading", "@leading"},
		{"a-very-long-name-indeed", "a-very-long-n…"},
		{"averylongemailaddress@example.com", "averylongemai…"},
	}
	for _, tt := range tests {
		if got := shortSeriesLabel(tt.in); got != tt.want {
			t.Errorf("shortSeriesLabel(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestBreakdownSeriesMonthly(t *testing.T) {
	stats := sampleStats()
	s := BreakdownSeries(stats, "monthly", "dev@example.com")

	if s.Label != "dev@example.com" {
		t.Errorf("label = %q", s.Label)
	}
	wantKeys := []string{"2025-01", "2025-02", "2025-03"}
	if len(s.Points) != len(wantKeys) {
		t.Fatalf("got %d points, want %d", len(s.Points), len(wantKeys))
	}
	for i, want := range wantKeys {
		if s.Points[i].Key != want {
			t.Errorf("point %d key = %q, want %q (points must be oldest first)", i, s.Points[i].Key, want)
		}
	}
	if s.Points[0].Label != "Jan 2025" {
		t.Errorf("first label = %q, want %q", s.Points[0].Label, "Jan 2025")
	}
	if s.Points[0].Value != 250 {
		t.Errorf("first value = %v, want 250 (net lines)", s.Points[0].Value)
	}
	if s.Accent || s.Dashed {
		t.Error("a single repo series should be neither accented nor dashed")
	}
}

func TestBreakdownSeriesEmpty(t *testing.T) {
	if s := BreakdownSeries(git.RepoStats{}, "monthly", "nobody"); len(s.Points) != 0 {
		t.Errorf("empty stats produced %d points", len(s.Points))
	}
}

// TestBreakdownSeriesFollowsGranularity is the guarantee that motivated the
// change: the chart must plot the same periods as the table beside it, so it
// is built from the same git.Breakdown rows.
func TestBreakdownSeriesFollowsGranularity(t *testing.T) {
	stats := sampleStats()

	for _, granularity := range []string{"monthly", "weekly", "daily"} {
		t.Run(granularity, func(t *testing.T) {
			rows := git.Breakdown(stats, granularity)
			series := BreakdownSeries(stats, granularity, "dev@example.com")

			if len(series.Points) != len(rows) {
				t.Fatalf("chart has %d points but the table has %d rows", len(series.Points), len(rows))
			}
			for i, r := range rows {
				p := series.Points[i]
				if p.Key != r.Key || p.Label != r.Label {
					t.Errorf("point %d is %q/%q, table row is %q/%q", i, p.Key, p.Label, r.Key, r.Label)
				}
				if p.Value != float64(r.Net) {
					t.Errorf("point %d plots %v but the table says %d", i, p.Value, r.Net)
				}
			}
		})
	}

	// The three granularities really are different, so the check above is not
	// passing by accident.
	monthly := BreakdownSeries(stats, "monthly", "x")
	weekly := BreakdownSeries(stats, "weekly", "x")
	daily := BreakdownSeries(stats, "daily", "x")
	if len(monthly.Points) == len(daily.Points) || len(weekly.Points) == len(daily.Points) {
		t.Errorf("granularities collapsed: monthly %d, weekly %d, daily %d",
			len(monthly.Points), len(weekly.Points), len(daily.Points))
	}
	if monthly.Points[0].Label != "Jan 2025" {
		t.Errorf("monthly label = %q", monthly.Points[0].Label)
	}
	if !strings.HasPrefix(weekly.Points[0].Label, "Week of ") {
		t.Errorf("weekly label = %q", weekly.Points[0].Label)
	}
	if daily.Points[0].Label != "Jan 15 2025" {
		t.Errorf("daily label = %q", daily.Points[0].Label)
	}
}

func TestBreakdownSeriesRejectsUnknownGranularity(t *testing.T) {
	for _, g := range []string{"", "hourly", "quarterly"} {
		if s := BreakdownSeries(sampleStats(), g, "x"); len(s.Points) != 0 {
			t.Errorf("granularity %q produced %d points, want none so the caller can explain itself", g, len(s.Points))
		}
	}
}

func TestChartGranularityTitle(t *testing.T) {
	tests := []struct{ in, want string }{
		{"monthly", "Net Lines by Month"},
		{"weekly", "Net Lines by Week"},
		{"daily", "Net Lines by Day"},
		{"", "Net Lines by Period"},
	}
	for _, tt := range tests {
		if got := chartGranularityTitle(tt.in); got != tt.want {
			t.Errorf("chartGranularityTitle(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestRenderChartDailyOverAYear is the stress case: a year of daily points has
// to thin its labels and drop its markers rather than turning into mush.
func TestRenderChartDailyOverAYear(t *testing.T) {
	stats := git.RepoStats{Daily: map[string]git.DayStats{}}
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 365; i++ {
		d := start.AddDate(0, 0, i).Format("2006-01-02")
		stats.Daily[d] = day(d, 100+i%40, 20+i%15, 1)
	}

	series := BreakdownSeries(stats, "daily", "dev@example.com")
	if len(series.Points) != 365 {
		t.Fatalf("got %d points, want 365", len(series.Points))
	}

	out := RenderChart([]ChartSeries{series}, ChartRenderOptions{Title: chartGranularityTitle("daily")})
	assertWellFormedSVG(t, out)
	body := string(out)

	labels := xTickLabels(t, out)
	if len(labels) > 14 {
		t.Errorf("drew %d x labels for 365 days, they would overlap", len(labels))
	}
	if len(labels) < 3 {
		t.Errorf("thinned 365 days down to %d labels, which names almost nothing", len(labels))
	}
	if last := labels[len(labels)-1]; last != series.Points[364].Label {
		t.Errorf("last x label = %q, want the most recent day %q", last, series.Points[364].Label)
	}

	// Markers at this density read as a solid band, so they are dropped.
	if strings.Contains(body, `<circle class="gr-chart-dot"`) {
		t.Error("365 points should not each carry a marker")
	}
	// The line itself, its end label and every hover band are still there.
	if got := strings.Count(body, `class="gr-chart-line"`); got != 1 {
		t.Errorf("drew %d lines, want 1", got)
	}
	if !strings.Contains(body, `<text class="gr-chart-end"`) {
		t.Error("the end-of-line label went missing")
	}
	if got := strings.Count(body, `class="gr-chart-hit"`); got != 365 {
		t.Errorf("got %d hover bands, want one per day", got)
	}

	// Every hover band must be wide enough to actually hit with a mouse.
	for _, w := range hitBandWidths(t, out) {
		if w <= 0 {
			t.Fatalf("a hover band has width %v", w)
		}
	}

	// Only the labelled periods are tab stops: 365 of them would trap a
	// keyboard user inside one chart.
	tabStops := strings.Count(body, `<rect class="gr-chart-hit" tabindex="0"`)
	if tabStops != len(labels) {
		t.Errorf("got %d tab stops for %d labelled periods, want them to match", tabStops, len(labels))
	}
}

// TestRenderChartTabStopsFollowLabels checks the keyboard path both ways: a
// short axis keeps every period focusable, a crowded one does not.
func TestRenderChartTabStopsFollowLabels(t *testing.T) {
	short := RenderChart(
		[]ChartSeries{BreakdownSeries(sampleStats(), "monthly", "dev")},
		ChartRenderOptions{Title: "t"},
	)
	body := string(short)
	if got, bands := strings.Count(body, `tabindex="0"`), strings.Count(body, `class="gr-chart-hit"`); got != bands {
		t.Errorf("a three period chart has %d tab stops for %d periods, want every one reachable", got, bands)
	}

	stats := git.RepoStats{Daily: map[string]git.DayStats{}}
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 200; i++ {
		d := start.AddDate(0, 0, i).Format("2006-01-02")
		stats.Daily[d] = day(d, 50, 10, 1)
	}
	crowded := RenderChart(
		[]ChartSeries{BreakdownSeries(stats, "daily", "dev")},
		ChartRenderOptions{Title: "t"},
	)
	body = string(crowded)
	stops := strings.Count(body, `tabindex="0"`)
	bands := strings.Count(body, `class="gr-chart-hit"`)
	if bands != 200 {
		t.Fatalf("got %d hover bands, want 200", bands)
	}
	if stops >= bands {
		t.Errorf("200 periods produced %d tab stops, want only the labelled ones", stops)
	}
	if stops == 0 {
		t.Error("the chart is unreachable by keyboard entirely")
	}
}

// hitBandWidths returns the width of every hover band, in SVG user units.
func hitBandWidths(t *testing.T, out template.HTML) []float64 {
	t.Helper()
	var widths []float64
	for _, chunk := range strings.Split(svgFragment(t, out), `class="gr-chart-hit"`)[1:] {
		i := strings.Index(chunk, `width="`)
		if i < 0 {
			t.Fatal("a hover band has no width")
		}
		rest := chunk[i+len(`width="`):]
		w, err := strconv.ParseFloat(rest[:strings.Index(rest, `"`)], 64)
		if err != nil {
			t.Fatalf("unparseable hover band width: %v", err)
		}
		widths = append(widths, w)
	}
	return widths
}

func TestTeamBreakdownSeriesOneLinePerMember(t *testing.T) {
	series := TeamBreakdownSeries(teamStatsFixture(), "monthly", ChartOptions{Enabled: true})
	if len(series) != 3 {
		t.Fatalf("got %d series, want one per member", len(series))
	}
	wantLabels := []string{"ana@example.com", "bo@example.com", "cy@example.com"}
	for i, want := range wantLabels {
		if series[i].Label != want {
			t.Errorf("series %d = %q, want %q (sorted so a colour follows an identity)", i, series[i].Label, want)
		}
		if series[i].Accent || series[i].Dashed {
			t.Errorf("series %q should be a plain member line", series[i].Label)
		}
	}
	// A member who skipped a month keeps only the months they were active;
	// the renderer is what lines them up.
	if len(series[1].Points) != 2 {
		t.Errorf("bo has %d points, want 2", len(series[1].Points))
	}
}

func TestTeamBreakdownSeriesEmpty(t *testing.T) {
	if s := TeamBreakdownSeries(git.TeamStats{}, "monthly", ChartOptions{Enabled: true}); s != nil {
		t.Errorf("empty team produced %+v, want nil", s)
	}
	noMonths := git.TeamStats{Members: map[string]git.RepoStats{"a@b.c": {Net: 10}}}
	if s := TeamBreakdownSeries(noMonths, "monthly", ChartOptions{Enabled: true}); s != nil {
		t.Errorf("members with no monthly buckets produced %+v, want nil", s)
	}
}

func TestTeamBreakdownSeriesHighlightAddsTeamAverage(t *testing.T) {
	series := TeamBreakdownSeries(teamStatsFixture(), "monthly", ChartOptions{Enabled: true, Highlight: "bo@example.com"})
	if len(series) != 2 {
		t.Fatalf("got %d series, want the member and the team average", len(series))
	}

	member, avg := series[0], series[1]
	if member.Label != "bo@example.com" || !member.Accent || member.Dashed {
		t.Errorf("first series = %+v, want the highlighted member accented", member)
	}
	if avg.Label != "Team average" || !avg.Dashed || avg.Accent {
		t.Errorf("second series = %+v, want a dashed team average", avg)
	}

	// The average spans every month anyone worked, and divides by the whole
	// team: Feb is (250 + 0 + -100) / 3.
	if len(avg.Points) != 3 {
		t.Fatalf("average has %d points, want 3", len(avg.Points))
	}
	wantFeb := (250.0 + 0.0 - 100.0) / 3.0
	if math.Abs(avg.Points[1].Value-wantFeb) > 1e-9 {
		t.Errorf("February average = %v, want %v (absent members count as zero)", avg.Points[1].Value, wantFeb)
	}
	wantJan := (300.0 + 100.0 + 0.0) / 3.0
	if math.Abs(avg.Points[0].Value-wantJan) > 1e-9 {
		t.Errorf("January average = %v, want %v", avg.Points[0].Value, wantJan)
	}
}

func TestTeamBreakdownSeriesHighlightMissAndCaseInsensitivity(t *testing.T) {
	// An unknown highlight falls back to one line per member rather than
	// silently charting nothing.
	series := TeamBreakdownSeries(teamStatsFixture(), "monthly", ChartOptions{Enabled: true, Highlight: "ghost@example.com"})
	if len(series) != 3 {
		t.Errorf("unmatched highlight gave %d series, want all members", len(series))
	}
	for _, s := range series {
		if s.Dashed {
			t.Errorf("unmatched highlight still produced a team average: %+v", s)
		}
	}

	upper := TeamBreakdownSeries(teamStatsFixture(), "monthly", ChartOptions{Enabled: true, Highlight: "  BO@Example.com  "})
	if len(upper) != 2 || !upper[0].Accent {
		t.Errorf("highlight matching should ignore case and surrounding space, got %d series", len(upper))
	}
}

func TestTeamBreakdownSeriesHighlightOnAOnePersonTeam(t *testing.T) {
	stats := git.TeamStats{Members: map[string]git.RepoStats{
		"solo@example.com": memberStats(map[string]int{"2025-01": 100, "2025-02": 200}),
	}}
	series := TeamBreakdownSeries(stats, "monthly", ChartOptions{Enabled: true, Highlight: "solo@example.com"})
	if len(series) != 1 {
		t.Fatalf("got %d series, want just the member: the average of a team of one is that member", len(series))
	}
	if !series[0].Accent || series[0].Dashed {
		t.Errorf("series = %+v, want the member accented", series[0])
	}
}

func TestTeamBreakdownSeriesFoldsPastEightMembers(t *testing.T) {
	stats := git.TeamStats{Members: map[string]git.RepoStats{}}
	for i := 0; i < 12; i++ {
		// Later members are quieter, so the folded tail is predictable.
		stats.Members[fmt.Sprintf("dev%02d@example.com", i)] = memberStats(map[string]int{"2025-01": 1000 - i*10, "2025-02": 500})
	}

	series := TeamBreakdownSeries(stats, "monthly", ChartOptions{Enabled: true})
	if len(series) != chartMaxSeries {
		t.Fatalf("got %d series, want %d", len(series), chartMaxSeries)
	}

	last := series[len(series)-1]
	if last.Label != "Other (5 members)" {
		t.Errorf("folded series = %q, want %q", last.Label, "Other (5 members)")
	}
	// The tail is summed, not averaged: five members at 500 each in February.
	if len(last.Points) != 2 {
		t.Fatalf("folded series has %d points, want 2", len(last.Points))
	}
	if last.Points[1].Value != 2500 {
		t.Errorf("folded February = %v, want 2500", last.Points[1].Value)
	}
	// The surviving named lines stay in identity order.
	for i := 1; i < len(series)-1; i++ {
		if series[i-1].Label > series[i].Label {
			t.Errorf("kept members out of order: %q before %q", series[i-1].Label, series[i].Label)
		}
	}
}

// TestTeamSeriesKeepsExactlyEightUnfolded checks the boundary: eight members
// still get eight named lines, and folding only starts at the ninth.
func TestTeamBreakdownSeriesKeepsExactlyEightUnfolded(t *testing.T) {
	stats := git.TeamStats{Members: map[string]git.RepoStats{}}
	for i := 0; i < chartMaxSeries; i++ {
		stats.Members[fmt.Sprintf("dev%02d@example.com", i)] = memberStats(map[string]int{"2025-01": 100 - i})
	}
	series := TeamBreakdownSeries(stats, "monthly", ChartOptions{Enabled: true})
	if len(series) != chartMaxSeries {
		t.Fatalf("got %d series, want %d", len(series), chartMaxSeries)
	}
	for _, s := range series {
		if strings.HasPrefix(s.Label, "Other") {
			t.Errorf("eight members should all be named, found %q", s.Label)
		}
	}

	stats.Members["dev99@example.com"] = memberStats(map[string]int{"2025-01": 1})
	folded := TeamBreakdownSeries(stats, "monthly", ChartOptions{Enabled: true})
	if len(folded) != chartMaxSeries {
		t.Fatalf("nine members gave %d series, want %d", len(folded), chartMaxSeries)
	}
	if got := folded[len(folded)-1].Label; got != "Other (2 members)" {
		t.Errorf("folded label = %q, want %q", got, "Other (2 members)")
	}
}

func TestSeriesColor(t *testing.T) {
	tests := []struct {
		name   string
		series ChartSeries
		slot   int
		dark   bool
		want   string
	}{
		{"first light slot", ChartSeries{}, 0, false, "var(--chart-1, #2a78d6)"},
		{"first dark slot", ChartSeries{}, 0, true, "var(--chart-1, #3987e5)"},
		{"last slot", ChartSeries{}, 7, false, "var(--chart-8, #e34948)"},
		{"a dashed reference stays out of the palette", ChartSeries{Dashed: true}, 0, false, "var(--chart-ref, #656d76)"},
		{"past the palette goes muted rather than repeating a hue", ChartSeries{}, 8, false, "var(--chart-ref, #656d76)"},
		{"a negative slot is not an index panic", ChartSeries{}, -1, false, "var(--chart-ref, #656d76)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := seriesColor(tt.series, tt.slot, tt.dark); got != tt.want {
				t.Errorf("seriesColor = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSeriesColorSlotsAreDistinct guards the rule that a hue is never reused:
// two entities sharing a colour makes them read as one line.
func TestSeriesColorSlotsAreDistinct(t *testing.T) {
	for _, dark := range []bool{false, true} {
		seen := map[string]int{}
		for slot := 0; slot < chartMaxSeries; slot++ {
			c := seriesColor(ChartSeries{}, slot, dark)
			if prev, dup := seen[c]; dup {
				t.Errorf("dark=%v slots %d and %d share colour %q", dark, prev, slot, c)
			}
			seen[c] = slot
		}
	}
}

func TestRenderChartZeroSeries(t *testing.T) {
	cases := []struct {
		name   string
		series []ChartSeries
	}{
		{"nil", nil},
		{"empty slice", []ChartSeries{}},
		{"series with no points", []ChartSeries{{Label: "nobody"}}},
		{"several empty series", []ChartSeries{{Label: "a"}, {Label: "b"}}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := RenderChart(tt.series, ChartRenderOptions{Title: "Net Lines"}); got != "" {
				t.Errorf("want an empty result so the section is left out, got:\n%s", got)
			}
		})
	}
}

func TestRenderChartDegenerateData(t *testing.T) {
	one := func(vals ...float64) []ChartSeries {
		pts := make([]ChartPoint, len(vals))
		for i, v := range vals {
			key := fmt.Sprintf("2025-%02d", i+1)
			pts[i] = ChartPoint{Key: key, Label: key, Value: v}
		}
		return []ChartSeries{{Label: "solo", Points: pts}}
	}

	cases := []struct {
		name   string
		series []ChartSeries
	}{
		{"a single point", one(400)},
		{"a single zero point", one(0)},
		{"every value identical", one(500, 500, 500, 500)},
		{"every value zero", one(0, 0, 0)},
		{"every value negative", one(-100, -400, -250)},
		{"values straddling zero", one(-100, 0, 300)},
		{"a huge range", one(1, 4200000)},
		{"a fractional series", one(0.4, 0.6, 0.5)},
		{"two series, one flat at zero", []ChartSeries{
			{Label: "flat", Points: []ChartPoint{{Key: "2025-01", Label: "Jan", Value: 0}, {Key: "2025-02", Label: "Feb", Value: 0}}},
			{Label: "moving", Points: []ChartPoint{{Key: "2025-01", Label: "Jan", Value: 900}, {Key: "2025-02", Label: "Feb", Value: -900}}},
		}},
		{"a series whose points arrive out of order", []ChartSeries{
			{Label: "shuffled", Points: []ChartPoint{
				{Key: "2025-03", Label: "Mar", Value: 30},
				{Key: "2025-01", Label: "Jan", Value: 10},
				{Key: "2025-02", Label: "Feb", Value: 20},
			}},
		}},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			out := RenderChart(tt.series, ChartRenderOptions{Title: "Net Lines by Month"})
			if out == "" {
				t.Fatal("nothing rendered for plottable data")
			}
			assertWellFormedSVG(t, out)
		})
	}
}

func TestRenderChartHonoursCanvasSize(t *testing.T) {
	series := []ChartSeries{{Label: "solo", Points: []ChartPoint{{Key: "2025-01", Label: "Jan", Value: 10}}}}

	out := RenderChart(series, ChartRenderOptions{Title: "t"})
	if !strings.Contains(string(out), fmt.Sprintf(`viewBox="0 0 %d %d"`, chartDefaultWidth, chartDefaultHeight)) {
		t.Errorf("zero width and height should fall back to the defaults:\n%s", out)
	}

	out = RenderChart(series, ChartRenderOptions{Title: "t", Width: 500, Height: 200})
	if !strings.Contains(string(out), `viewBox="0 0 500 200"`) {
		t.Errorf("explicit size was not honoured:\n%s", out)
	}
	assertWellFormedSVG(t, out)

	// A canvas with no room for a plot is refused rather than drawn broken.
	if got := RenderChart(series, ChartRenderOptions{Title: "t", Width: 60, Height: 60}); got != "" {
		t.Errorf("a canvas too small to hold a plot should render nothing, got:\n%s", got)
	}
}

// TestRenderChartUnionsPeriods is the alignment guarantee: a member missing a
// month still has a point on that month's x position.
func TestRenderChartUnionsPeriods(t *testing.T) {
	series := TeamBreakdownSeries(teamStatsFixture(), "monthly", ChartOptions{Enabled: true})
	out := RenderChart(series, ChartRenderOptions{Title: "Net Lines by Month"})
	assertWellFormedSVG(t, out)

	// Three months in the union, so three hover bands.
	if got := strings.Count(string(out), `class="gr-chart-hit"`); got != 3 {
		t.Errorf("got %d hover bands, want 3 (the union of every member's months)", got)
	}
	// Every line carries a value for every month, missing ones as zero.
	for _, values := range dataValues(t, out) {
		if len(values) != 3 {
			t.Errorf("a line has %d values, want 3: %v", len(values), values)
		}
	}
	// bo skipped February, which must read as zero rather than a gap.
	bo := dataValuesFor(t, out, "bo@example.com")
	want := []string{"100", "0", "400"}
	if strings.Join(bo, ",") != strings.Join(want, ",") {
		t.Errorf("bo's line = %v, want %v (a skipped month is a real zero)", bo, want)
	}
}

func TestRenderChartThinsCrowdedXLabels(t *testing.T) {
	pts := make([]ChartPoint, 0, 36)
	for i := 0; i < 36; i++ {
		y, m := 2023+i/12, i%12+1
		pts = append(pts, ChartPoint{
			Key:   fmt.Sprintf("%04d-%02d", y, m),
			Label: fmt.Sprintf("%s %d", getMonthName(m), y),
			Value: float64(100 + i*7),
		})
	}
	out := RenderChart([]ChartSeries{{Label: "three years", Points: pts}}, ChartRenderOptions{Title: "Net Lines by Month"})
	assertWellFormedSVG(t, out)

	labels := xTickLabels(t, out)
	if len(labels) >= len(pts) {
		t.Errorf("drew %d x labels for %d periods, they would overlap", len(labels), len(pts))
	}
	if len(labels) < 2 {
		t.Errorf("thinned the x axis down to %d labels, which names nothing", len(labels))
	}
	// The most recent period is the one a reader looks for first.
	if last := labels[len(labels)-1]; last != pts[len(pts)-1].Label {
		t.Errorf("last x label = %q, want the most recent period %q", last, pts[len(pts)-1].Label)
	}

	// A short axis is labelled in full.
	short := RenderChart([]ChartSeries{{Label: "one year", Points: pts[:6]}}, ChartRenderOptions{Title: "Net Lines by Month"})
	if got := len(xTickLabels(t, short)); got != 6 {
		t.Errorf("got %d labels for 6 periods, want all of them", got)
	}
}

func TestRenderChartEscapesLabels(t *testing.T) {
	nasty := `<script>alert("x")</script> & <b>bold</b>`
	series := []ChartSeries{
		{Label: nasty, Points: []ChartPoint{{Key: "2025-01", Label: `Jan & "Feb" <2025>`, Value: 10}}},
		{Label: "plain", Points: []ChartPoint{{Key: "2025-01", Label: `Jan & "Feb" <2025>`, Value: 20}}},
	}
	out := RenderChart(series, ChartRenderOptions{Title: `Net & <Lines>`})

	// Well-formedness is the real proof: an unescaped angle bracket would
	// break the parse.
	assertWellFormedSVG(t, out)

	if strings.Contains(string(out), "<script>alert") {
		t.Errorf("a series label escaped into live markup:\n%s", out)
	}
	if strings.Contains(string(out), "<b>bold</b>") {
		t.Errorf("a series label escaped into live markup:\n%s", out)
	}
	if !strings.Contains(string(out), "&lt;script&gt;") {
		t.Errorf("the label should still be present as text:\n%s", out)
	}
}

func TestRenderChartThemesFromCustomProperties(t *testing.T) {
	series := []ChartSeries{{Label: "solo", Points: []ChartPoint{{Key: "2025-01", Label: "Jan", Value: 5}}}}

	light := string(RenderChart(series, ChartRenderOptions{Title: "t", IsDark: false}))
	if !strings.Contains(light, `<figure class="gr-chart" id=`) {
		t.Errorf("the light chart is not plainly themed:\n%s", light)
	}
	if strings.Contains(light, `class="gr-chart is-dark"`) {
		t.Error("the light chart carries the dark class")
	}
	if !strings.Contains(light, "var(--chart-1, #2a78d6)") {
		t.Errorf("light chart does not use the light step:\n%s", light)
	}

	dark := string(RenderChart(series, ChartRenderOptions{Title: "t", IsDark: true}))
	if !strings.Contains(dark, `class="gr-chart is-dark"`) {
		t.Errorf("dark chart is missing its theme class:\n%s", dark)
	}
	if !strings.Contains(dark, "var(--chart-1, #3987e5)") {
		t.Errorf("dark chart does not use the dark step:\n%s", dark)
	}

	// Chrome follows the report's own tokens in both themes rather than
	// being hardcoded per theme.
	for name, out := range map[string]string{"light": light, "dark": dark} {
		for _, token := range []string{"var(--border,", "var(--text-secondary,", "var(--bg-secondary,"} {
			if !strings.Contains(out, token) {
				t.Errorf("%s chart does not inherit %s from the report", name, token)
			}
		}
	}
}

func TestRenderChartLegendAndDirectLabels(t *testing.T) {
	single := string(RenderChart(
		[]ChartSeries{{Label: "solo", Points: []ChartPoint{{Key: "2025-01", Label: "Jan", Value: 5}}}},
		ChartRenderOptions{Title: "Net Lines by Month"},
	))
	if strings.Contains(single, `<div class="gr-chart-legend">`) {
		t.Error("a single series needs no legend box, the title names it")
	}
	if !strings.Contains(single, `<text class="gr-chart-end"`) {
		t.Error("a single series should still be labelled at the end of its line")
	}

	team := string(RenderChart(TeamBreakdownSeries(teamStatsFixture(), "monthly", ChartOptions{Enabled: true}), ChartRenderOptions{Title: "Net Lines by Month"}))
	if !strings.Contains(team, `<div class="gr-chart-legend">`) {
		t.Error("three series must carry a legend: identity is never colour alone")
	}
	for _, label := range []string{"ana@example.com", "bo@example.com", "cy@example.com"} {
		if !strings.Contains(team, label) {
			t.Errorf("legend is missing %q", label)
		}
	}

	// Past the direct-label cap the legend carries identity on its own.
	many := make([]ChartSeries, 0, 6)
	for i := 0; i < 6; i++ {
		many = append(many, ChartSeries{
			Label:  fmt.Sprintf("dev%d@example.com", i),
			Points: []ChartPoint{{Key: "2025-01", Label: "Jan", Value: float64(i * 10)}},
		})
	}
	crowded := string(RenderChart(many, ChartRenderOptions{Title: "Net Lines by Month"}))
	if strings.Contains(crowded, `<text class="gr-chart-end"`) {
		t.Error("six series should drop the direct labels, they would collide")
	}
	if !strings.Contains(crowded, `<div class="gr-chart-legend">`) {
		t.Error("six series must carry a legend")
	}
}

func TestRenderChartHighlightDrawsDashedReference(t *testing.T) {
	series := TeamBreakdownSeries(teamStatsFixture(), "monthly", ChartOptions{Enabled: true, Highlight: "ana@example.com"})
	out := string(RenderChart(series, ChartRenderOptions{Title: "ana vs team"}))
	assertWellFormedSVG(t, template.HTML(out))

	if got := strings.Count(out, `class="gr-chart-line is-dashed"`); got != 1 {
		t.Errorf("found %d dashed lines, want exactly the team average", got)
	}
	if !strings.Contains(out, "var(--chart-ref, #656d76)") {
		t.Errorf("the team average should read as chrome, not as a team member:\n%s", out)
	}
	if !strings.Contains(out, "var(--chart-1, #2a78d6)") {
		t.Error("the highlighted member should hold the first palette slot")
	}
	// The accent line keeps its markers so the reader can follow it.
	if !strings.Contains(out, "gr-chart-dot") {
		t.Error("the accent line lost its point markers")
	}
}

func TestRenderChartZeroLineOnlyWhenCrossing(t *testing.T) {
	crossing := string(RenderChart(
		[]ChartSeries{{Label: "s", Points: []ChartPoint{
			{Key: "2025-01", Label: "Jan", Value: -400},
			{Key: "2025-02", Label: "Feb", Value: 600},
		}}},
		ChartRenderOptions{Title: "t"},
	))
	if !strings.Contains(crossing, `<line class="gr-chart-zero"`) {
		t.Error("a series straddling zero must show where zero is")
	}

	positive := string(RenderChart(
		[]ChartSeries{{Label: "s", Points: []ChartPoint{
			{Key: "2025-01", Label: "Jan", Value: 400},
			{Key: "2025-02", Label: "Feb", Value: 600},
		}}},
		ChartRenderOptions{Title: "t"},
	))
	if strings.Contains(positive, `<line class="gr-chart-zero"`) {
		t.Error("an all-positive series already has zero as its baseline tick")
	}
}

func TestRenderChartIsSelfContainedAndStatic(t *testing.T) {
	out := string(RenderChart(TeamBreakdownSeries(teamStatsFixture(), "monthly", ChartOptions{Enabled: true}), ChartRenderOptions{Title: "Net Lines by Month"}))

	// A self-contained report cannot reach the network.
	for _, forbidden := range []string{"http://", "https://", "//cdn", "src=\"//"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("the chart reaches outside the file: found %q", forbidden)
		}
	}
	// Everything that carries meaning is drawn before the script runs.
	svg := svgFragment(t, template.HTML(out))
	for _, required := range []string{"gr-chart-line", "gr-chart-tick", "gr-chart-grid"} {
		if !strings.Contains(svg, required) {
			t.Errorf("the static SVG is missing %q, so it would not read with JS disabled", required)
		}
	}
	if strings.Contains(svg, "<script") {
		t.Error("script markup leaked inside the SVG")
	}
}

func TestRenderChartIsDeterministic(t *testing.T) {
	series := TeamBreakdownSeries(teamStatsFixture(), "monthly", ChartOptions{Enabled: true})
	opts := ChartRenderOptions{Title: "Net Lines by Month"}
	first := RenderChart(series, opts)
	for i := 0; i < 5; i++ {
		if got := RenderChart(series, opts); got != first {
			t.Fatal("the same input rendered differently, so the markup churns between runs")
		}
	}
}

func TestEndLabelPositionsStayInsideThePlot(t *testing.T) {
	const top, bottom = 20.0, 300.0
	yAt := func(v float64) float64 { return bottom - v }

	// Four series whose last values are nearly identical: their labels must
	// be pushed apart without leaving the plot.
	values := [][]float64{{0, 100}, {0, 101}, {0, 102}, {0, 103}}
	got := endLabelPositions(values, yAt, 2, top, bottom)
	if len(got) != len(values) {
		t.Fatalf("got %d positions, want %d", len(got), len(values))
	}
	for i, y := range got {
		if y < top || y > bottom {
			t.Errorf("label %d at y=%v is outside the plot [%v, %v]", i, y, top, bottom)
		}
	}
	for i := range got {
		for j := i + 1; j < len(got); j++ {
			if math.Abs(got[i]-got[j]) < 1 {
				t.Errorf("labels %d and %d overlap at y=%v", i, j, got[i])
			}
		}
	}
}

func TestEndLabelPositionsSlideStackBackInside(t *testing.T) {
	const top, bottom = 20.0, 60.0
	yAt := func(v float64) float64 { return bottom - v }
	// Three labels needing 13px each in a 40px plot: they cannot all fit, but
	// the stack must not run off the bottom edge.
	got := endLabelPositions([][]float64{{0}, {0}, {0}}, yAt, 1, top, bottom)
	if got[0] > bottom || got[len(got)-1] < top-1 {
		t.Errorf("stack landed outside the plot: %v", got)
	}
}

// assertWellFormedSVG parses the SVG fragment, which fails loudly on an
// unescaped label or a malformed attribute, and rejects non-finite geometry.
func assertWellFormedSVG(t *testing.T, out template.HTML) {
	t.Helper()
	frag := svgFragment(t, out)

	dec := xml.NewDecoder(strings.NewReader(frag))
	for {
		_, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("malformed SVG: %v\n%s", err, frag)
		}
	}
	for _, bad := range []string{"NaN", "Inf"} {
		if strings.Contains(frag, bad) {
			t.Errorf("SVG contains %q, so some geometry divided by zero:\n%s", bad, frag)
		}
	}
	if strings.Contains(frag, `=""`) {
		t.Errorf("SVG has an empty attribute value:\n%s", frag)
	}
}

func svgFragment(t *testing.T, out template.HTML) string {
	t.Helper()
	s := string(out)
	start := strings.Index(s, "<svg")
	end := strings.Index(s, "</svg>")
	if start < 0 || end < 0 {
		t.Fatalf("no SVG element in the output:\n%s", s)
	}
	return s[start : end+len("</svg>")]
}

// dataValues pulls the per-line tooltip values back out of the markup.
func dataValues(t *testing.T, out template.HTML) [][]string {
	t.Helper()
	var all [][]string
	for _, chunk := range strings.Split(string(out), `data-values="`)[1:] {
		raw := chunk[:strings.Index(chunk, `"`)]
		all = append(all, strings.Split(raw, ";"))
	}
	if len(all) == 0 {
		t.Fatalf("no data-values attributes in the output:\n%s", out)
	}
	return all
}

func dataValuesFor(t *testing.T, out template.HTML, label string) []string {
	t.Helper()
	marker := `data-label="` + label + `" data-color="`
	i := strings.Index(string(out), marker)
	if i < 0 {
		t.Fatalf("no line labelled %q in the output:\n%s", label, out)
	}
	rest := string(out)[i:]
	j := strings.Index(rest, `data-values="`)
	rest = rest[j+len(`data-values="`):]
	return strings.Split(rest[:strings.Index(rest, `"`)], ";")
}

// xTickLabels returns the x axis labels in drawing order.
func xTickLabels(t *testing.T, out template.HTML) []string {
	t.Helper()
	var labels []string
	for _, chunk := range strings.Split(svgFragment(t, out), `<text class="gr-chart-tick gr-chart-xtick"`)[1:] {
		end := strings.Index(chunk, "</text>")
		if end < 0 {
			t.Fatalf("unterminated tick label in:\n%s", chunk)
		}
		el := chunk[:end]
		labels = append(labels, el[strings.Index(el, ">")+1:])
	}
	return labels
}

// TestRenderChartAnchorsEdgeLabels keeps the outermost period labels inside
// the canvas instead of hanging them off the side.
func TestRenderChartAnchorsEdgeLabels(t *testing.T) {
	pts := []ChartPoint{
		{Key: "2025-01-13", Label: "Week of Jan 13 2025", Value: 100},
		{Key: "2025-01-20", Label: "Week of Jan 20 2025", Value: 200},
		{Key: "2025-01-27", Label: "Week of Jan 27 2025", Value: 150},
	}
	svg := svgFragment(t, RenderChart([]ChartSeries{{Label: "weekly", Points: pts}}, ChartRenderOptions{Title: "t"}))

	xTicks := strings.Split(svg, `<text class="gr-chart-tick gr-chart-xtick"`)[1:]
	if len(xTicks) != 3 {
		t.Fatalf("got %d x labels, want 3", len(xTicks))
	}
	if !strings.Contains(xTicks[0], `text-anchor="start"`) {
		t.Errorf("the first x label is not anchored to the left edge: %s", xTicks[0])
	}
	if !strings.Contains(xTicks[len(xTicks)-1], `text-anchor="end"`) {
		t.Errorf("the last x label is not anchored to the right edge: %s", xTicks[len(xTicks)-1])
	}
	if !strings.Contains(xTicks[1], `text-anchor="middle"`) {
		t.Errorf("an interior x label should be centred on its point: %s", xTicks[1])
	}

	// A lone label has no edge to lean against, so it stays centred.
	solo := svgFragment(t, RenderChart(
		[]ChartSeries{{Label: "one", Points: pts[:1]}}, ChartRenderOptions{Title: "t"}))
	if !strings.Contains(strings.Split(solo, `gr-chart-xtick"`)[1], `text-anchor="middle"`) {
		t.Error("a single period label should stay centred")
	}
}
