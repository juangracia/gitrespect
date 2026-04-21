package metrics

import (
	"math"
	"testing"
	"time"
)

func baselineLines(n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += "line\n"
	}
	return out
}

func TestBaseline(t *testing.T) {
	r := newTestRepo(t)
	author := "Test <test@example.com>"

	// Baseline window: 90 days prior to periodStart.
	// Commit each day with a growing file so activity spans >= 30 days.
	periodStart := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 90; i++ {
		day := periodStart.Add(time.Duration(-(90 - i)) * 24 * time.Hour)
		r.writeFile("a.txt", baselineLines(10*(i+1)))
		r.commit("d", author, day.Add(12*time.Hour))
	}

	b, err := ComputeBaseline(r.path, "test@example.com", periodStart, 90*24*time.Hour, nil)
	if err != nil {
		t.Fatalf("ComputeBaseline: %v", err)
	}
	if b.InsufficientHistory {
		t.Errorf("should not be flagged insufficient")
	}
	if b.LOCPerDay <= 0 {
		t.Errorf("LOCPerDay=%v, want > 0", b.LOCPerDay)
	}
	if !b.WindowEnd.Equal(periodStart) {
		t.Errorf("WindowEnd=%v, want %v", b.WindowEnd, periodStart)
	}
	want := periodStart.Add(-90 * 24 * time.Hour)
	if math.Abs(float64(b.WindowStart.Sub(want))) > float64(time.Second) {
		t.Errorf("WindowStart=%v, want %v", b.WindowStart, want)
	}
}

func TestBaselineInsufficient(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("x.txt", "x\n")
	r.commit("only", "Test <test@example.com>", time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC))

	b, err := ComputeBaseline(r.path, "test@example.com",
		time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		90*24*time.Hour, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !b.InsufficientHistory {
		t.Error("expected InsufficientHistory=true for <30 days of activity")
	}
}
