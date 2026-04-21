# Metrics Redesign — Design Spec

Date: 2026-04-21
Status: Approved for implementation

## Table of Contents

1. [Motivation](#motivation)
2. [Goals and Non-Goals](#goals-and-non-goals)
3. [Design Decisions](#design-decisions)
4. [Architecture](#architecture)
5. [CLI Contract](#cli-contract)
6. [Terminal Output Format](#terminal-output-format)
7. [Data Structures](#data-structures)
8. [Algorithms](#algorithms)
9. [Edge Cases and Error Handling](#edge-cases-and-error-handling)
10. [Testing Strategy](#testing-strategy)
11. [Scope Matrix](#scope-matrix)
12. [Out of Scope (v2)](#out-of-scope-v2)

## Motivation

The current report shows a "vs Industry" block comparing the user's LOC/day against three benchmarks (Senior=20, Avg=50, Junior=100). This creates two problems:

1. **Counterintuitive presentation.** A "senior" appears to produce 5x less than a "junior", which reads as an error even though the rationale (seniors refactor more, write more reusable code, mentor) is defensible as a qualitative trend.
2. **Dubious provenance.** The specific numbers 20/50/100 have no documented source. They are an informal extrapolation from Brooks (1975, OS/360), McConnell (1990s commercial projects, wide range 1.5–125), and Capers Jones (1980s, 16–38). None of those authors use the tripartite senior/average/junior labeling, and none of those numbers account for modern AI-assisted coding.
3. **LOC as a single productivity metric is weak.** It varies by language, era, and tooling, penalizes code deletion (often the most valuable contribution), and is trivially manipulable.

This redesign replaces the external benchmark with a per-author historical baseline and adds four opt-in metrics that are more robust signals of work pattern and quality.

## Goals and Non-Goals

### Goals

- Remove the confusing Senior/Avg/Junior comparison from the default terminal output.
- Replace it with a period-relative personal baseline (compare this period to the same author's prior N days).
- Add four opt-in metrics, all computable from `git log` alone without external APIs.
- Keep the legacy behavior available behind `--legacy-benchmark` flag for users who want it.
- Preserve backward compatibility: the `gitrespect` default invocation still produces a report with LOC and working-day averages.

### Non-Goals

- Team mode integration (deferred to v2).
- `compare` command integration (deferred to v2).
- HTML output support for new metrics (deferred to v2 — JSON output is v1).
- Per-repo breakdown of new metrics (they only appear in combined/single-repo output).
- Integration with GitHub/GitLab APIs for PR data.
- DORA metrics that require CI/CD data (Deployment Frequency, Change Failure Rate, Time to Restore).

## Design Decisions

Captured from brainstorming session:

| # | Decision | Rationale |
|---|---|---|
| 1 | Personal baseline replaces Senior/Avg/Junior by default; old block behind `--legacy-benchmark`. | Kills confusion without losing functionality. |
| 2 | v1 ships four new metrics: commit size distribution, integration cadence, lead time branch→main, churn rate. Bus factor deferred to v2. | Balances coverage (includes one DORA, one quality signal, two hygiene signals) against implementation scope. |
| 3 | Opt-in via `--metrics=churn,lead-time,commit-size,cadence` with `all` shortcut. | Follows existing `--output=` pattern, granular without flag proliferation. |
| 4 | Baseline is period-relative: 90 days prior to the requested period start. Configurable via `--baseline-window=Nd`. | Adapts to historical queries (e.g., `--year=2024`), compares to pre-period state, not overlapping or everything-ever. |

## Architecture

A new `internal/metrics/` package, one file per metric, keeps each metric isolable and testable.

```
internal/
├── cmd/
│   └── root.go                  Parses --metrics, --baseline-window, --legacy-benchmark
├── git/
│   └── analyzer.go              Unchanged public API. New helper AnalyzeRange() for baseline queries.
├── metrics/
│   ├── options.go               Selection struct, ParseSelection("all" | "churn,..." | "")
│   ├── baseline.go              Baseline: run git.Analyze on the prior window, compute LOC/day
│   ├── commitsize.go            CommitSizeDistribution: bin commits by total LOC
│   ├── cadence.go               Cadence: median days between same-author commits on main
│   ├── leadtime.go              LeadTime: median age of feature branches at merge
│   └── churn.go                 Churn: fraction of added lines deleted within 30 days
├── benchmark/
│   └── industry.go              Unchanged. Only used when --legacy-benchmark is set.
└── report/
    ├── terminal.go              Renders LOC + baseline + each opt-in metric section
    └── json.go                  Serializes opt-in metrics into output JSON
```

**Flow:**

```
runAnalyze(cmd, args)
  ├─ parse --metrics → metrics.Selection
  ├─ parse --baseline-window → time.Duration
  ├─ parse --legacy-benchmark → bool
  ├─ git.Analyze(path, author, since, until, exclude) → RepoStats
  ├─ if !legacyBenchmark:
  │    metrics.ComputeBaseline(path, author, since, baselineWindow) → Baseline
  ├─ if selection.CommitSize: metrics.CommitSizeDistribution(path, author, since, until) → ...
  ├─ if selection.Cadence:    metrics.Cadence(path, author, since, until) → ...
  ├─ if selection.LeadTime:   metrics.LeadTime(path, author, since, until) → ...
  ├─ if selection.Churn:      metrics.Churn(path, author, since, until, churnWindow) → ...
  └─ report.Terminal(stats, bundle)  // bundle carries all optional metric results
```

## CLI Contract

New flags on the root `gitrespect` command:

| Flag | Default | Description |
|---|---|---|
| `--metrics=<list>` | `""` (none) | Comma-separated: `churn`, `lead-time`, `commit-size`, `cadence`. Special value `all` enables all four. |
| `--baseline-window=<duration>` | `90d` | How far back the personal baseline reaches. Formats: `30d`, `90d`, `6m`, `1y`. |
| `--churn-window=<duration>` | `30d` | Window for churn detection (lines added then removed within). |
| `--legacy-benchmark` | `false` | Show the deprecated Senior/Avg/Junior block. Mutually exclusive with new baseline block. |

If both `--metrics=` and `--legacy-benchmark` are passed, `--metrics=` wins for the benchmark slot and a warning is printed to stderr. (New metrics are independent and always rendered.)

Invalid metric names in `--metrics=` produce a clear error listing valid options.

## Terminal Output Format

Default invocation (no `--metrics=`) with a baseline available:

```
✦ gitrespect - juanmgracia@gmail.com
gitrespect (Jan 20 2026 to Apr 20 2026)
──────────────────────────────────────────────────

  Added       Deleted     Net         Commits
  ─────────────────────────────────────────────
  8,432       3,215       5,217       127

  Daily avg: 82 lines/day (65 working days)
  Activity:  Jan 22 2026 to Apr 18 2026

  Baseline (90d prior):
  └── Your normal: 58 lines/day → this period: 82 (+41%) ↑
```

With `--metrics=all`, the sections below are appended. Each section is independent and only prints if its metric was selected.

```
  Commit size distribution:
  ├── Micro (<10):        34%  ████████░░░░░░░░░░░░
  ├── Small (10-99):      48%  ██████████░░░░░░░░░░
  ├── Medium (100-499):   15%  ███░░░░░░░░░░░░░░░░░
  └── Large (500+):       3%   █░░░░░░░░░░░░░░░░░░░

  Integration cadence:
  └── Median 2.1 days between commits to main

  Lead time (branch → main):
  └── Median 1.8 days (32 merges analyzed)

  Churn rate:
  └── 18% of added lines rewritten within 30 days
```

With `--legacy-benchmark`, the baseline block is replaced by the existing "vs Industry" block (no change to that section's rendering).

## Data Structures

```go
// internal/metrics/options.go
package metrics

type Selection struct {
    CommitSize bool
    Cadence    bool
    LeadTime   bool
    Churn      bool
}

// ParseSelection parses CLI input. Accepted: "", "all", or comma-separated list.
// Valid names: "commit-size", "cadence", "lead-time", "churn".
// Returns an error on unknown names.
func ParseSelection(raw string) (Selection, error)

// Any reports whether at least one metric is selected.
func (s Selection) Any() bool
```

```go
// internal/metrics/baseline.go
type Baseline struct {
    WindowStart         time.Time
    WindowEnd           time.Time
    WorkingDays         int
    LOCPerDay           float64
    InsufficientHistory bool   // true if < 30 days of activity in window
    PeriodLOCPerDay     float64 // the period being compared
    PercentDelta        float64 // (period - baseline) / baseline * 100
}

func ComputeBaseline(repoPath, author string, periodStart time.Time, window time.Duration, exclude []string) (Baseline, error)
```

```go
// internal/metrics/commitsize.go
type SizeBucket int
const (
    BucketMicro  SizeBucket = iota // < 10 LOC
    BucketSmall                    // 10-99
    BucketMedium                   // 100-499
    BucketLarge                    // 500+
)

type CommitSizeDistribution struct {
    Counts    [4]int  // indexed by SizeBucket
    Total     int
}

func (d CommitSizeDistribution) Percent(b SizeBucket) float64

func ComputeCommitSize(repoPath, author string, since, until time.Time, exclude []string) (CommitSizeDistribution, error)
```

```go
// internal/metrics/cadence.go
type Cadence struct {
    MedianDaysBetween float64
    Samples           int    // number of intervals measured
    MainBranch        string // "main" or "master"; "" if not found
}

func ComputeCadence(repoPath, author string, since, until time.Time) (Cadence, error)
```

```go
// internal/metrics/leadtime.go
type LeadTime struct {
    MedianDays float64
    Samples    int     // number of merge commits analyzed
    MainBranch string
}

func ComputeLeadTime(repoPath, author string, since, until time.Time) (LeadTime, error)
```

```go
// internal/metrics/churn.go
type Churn struct {
    WindowDays   int
    AddedLines   int
    ChurnedLines int    // lines both added and later deleted within WindowDays
    Ratio        float64 // ChurnedLines / AddedLines, 0 if AddedLines == 0
}

func ComputeChurn(repoPath, author string, since, until time.Time, window time.Duration, exclude []string) (Churn, error)
```

```go
// internal/metrics/bundle.go
// Collected optional metrics for passing to the reporter.
type Bundle struct {
    Selection              Selection
    Baseline               *Baseline
    CommitSize             *CommitSizeDistribution
    Cadence                *Cadence
    LeadTime               *LeadTime
    Churn                  *Churn
    LegacyBenchmark        bool
}
```

## Algorithms

### Baseline

Run `git.Analyze()` on a shifted window: `[periodStart - baselineWindow, periodStart)`. Compute LOC/day using `git.WorkingDays()` (existing helper). If the baseline window has fewer than 30 days of actual activity (measured via first/last commit in window), mark `InsufficientHistory = true` and skip the comparison line (terminal renderer shows "*insufficient prior history*").

### Commit size distribution

Iterate over commits returned by `git log --numstat` (already done in `git.Analyze`). For each commit, sum added+deleted lines and assign to the corresponding bucket. This can be piggybacked on the existing `Analyze` loop by returning a parallel slice, OR computed with a second pass. **Decision: second pass** — keeps `Analyze` unchanged and the metric self-contained. Performance cost is acceptable (single additional `git log` invocation).

### Integration cadence

Detect the main branch:
1. Try `git symbolic-ref refs/remotes/origin/HEAD` → last component.
2. Fall back to `main`, then `master`, checking `git rev-parse --verify <name>`.

Run `git log --format='%ae %ct' <main> --since=... --until=... --author=<author> --no-merges`. Extract commit timestamps, sort ascending, compute deltas between consecutive commits, return median in days. Need at least 2 commits for a sample.

### Lead time branch → main

Run `git log --merges --first-parent <main> --since=... --until=... --format='%H %P %ct'` on the main branch. For each merge commit M with parents P1 (main) and P2 (branch tip):
1. Find the oldest commit reachable from P2 but not P1: `git log P1..P2 --format='%ct' --reverse | head -1`.
2. Lead time = merge_commit_time - oldest_branch_commit_time.

Filter to merges authored by the target author. Compute median. Samples = number of merges analyzed.

### Churn

For each commit C by the target author in [since, until]:
1. Get the added lines (`git show C --unified=0 --no-color` → parse `+` lines, excluding `+++` headers).
2. For each added line L, check whether any subsequent commit within `window` (default 30d) deletes L from the same file.

Implementation: use `git log -p --unified=0` to stream all diffs in chronological order. Maintain a map `(file, line_content) → first_seen_time`. When a `-` line appears for a file with a matching recent `+`, increment ChurnedLines.

Caveats: line content can appear multiple times; we use `(file, stripped_content)` as key but collisions are possible. This is an approximation, matching the precision of common churn tools. Document the caveat.

For v1, implement a simpler variant: count total lines deleted in the window from files touched by the author, divided by total lines added by the author in the prior window. Same magnitude, much simpler. **Decision: use the simpler variant for v1.** Document this in code comments.

## Edge Cases and Error Handling

| Scenario | Behavior |
|---|---|
| Repo has no `main`/`master` | Cadence and LeadTime print `no main branch detected` instead of a value. |
| Author has 0 commits in period | All metric sections print `no data` in place of values; LOC section renders normally. |
| `--metrics=foo` with invalid name | Fatal error, list valid names: `commit-size, cadence, lead-time, churn, all`. |
| Baseline window has <30 days of activity | Baseline block prints `insufficient prior history`, no comparison line. |
| `--legacy-benchmark` + `--metrics=...` | New metrics render; legacy block replaces baseline; stderr warning. |
| Period has fewer than 2 main-branch commits (cadence) | Cadence prints `insufficient data (need 2+ commits)`. |
| Period has 0 merge commits (lead time) | Lead time prints `no merges in period`. |
| Churn has 0 added lines in prior window | Churn prints `no added lines to analyze`. |
| `--baseline-window=` parse failure | Fatal error with format examples. |

No fallbacks for missing data — we render an explicit message, never silent zero. We never crash on empty input.

## Testing Strategy

- **Unit tests per metric** (`internal/metrics/*_test.go`). Each metric gets a table-driven test with synthetic `git log` output fixtures and expected parsed results. Use `testdata/` for multi-line fixtures.
- **Integration tests** (`internal/metrics/integration_test.go`). Create a temporary git repo with `git init`, `git commit`, known content, run the `Compute*` functions against it, assert results. Reuse a shared `setupRepo(t)` helper.
- **CLI smoke tests** (`cmd/root_test.go`, new). Invoke `runAnalyze` with various flag combinations against a fixture repo, capture terminal output, assert sections are present/absent correctly.
- **Regression**: existing tests in `git/analyzer_test.go` must still pass unchanged.
- **Build gate**: `make build` and `make test` must pass before commit. If either fails, fix and retry.

## Scope Matrix

| Scenario | v1 | v2 (deferred) |
|---|---|---|
| `gitrespect` (analyze, single author, terminal) | ✅ Full | — |
| `--output=json` | ✅ Serialize new metrics | — |
| `--output=html` | ❌ LOC-only | ✅ |
| `compare` command | ❌ Unchanged | ✅ |
| `--team` mode | ❌ Unchanged (no new metrics) | ✅ |
| `--per-repo` | New metrics only on combined | Per-repo breakdowns |
| Bus factor metric | ❌ | ✅ |
| DORA deployment frequency (via CI) | ❌ | ✅ (needs external data) |

## Out of Scope (v2)

- Bus factor per module.
- Lead time / cadence / churn in `compare` command output.
- Per-member rollup of new metrics in `--team` mode.
- HTML rendering of new metrics.
- Config file (`.gitrespect.yml`) for per-repo defaults.
- DORA metrics requiring CI/CD integration.
