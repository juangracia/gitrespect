package report

import (
	"strings"
	"testing"
)

// unionKeys builds the shared x axis for a multi-series chart. Every series'
// months must appear, or a member who was active in a month the first series
// never touched is silently dropped from the chart while still counting in the
// totals printed beside it.
func TestUnionKeysCoversEverySeries(t *testing.T) {
	sets := [][]ChartPoint{
		{{Key: "2025-01", Label: "Jan 2025", Value: 10}},
		{{Key: "2025-03", Label: "Mar 2025", Value: 30}},
		{{Key: "2025-02", Label: "Feb 2025", Value: 20}},
	}

	keys, labels := unionKeys(sets)

	if len(keys) != 3 {
		t.Fatalf("unionKeys = %v, want all three months: a member's month must not be dropped", keys)
	}
	want := []string{"2025-01", "2025-02", "2025-03"}
	for i, k := range want {
		if keys[i] != k {
			t.Fatalf("keys = %v, want %v in chronological order", keys, want)
		}
	}
	if labels["2025-03"] != "Mar 2025" {
		t.Errorf("label for 2025-03 = %q, want %q", labels["2025-03"], "Mar 2025")
	}
}

// A key present in several series must keep the first non-empty label rather
// than an empty one from whichever series happened to be scanned first.
func TestUnionKeysPrefersANonEmptyLabel(t *testing.T) {
	sets := [][]ChartPoint{
		{{Key: "2025-01", Label: ""}},
		{{Key: "2025-01", Label: "Jan 2025"}},
	}

	_, labels := unionKeys(sets)

	if labels["2025-01"] != "Jan 2025" {
		t.Errorf("label = %q, want the non-empty %q", labels["2025-01"], "Jan 2025")
	}
}

// A key that has no label anywhere must fall back to the key itself, so the
// axis never prints a blank tick.
func TestUnionKeysFallsBackToTheKeyWhenNoLabelExists(t *testing.T) {
	_, labels := unionKeys([][]ChartPoint{{{Key: "2025-05", Label: ""}}})

	if labels["2025-05"] != "2025-05" {
		t.Errorf("label = %q, want the key itself so the tick is not blank", labels["2025-05"])
	}
}

func TestUnionKeysOnNoSeries(t *testing.T) {
	keys, labels := unionKeys(nil)
	if len(keys) != 0 || len(labels) != 0 {
		t.Errorf("unionKeys(nil) = %v/%v, want empty", keys, labels)
	}
}

// A series with no points draws no line, so keeping it would add a legend entry
// pointing at nothing.
func TestPlottableSeriesDropsEmptySeries(t *testing.T) {
	in := []ChartSeries{
		{Label: "Alice", Points: []ChartPoint{{Key: "2025-01", Value: 10}}},
		{Label: "Ghost"},
		{Label: "Bob", Points: []ChartPoint{{Key: "2025-01", Value: 20}}},
	}

	got := plottableSeries(in)

	if len(got) != 2 {
		t.Fatalf("plottableSeries kept %d series, want 2: an empty series has no line to draw", len(got))
	}
	for _, s := range got {
		if s.Label == "Ghost" {
			t.Error("plottableSeries kept a series with no points; it would draw a legend entry pointing at nothing")
		}
		if len(s.Points) == 0 {
			t.Errorf("series %q kept with no points", s.Label)
		}
	}
}

// Nothing plottable must be nil, so the caller can omit the chart entirely
// rather than render an empty frame.
func TestPlottableSeriesIsNilWhenNothingHasPoints(t *testing.T) {
	if got := plottableSeries([]ChartSeries{{Label: "A"}, {Label: "B"}}); got != nil {
		t.Errorf("plottableSeries = %v, want nil so the chart is omitted", got)
	}
	if got := plottableSeries(nil); got != nil {
		t.Errorf("plottableSeries(nil) = %v, want nil", got)
	}
}

// A chart asked for with no plottable data must not emit a half-built SVG.
func TestRenderChartOmitsTheChartWhenNothingIsPlottable(t *testing.T) {
	got := string(RenderChart([]ChartSeries{{Label: "Empty"}}, ChartRenderOptions{}))

	if strings.Contains(got, "<svg") {
		t.Errorf("RenderChart emitted an SVG for a series with no points:\n%s", got)
	}
}
