package metrics

import (
	"math"
	"strings"
	"testing"
	"time"
)

func churnLines(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString("line\n")
	}
	return b.String()
}

func TestChurnBasic(t *testing.T) {
	repo := newTestRepo(t)
	author := "Dev User <dev@example.com>"

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Commit at base: write 100 lines
	repo.writeFile("a.txt", churnLines(100))
	repo.commit("initial commit", author, base)

	// Commit at base+10 days: overwrite with 70 lines (deletes 30)
	repo.writeFile("a.txt", churnLines(70))
	repo.commit("shrink file", author, base.Add(10*24*time.Hour))

	since := base.Add(5 * 24 * time.Hour)
	until := base.Add(15 * 24 * time.Hour)
	window := 30 * 24 * time.Hour

	c, err := ComputeChurn(repo.path, "dev@example.com", since, until, window, nil)
	if err != nil {
		t.Fatalf("ComputeChurn error: %v", err)
	}

	if c.AddedLines != 100 {
		t.Errorf("AddedLines = %d, want 100", c.AddedLines)
	}
	if c.ChurnedLines != 30 {
		t.Errorf("ChurnedLines = %d, want 30", c.ChurnedLines)
	}
	if math.Abs(c.Ratio-0.30) > 0.01 {
		t.Errorf("Ratio = %.4f, want ~0.30", c.Ratio)
	}
	if c.WindowDays != 30 {
		t.Errorf("WindowDays = %d, want 30", c.WindowDays)
	}
}

func TestChurnNoActivity(t *testing.T) {
	repo := newTestRepo(t)
	author := "Dev User <dev@example.com>"

	// Single commit in 2020
	seed := time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC)
	repo.writeFile("seed.txt", churnLines(10))
	repo.commit("seed", author, seed)

	// Query in 2026 — no activity in the window
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	window := 30 * 24 * time.Hour

	c, err := ComputeChurn(repo.path, "dev@example.com", since, until, window, nil)
	if err != nil {
		t.Fatalf("ComputeChurn error: %v", err)
	}

	if c.AddedLines != 0 {
		t.Errorf("AddedLines = %d, want 0", c.AddedLines)
	}
	if c.Ratio != 0 {
		t.Errorf("Ratio = %.4f, want 0", c.Ratio)
	}
}
