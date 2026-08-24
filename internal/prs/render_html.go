package prs

import (
	"fmt"
	"html/template"
	"os"
)

// htmlData is the flattened, pre-formatted view the template renders. Doing
// the arithmetic here keeps the template free of logic.
type htmlData struct {
	Provider  string
	Scope     string
	Since     string
	Until     string
	IsDark    bool
	Opened    string
	Merged    string
	MergeRate string
	LeadTime  string

	Authors       []htmlAuthor
	AuthorsShown  int
	AuthorsTotal  int
	AuthorsHidden int

	HasBreakdown   bool
	BreakdownTitle string
	Periods        []htmlPeriod

	Truncated bool
	Note      string

	UnmatchedTotal    int
	UnmatchedAccounts int
	UnmatchedHidden   int
	Unmatched         []UnmatchedAuthor
}

type htmlAuthor struct {
	Identity  string
	Handles   string
	Opened    int
	Merged    int
	PerMonth  string
	SharePct  float64
	LeadTime  string
	MergeRate string
}

type htmlPeriod struct {
	Label    string
	Opened   int
	Merged   int
	BarPct   float64
	IsLatest bool
}

// RenderHTML writes a self-contained report. filename is defaulted rather than
// required, matching the git side's behaviour.
func RenderHTML(res Result, filename, theme string) error {
	if filename == "" {
		filename = "gitrespect-prs.html"
	}

	data := htmlData{
		Provider:          res.Provider,
		Scope:             res.Scope,
		Since:             res.Since.Format("Jan 2, 2006"),
		Until:             res.Until.Format("Jan 2, 2006"),
		IsDark:            theme != "light",
		Opened:            formatNumber(res.Opened),
		Merged:            formatNumber(res.Merged),
		MergeRate:         mergeRate(res.Opened, res.Merged),
		LeadTime:          plainLeadTime(res.LeadTime),
		Truncated:         res.Truncated,
		Note:              res.Note,
		UnmatchedTotal:    res.UnmatchedTotal,
		UnmatchedAccounts: res.UnmatchedAccounts,
		UnmatchedHidden:   res.UnmatchedAccounts - len(res.Unmatched),
		Unmatched:         res.Unmatched,
	}

	data.AuthorsShown = len(res.Authors)
	data.AuthorsTotal = res.AuthorsTotal
	data.AuthorsHidden = res.AuthorsTotal - len(res.Authors)

	days := res.Days()
	for _, a := range res.Authors {
		var perMonth float64
		if days > 0 {
			perMonth = float64(a.Opened) / days * daysPerMonth
		}
		var share float64
		if res.Opened > 0 {
			share = float64(a.Opened) / float64(res.Opened) * 100
		}
		handles := ""
		if len(a.Handles) > 0 {
			handles = joinHandles(a.Handles)
		}
		data.Authors = append(data.Authors, htmlAuthor{
			Identity:  a.Identity,
			Handles:   handles,
			Opened:    a.Opened,
			Merged:    a.Merged,
			PerMonth:  fmt.Sprintf("%.1f", perMonth),
			SharePct:  share,
			LeadTime:  plainLeadTime(a.LeadTime),
			MergeRate: mergeRate(a.Opened, a.Merged),
		})
	}

	if res.Granularity != "" && len(res.Periods) > 0 {
		data.HasBreakdown = true
		data.BreakdownTitle = breakdownTitle(res.Granularity)
		peak := 0
		for _, p := range res.Periods {
			if p.Opened > peak {
				peak = p.Opened
			}
		}
		for i, p := range res.Periods {
			var bar float64
			if peak > 0 {
				bar = float64(p.Opened) / float64(peak) * 100
			}
			data.Periods = append(data.Periods, htmlPeriod{
				Label:    p.Label,
				Opened:   p.Opened,
				Merged:   p.Merged,
				BarPct:   bar,
				IsLatest: i == len(res.Periods)-1,
			})
		}
	}

	tmpl, err := template.New("prs").Parse(prsHTMLTemplate)
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

// plainLeadTime formats lead time without ANSI codes, for HTML and JSON
// adjacent output.
func plainLeadTime(lt *LeadTimeStats) string {
	if lt == nil || lt.Samples == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1fd", lt.MedianDays)
}

func joinHandles(handles []string) string {
	out := ""
	for i, h := range handles {
		if i > 0 {
			out += ", "
		}
		out += h
	}
	return out
}

const prsHTMLTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>gitrespect - merge requests - {{.Scope}}</title>
    <style>
        :root {
            {{if .IsDark}}
            --bg-primary: #0d1117;
            --bg-secondary: #161b22;
            --bg-tertiary: #21262d;
            --border: #30363d;
            --text-primary: #c9d1d9;
            --text-secondary: #8b949e;
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

        .logo {
            font-size: 14px;
            font-weight: 600;
            color: var(--text-secondary);
            margin-bottom: 8px;
            font-family: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, monospace;
        }

        h1 { font-size: 24px; font-weight: 600; }

        .period { font-size: 14px; color: var(--text-secondary); margin-top: 4px; }

        .stats-grid {
            display: grid;
            grid-template-columns: repeat(4, 1fr);
            gap: 16px;
            margin-bottom: 32px;
        }

        @media (max-width: 640px) { .stats-grid { grid-template-columns: repeat(2, 1fr); } }

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

        .stat-value.opened { color: var(--success); }
        .stat-value.merged { color: var(--accent); }

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

        table { width: 100%; border-collapse: collapse; font-size: 14px; }

        th {
            text-align: left;
            font-size: 12px;
            text-transform: uppercase;
            letter-spacing: 0.5px;
            color: var(--text-secondary);
            font-weight: 600;
            padding: 8px 12px 8px 0;
            border-bottom: 1px solid var(--border);
        }

        td { padding: 10px 12px 10px 0; border-bottom: 1px solid var(--border); }
        tr:last-child td { border-bottom: none; }

        td.num {
            font-family: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, monospace;
            text-align: right;
            white-space: nowrap;
        }

        .handles { display: block; font-size: 12px; color: var(--text-secondary); }

        .bar-cell { width: 40%; }

        .bar {
            height: 8px;
            border-radius: 4px;
            background: var(--accent);
            min-width: 2px;
        }

        .bar-track { background: var(--bg-tertiary); border-radius: 4px; height: 8px; }

        .callout {
            border: 1px solid var(--warning);
            border-left-width: 4px;
            border-radius: 6px;
            padding: 16px;
            margin-bottom: 24px;
            font-size: 14px;
        }

        .callout .callout-title { font-weight: 600; color: var(--warning); margin-bottom: 6px; }

        .muted { color: var(--text-secondary); font-size: 13px; margin-top: 8px; }

        footer { color: var(--text-secondary); font-size: 12px; margin-top: 32px; }
    </style>
</head>
<body>
<div class="container">
    <header>
        <div class="logo">gitrespect</div>
        <h1>Merge requests &middot; {{.Scope}}</h1>
        <div class="period">{{.Provider}} &middot; {{.Since}} to {{.Until}}</div>
    </header>

    <div class="stats-grid">
        <div class="stat-card">
            <div class="stat-label">Opened</div>
            <div class="stat-value opened">{{.Opened}}</div>
        </div>
        <div class="stat-card">
            <div class="stat-label">Merged</div>
            <div class="stat-value merged">{{.Merged}}</div>
        </div>
        <div class="stat-card">
            <div class="stat-label">Merge rate</div>
            <div class="stat-value">{{.MergeRate}}</div>
        </div>
        <div class="stat-card">
            <div class="stat-label">Lead time</div>
            <div class="stat-value">{{.LeadTime}}</div>
        </div>
    </div>

    {{if .Truncated}}
    <div class="callout">
        <div class="callout-title">Incomplete results</div>
        <div>{{.Note}}</div>
    </div>
    {{end}}

    {{if .Authors}}
    <div class="section">
        <div class="section-title">Contributors</div>
        <table>
            <thead>
            <tr>
                <th>Contributor</th>
                <th class="num">Opened</th>
                <th class="num">Merged</th>
                <th class="num">Merge rate</th>
                <th class="num">Per month</th>
                <th class="num">Lead time</th>
            </tr>
            </thead>
            <tbody>
            {{range .Authors}}
            <tr>
                <td>
                    {{.Identity}}
                    {{if .Handles}}<span class="handles">{{.Handles}}</span>{{end}}
                </td>
                <td class="num">{{.Opened}}</td>
                <td class="num">{{.Merged}}</td>
                <td class="num">{{.MergeRate}}</td>
                <td class="num">{{.PerMonth}}</td>
                <td class="num">{{.LeadTime}}</td>
            </tr>
            {{end}}
            </tbody>
        </table>
        {{if .AuthorsHidden}}<div class="muted">Showing the top {{.AuthorsShown}} of {{.AuthorsTotal}} contributors.</div>{{end}}
    </div>
    {{end}}

    {{if .HasBreakdown}}
    <div class="section">
        <div class="section-title">{{.BreakdownTitle}}</div>
        <table>
            <thead>
            <tr>
                <th>Period</th>
                <th class="num">Opened</th>
                <th class="num">Merged</th>
                <th class="bar-cell"></th>
            </tr>
            </thead>
            <tbody>
            {{range .Periods}}
            <tr>
                <td>{{.Label}}</td>
                <td class="num">{{.Opened}}</td>
                <td class="num">{{.Merged}}</td>
                <td class="bar-cell">
                    <div class="bar-track"><div class="bar" style="width: {{printf "%.1f" .BarPct}}%"></div></div>
                </td>
            </tr>
            {{end}}
            </tbody>
        </table>
    </div>
    {{end}}

    {{if .UnmatchedTotal}}
    <div class="callout">
        <div class="callout-title">{{.UnmatchedTotal}} merge requests from {{.UnmatchedAccounts}} accounts were not counted</div>
        <div>No requested identity matched these accounts:</div>
        <table>
            <tbody>
            {{range .Unmatched}}
            <tr><td>{{.Handle}}</td><td class="num">{{.Opened}}</td></tr>
            {{end}}
            </tbody>
        </table>
        {{if .UnmatchedHidden}}<div class="muted">and {{.UnmatchedHidden}} more accounts</div>{{end}}
        <div class="muted">The platform reports accounts, not commit emails. Pin one with --map you@corp.com=handle.</div>
    </div>
    {{end}}

    <footer>Generated by gitrespect from {{.Provider}} API data.</footer>
</div>
</body>
</html>
`
