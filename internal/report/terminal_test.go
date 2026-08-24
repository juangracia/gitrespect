package report

import "testing"

// TestFormatRate walks every branch of the rate formatter. The flat "0" for a
// small but real rate was a reported bug: a developer who netted 12 lines over
// a 30 day window was told they shipped nothing.
func TestFormatRate(t *testing.T) {
	tests := []struct {
		name string
		in   float64
		want string
	}{
		{"exact zero", 0, "0"},
		{"tiny positive keeps two decimals", 0.04, "0.04"},
		{"tiny negative keeps two decimals", -0.04, "-0.04"},
		{"just under the 0.1 boundary", 0.099, "0.10"},
		{"at the 0.1 boundary", 0.1, "0.1"},
		{"mid range rounds to one decimal", 5.55, "5.5"},
		{"negative mid range", -5.55, "-5.5"},
		{"just under ten", 9.94, "9.9"},
		{"at ten drops decimals", 10, "10"},
		{"above ten rounds to whole", 12.6, "13"},
		{"negative above ten", -12.6, "-13"},
		{"large", 1234.4, "1234"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatRate(tt.in); got != tt.want {
				t.Errorf("formatRate(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestFormatRateNeverFlattensRealOutput guards the specific regression: any
// non-zero rate must render as something other than a bare "0".
func TestFormatRateNeverFlattensRealOutput(t *testing.T) {
	for _, v := range []float64{0.001, 0.4, 0.49, -0.4, -0.001} {
		if got := formatRate(v); got == "0" || got == "-0" {
			t.Errorf("formatRate(%v) = %q, which reads as no output at all", v, got)
		}
	}
}

func TestFormatNumberAbs(t *testing.T) {
	tests := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{7, "7"},
		{999, "999"},
		{1000, "1,000"},
		{1234, "1,234"},
		{10000, "10,000"},
		{999999, "999,999"},
		{1000000, "1,000,000"},
		{1234567, "1,234,567"},
		{12345678, "12,345,678"},
		{1234567890, "1,234,567,890"},
	}
	for _, tt := range tests {
		if got := formatNumberAbs(tt.in); got != tt.want {
			t.Errorf("formatNumberAbs(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{-1, "-1"},
		{-999, "-999"},
		{-1000, "-1,000"},
		{-1234567, "-1,234,567"},
	}
	for _, tt := range tests {
		if got := formatNumber(tt.in); got != tt.want {
			t.Errorf("formatNumber(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPluralize(t *testing.T) {
	tests := []struct {
		n    int
		word string
		want string
	}{
		{1, "commit", "commit"},
		{0, "commit", "commits"},
		{2, "commit", "commits"},
		{-1, "merge", "merges"},
	}
	for _, tt := range tests {
		if got := pluralize(tt.n, tt.word); got != tt.want {
			t.Errorf("pluralize(%d, %q) = %q, want %q", tt.n, tt.word, got, tt.want)
		}
	}
}

// TestPeriodLabelWidth covers the column overflow bug: a label wider than the
// "Period" header used to leave the following columns unaligned.
func TestPeriodLabelWidth(t *testing.T) {
	tests := []struct {
		name   string
		labels []string
		want   int
	}{
		{"no labels falls back to the header", nil, len("Period")},
		{"short labels keep the header width", []string{"Q1", "Q2"}, len("Period")},
		{"label exactly the header width", []string{"123456"}, 6},
		{"one long label widens the column", []string{"2025-01:2025-06"}, 15},
		{"widest label wins", []string{"2025-01:2025-06", "before", "2024-01-01:2024-12-31"}, 21},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := periodLabelWidth(tt.labels...); got != tt.want {
				t.Errorf("periodLabelWidth(%q) = %d, want %d", tt.labels, got, tt.want)
			}
		})
	}
}

func TestPeriodLabelWidthFitsEveryLabel(t *testing.T) {
	labels := []string{"2025-01:2025-06", "a", "much-much-longer-period-label"}
	w := periodLabelWidth(labels...)
	for _, l := range labels {
		if len(l) > w {
			t.Errorf("width %d does not fit label %q (%d chars)", w, l, len(l))
		}
	}
}

func TestGetMonthName(t *testing.T) {
	tests := []struct {
		in   int
		want string
	}{
		{0, "???"},
		{1, "Jan"},
		{6, "Jun"},
		{12, "Dec"},
		{13, "???"},
		{-1, "???"},
	}
	for _, tt := range tests {
		if got := getMonthName(tt.in); got != tt.want {
			t.Errorf("getMonthName(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestGetChangeEmoji(t *testing.T) {
	tests := []struct {
		name string
		in   float64
		want string
	}{
		{"a drop gets the down arrow", 0.9, " 📉"},
		{"exactly flat is unremarkable", 1, ""},
		{"just under doubling stays quiet", 1.99, ""},
		{"doubling earns the chart", 2, " 📈"},
		{"just under 5x stays a chart", 4.99, " 📈"},
		{"5x earns the rocket", 5, " 🚀"},
		{"a zero multiplier is still a drop", 0, " 📉"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getChangeEmoji(tt.in); got != tt.want {
				t.Errorf("getChangeEmoji(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestBreakdownTitle(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"monthly", "Monthly Breakdown"},
		{"weekly", "Weekly Breakdown"},
		{"daily", "Daily Breakdown"},
		{"", "Monthly Breakdown"},
		{"nonsense", "Monthly Breakdown"},
	}
	for _, tt := range tests {
		if got := breakdownTitle(tt.in); got != tt.want {
			t.Errorf("breakdownTitle(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestRenderBarClampsToWidth(t *testing.T) {
	tests := []struct {
		name  string
		value float64
		width int
	}{
		{"zero", 0, 20},
		{"mid scale", 5, 20},
		{"at full scale", 10, 20},
		{"past full scale does not overrun", 40, 20},
		{"negative does not underrun", -3, 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bar := renderBar(tt.value, tt.width)
			filled := countRune(bar, '█')
			empty := countRune(bar, '░')
			if filled+empty != tt.width {
				t.Errorf("renderBar(%v, %d) drew %d cells, want %d", tt.value, tt.width, filled+empty, tt.width)
			}
			if filled < 0 || filled > tt.width {
				t.Errorf("renderBar(%v, %d) filled %d cells", tt.value, tt.width, filled)
			}
		})
	}
}

func countRune(s string, target rune) int {
	n := 0
	for _, r := range s {
		if r == target {
			n++
		}
	}
	return n
}
