# Metrics Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Senior/Avg/Junior LOC benchmark with an opt-in metrics system (commit-size, cadence, lead-time, churn) plus a personal-history baseline, keeping legacy behavior behind `--legacy-benchmark`.

**Architecture:** New `internal/metrics/` package with one file per metric. The existing `git.Analyze()` stays unchanged. `cmd/root.go` parses new flags, builds a `metrics.Bundle`, passes it to `report.Terminal()` / `report.JSON()` which render optional sections.

**Tech Stack:** Go 1.22+, Cobra, standard `os/exec` for git, stdlib testing.

**Spec:** `docs/superpowers/specs/2026-04-21-metrics-redesign-design.md`

---

## File Structure

**Create:**
- `internal/metrics/options.go` — `Selection` struct + `ParseSelection`
- `internal/metrics/options_test.go`
- `internal/metrics/bundle.go` — `Bundle` struct
- `internal/metrics/baseline.go` — `Baseline` struct + `ComputeBaseline`
- `internal/metrics/baseline_test.go`
- `internal/metrics/commitsize.go` — `CommitSizeDistribution` + `ComputeCommitSize`
- `internal/metrics/commitsize_test.go`
- `internal/metrics/cadence.go` — `Cadence` + `ComputeCadence`
- `internal/metrics/cadence_test.go`
- `internal/metrics/leadtime.go` — `LeadTime` + `ComputeLeadTime`
- `internal/metrics/leadtime_test.go`
- `internal/metrics/churn.go` — `Churn` + `ComputeChurn`
- `internal/metrics/churn_test.go`
- `internal/metrics/testutil_test.go` — shared `setupRepo(t)` helper

**Modify:**
- `internal/cmd/root.go` — new flags, bundle assembly, call-through
- `internal/report/terminal.go` — render baseline + metric sections
- `internal/report/json.go` — serialize bundle in JSON output
- (No change to `internal/benchmark/industry.go` — kept for `--legacy-benchmark`)

---

## Task 1: Metric selection parsing (`options.go`)

**Files:**
- Create: `internal/metrics/options.go`
- Create: `internal/metrics/options_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/metrics/options_test.go
package metrics

import (
	"strings"
	"testing"
)

func TestParseSelection(t *testing.T) {
	tests := []struct {
		raw          string
		wantCS       bool
		wantCad      bool
		wantLT       bool
		wantChurn    bool
		wantErr      bool
		wantErrMatch string
	}{
		{raw: "", wantCS: false, wantCad: false, wantLT: false, wantChurn: false},
		{raw: "all", wantCS: true, wantCad: true, wantLT: true, wantChurn: true},
		{raw: "churn", wantChurn: true},
		{raw: "commit-size,cadence", wantCS: true, wantCad: true},
		{raw: "lead-time,churn,cadence,commit-size", wantCS: true, wantCad: true, wantLT: true, wantChurn: true},
		{raw: " churn , lead-time ", wantChurn: true, wantLT: true}, // trims whitespace
		{raw: "foo", wantErr: true, wantErrMatch: "foo"},
		{raw: "churn,bogus", wantErr: true, wantErrMatch: "bogus"},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			sel, err := ParseSelection(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tc.wantErrMatch) {
					t.Errorf("error %q does not contain %q", err, tc.wantErrMatch)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if sel.CommitSize != tc.wantCS {
				t.Errorf("CommitSize: got %v want %v", sel.CommitSize, tc.wantCS)
			}
			if sel.Cadence != tc.wantCad {
				t.Errorf("Cadence: got %v want %v", sel.Cadence, tc.wantCad)
			}
			if sel.LeadTime != tc.wantLT {
				t.Errorf("LeadTime: got %v want %v", sel.LeadTime, tc.wantLT)
			}
			if sel.Churn != tc.wantChurn {
				t.Errorf("Churn: got %v want %v", sel.Churn, tc.wantChurn)
			}
		})
	}
}

func TestSelectionAny(t *testing.T) {
	if (Selection{}).Any() {
		t.Error("empty selection should report Any() == false")
	}
	if !(Selection{Churn: true}).Any() {
		t.Error("Churn-only should report Any() == true")
	}
}
```

- [ ] **Step 2: Run tests; verify they fail**

```bash
cd /Users/juangracia/dev/gitrespect && go test ./internal/metrics/...
```
Expected: package does not exist; compile errors.

- [ ] **Step 3: Implement**

```go
// internal/metrics/options.go
package metrics

import (
	"fmt"
	"strings"
)

type Selection struct {
	CommitSize bool
	Cadence    bool
	LeadTime   bool
	Churn      bool
}

var validMetricNames = []string{"commit-size", "cadence", "lead-time", "churn"}

// ParseSelection parses a comma-separated list of metric names.
// "" → empty selection; "all" → all metrics; otherwise individual names.
// Returns error for unknown names, listing valid options.
func ParseSelection(raw string) (Selection, error) {
	var s Selection
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return s, nil
	}
	if raw == "all" {
		return Selection{CommitSize: true, Cadence: true, LeadTime: true, Churn: true}, nil
	}
	for _, part := range strings.Split(raw, ",") {
		name := strings.TrimSpace(part)
		switch name {
		case "commit-size":
			s.CommitSize = true
		case "cadence":
			s.Cadence = true
		case "lead-time":
			s.LeadTime = true
		case "churn":
			s.Churn = true
		default:
			return Selection{}, fmt.Errorf("unknown metric %q (valid: %s, or 'all')", name, strings.Join(validMetricNames, ", "))
		}
	}
	return s, nil
}

func (s Selection) Any() bool {
	return s.CommitSize || s.Cadence || s.LeadTime || s.Churn
}
```

- [ ] **Step 4: Run tests; verify pass**

```bash
go test ./internal/metrics/... -run TestParseSelection -run TestSelectionAny -v
```
Expected: PASS on all subtests.

- [ ] **Step 5: No commit yet — all metric tasks commit together at the end.**

---

## Task 2: Bundle type (`bundle.go`)

**Files:**
- Create: `internal/metrics/bundle.go`

- [ ] **Step 1: Implement (no tests — pure struct)**

```go
// internal/metrics/bundle.go
package metrics

type Bundle struct {
	Selection       Selection
	Baseline        *Baseline
	CommitSize      *CommitSizeDistribution
	Cadence         *Cadence
	LeadTime        *LeadTime
	Churn           *Churn
	LegacyBenchmark bool
}
```

- [ ] **Step 2: Verify compile (will fail until baseline/commitsize/etc are added, that's fine — fix progressively)**

---

## Task 3: Shared git-repo test helper (`testutil_test.go`)

**Files:**
- Create: `internal/metrics/testutil_test.go`

This helper is used by all integration-style metric tests.

- [ ] **Step 1: Implement helper**

```go
// internal/metrics/testutil_test.go
package metrics

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

type testRepo struct {
	t    *testing.T
	path string
}

func newTestRepo(t *testing.T) *testRepo {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "git", "init", "-q", "-b", "main")
	run(t, dir, "git", "config", "user.email", "test@example.com")
	run(t, dir, "git", "config", "user.name", "Test")
	run(t, dir, "git", "config", "commit.gpgsign", "false")
	return &testRepo{t: t, path: dir}
}

// writeFile writes content to a file in the repo and stages it.
func (r *testRepo) writeFile(name, content string) {
	r.t.Helper()
	p := filepath.Join(r.path, name)
	if err := writeAll(p, content); err != nil {
		r.t.Fatalf("writeFile: %v", err)
	}
	run(r.t, r.path, "git", "add", name)
}

// commit creates a commit with the given message and backdates it to ts.
// author format: "Name <email@example.com>"
func (r *testRepo) commit(msg, author string, ts time.Time) {
	r.t.Helper()
	env := []string{
		"GIT_AUTHOR_NAME=" + parseName(author),
		"GIT_AUTHOR_EMAIL=" + parseEmail(author),
		"GIT_COMMITTER_NAME=" + parseName(author),
		"GIT_COMMITTER_EMAIL=" + parseEmail(author),
		"GIT_AUTHOR_DATE=" + ts.Format(time.RFC3339),
		"GIT_COMMITTER_DATE=" + ts.Format(time.RFC3339),
	}
	cmd := exec.Command("git", "-C", r.path, "commit", "-q", "-m", msg)
	cmd.Env = append(cmd.Env, env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		r.t.Fatalf("commit failed: %v\n%s", err, out)
	}
}

// Helpers (keep at bottom of file)
func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
}

func writeAll(path, content string) error {
	f, err := osCreate(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(content)
	return err
}

// osCreate is a thin wrapper so we can mock later if needed.
var osCreate = func(path string) (interface{ WriteString(string) (int, error); Close() error }, error) {
	return osCreateReal(path)
}

func parseName(author string) string  {
	// "Name <email>" → "Name"
	if i := indexRune(author, '<'); i >= 0 {
		return trimSpace(author[:i])
	}
	return author
}

func parseEmail(author string) string {
	if i := indexRune(author, '<'); i >= 0 {
		if j := indexRune(author, '>'); j > i {
			return author[i+1 : j]
		}
	}
	return author
}

func indexRune(s string, r rune) int {
	for i, c := range s {
		if c == r {
			return i
		}
	}
	return -1
}

func trimSpace(s string) string {
	for len(s) > 0 && s[len(s)-1] == ' ' {
		s = s[:len(s)-1]
	}
	for len(s) > 0 && s[0] == ' ' {
		s = s[1:]
	}
	return s
}
```

NOTE: Use stdlib `os.Create` and `strings.Index`/`strings.TrimSpace` directly instead of the mock wrappers above. The above is over-engineered. Simplify to:

```go
import (
	"os"
	"strings"
)

func writeAll(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}

func parseName(author string) string {
	if i := strings.Index(author, "<"); i >= 0 {
		return strings.TrimSpace(author[:i])
	}
	return author
}

func parseEmail(author string) string {
	if i := strings.Index(author, "<"); i >= 0 {
		if j := strings.Index(author, ">"); j > i {
			return author[i+1 : j]
		}
	}
	return author
}
```

Remove the `osCreate`, `osCreateReal`, `indexRune`, `trimSpace` helpers entirely.

- [ ] **Step 2: Verify this helper file compiles alongside later tests.** No tests run yet (this is a helper-only file).

---

## Task 4: Commit size distribution (`commitsize.go`)

**Files:**
- Create: `internal/metrics/commitsize.go`
- Create: `internal/metrics/commitsize_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/metrics/commitsize_test.go
package metrics

import (
	"testing"
	"time"
)

func TestCommitSizeDistribution(t *testing.T) {
	r := newTestRepo(t)
	author := "Test <test@example.com>"
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	// Micro (< 10 lines total change)
	r.writeFile("a.txt", "one\n")
	r.commit("micro 1", author, base)

	// Small (10-99): add 20 lines
	r.writeFile("b.txt", lines(20))
	r.commit("small 1", author, base.Add(1*time.Hour))

	// Medium (100-499): add 150 lines
	r.writeFile("c.txt", lines(150))
	r.commit("medium 1", author, base.Add(2*time.Hour))

	// Large (500+): add 600 lines
	r.writeFile("d.txt", lines(600))
	r.commit("large 1", author, base.Add(3*time.Hour))

	since := base.Add(-1 * time.Hour)
	until := base.Add(4 * time.Hour)

	dist, err := ComputeCommitSize(r.path, "test@example.com", since, until, nil)
	if err != nil {
		t.Fatalf("ComputeCommitSize: %v", err)
	}
	if dist.Total != 4 {
		t.Errorf("Total=%d, want 4", dist.Total)
	}
	if dist.Counts[BucketMicro] != 1 {
		t.Errorf("Micro=%d, want 1", dist.Counts[BucketMicro])
	}
	if dist.Counts[BucketSmall] != 1 {
		t.Errorf("Small=%d, want 1", dist.Counts[BucketSmall])
	}
	if dist.Counts[BucketMedium] != 1 {
		t.Errorf("Medium=%d, want 1", dist.Counts[BucketMedium])
	}
	if dist.Counts[BucketLarge] != 1 {
		t.Errorf("Large=%d, want 1", dist.Counts[BucketLarge])
	}
	if got := dist.Percent(BucketMicro); got != 25.0 {
		t.Errorf("Percent(Micro)=%v, want 25.0", got)
	}
}

func TestCommitSizeEmpty(t *testing.T) {
	r := newTestRepo(t)
	// One commit to make repo non-empty, but outside the query window
	r.writeFile("a.txt", "x\n")
	r.commit("seed", "Test <test@example.com>", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))

	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	dist, err := ComputeCommitSize(r.path, "test@example.com", since, until, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dist.Total != 0 {
		t.Errorf("Total=%d, want 0", dist.Total)
	}
	if dist.Percent(BucketMicro) != 0 {
		t.Errorf("Percent on empty should be 0")
	}
}

// lines returns a string of n lines with content "line N\n".
func lines(n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += "line\n"
	}
	return out
}
```

- [ ] **Step 2: Run tests; verify they fail (compile errors — no implementation yet)**

```bash
go test ./internal/metrics/... -run TestCommitSize -v
```
Expected: compile errors.

- [ ] **Step 3: Implement**

```go
// internal/metrics/commitsize.go
package metrics

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type SizeBucket int

const (
	BucketMicro SizeBucket = iota
	BucketSmall
	BucketMedium
	BucketLarge
)

type CommitSizeDistribution struct {
	Counts [4]int
	Total  int
}

func (d CommitSizeDistribution) Percent(b SizeBucket) float64 {
	if d.Total == 0 {
		return 0
	}
	return float64(d.Counts[b]) * 100.0 / float64(d.Total)
}

// ComputeCommitSize bins each of the author's commits in [since, until] by total
// LOC changed (added + deleted, excluding binary files).
func ComputeCommitSize(repoPath, author string, since, until time.Time, exclude []string) (CommitSizeDistribution, error) {
	var dist CommitSizeDistribution

	args := []string{
		"-C", repoPath, "log",
		"--author=" + author,
		"--since=" + since.Format("2006-01-02"),
		"--until=" + until.Format("2006-01-02"),
		"--pretty=format:COMMIT %H",
		"--numstat",
	}
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return dist, fmt.Errorf("git log: %w", err)
	}

	var curTotal int
	flush := func() {
		if curTotal == 0 && !sawCommit {
			return
		}
		dist.Counts[bucketFor(curTotal)]++
		dist.Total++
		curTotal = 0
		sawCommit = false
	}
	// Use a small closure state machine via nested funcs — but Go can't close over vars declared later.
	// Rewrite iteratively:
	lines := strings.Split(string(out), "\n")
	inCommit := false
	for _, line := range lines {
		if strings.HasPrefix(line, "COMMIT ") {
			if inCommit {
				dist.Counts[bucketFor(curTotal)]++
				dist.Total++
				curTotal = 0
			}
			inCommit = true
			continue
		}
		if line == "" || !inCommit {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		if fields[0] == "-" || fields[1] == "-" {
			continue // binary
		}
		filename := strings.Join(fields[2:], " ")
		if shouldExcludeFile(filename, exclude) {
			continue
		}
		added, err1 := strconv.Atoi(fields[0])
		deleted, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		curTotal += added + deleted
	}
	if inCommit {
		dist.Counts[bucketFor(curTotal)]++
		dist.Total++
	}
	return dist, nil
}

func bucketFor(totalLOC int) SizeBucket {
	switch {
	case totalLOC < 10:
		return BucketMicro
	case totalLOC < 100:
		return BucketSmall
	case totalLOC < 500:
		return BucketMedium
	default:
		return BucketLarge
	}
}

// shouldExcludeFile mirrors git.shouldExclude but lives here to avoid coupling.
func shouldExcludeFile(filename string, patterns []string) bool {
	for _, p := range patterns {
		if m, _ := filepath.Match(p, filename); m {
			return true
		}
		if m, _ := filepath.Match(p, filepath.Base(filename)); m {
			return true
		}
		if strings.Contains(p, "/") {
			parts := strings.SplitN(p, "/", 2)
			if len(parts) == 2 && strings.HasPrefix(filename, parts[0]+"/") {
				if parts[1] == "*" {
					return true
				}
				if m, _ := filepath.Match(parts[1], filename[len(parts[0])+1:]); m {
					return true
				}
			}
		}
	}
	return false
}
```

NOTE: The `flush`/`sawCommit` closure in the draft doesn't compile — use the iterative version shown with `inCommit`. Drop the first attempt. (Agent: just implement the iterative loop verbatim.)

- [ ] **Step 4: Run tests; verify pass**

```bash
go test ./internal/metrics/... -run TestCommitSize -v
```
Expected: PASS.

---

## Task 5: Integration cadence (`cadence.go`)

**Files:**
- Create: `internal/metrics/cadence.go`
- Create: `internal/metrics/cadence_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/metrics/cadence_test.go
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

	// 4 commits 2 days apart → median interval = 2 days
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
```

- [ ] **Step 2: Run tests; verify they fail**

- [ ] **Step 3: Implement**

```go
// internal/metrics/cadence.go
package metrics

import (
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Cadence struct {
	MedianDaysBetween float64
	Samples           int
	MainBranch        string
}

func ComputeCadence(repoPath, author string, since, until time.Time) (Cadence, error) {
	var c Cadence
	branch := detectMainBranch(repoPath)
	if branch == "" {
		return c, nil
	}
	c.MainBranch = branch

	args := []string{
		"-C", repoPath, "log", branch,
		"--author=" + author,
		"--since=" + since.Format("2006-01-02"),
		"--until=" + until.Format("2006-01-02"),
		"--no-merges",
		"--format=%ct",
	}
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return c, fmt.Errorf("git log: %w", err)
	}
	var timestamps []int64
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		ts, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			continue
		}
		timestamps = append(timestamps, ts)
	}
	if len(timestamps) < 2 {
		return c, nil
	}
	sort.Slice(timestamps, func(i, j int) bool { return timestamps[i] < timestamps[j] })
	intervals := make([]float64, 0, len(timestamps)-1)
	for i := 1; i < len(timestamps); i++ {
		delta := float64(timestamps[i]-timestamps[i-1]) / 86400.0
		intervals = append(intervals, delta)
	}
	c.Samples = len(intervals)
	c.MedianDaysBetween = median(intervals)
	return c, nil
}

// detectMainBranch returns "main", "master", or the resolved origin HEAD.
// Empty string means no main-like branch found.
func detectMainBranch(repoPath string) string {
	// Try origin/HEAD
	if out, err := exec.Command("git", "-C", repoPath, "symbolic-ref", "refs/remotes/origin/HEAD").Output(); err == nil {
		ref := strings.TrimSpace(string(out))
		// ref looks like "refs/remotes/origin/main"
		if idx := strings.LastIndex(ref, "/"); idx >= 0 {
			name := ref[idx+1:]
			if branchExists(repoPath, name) {
				return name
			}
		}
	}
	for _, candidate := range []string{"main", "master"} {
		if branchExists(repoPath, candidate) {
			return candidate
		}
	}
	return ""
}

func branchExists(repoPath, name string) bool {
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "--verify", name)
	return cmd.Run() == nil
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sorted := append([]float64(nil), xs...)
	sort.Float64s(sorted)
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}
```

- [ ] **Step 4: Run tests; verify pass**

```bash
go test ./internal/metrics/... -run TestCadence -v
```

---

## Task 6: Lead time branch → main (`leadtime.go`)

**Files:**
- Create: `internal/metrics/leadtime.go`
- Create: `internal/metrics/leadtime_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/metrics/leadtime_test.go
package metrics

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLeadTime(t *testing.T) {
	r := newTestRepo(t)
	author := "Test <test@example.com>"

	// Create an initial commit on main
	r.writeFile("main.txt", "m0\n")
	r.commit("init", author, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))

	// Create a feature branch, make 2 commits over 3 days
	run(t, r.path, "git", "checkout", "-q", "-b", "feature")
	r.writeFile("f.txt", "f0\n")
	r.commit("f0", author, time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC))
	r.writeFile("f.txt", "f0\nf1\n")
	r.commit("f1", author, time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC))

	// Merge back to main with --no-ff to force a merge commit
	run(t, r.path, "git", "checkout", "-q", "main")
	runEnv(t, r.path,
		[]string{
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@example.com",
			"GIT_AUTHOR_DATE=" + time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
			"GIT_COMMITTER_DATE=" + time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
		},
		"git", "-C", r.path, "merge", "-q", "--no-ff", "-m", "merge feature", "feature")

	lt, err := ComputeLeadTime(r.path, "test@example.com",
		time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ComputeLeadTime: %v", err)
	}
	if lt.Samples != 1 {
		t.Fatalf("Samples=%d, want 1", lt.Samples)
	}
	// Branch started 2026-03-02 12:00 UTC, merged 2026-03-06 12:00 UTC → 4 days
	if lt.MedianDays < 3.9 || lt.MedianDays > 4.1 {
		t.Errorf("MedianDays=%v, want ~4", lt.MedianDays)
	}
}

// runEnv: like run but with explicit env
func runEnv(t *testing.T, dir string, env []string, name string, args ...string) {
	t.Helper()
	_ = dir
	cmd := exec.Command(name, args...)
	cmd.Env = append(cmd.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
}

// silence unused import warning in some builds
var _ = filepath.Base
```

- [ ] **Step 2: Run; verify fail**

- [ ] **Step 3: Implement**

```go
// internal/metrics/leadtime.go
package metrics

import (
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

type LeadTime struct {
	MedianDays float64
	Samples    int
	MainBranch string
}

func ComputeLeadTime(repoPath, author string, since, until time.Time) (LeadTime, error) {
	var lt LeadTime
	branch := detectMainBranch(repoPath)
	if branch == "" {
		return lt, nil
	}
	lt.MainBranch = branch

	// List merge commits on the first-parent chain of main within the window.
	// Filter by author.
	args := []string{
		"-C", repoPath, "log", branch,
		"--merges", "--first-parent",
		"--author=" + author,
		"--since=" + since.Format("2006-01-02"),
		"--until=" + until.Format("2006-01-02"),
		"--format=%H %P %ct",
	}
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return lt, fmt.Errorf("git log merges: %w", err)
	}
	var durations []float64
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		// parts: [mergeSHA parent1 parent2 ... mergeCT]
		// Last field = commit timestamp. Between SHA and timestamp are parents.
		mergeCT, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
		if err != nil {
			continue
		}
		parents := parts[1 : len(parts)-1]
		if len(parents) < 2 {
			continue // not a real merge
		}
		p1 := parents[0]
		p2 := parents[1]
		// Find the oldest commit reachable from p2 but not p1
		oldestCT, ok := oldestBranchCommitTS(repoPath, p1, p2)
		if !ok {
			continue
		}
		delta := float64(mergeCT-oldestCT) / 86400.0
		durations = append(durations, delta)
	}
	lt.Samples = len(durations)
	if lt.Samples > 0 {
		sort.Float64s(durations)
		lt.MedianDays = median(durations)
	}
	return lt, nil
}

func oldestBranchCommitTS(repoPath, p1, p2 string) (int64, bool) {
	// git log p1..p2 --format=%ct --reverse → first line is oldest
	out, err := exec.Command("git", "-C", repoPath, "log",
		p1+".."+p2, "--format=%ct", "--reverse").Output()
	if err != nil {
		return 0, false
	}
	line := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	if line == "" {
		return 0, false
	}
	ts, err := strconv.ParseInt(line, 10, 64)
	if err != nil {
		return 0, false
	}
	return ts, true
}
```

- [ ] **Step 4: Run tests; verify pass**

```bash
go test ./internal/metrics/... -run TestLeadTime -v
```

---

## Task 7: Churn (`churn.go`)

**Files:**
- Create: `internal/metrics/churn.go`
- Create: `internal/metrics/churn_test.go`

**Simpler variant per spec:** `churn_ratio = lines_deleted_in_period_by_author / lines_added_in_prior_window_by_author`. Documents as "fraction of added lines rewritten within window".

- [ ] **Step 1: Write failing test**

```go
// internal/metrics/churn_test.go
package metrics

import (
	"testing"
	"time"
)

func TestChurn(t *testing.T) {
	r := newTestRepo(t)
	author := "Test <test@example.com>"
	base := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)

	// Day 0: add 100 lines
	r.writeFile("a.txt", lines(100))
	r.commit("init 100", author, base)

	// Day 10: delete 30 lines (keep first 70)
	r.writeFile("a.txt", lines(70))
	r.commit("drop 30", author, base.Add(10*24*time.Hour))

	// Period: the day-10 commit and window = 30d prior (covers day 0)
	since := base.Add(5 * 24 * time.Hour)
	until := base.Add(15 * 24 * time.Hour)

	churn, err := ComputeChurn(r.path, "test@example.com", since, until, 30*24*time.Hour, nil)
	if err != nil {
		t.Fatalf("ComputeChurn: %v", err)
	}
	if churn.AddedLines != 100 {
		t.Errorf("AddedLines=%d, want 100", churn.AddedLines)
	}
	if churn.ChurnedLines != 30 {
		t.Errorf("ChurnedLines=%d, want 30", churn.ChurnedLines)
	}
	if ratio := churn.Ratio; ratio < 0.29 || ratio > 0.31 {
		t.Errorf("Ratio=%v, want ~0.30", ratio)
	}
}

func TestChurnZeroAdded(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "x\n")
	r.commit("seed", "Test <test@example.com>", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))

	c, err := ComputeChurn(r.path, "test@example.com",
		time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		30*24*time.Hour, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.AddedLines != 0 {
		t.Errorf("AddedLines=%d, want 0", c.AddedLines)
	}
	if c.Ratio != 0 {
		t.Errorf("Ratio=%v, want 0", c.Ratio)
	}
}
```

- [ ] **Step 2: Run; verify fail**

- [ ] **Step 3: Implement**

```go
// internal/metrics/churn.go
package metrics

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type Churn struct {
	WindowDays   int
	AddedLines   int
	ChurnedLines int
	Ratio        float64
}

// ComputeChurn estimates the fraction of lines added by `author` in the
// `window` period preceding `until` that were subsequently deleted by the
// same author within [since, until].
//
// Simplified heuristic: added_in_window_before_period / deleted_in_period
// approximates the "rewrite ratio" without doing line-level content matching.
// This matches the precision of common churn reporting tools.
func ComputeChurn(repoPath, author string, since, until time.Time, window time.Duration, exclude []string) (Churn, error) {
	c := Churn{WindowDays: int(window.Hours() / 24)}

	priorStart := since.Add(-window)
	added, err := sumAdded(repoPath, author, priorStart, since, exclude)
	if err != nil {
		return c, err
	}
	deleted, err := sumDeleted(repoPath, author, since, until, exclude)
	if err != nil {
		return c, err
	}
	c.AddedLines = added
	c.ChurnedLines = deleted
	if added > 0 {
		c.Ratio = float64(deleted) / float64(added)
	}
	return c, nil
}

func sumAdded(repoPath, author string, since, until time.Time, exclude []string) (int, error) {
	added, _, err := sumNumstat(repoPath, author, since, until, exclude)
	return added, err
}
func sumDeleted(repoPath, author string, since, until time.Time, exclude []string) (int, error) {
	_, deleted, err := sumNumstat(repoPath, author, since, until, exclude)
	return added_ignored(deleted), err
}

// added_ignored just passes through — exists only to keep the sumDeleted signature symmetric with sumAdded
// (remove in cleanup — see note below).

func sumNumstat(repoPath, author string, since, until time.Time, exclude []string) (int, int, error) {
	args := []string{
		"-C", repoPath, "log",
		"--author=" + author,
		"--since=" + since.Format("2006-01-02"),
		"--until=" + until.Format("2006-01-02"),
		"--pretty=format:",
		"--numstat",
	}
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return 0, 0, fmt.Errorf("git log: %w", err)
	}
	totalAdded, totalDeleted := 0, 0
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		if fields[0] == "-" || fields[1] == "-" {
			continue
		}
		filename := strings.Join(fields[2:], " ")
		if shouldExcludeFile(filename, exclude) {
			continue
		}
		a, err1 := strconv.Atoi(fields[0])
		d, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		totalAdded += a
		totalDeleted += d
	}
	return totalAdded, totalDeleted, nil
}
```

**NOTE for implementing agent:** The above draft has `added_ignored` cruft and wraps `sumNumstat` unnecessarily. Simplify to a single call:

```go
func ComputeChurn(repoPath, author string, since, until time.Time, window time.Duration, exclude []string) (Churn, error) {
	c := Churn{WindowDays: int(window.Hours() / 24)}
	priorStart := since.Add(-window)
	added, _, err := sumNumstat(repoPath, author, priorStart, since, exclude)
	if err != nil {
		return c, err
	}
	_, deleted, err := sumNumstat(repoPath, author, since, until, exclude)
	if err != nil {
		return c, err
	}
	c.AddedLines = added
	c.ChurnedLines = deleted
	if added > 0 {
		c.Ratio = float64(deleted) / float64(added)
	}
	return c, nil
}
```

Drop `sumAdded`, `sumDeleted`, `added_ignored`. Keep only `sumNumstat`.

- [ ] **Step 4: Run tests; verify pass**

```bash
go test ./internal/metrics/... -run TestChurn -v
```

---

## Task 8: Baseline (`baseline.go`)

**Files:**
- Create: `internal/metrics/baseline.go`
- Create: `internal/metrics/baseline_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/metrics/baseline_test.go
package metrics

import (
	"math"
	"testing"
	"time"
)

func TestBaseline(t *testing.T) {
	r := newTestRepo(t)
	author := "Test <test@example.com>"

	// Baseline window: 90 days prior to periodStart → add 900 lines over 60 working days
	// We'll keep it simple: commit 10 lines per day for 90 days prior.
	periodStart := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 90; i++ {
		day := periodStart.Add(time.Duration(-(90 - i)) * 24 * time.Hour)
		r.writeFile("a.txt", lines(10*(i+1)))
		r.commit("d", author, day.Add(12*time.Hour))
	}

	b, err := ComputeBaseline(r.path, "test@example.com", periodStart, 90*24*time.Hour, nil)
	if err != nil {
		t.Fatalf("ComputeBaseline: %v", err)
	}
	if b.InsufficientHistory {
		t.Errorf("should not be flagged insufficient")
	}
	// Each commit adds 10 net lines (rewriting full file adds 10 new + deletes prior).
	// Net growth total = 900 lines over ~64 working days → ~14 net/day.
	// We only assert LOCPerDay > 0 to keep the test deterministic across git versions.
	if b.LOCPerDay <= 0 {
		t.Errorf("LOCPerDay=%v, want > 0", b.LOCPerDay)
	}
	// Window end must be periodStart; start must be periodStart - 90d
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
```

- [ ] **Step 2: Run; verify fail**

- [ ] **Step 3: Implement**

```go
// internal/metrics/baseline.go
package metrics

import (
	"time"

	"github.com/juangracia/gitrespect/internal/git"
)

type Baseline struct {
	WindowStart         time.Time
	WindowEnd           time.Time
	WorkingDays         int
	LOCPerDay           float64 // net LOC/day during baseline window
	InsufficientHistory bool
	PeriodLOCPerDay     float64 // set by caller after computing; 0 by default
	PercentDelta        float64 // computed when SetPeriod is called
}

// ComputeBaseline runs git.Analyze on the window [periodStart - window, periodStart)
// and returns the resulting net LOC/day. If the actual commit activity span in
// the window is under 30 days, marks InsufficientHistory.
func ComputeBaseline(repoPath, author string, periodStart time.Time, window time.Duration, exclude []string) (Baseline, error) {
	b := Baseline{
		WindowStart: periodStart.Add(-window),
		WindowEnd:   periodStart,
	}
	stats, err := git.Analyze(repoPath, author, b.WindowStart, b.WindowEnd, exclude)
	if err != nil {
		return b, err
	}
	if stats.FirstCommit.IsZero() || stats.LastCommit.IsZero() {
		b.InsufficientHistory = true
		return b, nil
	}
	activitySpanDays := int(stats.LastCommit.Sub(stats.FirstCommit).Hours() / 24)
	if activitySpanDays < 30 {
		b.InsufficientHistory = true
		return b, nil
	}
	b.WorkingDays = git.WorkingDays(b.WindowStart, b.WindowEnd)
	if b.WorkingDays > 0 {
		b.LOCPerDay = float64(stats.Net) / float64(b.WorkingDays)
	}
	return b, nil
}

// SetPeriod computes the comparison against the current period's LOC/day.
func (b *Baseline) SetPeriod(periodLOCPerDay float64) {
	b.PeriodLOCPerDay = periodLOCPerDay
	if b.InsufficientHistory || b.LOCPerDay == 0 {
		return
	}
	b.PercentDelta = (periodLOCPerDay - b.LOCPerDay) / b.LOCPerDay * 100
}
```

- [ ] **Step 4: Run tests; verify pass**

```bash
go test ./internal/metrics/... -run TestBaseline -v
```

---

## Task 9: Wire CLI flags (`cmd/root.go`)

**Files:**
- Modify: `/Users/juangracia/dev/gitrespect/internal/cmd/root.go`

- [ ] **Step 1: Add new package-level vars**

In the `var (...)` block (currently lines 14-27), append:

```go
	metricsFlag     string
	baselineWindow  string
	churnWindow     string
	legacyBenchmark bool
```

- [ ] **Step 2: Register flags in `init()` (after existing flags)**

```go
	rootCmd.Flags().StringVar(&metricsFlag, "metrics", "", "Opt-in metrics: comma list of churn,lead-time,commit-size,cadence, or 'all'")
	rootCmd.Flags().StringVar(&baselineWindow, "baseline-window", "90d", "Personal baseline window (e.g. 30d, 90d, 6m, 1y)")
	rootCmd.Flags().StringVar(&churnWindow, "churn-window", "30d", "Churn detection window")
	rootCmd.Flags().BoolVar(&legacyBenchmark, "legacy-benchmark", false, "Show deprecated Senior/Avg/Junior comparison instead of personal baseline")
```

- [ ] **Step 3: Add a duration parser helper at the bottom of the file**

```go
func parseWindow(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("empty window")
	}
	if len(raw) < 2 {
		return 0, fmt.Errorf("invalid window %q (examples: 30d, 90d, 6m, 1y)", raw)
	}
	unit := raw[len(raw)-1]
	n, err := strconv.Atoi(raw[:len(raw)-1])
	if err != nil {
		return 0, fmt.Errorf("invalid window %q: %w", raw, err)
	}
	switch unit {
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	case 'w':
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	case 'm':
		return time.Duration(n) * 30 * 24 * time.Hour, nil
	case 'y':
		return time.Duration(n) * 365 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown unit %q in %q (use d/w/m/y)", string(unit), raw)
	}
}
```

Add `"strconv"` and `"strings"` to imports if not present.

- [ ] **Step 4: Wire metric selection + bundle in `runAnalyze` (just before the `// Generate output` switch)**

Replace the block:

```go
	// Aggregate stats
	combined := git.CombineStats(allStats)
```

with:

```go
	// Aggregate stats
	combined := git.CombineStats(allStats)

	// Parse metric options
	selection, err := metrics.ParseSelection(metricsFlag)
	if err != nil {
		return err
	}
	bWindow, err := parseWindow(baselineWindow)
	if err != nil {
		return fmt.Errorf("invalid --baseline-window: %w", err)
	}
	cWindow, err := parseWindow(churnWindow)
	if err != nil {
		return fmt.Errorf("invalid --churn-window: %w", err)
	}

	if legacyBenchmark && selection.Any() {
		fmt.Fprintln(os.Stderr, "warning: --legacy-benchmark ignored for new metrics (they coexist with personal baseline)")
	}

	bundle := metrics.Bundle{Selection: selection, LegacyBenchmark: legacyBenchmark}

	// Personal baseline (skip if --legacy-benchmark explicitly requested)
	if !legacyBenchmark {
		// Use first path for baseline (baseline is per-repo in v1; combined not supported)
		baseline, err := metrics.ComputeBaseline(paths[0], authorEmail, sinceTime, bWindow, exclude)
		if err == nil {
			locPerDay := 0.0
			wd := git.WorkingDays(sinceTime, untilTime)
			if wd > 0 {
				locPerDay = float64(combined.Net) / float64(wd)
			}
			baseline.SetPeriod(locPerDay)
			bundle.Baseline = &baseline
		}
	}

	// Opt-in metrics (v1: computed on first path only)
	if selection.CommitSize {
		if d, err := metrics.ComputeCommitSize(paths[0], authorEmail, sinceTime, untilTime, exclude); err == nil {
			bundle.CommitSize = &d
		}
	}
	if selection.Cadence {
		if c, err := metrics.ComputeCadence(paths[0], authorEmail, sinceTime, untilTime); err == nil {
			bundle.Cadence = &c
		}
	}
	if selection.LeadTime {
		if lt, err := metrics.ComputeLeadTime(paths[0], authorEmail, sinceTime, untilTime); err == nil {
			bundle.LeadTime = &lt
		}
	}
	if selection.Churn {
		if ch, err := metrics.ComputeChurn(paths[0], authorEmail, sinceTime, untilTime, cWindow, exclude); err == nil {
			bundle.Churn = &ch
		}
	}
```

Add import: `"github.com/juangracia/gitrespect/internal/metrics"`.

- [ ] **Step 5: Update output dispatch to pass bundle to reporters**

Replace:

```go
	// Generate output
	switch output {
	case "json":
		return report.JSON(combined, file, breakdown)
	case "html":
		return report.HTML(combined, file, breakdown, theme)
	default:
		if perRepo && len(allStats) > 1 {
			return report.TerminalWithRepos(combined, allStats, breakdown)
		}
		return report.Terminal(combined, breakdown)
	}
```

with:

```go
	// Generate output
	switch output {
	case "json":
		return report.JSON(combined, file, breakdown, bundle)
	case "html":
		return report.HTML(combined, file, breakdown, theme)
	default:
		if perRepo && len(allStats) > 1 {
			return report.TerminalWithRepos(combined, allStats, breakdown, bundle)
		}
		return report.Terminal(combined, breakdown, bundle)
	}
```

- [ ] **Step 6: `go build` should fail on reporter signature mismatch.** That's expected — Task 10 fixes it.

---

## Task 10: Terminal renderer (`report/terminal.go`)

**Files:**
- Modify: `/Users/juangracia/dev/gitrespect/internal/report/terminal.go`

- [ ] **Step 1: Update signatures**

Replace `func Terminal(stats git.RepoStats, breakdown string) error` with:
```go
func Terminal(stats git.RepoStats, breakdown string, bundle metrics.Bundle) error
```

Same for `TerminalWithRepos`: add `bundle metrics.Bundle` parameter.

Add import: `"github.com/juangracia/gitrespect/internal/metrics"`.

- [ ] **Step 2: Replace the industry-comparison block**

Find the block starting with `// Industry comparison - only show for periods >= 30 days` (around line 66).

Replace the whole `if workingDays >= 21 { ... } else { ... }` block with:

```go
	// Baseline or legacy benchmark
	if bundle.LegacyBenchmark {
		if workingDays >= 21 {
			comparisons := benchmark.Compare(locPerDay)
			fmt.Printf("  %svs Industry:%s\n", colorDim, colorReset)
			for i, c := range comparisons {
				prefix := "├──"
				if i == len(comparisons)-1 {
					prefix = "└──"
				}
				bar := renderBar(c.Multiplier, 20)
				fmt.Printf("  %s %s (%d/day): %s%.1fx%s %s\n",
					prefix, c.Label, c.Benchmark, colorYellow, c.Multiplier, colorReset, bar)
			}
		} else {
			fmt.Printf("  %sPace:%s %.0f lines/day\n", colorDim, colorReset, locPerDay)
			fmt.Printf("  %s(Industry comparison requires 30+ days of activity)%s\n", colorDim, colorReset)
		}
	} else if bundle.Baseline != nil {
		b := bundle.Baseline
		fmt.Printf("  %sBaseline (%d day prior):%s\n", colorDim, int(b.WindowEnd.Sub(b.WindowStart).Hours()/24), colorReset)
		if b.InsufficientHistory {
			fmt.Printf("  └── %sinsufficient prior history%s\n", colorDim, colorReset)
		} else {
			arrow := "→"
			sign := "+"
			color := colorGreen
			if b.PercentDelta < 0 {
				sign = ""
				color = colorYellow
			}
			fmt.Printf("  └── Your normal: %.0f lines/day %s this period: %.0f (%s%s%.0f%%%s)\n",
				b.LOCPerDay, arrow, b.PeriodLOCPerDay, color, sign, b.PercentDelta, colorReset)
		}
	}
	fmt.Println()
```

- [ ] **Step 3: Add metric sections at the end (before `if breakdown == "monthly"`)**

```go
	// Opt-in metric sections
	renderMetrics(bundle)
```

Then add the new function at the bottom of the file:

```go
func renderMetrics(b metrics.Bundle) {
	if b.CommitSize != nil {
		d := b.CommitSize
		fmt.Printf("  %sCommit size distribution:%s\n", colorDim, colorReset)
		rows := []struct {
			label  string
			bucket metrics.SizeBucket
		}{
			{"Micro (<10)", metrics.BucketMicro},
			{"Small (10-99)", metrics.BucketSmall},
			{"Medium (100-499)", metrics.BucketMedium},
			{"Large (500+)", metrics.BucketLarge},
		}
		for i, row := range rows {
			prefix := "├──"
			if i == len(rows)-1 {
				prefix = "└──"
			}
			pct := d.Percent(row.bucket)
			bar := renderBar(pct/10, 20)
			fmt.Printf("  %s %-18s %3.0f%%  %s\n", prefix, row.label+":", pct, bar)
		}
		fmt.Println()
	}
	if b.Cadence != nil {
		c := b.Cadence
		fmt.Printf("  %sIntegration cadence:%s\n", colorDim, colorReset)
		if c.MainBranch == "" {
			fmt.Printf("  └── %sno main branch detected%s\n", colorDim, colorReset)
		} else if c.Samples < 1 {
			fmt.Printf("  └── %sinsufficient data (need 2+ commits on %s)%s\n", colorDim, c.MainBranch, colorReset)
		} else {
			fmt.Printf("  └── Median %.1f days between commits to %s\n", c.MedianDaysBetween, c.MainBranch)
		}
		fmt.Println()
	}
	if b.LeadTime != nil {
		lt := b.LeadTime
		fmt.Printf("  %sLead time (branch → main):%s\n", colorDim, colorReset)
		if lt.MainBranch == "" {
			fmt.Printf("  └── %sno main branch detected%s\n", colorDim, colorReset)
		} else if lt.Samples == 0 {
			fmt.Printf("  └── %sno merges in period%s\n", colorDim, colorReset)
		} else {
			fmt.Printf("  └── Median %.1f days (%d merges analyzed)\n", lt.MedianDays, lt.Samples)
		}
		fmt.Println()
	}
	if b.Churn != nil {
		c := b.Churn
		fmt.Printf("  %sChurn rate:%s\n", colorDim, colorReset)
		if c.AddedLines == 0 {
			fmt.Printf("  └── %sno added lines to analyze%s\n", colorDim, colorReset)
		} else {
			fmt.Printf("  └── %.0f%% of added lines rewritten within %d days\n", c.Ratio*100, c.WindowDays)
		}
		fmt.Println()
	}
}
```

- [ ] **Step 4: Update `TerminalWithRepos` to pass bundle through to `Terminal`**

```go
func TerminalWithRepos(combined git.RepoStats, repos []git.RepoStats, breakdown string, bundle metrics.Bundle) error {
	if err := Terminal(combined, breakdown, bundle); err != nil {
		return err
	}
	// ... rest unchanged
}
```

- [ ] **Step 5: `go build`**

```bash
cd /Users/juangracia/dev/gitrespect && go build ./...
```

Expected: succeeds (JSON signature still breaks — that's Task 11).

---

## Task 11: JSON reporter (`report/json.go`)

**Files:**
- Modify: `/Users/juangracia/dev/gitrespect/internal/report/json.go`

- [ ] **Step 1: Read current signature to understand existing shape**

```bash
cat /Users/juangracia/dev/gitrespect/internal/report/json.go
```

- [ ] **Step 2: Update the public function signature**

Change:
```go
func JSON(stats git.RepoStats, file, breakdown string) error
```

to:
```go
func JSON(stats git.RepoStats, file, breakdown string, bundle metrics.Bundle) error
```

Add import `"github.com/juangracia/gitrespect/internal/metrics"`.

- [ ] **Step 3: Extend the serialized struct to include bundle**

Add a `Metrics *metricsPayload` field (name as appropriate for existing struct). Define:

```go
type metricsPayload struct {
	Baseline   *metrics.Baseline               `json:"baseline,omitempty"`
	CommitSize *metrics.CommitSizeDistribution `json:"commit_size,omitempty"`
	Cadence    *metrics.Cadence                `json:"cadence,omitempty"`
	LeadTime   *metrics.LeadTime               `json:"lead_time,omitempty"`
	Churn      *metrics.Churn                  `json:"churn,omitempty"`
}
```

Populate it from `bundle` before marshaling.

- [ ] **Step 4: `go build`**

```bash
go build ./...
```
Expected: success.

---

## Task 12: Full build + test sweep

- [ ] **Step 1: Full build**

```bash
cd /Users/juangracia/dev/gitrespect && go build ./...
```
Expected: no errors.

- [ ] **Step 2: Full test suite**

```bash
go test ./...
```
Expected: all tests pass. If any existing tests break, fix them (likely in `cmd/root_test.go` if one exists, or because of signature changes).

- [ ] **Step 3: Lint**

```bash
make lint 2>/dev/null || gofmt -l . | grep -v vendor | head
```
Fix any formatting issues with `gofmt -w`.

---

## Task 13: Manual smoke tests

- [ ] **Step 1: Build binary locally**

```bash
cd /Users/juangracia/dev/gitrespect && make build
```

- [ ] **Step 2: Default invocation (baseline replaces Senior/Avg/Junior)**

```bash
./gitrespect -s "90 days ago"
```
Expected:
- No "vs Industry" section.
- "Baseline (90 day prior):" section with either a comparison or "insufficient prior history".

- [ ] **Step 3: `--metrics=all` shows all 4 new sections**

```bash
./gitrespect -s "90 days ago" --metrics=all
```
Expected: Commit size distribution, Integration cadence, Lead time, Churn rate each present.

- [ ] **Step 4: `--metrics=churn` shows only churn**

```bash
./gitrespect -s "90 days ago" --metrics=churn
```

- [ ] **Step 5: `--legacy-benchmark` restores old behavior**

```bash
./gitrespect -s "90 days ago" --legacy-benchmark
```
Expected: "vs Industry" block returns.

- [ ] **Step 6: Invalid metric name → clear error**

```bash
./gitrespect --metrics=foo
```
Expected: stderr: `unknown metric "foo" (valid: commit-size, cadence, lead-time, churn, or 'all')`; non-zero exit.

- [ ] **Step 7: JSON output with metrics**

```bash
./gitrespect --metrics=all -o json -s "90 days ago" | head -80
```
Expected: JSON contains top-level `metrics` object with nested metric results.

- [ ] **Step 8: Test against another local repo**

```bash
./gitrespect --metrics=all -s "90 days ago" ~/dev/yspent
```
Expected: no crash, realistic numbers.

---

## Task 14: Commit + push

- [ ] **Step 1: Review diff**

```bash
cd /Users/juangracia/dev/gitrespect && git status && git diff --stat
```

- [ ] **Step 2: Commit spec + plan + implementation in one commit**

```bash
git add docs/superpowers/ internal/metrics/ internal/cmd/root.go internal/report/terminal.go internal/report/json.go
git commit -m "$(cat <<'EOF'
feat: replace industry benchmark with personal baseline + opt-in metrics

- Remove Senior/Avg/Junior comparison from default terminal output
- Add --metrics flag (commit-size, cadence, lead-time, churn, or all)
- Add personal baseline using prior N days (default 90d)
- Keep legacy behavior behind --legacy-benchmark flag
- Serialize new metrics in JSON output
EOF
)"
```

- [ ] **Step 3: Push**

```bash
GH_TOKEN=$(gh auth token --user juangracia) git push origin main
```

(Use `GH_TOKEN` per user's global CLAUDE.md; plain `git push origin main` also works if credentials are cached.)

---

## Self-Review (filled out)

**Spec coverage:**
- Goals 1-5 → Tasks 1, 8, 9, 10
- New metrics (4) → Tasks 4-7
- Baseline → Task 8
- CLI flags → Task 9
- Terminal output → Task 10
- JSON output → Task 11
- Legacy flag → Task 10 (renderer branches on `LegacyBenchmark`)
- Edge cases (no main, no merges, insufficient history, invalid metric name, empty add) → Tasks 5-8, 9 (error handling), 10 (rendering)
- Build/test gate → Task 12
- Smoke tests → Task 13
- Commit/push → Task 14

**Placeholder scan:** No TBDs. The Task 4 and Task 7 code blocks contain an "initial draft" + simplified version; the NOTE under each tells the agent which to implement. Non-ambiguous.

**Type consistency:** `Selection`, `Bundle`, `Baseline`, `CommitSizeDistribution`, `Cadence`, `LeadTime`, `Churn` all match between task definitions and usage. Field names stable.

## Execution

Per user instruction, implementation runs via parallel Sonnet subagents. Tasks 1-8 are independent (different files) and dispatched in parallel. Tasks 9-11 are sequential (modify shared files). Tasks 12-14 are human-driven verification.
