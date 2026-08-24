package report

import (
	"fmt"
	"hash/fnv"
	"html/template"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/juangracia/gitrespect/internal/git"
)

// ChartOptions turns the HTML trend chart on and picks whose line to
// emphasise. Its zero value leaves the chart off, so the callers that do not
// ask for one keep getting today's lighter report.
type ChartOptions struct {
	Enabled bool
	// Highlight names one team member (the email or label the report uses).
	// That member is drawn as the accent series against a derived team
	// average, which is the "me vs the team" view the request asked for.
	Highlight string
}

// ChartPoint is one period of one series.
type ChartPoint struct {
	Key   string // sortable period key, e.g. "2025-03"
	Label string // display label, e.g. "Mar 2025"
	Value float64
}

// ChartSeries is a single line on the chart.
type ChartSeries struct {
	Label  string
	Points []ChartPoint
	Accent bool // emphasised (the highlighted member)
	Dashed bool // used for the derived team average
}

// ChartRenderOptions carries the presentation choices the renderer needs but
// that are not part of the data.
type ChartRenderOptions struct {
	Title string
	// IsDark selects the dark step of the categorical palette. The report
	// bakes its theme into :root at generation time rather than reacting to
	// the viewer's OS, so the chart is told which one it renders into.
	IsDark bool
	Width  int
	Height int
}

const (
	chartDefaultWidth  = 860
	chartDefaultHeight = 320

	// chartMaxSeries caps how many lines are drawn. The categorical palette
	// only guarantees colour-vision separation across its eight slots, and a
	// ninth hue would have to be invented, so the tail folds into one
	// "Other" line instead.
	chartMaxSeries = 8

	// chartDirectLabelMax is how many series still get a label at the end of
	// their line. Past this the labels collide and the legend carries
	// identity on its own.
	chartDirectLabelMax = 4

	// chartMaxDots is the point count past which per-period markers stop
	// reading as points and start reading as a bead curtain.
	chartMaxDots = 24

	chartMarginTop    = 18
	chartMarginBottom = 44
	chartTickTarget   = 4
	chartMaxTicks     = 12
)

// MonthlySeries builds a single line from a repo's monthly buckets, plotting
// net lines per month.
func MonthlySeries(stats git.RepoStats, label string) ChartSeries {
	return ChartSeries{Label: label, Points: monthlyPoints(stats.Monthly)}
}

// chartMember is one candidate line while the team chart is being assembled.
type chartMember struct {
	label string
	pts   []ChartPoint
	total float64 // absolute output, used only to decide who survives the cap
}

// TeamSeries builds one line per team member. When opts.Highlight names a
// member, the result is instead that member's line plus a derived team
// average, so a single developer can read their own trend against the group.
func TeamSeries(stats git.TeamStats, opts ChartOptions) []ChartSeries {
	emails := make([]string, 0, len(stats.Members))
	for e := range stats.Members {
		emails = append(emails, e)
	}
	sort.Strings(emails)

	members := make([]chartMember, 0, len(emails))
	for _, e := range emails {
		pts := monthlyPoints(stats.Members[e].Monthly)
		if len(pts) == 0 {
			continue
		}
		total := 0.0
		for _, p := range pts {
			total += math.Abs(p.Value)
		}
		members = append(members, chartMember{label: e, pts: pts, total: total})
	}
	if len(members) == 0 {
		return nil
	}

	if h := strings.TrimSpace(opts.Highlight); h != "" {
		for _, m := range members {
			if !strings.EqualFold(m.label, h) {
				continue
			}
			highlighted := []ChartSeries{{Label: m.label, Points: m.pts, Accent: true}}
			if len(members) == 1 {
				// The average of a one person team is that person. Drawing
				// both would be the same line twice.
				return highlighted
			}
			return append(highlighted, ChartSeries{
				Label:  "Team average",
				Points: averagePoints(members),
				Dashed: true,
			})
		}
	}

	if len(members) > chartMaxSeries {
		// Rank only to decide who is shown; the surviving lines then go back
		// into a stable order so a member's colour follows their identity
		// rather than their position in the ranking.
		ranked := make([]chartMember, len(members))
		copy(ranked, members)
		sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].total > ranked[j].total })
		head := ranked[:chartMaxSeries-1]
		tail := ranked[chartMaxSeries-1:]

		kept := make([]chartMember, len(head))
		copy(kept, head)
		sort.Slice(kept, func(i, j int) bool { return kept[i].label < kept[j].label })
		members = append(kept, chartMember{
			label: fmt.Sprintf("Other (%d %s)", len(tail), pluralize(len(tail), "member")),
			pts:   sumPoints(tail),
		})
	}

	out := make([]ChartSeries, 0, len(members))
	for _, m := range members {
		out = append(out, ChartSeries{Label: m.label, Points: m.pts})
	}
	return out
}

// monthlyPoints turns a monthly bucket map into points sorted oldest first.
func monthlyPoints(monthly map[string]git.MonthStats) []ChartPoint {
	keys := make([]string, 0, len(monthly))
	for k := range monthly {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pts := make([]ChartPoint, 0, len(keys))
	for _, k := range keys {
		m := monthly[k]
		pts = append(pts, ChartPoint{Key: k, Label: monthKeyLabel(k, m), Value: float64(m.Net)})
	}
	return pts
}

// monthKeyLabel names a "2006-01" bucket, falling back to the raw key so a
// bucket with no parsed year or month still lines up on the axis.
func monthKeyLabel(key string, m git.MonthStats) string {
	if m.Year > 0 && m.Month >= 1 && m.Month <= 12 {
		return fmt.Sprintf("%s %d", getMonthName(m.Month), m.Year)
	}
	return key
}

// averagePoints derives the per-period mean across members. The divisor is
// every member, not just those active in the period, because a member with no
// commits that month genuinely contributed zero lines to the team.
func averagePoints(members []chartMember) []ChartPoint {
	n := float64(len(members))
	return foldPoints(members, func(sum float64) float64 { return sum / n })
}

// sumPoints collapses several members into one line by adding them period by
// period, used for the folded "Other" series.
func sumPoints(members []chartMember) []ChartPoint {
	return foldPoints(members, func(sum float64) float64 { return sum })
}

// foldPoints combines members period by period, applying fold to each total.
func foldPoints(members []chartMember, fold func(float64) float64) []ChartPoint {
	if len(members) == 0 {
		return nil
	}
	sets := make([][]ChartPoint, len(members))
	for i, m := range members {
		sets[i] = m.pts
	}
	keys, labels := unionKeys(sets)
	out := make([]ChartPoint, 0, len(keys))
	for _, k := range keys {
		sum := 0.0
		for _, set := range sets {
			for _, p := range set {
				if p.Key == k {
					sum += p.Value
					break
				}
			}
		}
		out = append(out, ChartPoint{Key: k, Label: labels[k], Value: fold(sum)})
	}
	return out
}

// unionKeys returns every period key any set reported, sorted, with a display
// label per key. A key nobody labelled falls back to the key itself, so the
// axis is never blank.
func unionKeys(sets [][]ChartPoint) ([]string, map[string]string) {
	labels := make(map[string]string)
	for _, set := range sets {
		for _, p := range set {
			if existing, ok := labels[p.Key]; !ok || existing == "" {
				labels[p.Key] = p.Label
			}
		}
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
		if labels[k] == "" {
			labels[k] = k
		}
	}
	sort.Strings(keys)
	return keys, labels
}

// chartLightSlots and chartDarkSlots are the categorical palette: the same
// eight hues, each stepped for its own surface. The order is the
// colour-vision-safety mechanism rather than decoration, so it is fixed and
// never cycled. Validated against this report's own surfaces (light #f6f8fa,
// dark #161b22): every slot clears the lightness band, the chroma floor, the
// adjacent-pair protan/deutan separation and the normal-vision floor. Three
// light-mode hues sit under 3:1 against the light surface, which is why every
// series also carries a text label (the legend always, plus a direct label
// when there are few enough lines) and the breakdown table stays in the
// report.
var (
	chartLightSlots = [chartMaxSeries]string{"#2a78d6", "#eb6834", "#1baf7a", "#eda100", "#e87ba4", "#008300", "#4a3aa7", "#e34948"}
	chartDarkSlots  = [chartMaxSeries]string{"#3987e5", "#d95926", "#199e70", "#c98500", "#d55181", "#008300", "#9085e9", "#e66767"}
)

// chartStyle themes the chart through custom properties, the same way the
// report themes itself. Only the series hues need a dark variant; the chrome
// borrows the report's own tokens so the chart cannot drift from the page it
// sits on, and the fallbacks keep the markup legible on its own.
const chartStyle = `<style>
.gr-chart{--chart-1:#2a78d6;--chart-2:#eb6834;--chart-3:#1baf7a;--chart-4:#eda100;--chart-5:#e87ba4;--chart-6:#008300;--chart-7:#4a3aa7;--chart-8:#e34948;--chart-grid:var(--border,#d0d7de);--chart-axis:var(--text-muted,#8c959f);--chart-tick:var(--text-secondary,#656d76);--chart-ink:var(--text-primary,#1f2328);--chart-muted:var(--text-secondary,#656d76);--chart-surface:var(--bg-secondary,#f6f8fa);--chart-tip:var(--bg-tertiary,#eaeef2);--chart-ref:var(--text-secondary,#656d76);margin:0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI','Noto Sans',Helvetica,Arial,sans-serif}
.gr-chart.is-dark{--chart-1:#3987e5;--chart-2:#d95926;--chart-3:#199e70;--chart-4:#c98500;--chart-5:#d55181;--chart-6:#008300;--chart-7:#9085e9;--chart-8:#e66767}
.gr-chart-title{font-size:14px;font-weight:600;color:var(--chart-muted);margin-bottom:16px;text-transform:uppercase;letter-spacing:.5px}
.gr-chart-plot{position:relative}
.gr-chart-plot svg{display:block;width:100%;height:auto}
.gr-chart-grid{stroke:var(--chart-grid);stroke-width:1}
.gr-chart-zero{stroke:var(--chart-axis);stroke-width:1}
.gr-chart-tick{fill:var(--chart-tick);font-size:11px;font-variant-numeric:tabular-nums}
.gr-chart-line{fill:none;stroke-width:2;stroke-linecap:round;stroke-linejoin:round}
.gr-chart-line.is-dashed{stroke-dasharray:6 4}
.gr-chart-dot{stroke:var(--chart-surface);stroke-width:2}
.gr-chart-end{font-size:11px;font-weight:600}
.gr-chart-cross{stroke:var(--chart-axis);stroke-width:1;stroke-dasharray:3 3;opacity:0}
.gr-chart-hit{fill:transparent;outline:none}
.gr-chart-hit:focus{fill:var(--chart-grid);fill-opacity:.4}
.gr-chart-tip{position:absolute;top:0;left:0;pointer-events:none;opacity:0;background:var(--chart-tip);border:1px solid var(--chart-grid);border-radius:6px;padding:8px 10px;font-size:12px;color:var(--chart-ink);white-space:nowrap;z-index:2}
.gr-chart-tip-head{font-weight:600;margin-bottom:4px}
.gr-chart-tip-row{display:flex;align-items:center;gap:8px;line-height:1.6}
.gr-chart-swatch{width:14px;height:3px;border-radius:2px;flex:none}
.gr-chart-tip-name{color:var(--chart-muted)}
.gr-chart-tip-val{margin-left:auto;padding-left:14px;font-variant-numeric:tabular-nums;font-weight:600}
.gr-chart-legend{display:flex;flex-wrap:wrap;gap:8px 18px;margin-top:14px}
.gr-chart-key{display:flex;align-items:center;gap:8px;font-size:12px;color:var(--chart-muted)}
</style>`

// RenderChart draws the series as an inline SVG line chart. It returns an
// empty value when there is nothing worth plotting, so a caller can leave the
// whole section out rather than ship an empty frame.
func RenderChart(series []ChartSeries, opts ChartRenderOptions) template.HTML {
	series = plottableSeries(series)
	if len(series) == 0 {
		return ""
	}
	sets := make([][]ChartPoint, len(series))
	for i, s := range series {
		sets[i] = s.Points
	}
	keys, labels := unionKeys(sets)
	n := len(keys)
	if n == 0 {
		return ""
	}

	width, height := opts.Width, opts.Height
	if width <= 0 {
		width = chartDefaultWidth
	}
	if height <= 0 {
		height = chartDefaultHeight
	}

	values := make([][]float64, len(series))
	for si, s := range series {
		byKey := make(map[string]float64, len(s.Points))
		for _, p := range s.Points {
			byKey[p.Key] = p.Value
		}
		row := make([]float64, n)
		for i, k := range keys {
			// A period a series never reported is plotted as zero, not as a
			// gap. For commit and line counts "no commits that month" is a
			// real zero, and gapping it would punch a hole in every line the
			// moment one member took a month off.
			row[i] = byKey[k]
		}
		values[si] = row
	}

	// The zero baseline is always in frame: net lines can go negative and the
	// sign is the point, so an axis that floated its own floor would hide it.
	minV, maxV := 0.0, 0.0
	for _, row := range values {
		for _, v := range row {
			if v < minV {
				minV = v
			}
			if v > maxV {
				maxV = v
			}
		}
	}
	lo, hi, step := niceTicks(minV, maxV, chartTickTarget)
	ticks := make([]float64, 0, chartMaxTicks)
	for v := lo; v <= hi+step/2 && len(ticks) < chartMaxTicks; v += step {
		ticks = append(ticks, v)
	}
	if len(ticks) < 2 {
		ticks = []float64{lo, hi}
	}

	tickLabels := make([]string, len(ticks))
	marginLeft := 28
	for i, t := range ticks {
		tickLabels[i] = formatAxisNumber(t)
		if w := len(tickLabels[i])*7 + 16; w > marginLeft {
			marginLeft = w
		}
	}
	if limit := width / 4; marginLeft > limit {
		marginLeft = limit
	}

	direct := len(series) <= chartDirectLabelMax
	endLabels := make([]string, len(series))
	marginRight := 16
	if direct {
		for i, s := range series {
			endLabels[i] = shortSeriesLabel(s.Label)
			if w := len(endLabels[i])*6 + 18; w > marginRight {
				marginRight = w
			}
		}
		if limit := width / 3; marginRight > limit {
			marginRight = limit
		}
	}

	plotLeft := float64(marginLeft)
	plotTop := float64(chartMarginTop)
	plotRight := float64(width - marginRight)
	plotBottom := float64(height - chartMarginBottom)
	plotW, plotH := plotRight-plotLeft, plotBottom-plotTop
	if plotW < 40 || plotH < 40 {
		// The caller asked for a canvas too small to hold axes and a plot.
		return ""
	}

	spanY := hi - lo
	if spanY <= 0 {
		spanY = 1
	}
	xAt := func(i int) float64 {
		if n == 1 {
			// One period has no span to spread over, so it sits centred
			// instead of dividing by zero.
			return plotLeft + plotW/2
		}
		return plotLeft + float64(i)*plotW/float64(n-1)
	}
	yAt := func(v float64) float64 { return plotBottom - (v-lo)/spanY*plotH }

	// Thin the x labels until they fit, counting back from the end so the
	// most recent period is always named.
	maxLabelLen := 1
	for _, k := range keys {
		if l := len(labels[k]); l > maxLabelLen {
			maxLabelLen = l
		}
	}
	xStep := 1
	if fit := int(plotW / float64(maxLabelLen*6+16)); fit < n {
		if fit < 1 {
			fit = 1
		}
		xStep = int(math.Ceil(float64(n) / float64(fit)))
	}

	esc := template.HTMLEscapeString
	id := chartID(opts.Title, series)

	var b strings.Builder
	class := "gr-chart"
	if opts.IsDark {
		class += " is-dark"
	}
	fmt.Fprintf(&b, "<figure class=\"%s\" id=\"%s\">", class, id)
	b.WriteString(chartStyle)
	if opts.Title != "" {
		fmt.Fprintf(&b, "<figcaption class=\"gr-chart-title\">%s</figcaption>", esc(opts.Title))
	}
	b.WriteString("<div class=\"gr-chart-plot\">")
	fmt.Fprintf(&b, "<svg viewBox=\"0 0 %d %d\" role=\"img\" preserveAspectRatio=\"xMidYMid meet\" aria-labelledby=\"%s-t %s-d\">", width, height, id, id)
	fmt.Fprintf(&b, "<title id=\"%s-t\">%s</title>", id, esc(chartTitleOrDefault(opts.Title)))
	fmt.Fprintf(&b, "<desc id=\"%s-d\">%s</desc>", id, esc(chartDescription(series, labels[keys[0]], labels[keys[n-1]])))

	// Gridlines and y ticks.
	b.WriteString("<g>")
	for i, t := range ticks {
		gy := yAt(t)
		fmt.Fprintf(&b, "<line class=\"gr-chart-grid\" x1=\"%s\" y1=\"%s\" x2=\"%s\" y2=\"%s\"/>",
			num(plotLeft), num(gy), num(plotRight), num(gy))
		fmt.Fprintf(&b, "<text class=\"gr-chart-tick\" x=\"%s\" y=\"%s\" text-anchor=\"end\" dominant-baseline=\"middle\">%s</text>",
			num(plotLeft-8), num(gy), esc(tickLabels[i]))
	}
	if lo < 0 && hi > 0 {
		zy := yAt(0)
		fmt.Fprintf(&b, "<line class=\"gr-chart-zero\" x1=\"%s\" y1=\"%s\" x2=\"%s\" y2=\"%s\"/>",
			num(plotLeft), num(zy), num(plotRight), num(zy))
	}
	b.WriteString("</g>")

	// X ticks. The outermost labels are anchored to the edge they sit on:
	// centred, a long period label ("Week of Jan 13 2025") would hang off the
	// side of the canvas and be clipped.
	b.WriteString("<g>")
	for i, k := range keys {
		if (n-1-i)%xStep != 0 {
			continue
		}
		anchor := "middle"
		switch {
		case i == 0 && n > 1:
			anchor = "start"
		case i == n-1 && n > 1:
			anchor = "end"
		}
		fmt.Fprintf(&b, "<text class=\"gr-chart-tick gr-chart-xtick\" x=\"%s\" y=\"%s\" text-anchor=\"%s\">%s</text>",
			num(xAt(i)), num(plotBottom+18), anchor, esc(labels[k]))
	}
	b.WriteString("</g>")

	colors := make([]string, len(series))
	slot := 0
	for si, s := range series {
		colors[si] = seriesColor(s, slot, opts.IsDark)
		if !s.Dashed {
			slot++
		}
	}

	// Series lines. The per-series data attributes feed the tooltip; they are
	// plain text, read back with getAttribute and written with textContent.
	for si, s := range series {
		var d strings.Builder
		for i := range keys {
			if i == 0 {
				d.WriteString("M")
			} else {
				d.WriteString(" L")
			}
			fmt.Fprintf(&d, "%s,%s", num(xAt(i)), num(yAt(values[si][i])))
		}
		cls := "gr-chart-line"
		if s.Dashed {
			cls += " is-dashed"
		}
		fmt.Fprintf(&b, "<path class=\"%s\" d=\"%s\" style=\"stroke:%s\" data-label=\"%s\" data-color=\"%s\" data-values=\"%s\"/>",
			cls, d.String(), colors[si], esc(s.Label), colors[si], esc(joinValues(values[si])))
	}

	// Markers, while they still read as points. The accent line keeps them
	// regardless, since it is the one the reader was told to follow.
	for si, s := range series {
		if s.Dashed || (n > chartMaxDots && !s.Accent) {
			continue
		}
		for i := range keys {
			fmt.Fprintf(&b, "<circle class=\"gr-chart-dot\" cx=\"%s\" cy=\"%s\" r=\"4\" style=\"fill:%s\"/>",
				num(xAt(i)), num(yAt(values[si][i])), colors[si])
		}
	}

	// Direct labels at the end of each line.
	if direct {
		for si, ly := range endLabelPositions(values, yAt, n, plotTop, plotBottom) {
			if endLabels[si] == "" {
				continue
			}
			fmt.Fprintf(&b, "<text class=\"gr-chart-end\" x=\"%s\" y=\"%s\" dominant-baseline=\"middle\" style=\"fill:%s\">%s</text>",
				num(plotRight+8), num(ly), colors[si], esc(endLabels[si]))
		}
	}

	// Hover layer: a crosshair plus one hit band per period. Everything above
	// it is static, so the chart still reads with JS disabled.
	fmt.Fprintf(&b, "<line class=\"gr-chart-cross\" x1=\"%s\" y1=\"%s\" x2=\"%s\" y2=\"%s\"/>",
		num(plotLeft), num(plotTop), num(plotLeft), num(plotBottom))
	for i, k := range keys {
		left, right := plotLeft, plotRight
		if i > 0 {
			left = (xAt(i-1) + xAt(i)) / 2
		}
		if i < n-1 {
			right = (xAt(i) + xAt(i+1)) / 2
		}
		fmt.Fprintf(&b, "<rect class=\"gr-chart-hit\" tabindex=\"0\" x=\"%s\" y=\"%s\" width=\"%s\" height=\"%s\" data-i=\"%d\" data-cx=\"%s\" data-x=\"%s\"/>",
			num(left), num(plotTop), num(right-left), num(plotH), i, num(xAt(i)), esc(labels[k]))
	}
	b.WriteString("</svg>")
	b.WriteString("<div class=\"gr-chart-tip\" aria-hidden=\"true\"></div>")
	b.WriteString("</div>")

	// A single series is named by the title, so it needs no legend box.
	if len(series) > 1 {
		b.WriteString("<div class=\"gr-chart-legend\">")
		for si, s := range series {
			style := "background:" + colors[si]
			if s.Dashed {
				style = fmt.Sprintf("background:repeating-linear-gradient(90deg,%s 0 5px,transparent 5px 9px)", colors[si])
			}
			fmt.Fprintf(&b, "<span class=\"gr-chart-key\"><span class=\"gr-chart-swatch\" style=\"%s\"></span>%s</span>", style, esc(s.Label))
		}
		b.WriteString("</div>")
	}

	b.WriteString(chartScript(id, width, plotTop))
	b.WriteString("</figure>")
	return template.HTML(b.String())
}

// chartScript wires the crosshair and tooltip. Values reach it through data
// attributes and are written with textContent, so a series label containing
// markup cannot escape into the document.
func chartScript(id string, width int, plotTop float64) string {
	return `<script>(function(){var r=document.getElementById("` + id + `");if(!r)return;` +
		`var s=r.querySelector("svg"),t=r.querySelector(".gr-chart-tip"),c=r.querySelector(".gr-chart-cross"),` +
		`L=r.querySelectorAll(".gr-chart-line"),H=r.querySelectorAll(".gr-chart-hit"),VB=` + strconv.Itoa(width) + `,PT=` + num(plotTop) + `;` +
		`if(!s||!t||!c)return;` +
		`function hide(){c.style.opacity="0";t.style.opacity="0";}` +
		`function show(e){var el=e.currentTarget,i=parseInt(el.getAttribute("data-i"),10),x=parseFloat(el.getAttribute("data-cx"));` +
		`c.setAttribute("x1",x);c.setAttribute("x2",x);c.style.opacity="1";` +
		`while(t.firstChild)t.removeChild(t.firstChild);` +
		`var h=document.createElement("div");h.className="gr-chart-tip-head";h.textContent=el.getAttribute("data-x");t.appendChild(h);` +
		`for(var k=0;k<L.length;k++){var l=L[k],v=(l.getAttribute("data-values")||"").split(";");` +
		`var row=document.createElement("div");row.className="gr-chart-tip-row";` +
		`var sw=document.createElement("span");sw.className="gr-chart-swatch";sw.style.background=l.getAttribute("data-color");row.appendChild(sw);` +
		`var nm=document.createElement("span");nm.className="gr-chart-tip-name";nm.textContent=l.getAttribute("data-label");row.appendChild(nm);` +
		`var vv=document.createElement("span");vv.className="gr-chart-tip-val";vv.textContent=v[i]||"";row.appendChild(vv);` +
		`t.appendChild(row);}` +
		`var b=s.getBoundingClientRect(),sc=b.width/VB;t.style.opacity="1";` +
		`var left=x*sc-t.offsetWidth/2;if(left<0){left=0;}if(left>b.width-t.offsetWidth){left=b.width-t.offsetWidth;}` +
		`t.style.left=left+"px";t.style.top=(PT*sc)+"px";}` +
		`for(var i=0;i<H.length;i++){H[i].addEventListener("mouseenter",show);H[i].addEventListener("focus",show);H[i].addEventListener("blur",hide);}` +
		`r.addEventListener("mouseleave",hide);})();</script>`
}

// plottableSeries drops series that carry no points at all.
func plottableSeries(series []ChartSeries) []ChartSeries {
	out := make([]ChartSeries, 0, len(series))
	for _, s := range series {
		if len(s.Points) == 0 {
			continue
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// seriesColor picks the CSS value for a line. The custom property carries the
// theme; the fallback is the step for the theme being rendered, so the markup
// still reads if the style block is stripped.
func seriesColor(s ChartSeries, slot int, dark bool) string {
	// The team average is a derived reference rather than a person, so it
	// stays out of the categorical palette and reads as chrome. Past the
	// eighth slot the builders have already folded the tail, so this is a
	// defensive path: reusing a hue would make two people look like one.
	if s.Dashed || slot < 0 || slot >= chartMaxSeries {
		return "var(--chart-ref, #656d76)"
	}
	fallback := chartLightSlots[slot]
	if dark {
		fallback = chartDarkSlots[slot]
	}
	return fmt.Sprintf("var(--chart-%d, %s)", slot+1, fallback)
}

// endLabelPositions places one direct label per series at its last point,
// pushing overlapping labels apart and sliding the stack back inside the plot.
func endLabelPositions(values [][]float64, yAt func(float64) float64, n int, top, bottom float64) []float64 {
	type entry struct {
		si int
		y  float64
	}
	items := make([]entry, 0, len(values))
	for si := range values {
		items = append(items, entry{si, yAt(values[si][n-1])})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].y < items[j].y })

	const gap = 13.0
	for i := 1; i < len(items); i++ {
		if items[i].y-items[i-1].y < gap {
			items[i].y = items[i-1].y + gap
		}
	}
	if len(items) > 0 {
		if over := items[len(items)-1].y - bottom; over > 0 {
			for i := range items {
				items[i].y -= over
			}
		}
		if under := top - items[0].y; under > 0 {
			for i := range items {
				items[i].y += under
			}
		}
	}

	out := make([]float64, len(values))
	for _, it := range items {
		out[it.si] = it.y
	}
	return out
}

// niceTicks rounds a value range out to a 1/2/5 tick step. The step never
// drops below 1, because every measure the chart plots is a whole count of
// lines or commits and a fractional gridline would name nothing.
func niceTicks(min, max float64, target int) (lo, hi, step float64) {
	if target < 2 {
		target = 2
	}
	if math.IsNaN(min) || math.IsNaN(max) || math.IsInf(min, 0) || math.IsInf(max, 0) {
		min, max = 0, 1
	}
	if max < min {
		min, max = max, min
	}
	if max-min <= 0 {
		// Every value is identical, commonly all zero. Give the axis a
		// nominal span so the line lands in frame instead of dividing by
		// zero.
		max = min + 1
	}

	raw := (max - min) / float64(target)
	mag := math.Pow(10, math.Floor(math.Log10(raw)))
	if mag <= 0 || math.IsInf(mag, 0) || math.IsNaN(mag) {
		mag = 1
	}
	// Round to the nearest nice multiple rather than up to the next one, so a
	// range like 0..900 gets six readable gridlines instead of three coarse
	// ones.
	switch norm := raw / mag; {
	case norm < 1.5:
		step = mag
	case norm < 3:
		step = 2 * mag
	case norm < 7:
		step = 5 * mag
	default:
		step = 10 * mag
	}
	if step < 1 {
		step = 1
	}

	lo = math.Floor(min/step) * step
	hi = math.Ceil(max/step) * step
	if hi <= lo {
		hi = lo + step
	}
	return lo, hi, step
}

// joinValues renders a series for the tooltip. Semicolon separated because
// the formatted numbers already contain commas.
func joinValues(values []float64) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = formatChartNumber(v)
	}
	return strings.Join(parts, ";")
}

// formatChartNumber renders a plotted value the same way the terminal report
// renders the same figure, so the chart and the table beside it agree.
func formatChartNumber(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "0"
	}
	return formatNumber(int(math.Round(v)))
}

// formatAxisNumber keeps axis ticks short so the left margin stays narrow.
func formatAxisNumber(v float64) string {
	a := math.Abs(v)
	switch {
	case a >= 1e6:
		return trimTrailingZero(strconv.FormatFloat(v/1e6, 'f', 1, 64)) + "M"
	case a >= 1e4:
		return strconv.FormatFloat(v/1e3, 'f', 0, 64) + "k"
	default:
		return formatChartNumber(v)
	}
}

func trimTrailingZero(s string) string {
	return strings.TrimSuffix(s, ".0")
}

// shortSeriesLabel trims a label down to something that fits beside the end
// of a line. Emails lose their domain, which is the part that repeats.
func shortSeriesLabel(label string) string {
	s := label
	if at := strings.IndexByte(s, '@'); at > 0 {
		s = s[:at]
	}
	r := []rune(s)
	if len(r) > 14 {
		return string(r[:13]) + "…"
	}
	return s
}

// num renders an SVG coordinate without a pointless trailing ".0".
func num(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		f = 0
	}
	return trimTrailingZero(strconv.FormatFloat(f, 'f', 1, 64))
}

// chartID derives a stable element id, so republishing the same report does
// not churn the markup.
func chartID(title string, series []ChartSeries) string {
	h := fnv.New32a()
	h.Write([]byte(title))
	for _, s := range series {
		h.Write([]byte{0})
		h.Write([]byte(s.Label))
	}
	return fmt.Sprintf("grc%08x", h.Sum32())
}

func chartTitleOrDefault(title string) string {
	if title == "" {
		return "Trend chart"
	}
	return title
}

// chartDescription is the screen-reader summary of the plot.
func chartDescription(series []ChartSeries, first, last string) string {
	if len(series) == 1 {
		return fmt.Sprintf("Line chart of %s from %s to %s.", series[0].Label, first, last)
	}
	return fmt.Sprintf("Line chart of %d series from %s to %s.", len(series), first, last)
}
