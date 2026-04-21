package metrics

import (
	"math"
	"testing"
	"time"
)

func TestCadence(t *testing.T) {
	r := newTestRepo(t)
	author := "Test <test@example.com>"
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	// 4 commits 2 days apart → 3 intervals of 2 days → median = 2.0
	for i := 0; i < 4; i++ {
		r.writeFile("f.txt", line(i))
		r.commit("c", author, base.Add(time.Duration(i*48)*time.Hour))
	}

	since := base.Add(-1 * time.Hour)
	until := base.Add(10 * 24 * time.Hour)

	c, err := ComputeCadence(r.path, "test@example.com", since, until)
	if err != nil {
		t.Fatalf("ComputeCadence: %v", err)
	}
	if c.Samples != 3 {
		t.Errorf("Samples=%d, want 3", c.Samples)
	}
	if math.Abs(c.MedianDaysBetween-2.0) > 0.01 {
		t.Errorf("MedianDays=%v, want 2.0", c.MedianDaysBetween)
	}
	if c.MainBranch != "main" {
		t.Errorf("MainBranch=%q, want main", c.MainBranch)
	}
}

func TestCadenceInsufficient(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("f.txt", "x\n")
	r.commit("only", "Test <test@example.com>", time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC))

	c, err := ComputeCadence(r.path, "test@example.com",
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Samples != 0 {
		t.Errorf("Samples=%d, want 0 (need 2+ commits)", c.Samples)
	}
}

func line(i int) string { return string(rune('a'+i)) + "\n" }
