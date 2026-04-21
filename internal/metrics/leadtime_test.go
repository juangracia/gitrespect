package metrics

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

func runEnv(t *testing.T, env []string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
}

func TestLeadTime(t *testing.T) {
	r := newTestRepo(t)

	author := "Dev User <dev@example.com>"
	now := time.Date(2024, 6, 10, 12, 0, 0, 0, time.UTC)

	// Initial commit on main.
	r.writeFile("README.md", "init")
	r.commit("init", author, now.Add(-10*24*time.Hour))

	// Create feature branch.
	run(t, r.path, "git", "checkout", "-b", "feature")

	// First feature commit: day 0 of branch (t0 = now - 4 days).
	t0 := now.Add(-4 * 24 * time.Hour)
	r.writeFile("feature.go", "package main")
	r.commit("feat: first feature commit", author, t0)

	// Second feature commit: 3 days later.
	t1 := t0.Add(3 * 24 * time.Hour)
	r.writeFile("feature.go", "package main\n// done")
	r.commit("feat: second feature commit", author, t1)

	// Switch back to main and merge with --no-ff.
	run(t, r.path, "git", "checkout", "main")

	mergeTime := now
	dateStr := mergeTime.Format(time.RFC3339)
	dateEnv := []string{
		"GIT_AUTHOR_DATE=" + dateStr,
		"GIT_COMMITTER_DATE=" + dateStr,
		"GIT_AUTHOR_NAME=Dev User",
		"GIT_AUTHOR_EMAIL=dev@example.com",
		"GIT_COMMITTER_NAME=Dev User",
		"GIT_COMMITTER_EMAIL=dev@example.com",
	}
	runEnv(t, dateEnv, "git", "-C", r.path, "merge", "--no-ff", "-m", "Merge feature", "feature")

	since := now.Add(-30 * 24 * time.Hour)
	until := now.Add(24 * time.Hour)

	lt, err := ComputeLeadTime(r.path, "dev@example.com", since, until)
	if err != nil {
		t.Fatalf("ComputeLeadTime: %v", err)
	}

	if lt.Samples != 1 {
		t.Errorf("expected Samples=1, got %d", lt.Samples)
	}
	if lt.MedianDays < 3.9 || lt.MedianDays > 4.1 {
		t.Errorf("expected MedianDays ~4.0, got %f", lt.MedianDays)
	}
	if lt.MainBranch != "main" {
		t.Errorf("expected MainBranch=main, got %q", lt.MainBranch)
	}
}
