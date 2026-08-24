package report

import (
	"fmt"
	"html/template"
	"os"
	"sort"

	"github.com/juangracia/gitrespect/internal/benchmark"
	"github.com/juangracia/gitrespect/internal/git"
	"github.com/juangracia/gitrespect/internal/metrics"
)

type HTMLData struct {
	Author         string
	Since          string
	Until          string
	Added          int
	Deleted        int
	Net            int
	Commits        int
	WorkingDays    int
	PerDay         float64
	Monthly        []MonthlyHTMLData
	HasMonthly     bool
	BreakdownTitle string
	// Chart is the inline SVG trend chart, empty unless one was requested.
	HasChart bool
	Chart    template.HTML
	// ChartNote replaces the chart when one was asked for but cannot be
	// drawn, so the omission is explained rather than silent.
	ChartNote  string
	Theme      string
	IsDark     bool
	Baseline   *BaselineHTMLData
	CommitSize *CommitSizeHTMLData
	Cadence    *CadenceHTMLData
	LeadTime   *LeadTimeHTMLData
	Churn      *ChurnHTMLData
}

type BaselineHTMLData struct {
	WindowDays   int
	Normal       float64
	Period       float64
	PercentDelta float64
	IsAbove      bool
	Insufficient bool
}

type CommitSizeHTMLData struct {
	MicroPct  float64
	SmallPct  float64
	MediumPct float64
	LargePct  float64
}

type CadenceHTMLData struct {
	MedianDays float64
	Samples    int
}

type LeadTimeHTMLData struct {
	MedianDays float64
	Samples    int
}

type ChurnHTMLData struct {
	Ratio      float64
	WindowDays int
}

type MonthlyHTMLData struct {
	Label   string
	Added   int
	Deleted int
	Net     int
	IsMax   bool
}

// buildBreakdownHTML converts stats into breakdown rows for the templates,
// flagging the strongest period so it can be highlighted.
func buildBreakdownHTML(stats git.RepoStats, granularity string) []MonthlyHTMLData {
	rows := git.Breakdown(stats, granularity)
	if len(rows) == 0 {
		return nil
	}
	maxNet := 0
	maxKey := ""
	for _, r := range rows {
		if r.Net > maxNet {
			maxNet = r.Net
			maxKey = r.Key
		}
	}
	out := make([]MonthlyHTMLData, 0, len(rows))
	for _, r := range rows {
		out = append(out, MonthlyHTMLData{
			Label:   r.Label,
			Added:   r.Added,
			Deleted: r.Deleted,
			Net:     r.Net,
			IsMax:   r.Key == maxKey,
		})
	}
	return out
}

// reportFuncs are the helpers the HTML templates call.
//
// Grouping is done through a template function rather than a Formatted* field
// beside every count, so a count and its rendered form cannot drift apart and
// a field added later cannot quietly ship ungrouped.
var reportFuncs = template.FuncMap{
	"num":  formatNumber,
	"rate": formatRoundedNumber,
}

// chartUnavailableNote explains, in the report itself, why a requested chart
// is missing. Silently dropping the section leaves the reader assuming the
// tool decided there was nothing worth showing.
func chartUnavailableNote(rendered bool, granularity string) string {
	if rendered {
		return ""
	}
	if !git.ValidGranularity(granularity) {
		return "Chart not shown: a trend chart needs --breakdown to say what a point covers."
	}
	return "Chart not shown: no " + granularity + " period in this range has any activity to plot."
}

// breakdownHTMLTitle names the breakdown section for the requested granularity.
func breakdownHTMLTitle(granularity string) string {
	switch granularity {
	case "weekly":
		return "Weekly Breakdown"
	case "daily":
		return "Daily Breakdown"
	default:
		return "Monthly Breakdown"
	}
}

type CompareHTMLData struct {
	BeforeLabel  string
	AfterLabel   string
	BeforeNet    int
	AfterNet     int
	BeforeDays   int
	AfterDays    int
	BeforePerDay float64
	AfterPerDay  float64
	Multiplier   float64
	ChangeEmoji  string
	Theme        string
	IsDark       bool
	// Members is populated only for team comparisons.
	Members []CompareMemberHTMLData
}

type CompareMemberHTMLData struct {
	Email      string
	BeforeNet  int
	AfterNet   int
	Multiplier float64
	// HasBaseline is false when the member produced nothing in the before
	// period, so no multiplier is meaningful.
	HasBaseline bool
}

const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>gitrespect - {{.Author}}</title>
    <style>
        :root {
            {{if .IsDark}}
            --bg-primary: #0d1117;
            --bg-secondary: #161b22;
            --bg-tertiary: #21262d;
            --border: #30363d;
            --text-primary: #c9d1d9;
            --text-secondary: #8b949e;
            --text-muted: #484f58;
            --accent: #58a6ff;
            --accent-secondary: #238636;
            --success: #3fb950;
            --warning: #d29922;
            {{else}}
            --bg-primary: #ffffff;
            --bg-secondary: #f6f8fa;
            --bg-tertiary: #eaeef2;
            --border: #d0d7de;
            --text-primary: #1f2328;
            --text-secondary: #656d76;
            --text-muted: #8c959f;
            --accent: #0969da;
            --accent-secondary: #1a7f37;
            --success: #1a7f37;
            --warning: #9a6700;
            {{end}}
        }

        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }

        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'Noto Sans', Helvetica, Arial, sans-serif;
            background: var(--bg-primary);
            color: var(--text-primary);
            line-height: 1.5;
            min-height: 100vh;
        }

        .container {
            max-width: 900px;
            margin: 0 auto;
            padding: 32px 24px;
        }

        header {
            margin-bottom: 32px;
            padding-bottom: 16px;
            border-bottom: 1px solid var(--border);
        }

        .logo {
            font-size: 14px;
            font-weight: 600;
            color: var(--text-secondary);
            margin-bottom: 8px;
            font-family: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, monospace;
        }

        h1 {
            font-size: 24px;
            font-weight: 600;
            color: var(--text-primary);
        }

        .period {
            font-size: 14px;
            color: var(--text-secondary);
            margin-top: 4px;
        }

        .stats-grid {
            display: grid;
            grid-template-columns: repeat(4, 1fr);
            gap: 16px;
            margin-bottom: 32px;
        }

        @media (max-width: 640px) {
            .stats-grid {
                grid-template-columns: repeat(2, 1fr);
            }
        }

        .stat-card {
            background: var(--bg-secondary);
            border: 1px solid var(--border);
            border-radius: 6px;
            padding: 16px;
        }

        .stat-label {
            font-size: 12px;
            color: var(--text-secondary);
            text-transform: uppercase;
            letter-spacing: 0.5px;
            margin-bottom: 4px;
        }

        .stat-value {
            font-size: 28px;
            font-weight: 600;
            font-family: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, monospace;
        }

        .stat-value.added { color: var(--success); }
        .stat-value.deleted { color: var(--warning); }
        .stat-value.net { color: var(--accent); }

        .section {
            background: var(--bg-secondary);
            border: 1px solid var(--border);
            border-radius: 6px;
            padding: 20px;
            margin-bottom: 24px;
        }

        .section-title {
            font-size: 14px;
            font-weight: 600;
            color: var(--text-secondary);
            margin-bottom: 16px;
            text-transform: uppercase;
            letter-spacing: 0.5px;
        }

        .chart-note {
            font-size: 14px;
            color: var(--text-secondary);
        }

        .daily-stat {
            font-size: 32px;
            font-weight: 600;
            color: var(--accent);
            font-family: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, monospace;
        }

        .daily-label {
            color: var(--text-secondary);
            font-size: 14px;
        }

        .metric-row {
            display: flex;
            align-items: center;
            justify-content: space-between;
            padding: 10px 0;
            border-bottom: 1px solid var(--border);
        }

        .metric-row:last-child {
            border-bottom: none;
        }

        .metric-label {
            font-size: 14px;
            color: var(--text-secondary);
        }

        .metric-value {
            font-size: 15px;
            font-weight: 600;
            font-family: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, monospace;
            color: var(--accent);
        }

        .metric-value.delta-up { color: var(--success); }
        .metric-value.delta-down { color: var(--warning); }

        .bar-row {
            display: flex;
            align-items: center;
            padding: 6px 0;
        }

        .bar-label {
            width: 90px;
            font-size: 13px;
            color: var(--text-secondary);
        }

        .bar-track {
            flex: 1;
            height: 8px;
            background: var(--bg-tertiary);
            border-radius: 4px;
            overflow: hidden;
            margin: 0 12px;
        }

        .bar-fill {
            height: 100%;
            background: linear-gradient(90deg, var(--accent), var(--success));
            border-radius: 4px;
        }

        .bar-pct {
            width: 42px;
            text-align: right;
            font-size: 13px;
            font-family: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, monospace;
            color: var(--text-secondary);
        }

        table {
            width: 100%;
            border-collapse: collapse;
            font-size: 14px;
        }

        th {
            text-align: left;
            padding: 10px 12px;
            font-size: 12px;
            font-weight: 600;
            color: var(--text-secondary);
            text-transform: uppercase;
            letter-spacing: 0.5px;
            border-bottom: 1px solid var(--border);
        }

        th:not(:first-child) {
            text-align: right;
        }

        td {
            padding: 10px 12px;
            border-bottom: 1px solid var(--border);
            font-family: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, monospace;
        }

        td:not(:first-child) {
            text-align: right;
        }

        tr:hover {
            background: var(--bg-tertiary);
        }

        .max-row td {
            color: var(--success);
            font-weight: 600;
        }

        footer {
            text-align: center;
            padding: 24px;
            color: var(--text-muted);
            font-size: 12px;
        }

        footer a {
            color: var(--accent);
            text-decoration: none;
        }

        footer a:hover {
            text-decoration: underline;
        }
    </style>
</head>
<body>
    <div class="container">
        <header>
            <div class="logo">$ gitrespect</div>
            <h1>{{.Author}}</h1>
            <div class="period">{{.Since}} — {{.Until}}</div>
        </header>

        <div class="stats-grid">
            <div class="stat-card">
                <div class="stat-label">Added</div>
                <div class="stat-value added">+{{num .Added}}</div>
            </div>
            <div class="stat-card">
                <div class="stat-label">Deleted</div>
                <div class="stat-value deleted">-{{num .Deleted}}</div>
            </div>
            <div class="stat-card">
                <div class="stat-label">Net</div>
                <div class="stat-value net">{{num .Net}}</div>
            </div>
            <div class="stat-card">
                <div class="stat-label">Commits</div>
                <div class="stat-value">{{num .Commits}}</div>
            </div>
        </div>

        {{if .Baseline}}
        <div class="section">
            <div class="section-title">Personal Baseline</div>
            {{if .Baseline.Insufficient}}
            <div class="metric-row">
                <div class="metric-label">Baseline (prior {{.Baseline.WindowDays}} days)</div>
                <div class="metric-value">insufficient history</div>
            </div>
            {{else}}
            <div class="metric-row">
                <div class="metric-label">Your normal ({{.Baseline.WindowDays}}d prior)</div>
                <div class="metric-value">{{printf "%.0f" .Baseline.Normal}} lines/day</div>
            </div>
            <div class="metric-row">
                <div class="metric-label">This period</div>
                <div class="metric-value">{{printf "%.0f" .Baseline.Period}} lines/day</div>
            </div>
            <div class="metric-row">
                <div class="metric-label">Change vs baseline</div>
                <div class="metric-value {{if .Baseline.IsAbove}}delta-up{{else}}delta-down{{end}}">{{if .Baseline.IsAbove}}+{{end}}{{printf "%.0f" .Baseline.PercentDelta}}% {{if .Baseline.IsAbove}}&#8593;{{else}}&#8595;{{end}}</div>
            </div>
            {{end}}
        </div>
        {{end}}

        {{if .CommitSize}}
        <div class="section">
            <div class="section-title">Commit Size Distribution</div>
            <div class="bar-row">
                <div class="bar-label">Micro (&lt;10)</div>
                <div class="bar-track"><div class="bar-fill" style="width: {{printf "%.0f" .CommitSize.MicroPct}}%"></div></div>
                <div class="bar-pct">{{printf "%.0f" .CommitSize.MicroPct}}%</div>
            </div>
            <div class="bar-row">
                <div class="bar-label">Small (10-99)</div>
                <div class="bar-track"><div class="bar-fill" style="width: {{printf "%.0f" .CommitSize.SmallPct}}%"></div></div>
                <div class="bar-pct">{{printf "%.0f" .CommitSize.SmallPct}}%</div>
            </div>
            <div class="bar-row">
                <div class="bar-label">Medium (100-499)</div>
                <div class="bar-track"><div class="bar-fill" style="width: {{printf "%.0f" .CommitSize.MediumPct}}%"></div></div>
                <div class="bar-pct">{{printf "%.0f" .CommitSize.MediumPct}}%</div>
            </div>
            <div class="bar-row">
                <div class="bar-label">Large (500+)</div>
                <div class="bar-track"><div class="bar-fill" style="width: {{printf "%.0f" .CommitSize.LargePct}}%"></div></div>
                <div class="bar-pct">{{printf "%.0f" .CommitSize.LargePct}}%</div>
            </div>
        </div>
        {{end}}

        {{if or .Cadence .LeadTime .Churn}}
        <div class="section">
            <div class="section-title">Flow &amp; Quality Metrics</div>
            {{if .Cadence}}
            <div class="metric-row">
                <div class="metric-label">Integration cadence (median)</div>
                <div class="metric-value">{{printf "%.1f" .Cadence.MedianDays}} days</div>
            </div>
            {{end}}
            {{if .LeadTime}}
            <div class="metric-row">
                <div class="metric-label">Lead time branch &#8594; main (median)</div>
                <div class="metric-value">{{printf "%.1f" .LeadTime.MedianDays}} days</div>
            </div>
            {{end}}
            {{if .Churn}}
            <div class="metric-row">
                <div class="metric-label">Churn ({{.Churn.WindowDays}}d rewrite rate)</div>
                <div class="metric-value">{{printf "%.0f" .Churn.Ratio}}%</div>
            </div>
            {{end}}
        </div>
        {{end}}

        <div class="section">
            <div class="section-title">Daily Output</div>
            <div class="daily-stat">{{rate .PerDay}}</div>
            <div class="daily-label">lines/day ({{.WorkingDays}} working days)</div>
        </div>

        {{if .HasChart}}
        <div class="section">{{.Chart}}</div>
        {{else if .ChartNote}}
        <div class="section"><div class="chart-note">{{.ChartNote}}</div></div>
        {{end}}

        {{if .HasMonthly}}
        <div class="section">
            <div class="section-title">{{.BreakdownTitle}}</div>
            <table>
                <thead>
                    <tr>
                        <th>Period</th>
                        <th>Added</th>
                        <th>Deleted</th>
                        <th>Net</th>
                    </tr>
                </thead>
                <tbody>
                    {{range .Monthly}}
                    <tr{{if .IsMax}} class="max-row"{{end}}>
                        <td>{{.Label}}</td>
                        <td>+{{num .Added}}</td>
                        <td>-{{num .Deleted}}</td>
                        <td>{{num .Net}}</td>
                    </tr>
                    {{end}}
                </tbody>
            </table>
        </div>
        {{end}}

        <footer>
            Generated by <a href="https://github.com/juangracia/gitrespect">gitrespect</a>
        </footer>
    </div>
</body>
</html>`

const compareHtmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>gitrespect - Period Comparison</title>
    <style>
        :root {
            {{if .IsDark}}
            --bg-primary: #0d1117;
            --bg-secondary: #161b22;
            --border: #30363d;
            --text-primary: #c9d1d9;
            --text-secondary: #8b949e;
            --accent: #58a6ff;
            --success: #3fb950;
            {{else}}
            --bg-primary: #ffffff;
            --bg-secondary: #f6f8fa;
            --border: #d0d7de;
            --text-primary: #1f2328;
            --text-secondary: #656d76;
            --accent: #0969da;
            --success: #1a7f37;
            {{end}}
        }

        * { margin: 0; padding: 0; box-sizing: border-box; }

        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'Noto Sans', Helvetica, Arial, sans-serif;
            background: var(--bg-primary);
            color: var(--text-primary);
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 24px;
        }

        .card {
            background: var(--bg-secondary);
            border: 1px solid var(--border);
            border-radius: 12px;
            padding: 32px;
            max-width: 500px;
            width: 100%;
        }

        .logo {
            font-size: 14px;
            font-weight: 600;
            color: var(--text-secondary);
            margin-bottom: 24px;
            font-family: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, monospace;
        }

        h1 {
            font-size: 20px;
            margin-bottom: 24px;
        }

        .comparison {
            display: grid;
            grid-template-columns: 1fr 1fr;
            gap: 24px;
            margin-bottom: 32px;
        }

        .period-card {
            padding: 16px;
            border-radius: 8px;
            background: var(--bg-primary);
        }

        .period-label {
            font-size: 12px;
            color: var(--text-secondary);
            margin-bottom: 8px;
        }

        .period-value {
            font-size: 28px;
            font-weight: 600;
            font-family: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, monospace;
        }

        .period-value.after { color: var(--success); }

        .period-perday {
            font-size: 14px;
            color: var(--text-secondary);
            margin-top: 4px;
        }

        .result {
            text-align: center;
            padding: 24px;
            background: linear-gradient(135deg, rgba(56, 139, 253, 0.1), rgba(63, 185, 80, 0.1));
            border-radius: 8px;
        }

        .multiplier {
            font-size: 48px;
            font-weight: 700;
            color: var(--success);
            font-family: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, monospace;
        }

        .result-label {
            color: var(--text-secondary);
            font-size: 14px;
            margin-top: 8px;
        }

        footer {
            text-align: center;
            margin-top: 24px;
            font-size: 12px;
            color: var(--text-secondary);
        }

        footer a {
            color: var(--accent);
            text-decoration: none;
        }

        table {
            width: 100%;
            border-collapse: collapse;
            margin-top: 28px;
            font-size: 14px;
        }

        th {
            text-align: left;
            padding: 10px 8px;
            border-bottom: 1px solid var(--border);
            color: var(--text-secondary);
            font-weight: 500;
        }

        td {
            padding: 10px 8px;
            border-bottom: 1px solid var(--border);
        }

        tbody tr:last-child td {
            border-bottom: none;
        }

        td:not(:first-child), th:not(:first-child) {
            text-align: right;
            font-family: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, monospace;
        }
    </style>
</head>
<body>
    <div class="card">
        <div class="logo">$ gitrespect compare</div>
        <h1>Productivity Comparison</h1>

        <div class="comparison">
            <div class="period-card">
                <div class="period-label">{{.BeforeLabel}}</div>
                <div class="period-value">{{num .BeforeNet}}</div>
                <div class="period-perday">{{rate .BeforePerDay}} lines/day</div>
            </div>
            <div class="period-card">
                <div class="period-label">{{.AfterLabel}}</div>
                <div class="period-value after">{{num .AfterNet}}</div>
                <div class="period-perday">{{rate .AfterPerDay}} lines/day</div>
            </div>
        </div>

        <div class="result">
            <div class="multiplier">{{printf "%.1f" .Multiplier}}x {{.ChangeEmoji}}</div>
            <div class="result-label">productivity increase</div>
        </div>

        {{if .Members}}
        <table>
            <thead>
                <tr><th>Contributor</th><th>Before</th><th>After</th><th>Change</th></tr>
            </thead>
            <tbody>
                {{range .Members}}
                <tr>
                    <td>{{.Email}}</td>
                    <td>{{num .BeforeNet}}</td>
                    <td>{{num .AfterNet}}</td>
                    <td>{{if .HasBaseline}}{{printf "%.1f" .Multiplier}}x{{else}}n/a{{end}}</td>
                </tr>
                {{end}}
            </tbody>
        </table>
        {{end}}

        <footer>
            Generated by <a href="https://github.com/juangracia/gitrespect">gitrespect</a>
        </footer>
    </div>
</body>
</html>`

// HTML renders the single-author report without a trend chart.
func HTML(stats git.RepoStats, filename string, breakdown string, theme string, bundle metrics.Bundle) error {
	return HTMLWithOptions(stats, filename, breakdown, theme, bundle, ChartOptions{})
}

// HTMLWithOptions renders the single-author report, adding the inline SVG
// trend chart when chart.Enabled is set.
func HTMLWithOptions(stats git.RepoStats, filename string, breakdown string, theme string, bundle metrics.Bundle, chart ChartOptions) error {
	workingDays := git.WorkingDays(stats.Since, stats.Until)
	locPerDay := float64(stats.Net) / float64(workingDays)

	isDark := theme != "light"

	data := HTMLData{
		Author:      stats.Author,
		Since:       stats.Since.Format("Jan 2, 2006"),
		Until:       stats.Until.Format("Jan 2, 2006"),
		Added:       stats.Added,
		Deleted:     stats.Deleted,
		Net:         stats.Net,
		Commits:     stats.Commits,
		WorkingDays: workingDays,
		PerDay:      locPerDay,
		Theme:       theme,
		IsDark:      isDark,
	}

	if bundle.Baseline != nil {
		b := bundle.Baseline
		data.Baseline = &BaselineHTMLData{
			WindowDays:   int(b.WindowEnd.Sub(b.WindowStart).Hours() / 24),
			Normal:       b.LOCPerDay,
			Period:       b.PeriodLOCPerDay,
			PercentDelta: b.PercentDelta,
			IsAbove:      b.PercentDelta >= 0,
			Insufficient: b.InsufficientHistory,
		}
	}
	if bundle.CommitSize != nil && bundle.CommitSize.Total > 0 {
		d := bundle.CommitSize
		data.CommitSize = &CommitSizeHTMLData{
			MicroPct:  d.Percent(metrics.BucketMicro),
			SmallPct:  d.Percent(metrics.BucketSmall),
			MediumPct: d.Percent(metrics.BucketMedium),
			LargePct:  d.Percent(metrics.BucketLarge),
		}
	}
	if bundle.Cadence != nil && bundle.Cadence.Samples >= 2 {
		data.Cadence = &CadenceHTMLData{
			MedianDays: bundle.Cadence.MedianDaysBetween,
			Samples:    bundle.Cadence.Samples,
		}
	}
	if bundle.LeadTime != nil && bundle.LeadTime.Samples > 0 {
		data.LeadTime = &LeadTimeHTMLData{
			MedianDays: bundle.LeadTime.MedianDays,
			Samples:    bundle.LeadTime.Samples,
		}
	}
	if bundle.Churn != nil && bundle.Churn.AddedLines > 0 {
		data.Churn = &ChurnHTMLData{
			Ratio:      bundle.Churn.Ratio * 100,
			WindowDays: bundle.Churn.WindowDays,
		}
	}

	data.Monthly = buildBreakdownHTML(stats, breakdown)
	data.HasMonthly = len(data.Monthly) > 0
	data.BreakdownTitle = breakdownHTMLTitle(breakdown)

	if chart.Enabled {
		label := stats.Author
		if label == "" {
			label = "Net lines"
		}
		data.Chart = RenderChart(
			[]ChartSeries{BreakdownSeries(stats, breakdown, label)},
			ChartRenderOptions{Title: chartGranularityTitle(breakdown), IsDark: isDark},
		)
		data.HasChart = data.Chart != ""
		data.ChartNote = chartUnavailableNote(data.HasChart, breakdown)
	}

	tmpl, err := template.New("report").Funcs(reportFuncs).Parse(htmlTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	if filename == "" {
		filename = "gitrespect-report.html"
	}

	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	fmt.Printf("✓ Report saved to %s\n", filename)
	return nil
}

// TeamCompareHTML renders a before/after comparison for a whole team, with a
// per-member table under the headline multiplier.
func TeamCompareHTML(c git.TeamCompareStats, filename string, theme string) error {
	beforeDays := git.WorkingDays(c.Before.Since, c.Before.Until)
	afterDays := git.WorkingDays(c.After.Since, c.After.Until)

	beforePerDay := float64(c.Before.TotalNet) / float64(beforeDays)
	afterPerDay := float64(c.After.TotalNet) / float64(afterDays)
	multiplier := benchmark.CalculateMultiplier(beforePerDay, afterPerDay)

	data := CompareHTMLData{
		BeforeLabel:  c.BeforeLabel,
		AfterLabel:   c.AfterLabel,
		BeforeNet:    c.Before.TotalNet,
		AfterNet:     c.After.TotalNet,
		BeforeDays:   beforeDays,
		AfterDays:    afterDays,
		BeforePerDay: beforePerDay,
		AfterPerDay:  afterPerDay,
		Multiplier:   multiplier,
		ChangeEmoji:  changeEmoji(multiplier),
		Theme:        theme,
		IsDark:       theme != "light",
	}

	for _, email := range c.MemberEmails() {
		before := c.Before.Members[email]
		after := c.After.Members[email]
		row := CompareMemberHTMLData{
			Email:     email,
			BeforeNet: before.Net,
			AfterNet:  after.Net,
		}
		if before.Net > 0 {
			row.HasBaseline = true
			row.Multiplier = benchmark.CalculateMultiplier(
				float64(before.Net)/float64(beforeDays),
				float64(after.Net)/float64(afterDays),
			)
		}
		data.Members = append(data.Members, row)
	}

	if filename == "" {
		filename = "gitrespect-team-compare.html"
	}
	return renderCompareHTML(data, filename)
}

// changeEmoji picks a headline emoji for a productivity multiplier.
func changeEmoji(multiplier float64) string {
	switch {
	case multiplier >= 5:
		return "🚀"
	case multiplier >= 2:
		return "📈"
	default:
		return ""
	}
}

// renderCompareHTML writes the shared comparison template to filename.
func renderCompareHTML(data CompareHTMLData, filename string) error {
	tmpl, err := template.New("compare").Funcs(reportFuncs).Parse(compareHtmlTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	fmt.Printf("✓ Report saved to %s\n", filename)
	return nil
}

func CompareHTML(comparison git.CompareStats, filename string, theme string) error {
	beforeDays := git.WorkingDays(comparison.Before.Since, comparison.Before.Until)
	afterDays := git.WorkingDays(comparison.After.Since, comparison.After.Until)

	beforePerDay := float64(comparison.Before.Net) / float64(beforeDays)
	afterPerDay := float64(comparison.After.Net) / float64(afterDays)

	multiplier := benchmark.CalculateMultiplier(beforePerDay, afterPerDay)

	data := CompareHTMLData{
		BeforeLabel:  comparison.BeforeLabel,
		AfterLabel:   comparison.AfterLabel,
		BeforeNet:    comparison.Before.Net,
		AfterNet:     comparison.After.Net,
		BeforeDays:   beforeDays,
		AfterDays:    afterDays,
		BeforePerDay: beforePerDay,
		AfterPerDay:  afterPerDay,
		Multiplier:   multiplier,
		ChangeEmoji:  changeEmoji(multiplier),
		Theme:        theme,
		IsDark:       theme != "light",
	}

	if filename == "" {
		filename = "gitrespect-compare.html"
	}
	return renderCompareHTML(data, filename)
}

type TeamHTMLData struct {
	Since            string
	Until            string
	TotalAdded       int
	TotalDeleted     int
	TotalNet         int
	TotalCommits     int
	WorkingDays      int
	PerDay           float64
	Members          []TeamMemberHTMLData
	HasMonthly       bool
	Monthly          []MonthlyHTMLData
	BreakdownTitle   string
	HasMemberMetrics bool
	// Chart is the inline SVG trend chart, empty unless one was requested.
	HasChart bool
	Chart    template.HTML
	// ChartNote replaces the chart when one was asked for but cannot be
	// drawn, so the omission is explained rather than silent.
	ChartNote string
	Theme     string
	IsDark    bool
}

type TeamMemberHTMLData struct {
	Email      string
	Added      int
	Deleted    int
	Net        int
	Commits    int
	PerDay     float64
	IsTop      bool
	HasMetrics bool
	CommitSize *CommitSizeHTMLData
	Cadence    *CadenceHTMLData
	LeadTime   *LeadTimeHTMLData
	Churn      *ChurnHTMLData
}

const teamHtmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>gitrespect - Team Report</title>
    <style>
        :root {
            {{if .IsDark}}
            --bg-primary: #0d1117;
            --bg-secondary: #161b22;
            --bg-tertiary: #21262d;
            --border: #30363d;
            --text-primary: #c9d1d9;
            --text-secondary: #8b949e;
            --text-muted: #484f58;
            --accent: #58a6ff;
            --success: #3fb950;
            --warning: #d29922;
            {{else}}
            --bg-primary: #ffffff;
            --bg-secondary: #f6f8fa;
            --bg-tertiary: #eaeef2;
            --border: #d0d7de;
            --text-primary: #1f2328;
            --text-secondary: #656d76;
            --text-muted: #8c959f;
            --accent: #0969da;
            --success: #1a7f37;
            --warning: #9a6700;
            {{end}}
        }

        * { margin: 0; padding: 0; box-sizing: border-box; }

        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'Noto Sans', Helvetica, Arial, sans-serif;
            background: var(--bg-primary);
            color: var(--text-primary);
            line-height: 1.5;
            min-height: 100vh;
        }

        .container { max-width: 900px; margin: 0 auto; padding: 32px 24px; }

        header { margin-bottom: 32px; padding-bottom: 16px; border-bottom: 1px solid var(--border); }
        .logo { font-size: 14px; font-weight: 600; color: var(--text-secondary); margin-bottom: 8px; font-family: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, monospace; }
        h1 { font-size: 24px; font-weight: 600; }
        .period { font-size: 14px; color: var(--text-secondary); margin-top: 4px; }

        .stats-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 16px; margin-bottom: 32px; }
        @media (max-width: 640px) { .stats-grid { grid-template-columns: repeat(2, 1fr); } }

        .stat-card { background: var(--bg-secondary); border: 1px solid var(--border); border-radius: 6px; padding: 16px; }
        .stat-label { font-size: 12px; color: var(--text-secondary); text-transform: uppercase; letter-spacing: 0.5px; margin-bottom: 4px; }
        .stat-value { font-size: 28px; font-weight: 600; font-family: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, monospace; }
        .stat-value.added { color: var(--success); }
        .stat-value.deleted { color: var(--warning); }
        .stat-value.net { color: var(--accent); }

        .section { background: var(--bg-secondary); border: 1px solid var(--border); border-radius: 6px; padding: 20px; margin-bottom: 24px; }
        .section-title { font-size: 14px; font-weight: 600; color: var(--text-secondary); margin-bottom: 16px; text-transform: uppercase; letter-spacing: 0.5px; }
        .chart-note { font-size: 14px; color: var(--text-secondary); }
        .daily-stat { font-size: 32px; font-weight: 600; color: var(--accent); font-family: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, monospace; }
        .daily-label { color: var(--text-secondary); font-size: 14px; }

        table { width: 100%; border-collapse: collapse; font-size: 14px; }
        th { text-align: left; padding: 10px 12px; font-size: 12px; font-weight: 600; color: var(--text-secondary); text-transform: uppercase; letter-spacing: 0.5px; border-bottom: 1px solid var(--border); }
        th:not(:first-child) { text-align: right; }
        td { padding: 10px 12px; border-bottom: 1px solid var(--border); font-family: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, monospace; }
        td:not(:first-child) { text-align: right; }
        tr:hover { background: var(--bg-tertiary); }
        .top-row td { color: var(--success); font-weight: 600; }

        .member-card { background: var(--bg-tertiary); border: 1px solid var(--border); border-radius: 6px; padding: 16px; margin-bottom: 16px; }
        .member-card:last-child { margin-bottom: 0; }
        .member-card-title { font-size: 14px; font-weight: 600; color: var(--text-primary); margin-bottom: 12px; font-family: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, monospace; }
        .member-subtitle { font-size: 11px; font-weight: 600; color: var(--text-secondary); text-transform: uppercase; letter-spacing: 0.5px; margin: 8px 0 4px; }

        .metric-row { display: flex; align-items: center; justify-content: space-between; padding: 8px 0; border-bottom: 1px solid var(--border); }
        .metric-row:last-child { border-bottom: none; }
        .metric-label { font-size: 14px; color: var(--text-secondary); }
        .metric-value { font-size: 15px; font-weight: 600; font-family: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, monospace; color: var(--accent); }

        .bar-row { display: flex; align-items: center; padding: 5px 0; }
        .bar-label { width: 130px; font-size: 13px; color: var(--text-secondary); }
        .bar-track { flex: 1; height: 8px; background: var(--bg-secondary); border-radius: 4px; overflow: hidden; margin: 0 12px; }
        .bar-fill { height: 100%; background: linear-gradient(90deg, var(--accent), var(--success)); border-radius: 4px; }
        .bar-pct { width: 42px; text-align: right; font-size: 13px; font-family: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, monospace; color: var(--text-secondary); }

        footer { text-align: center; padding: 24px; color: var(--text-muted); font-size: 12px; }
        footer a { color: var(--accent); text-decoration: none; }
    </style>
</head>
<body>
    <div class="container">
        <header>
            <div class="logo">$ gitrespect --team</div>
            <h1>Team Report</h1>
            <div class="period">{{.Since}} — {{.Until}}</div>
        </header>

        <div class="stats-grid">
            <div class="stat-card"><div class="stat-label">Team Added</div><div class="stat-value added">+{{num .TotalAdded}}</div></div>
            <div class="stat-card"><div class="stat-label">Team Deleted</div><div class="stat-value deleted">-{{num .TotalDeleted}}</div></div>
            <div class="stat-card"><div class="stat-label">Team Net</div><div class="stat-value net">{{num .TotalNet}}</div></div>
            <div class="stat-card"><div class="stat-label">Team Commits</div><div class="stat-value">{{num .TotalCommits}}</div></div>
        </div>

        <div class="section">
            <div class="section-title">Team Daily Output</div>
            <div class="daily-stat">{{rate .PerDay}}</div>
            <div class="daily-label">lines/day ({{.WorkingDays}} working days)</div>
        </div>

        <div class="section">
            <div class="section-title">Team Members</div>
            <table>
                <thead><tr><th>Contributor</th><th>Added</th><th>Deleted</th><th>Net</th><th>Commits</th><th>/Day</th></tr></thead>
                <tbody>
                    {{range .Members}}
                    <tr{{if .IsTop}} class="top-row"{{end}}><td>{{.Email}}</td><td>+{{num .Added}}</td><td>-{{num .Deleted}}</td><td>{{num .Net}}</td><td>{{num .Commits}}</td><td>{{rate .PerDay}}</td></tr>
                    {{end}}
                </tbody>
            </table>
        </div>

        {{if .HasMemberMetrics}}
        <div class="section">
            <div class="section-title">Per-Member Metrics</div>
            {{range .Members}}
            {{if .HasMetrics}}
            <div class="member-card">
                <div class="member-card-title">{{.Email}}</div>
                {{if .CommitSize}}
                <div class="member-subtitle">Commit Size Distribution</div>
                <div class="bar-row"><div class="bar-label">Micro (&lt;10)</div><div class="bar-track"><div class="bar-fill" style="width: {{printf "%.0f" .CommitSize.MicroPct}}%"></div></div><div class="bar-pct">{{printf "%.0f" .CommitSize.MicroPct}}%</div></div>
                <div class="bar-row"><div class="bar-label">Small (10-99)</div><div class="bar-track"><div class="bar-fill" style="width: {{printf "%.0f" .CommitSize.SmallPct}}%"></div></div><div class="bar-pct">{{printf "%.0f" .CommitSize.SmallPct}}%</div></div>
                <div class="bar-row"><div class="bar-label">Medium (100-499)</div><div class="bar-track"><div class="bar-fill" style="width: {{printf "%.0f" .CommitSize.MediumPct}}%"></div></div><div class="bar-pct">{{printf "%.0f" .CommitSize.MediumPct}}%</div></div>
                <div class="bar-row"><div class="bar-label">Large (500+)</div><div class="bar-track"><div class="bar-fill" style="width: {{printf "%.0f" .CommitSize.LargePct}}%"></div></div><div class="bar-pct">{{printf "%.0f" .CommitSize.LargePct}}%</div></div>
                {{end}}
                {{if or .Cadence .LeadTime .Churn}}
                <div class="member-subtitle">Flow &amp; Quality</div>
                {{if .Cadence}}<div class="metric-row"><div class="metric-label">Integration cadence (median)</div><div class="metric-value">{{printf "%.1f" .Cadence.MedianDays}} days</div></div>{{end}}
                {{if .LeadTime}}<div class="metric-row"><div class="metric-label">Lead time branch &#8594; main (median)</div><div class="metric-value">{{printf "%.1f" .LeadTime.MedianDays}} days</div></div>{{end}}
                {{if .Churn}}<div class="metric-row"><div class="metric-label">Churn ({{.Churn.WindowDays}}d rewrite rate)</div><div class="metric-value">{{printf "%.0f" .Churn.Ratio}}%</div></div>{{end}}
                {{end}}
            </div>
            {{end}}
            {{end}}
        </div>
        {{end}}

        {{if .HasChart}}
        <div class="section">{{.Chart}}</div>
        {{else if .ChartNote}}
        <div class="section"><div class="chart-note">{{.ChartNote}}</div></div>
        {{end}}

        {{if .HasMonthly}}
        <div class="section">
            <div class="section-title">{{.BreakdownTitle}}</div>
            <table>
                <thead><tr><th>Period</th><th>Added</th><th>Deleted</th><th>Net</th></tr></thead>
                <tbody>
                    {{range .Monthly}}
                    <tr{{if .IsMax}} class="top-row"{{end}}><td>{{.Label}}</td><td>+{{num .Added}}</td><td>-{{num .Deleted}}</td><td>{{num .Net}}</td></tr>
                    {{end}}
                </tbody>
            </table>
        </div>
        {{end}}

        <footer>Generated by <a href="https://github.com/juangracia/gitrespect">gitrespect</a></footer>
    </div>
</body>
</html>`

// TeamHTML renders the team report without a trend chart.
func TeamHTML(stats git.TeamStats, filename string, theme string, breakdown string, bundles map[string]metrics.Bundle) error {
	return TeamHTMLWithOptions(stats, filename, theme, breakdown, bundles, ChartOptions{})
}

// TeamHTMLWithOptions renders the team report, adding the inline SVG trend
// chart when chart.Enabled is set. With chart.Highlight naming a member, the
// chart becomes that member's line against a derived team average.
func TeamHTMLWithOptions(stats git.TeamStats, filename string, theme string, breakdown string, bundles map[string]metrics.Bundle, chart ChartOptions) error {
	workingDays := git.WorkingDays(stats.Since, stats.Until)

	isDark := theme != "light"

	data := TeamHTMLData{
		Since:        stats.Since.Format("Jan 2, 2006"),
		Until:        stats.Until.Format("Jan 2, 2006"),
		TotalAdded:   stats.TotalAdded,
		TotalDeleted: stats.TotalDeleted,
		TotalNet:     stats.TotalNet,
		TotalCommits: stats.TotalCommits,
		WorkingDays:  workingDays,
		PerDay:       float64(stats.TotalNet) / float64(workingDays),
		Theme:        theme,
		IsDark:       isDark,
	}

	// Sort members by net lines descending
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

	for i, m := range members {
		md := TeamMemberHTMLData{
			Email:   m.email,
			Added:   m.stats.Added,
			Deleted: m.stats.Deleted,
			Net:     m.stats.Net,
			Commits: m.stats.Commits,
			PerDay:  float64(m.stats.Net) / float64(workingDays),
			IsTop:   i == 0,
		}
		if b, ok := bundles[m.email]; ok {
			if b.CommitSize != nil && b.CommitSize.Total > 0 {
				d := b.CommitSize
				md.CommitSize = &CommitSizeHTMLData{
					MicroPct:  d.Percent(metrics.BucketMicro),
					SmallPct:  d.Percent(metrics.BucketSmall),
					MediumPct: d.Percent(metrics.BucketMedium),
					LargePct:  d.Percent(metrics.BucketLarge),
				}
			}
			if b.Cadence != nil && b.Cadence.Samples >= 2 {
				md.Cadence = &CadenceHTMLData{MedianDays: b.Cadence.MedianDaysBetween, Samples: b.Cadence.Samples}
			}
			if b.LeadTime != nil && b.LeadTime.Samples > 0 {
				md.LeadTime = &LeadTimeHTMLData{MedianDays: b.LeadTime.MedianDays, Samples: b.LeadTime.Samples}
			}
			if b.Churn != nil && b.Churn.AddedLines > 0 {
				md.Churn = &ChurnHTMLData{Ratio: b.Churn.Ratio * 100, WindowDays: b.Churn.WindowDays}
			}
			md.HasMetrics = md.CommitSize != nil || md.Cadence != nil || md.LeadTime != nil || md.Churn != nil
			if md.HasMetrics {
				data.HasMemberMetrics = true
			}
		}
		data.Members = append(data.Members, md)
	}

	// Team-wide breakdown
	data.Monthly = buildBreakdownHTML(git.RepoStats{Monthly: stats.Monthly, Daily: stats.Daily}, breakdown)
	data.HasMonthly = len(data.Monthly) > 0
	data.BreakdownTitle = "Team " + breakdownHTMLTitle(breakdown)

	if chart.Enabled {
		series := TeamBreakdownSeries(stats, breakdown, chart)
		// Name the chart after what was actually built: a Highlight that
		// matches nobody falls back to one line per member, and the title
		// must not claim a comparison the chart does not show.
		title := chartGranularityTitle(breakdown)
		if len(series) == 2 && series[1].Dashed {
			title = fmt.Sprintf("%s: %s vs %s", title, series[0].Label, series[1].Label)
		}
		data.Chart = RenderChart(series, ChartRenderOptions{Title: title, IsDark: isDark})
		data.HasChart = data.Chart != ""
		data.ChartNote = chartUnavailableNote(data.HasChart, breakdown)
	}

	tmpl, err := template.New("team").Funcs(reportFuncs).Parse(teamHtmlTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	if filename == "" {
		filename = "gitrespect-team.html"
	}

	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	fmt.Printf("✓ Report saved to %s\n", filename)
	return nil
}
