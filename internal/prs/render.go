package prs

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// ANSI styles, blanked when the output is not an interactive terminal so
// piping or redirecting produces clean text instead of escape codes. This
// mirrors the git side's terminal reporter.
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

// RenderTerminal prints the report to stdout.
func RenderTerminal(res Result) error { return WriteTerminal(os.Stdout, res) }

// WriteTerminal renders to any writer, which is what makes the layout
// testable without capturing stdout.
func WriteTerminal(w io.Writer, res Result) error {
	dateRange := fmt.Sprintf("%s to %s",
		res.Since.Format("Jan 2 2006"), res.Until.Format("Jan 2 2006"))

	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s%s gitrespect%s - Merge Requests\n", colorBold, colorCyan, colorReset)
	fmt.Fprintf(w, "%s%s %s (%s)%s\n", colorDim, sanitize(res.Provider), sanitize(res.Scope), dateRange, colorReset)
	fmt.Fprintln(w, strings.Repeat("─", 60))
	fmt.Fprintln(w)

	fmt.Fprintf(w, "  %sOpened%s      %sMerged%s      %sMerge rate%s  %sLead time%s\n",
		colorDim, colorReset, colorDim, colorReset, colorDim, colorReset, colorDim, colorReset)
	fmt.Fprintln(w, "  "+strings.Repeat("─", 50))
	fmt.Fprintf(w, "  %s%-11s%s %s%-11s%s %-11s %s\n",
		colorGreen, formatNumber(res.Opened), colorReset,
		colorCyan, formatNumber(res.Merged), colorReset,
		mergeRate(res.Opened, res.Merged),
		leadTimeSummary(res.LeadTime))
	fmt.Fprintln(w)

	if res.Opened == 0 {
		fmt.Fprintf(w, "  %sNo merge requests were created in this window.%s\n", colorDim, colorReset)
		fmt.Fprintln(w)
	} else {
		writeAuthorTable(w, res)
	}

	if res.Granularity != "" && len(res.Periods) > 0 {
		writeBreakdown(w, res)
	}

	writeCaveats(w, res)
	return nil
}

func writeAuthorTable(w io.Writer, res Result) {
	label := "Contributor"
	// Sanitize once, up front, so the width is measured on exactly the string
	// that gets printed. Measuring the raw name and printing a cleaned one
	// would misalign every row below it.
	labels := make([]string, len(res.Authors))
	width := displayWidth(label)
	for i, a := range res.Authors {
		labels[i] = sanitize(a.Identity)
		if n := displayWidth(labels[i]); n > width {
			width = n
		}
	}
	if width > 38 {
		width = 38
	}

	fmt.Fprintf(w, "  %sContributors%s\n", colorBold, colorReset)
	fmt.Fprintf(w, "  %s%-*s%s %sOpened%s   %sMerged%s   %s/month%s\n",
		colorDim, width, label, colorReset,
		colorDim, colorReset, colorDim, colorReset, colorDim, colorReset)
	fmt.Fprintln(w, "  "+strings.Repeat("─", width+26))

	days := res.Days()
	for i, a := range res.Authors {
		var perMonth float64
		if days > 0 {
			perMonth = float64(a.Opened) / days * daysPerMonth
		}
		fmt.Fprintf(w, "  %-*s %s%-8s%s %-8s %.1f\n",
			width, truncate(labels[i], width),
			colorCyan, formatNumber(a.Opened), colorReset,
			formatNumber(a.Merged),
			perMonth)
	}
	// A trimmed table must say so, or the reader takes it for the whole group.
	if hidden := res.AuthorsTotal - len(res.Authors); hidden > 0 {
		fmt.Fprintf(w, "  %sshowing the top %d of %d contributors (%d more not listed)%s\n",
			colorDim, len(res.Authors), res.AuthorsTotal, hidden, colorReset)
	}
	fmt.Fprintln(w)
}

func writeBreakdown(w io.Writer, res Result) {
	labelWidth := displayWidth("Period")
	for _, p := range res.Periods {
		if n := displayWidth(p.Label); n > labelWidth {
			labelWidth = n
		}
	}

	fmt.Fprintf(w, "  %s%s:%s\n", colorDim, breakdownTitle(res.Granularity), colorReset)
	fmt.Fprintln(w, "  "+strings.Repeat("─", labelWidth+20))
	fmt.Fprintf(w, "  %s%-*s%s %sOpened%s   %sMerged%s\n",
		colorDim, labelWidth, "Period", colorReset,
		colorDim, colorReset, colorDim, colorReset)
	fmt.Fprintln(w, "  "+strings.Repeat("─", labelWidth+20))
	for _, p := range res.Periods {
		fmt.Fprintf(w, "  %-*s %s%-8s%s %s\n",
			labelWidth, p.Label,
			colorCyan, formatNumber(p.Opened), colorReset,
			formatNumber(p.Merged))
	}
	fmt.Fprintln(w)

	// Per contributor breakdown, only when it adds something a single row
	// cannot: more than one person in the report.
	if len(res.Authors) < 2 {
		return
	}
	for _, a := range res.Authors {
		if a.Opened == 0 {
			continue
		}
		fmt.Fprintf(w, "  %s● %s%s\n", colorBold, sanitize(a.Identity), colorReset)
		cells := make([]string, 0, len(a.Periods))
		for _, p := range a.Periods {
			cells = append(cells, fmt.Sprintf("%s %d", p.Label, p.Opened))
		}
		fmt.Fprintf(w, "  └── %s\n", strings.Join(cells, "  |  "))
	}
	fmt.Fprintln(w)
}

// writeCaveats prints everything that qualifies the numbers above. These are
// not optional decoration: an unmatched author or a truncated fetch means the
// counts understate reality, and hiding that would make the report wrong.
func writeCaveats(w io.Writer, res Result) {
	if res.Truncated {
		fmt.Fprintf(w, "  %s⚠ incomplete results%s\n", colorYellow, colorReset)
		fmt.Fprintf(w, "  └── %s\n", res.Note)
		fmt.Fprintln(w)
	}
	if res.UnmatchedTotal > 0 {
		fmt.Fprintf(w, "  %s%d merge %s from %d %s matched nobody and %s not counted%s\n",
			colorYellow, res.UnmatchedTotal, pluralize(res.UnmatchedTotal, "request"),
			res.UnmatchedAccounts, pluralize(res.UnmatchedAccounts, "account"),
			wasWere(res.UnmatchedTotal), colorReset)
		for i, u := range res.Unmatched {
			prefix := "├──"
			if i == len(res.Unmatched)-1 {
				prefix = "└──"
			}
			fmt.Fprintf(w, "  %s %s (%d)\n", prefix, sanitize(u.Handle), u.Opened)
		}
		// The list is capped, so say when it is not the whole story.
		if hidden := res.UnmatchedAccounts - len(res.Unmatched); hidden > 0 {
			fmt.Fprintf(w, "  %sand %d more %s%s\n", colorDim, hidden, pluralize(hidden, "account"), colorReset)
		}
		fmt.Fprintf(w, "  %sthe platform reports accounts, not commit emails; pin one with --map you@corp.com=handle%s\n",
			colorDim, colorReset)
		fmt.Fprintln(w)
	}
}

// RenderComparisonTerminal prints a before/after merge request comparison.
func RenderComparisonTerminal(c Comparison) error {
	return WriteComparisonTerminal(os.Stdout, c)
}

// WriteComparisonTerminal renders the comparison to any writer.
func WriteComparisonTerminal(w io.Writer, c Comparison) error {
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s%s gitrespect%s - Merge Request Comparison\n", colorBold, colorCyan, colorReset)
	fmt.Fprintf(w, "%s%s %s%s\n", colorDim, sanitize(c.After.Provider), sanitize(c.After.Scope), colorReset)
	fmt.Fprintln(w, strings.Repeat("─", 60))
	fmt.Fprintln(w)

	width := displayWidth("Period")
	for _, l := range []string{c.BeforeLabel, c.AfterLabel} {
		if n := displayWidth(l); n > width {
			width = n
		}
	}

	fmt.Fprintf(w, "  %s%-*s%s %sOpened%s   %sMerged%s   %s/month%s\n",
		colorDim, width, "Period", colorReset,
		colorDim, colorReset, colorDim, colorReset, colorDim, colorReset)
	fmt.Fprintln(w, "  "+strings.Repeat("─", width+26))
	fmt.Fprintf(w, "  %-*s %s%-8s%s %-8s %.1f\n",
		width, c.BeforeLabel, colorDim, formatNumber(c.Before.Opened), colorReset,
		formatNumber(c.Before.Merged), c.BeforePerMonth)
	fmt.Fprintf(w, "  %-*s %s%-8s%s %-8s %.1f\n",
		width, c.AfterLabel, colorCyan, formatNumber(c.After.Opened), colorReset,
		formatNumber(c.After.Merged), c.AfterPerMonth)
	fmt.Fprintln(w)

	// The headline is the rate, because two windows are rarely the same
	// length. The raw volume is shown next to it so the reader can see both
	// numbers rather than wondering where the ratio came from.
	fmt.Fprintf(w, "  %sChange:%s %s per month  %s(volume %d → %d, %s%s)%s\n",
		colorDim, colorReset,
		multiplierText(c.PerMonthMultiple, c.PerMonthUndefine),
		colorDim, c.Before.Opened, c.After.Opened,
		multiplierText(c.OpenedMultiplier, c.OpenedUndefined), colorDim, colorReset)
	fmt.Fprintln(w)

	if len(c.Authors) > 0 {
		labels := make([]string, len(c.Authors))
		aw := displayWidth("Contributor")
		for i, a := range c.Authors {
			labels[i] = sanitize(a.Identity)
			if n := displayWidth(labels[i]); n > aw {
				aw = n
			}
		}
		if aw > 38 {
			aw = 38
		}
		fmt.Fprintf(w, "  %sPer contributor%s\n", colorBold, colorReset)
		fmt.Fprintf(w, "  %s%-*s%s %sBefore%s   %sAfter%s    %sChange%s\n",
			colorDim, aw, "Contributor", colorReset,
			colorDim, colorReset, colorDim, colorReset, colorDim, colorReset)
		fmt.Fprintln(w, "  "+strings.Repeat("─", aw+28))
		for i, a := range c.Authors {
			fmt.Fprintf(w, "  %-*s %-8d %-8d %s\n",
				aw, truncate(labels[i], aw), a.Before, a.After,
				multiplierText(a.Multiplier, a.Undefined))
		}
		fmt.Fprintln(w)
	}
	return nil
}

// multiplierText renders a ratio, spelling out the case where there is no
// baseline instead of printing a misleading 0.0x.
func multiplierText(multiplier float64, undefined bool) string {
	if undefined {
		return fmt.Sprintf("%sno baseline%s", colorDim, colorReset)
	}
	color := colorGreen
	sign := "+"
	if multiplier < 1 {
		color, sign = colorYellow, ""
	}
	return fmt.Sprintf("%s%s%.1fx%s", color, sign, multiplier, colorReset)
}

// RenderJSON writes the report as JSON to a file, or to stdout when filename
// is empty. No token or credential ever reaches this payload: Result has no
// field to carry one.
func RenderJSON(res Result, filename string) error {
	return writeJSON(res, filename)
}

// RenderComparisonJSON writes a before/after comparison as JSON.
func RenderComparisonJSON(c Comparison, filename string) error {
	return writeJSON(c, filename)
}

func writeJSON(v any, filename string) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	if filename == "" {
		fmt.Println(string(data))
		return nil
	}
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	fmt.Printf("✓ Report saved to %s\n", filename)
	return nil
}

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

func mergeRate(opened, merged int) string {
	if opened == 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", float64(merged)/float64(opened)*100)
}

func leadTimeSummary(lt *LeadTimeStats) string {
	if lt == nil || lt.Samples == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1fd median", lt.MedianDays)
}

// sanitize strips control characters out of text that came from a review
// platform before it reaches a terminal.
//
// A GitLab or GitHub display name can contain any byte, unlike a git ident, so
// an author who wants to can embed ESC sequences that erase lines of the report
// someone is reading or retitle their terminal. Tab survives because it is
// ordinary whitespace; every other C0 control, DEL, and the C1 range are
// replaced with U+FFFD so the tampering is visible rather than silently
// swallowed. This is a display-layer concern only: the values used for identity
// matching are never touched, or a crafted name could change who work is
// attributed to.
func sanitize(s string) string {
	if !needsSanitizing(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if isControl(r) {
			b.WriteRune(utf8.RuneError)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func needsSanitizing(s string) bool {
	for _, r := range s {
		if isControl(r) {
			return true
		}
	}
	return false
}

// isControl reports whether r is a C0 control other than tab, DEL, or a C1
// control. Newline and carriage return count: a name carrying either can forge
// extra rows in the table.
func isControl(r rune) bool {
	switch {
	case r == '\t':
		return false
	case r < 0x20, r == 0x7f:
		return true
	case r >= 0x80 && r <= 0x9f:
		return true
	default:
		return false
	}
}

// displayWidth counts runes, which is what fmt's %-*s padding counts. Using
// byte length here would misalign every row containing a non-ASCII name.
func displayWidth(s string) int {
	return utf8.RuneCountInString(s)
}

func truncate(s string, width int) string {
	if width <= 3 || displayWidth(s) <= width {
		return s
	}
	runes := []rune(s)
	return string(runes[:width-3]) + "..."
}

func pluralize(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func wasWere(n int) string {
	if n == 1 {
		return "was"
	}
	return "were"
}

// formatNumber groups thousands. It handles arbitrarily large values rather
// than only the four digit case.
func formatNumber(n int) string {
	if n < 0 {
		return "-" + formatNumber(-n)
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	lead := len(s) % 3
	if lead > 0 {
		b.WriteString(s[:lead])
	}
	for i := lead; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}
