package metrics

import (
	"strings"
	"testing"
	"time"
)

func lines(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString("line\n")
	}
	return b.String()
}

func TestCommitSize(t *testing.T) {
	author := "Test User <test@example.com>"
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("four buckets", func(t *testing.T) {
		repo := newTestRepo(t)

		// BucketMicro: single char change (<10 LOC total)
		repo.writeFile("micro.txt", "x")
		repo.commit("micro commit", author, base.Add(0))

		// BucketSmall: 20 lines added (10-99)
		repo.writeFile("small.txt", lines(20))
		repo.commit("small commit", author, base.Add(24*time.Hour))

		// BucketMedium: 150 lines added (100-499)
		repo.writeFile("medium.txt", lines(150))
		repo.commit("medium commit", author, base.Add(48*time.Hour))

		// BucketLarge: 600 lines added (500+)
		repo.writeFile("large.txt", lines(600))
		repo.commit("large commit", author, base.Add(72*time.Hour))

		since := base.Add(-time.Hour)
		until := base.Add(96 * time.Hour)

		dist, err := ComputeCommitSize(repo.path, author, since, until, nil)
		if err != nil {
			t.Fatalf("ComputeCommitSize: %v", err)
		}

		if dist.Total != 4 {
			t.Errorf("Total = %d, want 4", dist.Total)
		}
		for _, tc := range []struct {
			bucket SizeBucket
			name   string
		}{
			{BucketMicro, "Micro"},
			{BucketSmall, "Small"},
			{BucketMedium, "Medium"},
			{BucketLarge, "Large"},
		} {
			if dist.Counts[tc.bucket] != 1 {
				t.Errorf("Counts[%s] = %d, want 1", tc.name, dist.Counts[tc.bucket])
			}
		}
	})

	t.Run("empty window returns zero distribution", func(t *testing.T) {
		repo := newTestRepo(t)

		repo.writeFile("file.txt", lines(50))
		repo.commit("some commit", author, base)

		// Window entirely before the commit.
		since := base.Add(-48 * time.Hour)
		until := base.Add(-24 * time.Hour)

		dist, err := ComputeCommitSize(repo.path, author, since, until, nil)
		if err != nil {
			t.Fatalf("ComputeCommitSize: %v", err)
		}
		if dist.Total != 0 {
			t.Errorf("Total = %d, want 0", dist.Total)
		}
		for i, c := range dist.Counts {
			if c != 0 {
				t.Errorf("Counts[%d] = %d, want 0", i, c)
			}
		}
	})
}
