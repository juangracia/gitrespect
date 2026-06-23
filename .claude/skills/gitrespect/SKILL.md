---
name: gitrespect
description: Run the gitrespect CLI to measure developer productivity from git history — lines added/deleted/net, commits, a personal baseline comparison, and opt-in flow/quality metrics (commit size, integration cadence, lead time, churn). Use this whenever the user wants to analyze git activity, measure their or a team's output, quantify the impact of an AI coding tool before/after, generate a productivity report (terminal/HTML/JSON), or asks "how productive have I been", "how much did I ship", "compare my output", "DORA metrics", or "the gitrespect report" — even if they don't name the tool explicitly.
---

# gitrespect

`gitrespect` is a Go CLI that analyzes git repositories and reports developer
productivity metrics for a given author and date range. Your job with this skill
is to pick the right invocation, run it, and explain the output in plain language.

## Prerequisites

- The target directory must be a git repository (or a parent of several when using `-r`).
- The binary is `gitrespect`. If it isn't on `PATH`, build it from the repo with
  `go build -o gitrespect ./cmd/gitrespect` and call `./gitrespect`.
- Check it's available first: `gitrespect version` (or `gitrespect --help`).

## The core command

```bash
gitrespect [paths...] [flags]
```

With no paths it analyzes the current directory. With no `--author` it uses
`git config user.email`. With no dates it covers the last 30 days.

Run it, read the output, then summarize for the user. Don't dump raw output and
stop — interpret it (see "Interpreting output").

## Choosing flags

Map what the user wants to the right flags. Common intents:

| User wants… | Invocation |
|-------------|-----------|
| My stats, last 30 days | `gitrespect` |
| A specific person | `gitrespect -a alice@example.com` |
| A specific window | `gitrespect -s 2025-01-01 -u 2025-06-30` |
| A whole year | `gitrespect --year=2025` |
| A monthly trend | `gitrespect --year=2025 -b monthly` |
| A team total | `gitrespect -t a@x.com,b@x.com,c@x.com --year=2025` |
| Several repos at once | `gitrespect ./api ./web ./gateway` |
| Every repo under a folder | `gitrespect -r ~/projects` |
| Per-repo breakdown | `gitrespect -r ~/projects --per-repo` |
| Exclude noise (vendored/generated) | `gitrespect -e 'vendor/*' -e '*.pb.go'` |
| An HTML report to share | `gitrespect --year=2025 -b monthly -o html -f report.html` |
| Machine-readable output | `gitrespect -o json -f stats.json` |

Dates accept absolute `YYYY-MM-DD` or relative `"30 days ago"`, `"6 months ago"`.

## Opt-in metrics

By default gitrespect shows lines, commits, daily average, and a **personal
baseline** (this period vs the author's own prior output). Deeper metrics run
extra git queries, so they're opt-in via `--metrics` (comma list or `all`):

```bash
gitrespect --metrics=all                       # every metric
gitrespect --metrics=commit-size,churn         # just these two
gitrespect --year=2025 -b monthly --metrics=all -o html -f report.html  # full report
```

| Value | Section | Meaning |
|-------|---------|---------|
| `commit-size` | Commit Size Distribution | % of commits that are micro (<10), small (10-99), medium (100-499), large (500+ lines) |
| `cadence` | Integration cadence | Median days between commits on the main branch (smaller = integrates more often) |
| `lead-time` | Lead time (branch → main) | Median days from a feature branch's first commit to its merge into main |
| `churn` | Churn rate | % of recently added lines rewritten within the churn window |

Tuning windows:
- `--baseline-window` (default `90d`) — how far back the personal baseline looks. Accepts `30d`, `90d`, `6m`, `1y`.
- `--churn-window` (default `30d`) — the churn lookback.
- `--legacy-benchmark` — bring back the deprecated Senior/Avg/Junior industry comparison instead of the personal baseline. Only use if the user explicitly asks for the old industry numbers.

"Flow" or "integration flow" in a user's request maps to `cadence` + `lead-time`.
"Velocity"/"throughput" maps to lines + commits + the personal baseline.

If a metric can't be computed (e.g. lead time with no branch merges in the
window, or a baseline with under ~30 days of prior history), gitrespect says so
instead of inventing a number. That's expected — report it honestly. When the
**personal baseline** is the user's actual question but comes back "insufficient
prior history," don't just shrug: add `-b monthly` so they can see their own
trend within the period as a proxy for "usual output," and explain why the
baseline couldn't be computed (see Gotchas).

## Before/after comparison (measuring AI tool impact)

The `compare` subcommand quantifies a productivity change between two periods —
the canonical use is "before vs after I adopted Copilot/Claude/Cursor":

```bash
gitrespect compare --before=2025-01:2025-06 --after=2025-07:2025-12
```

Periods are `YYYY-MM:YYYY-MM`. Output reports net lines/day for each period and
the multiplier between them. `compare` takes `[paths...]`, `-a/--author`,
`-e/--exclude`, `-o/--output`, `-f/--file`, and `--theme` — note it does **not**
take `-t/--team`; omit `-a` to include all authors, or run it per author for
per-person deltas.

## Output formats

- `terminal` (default) — ANSI-colored summary for the console.
- `html` (`-o html -f file.html`) — self-contained shareable report; `--theme dark` (default) or `--theme light`.
- `json` (`-o json -f file.json`) — structured data for further processing.

HTML and JSON require `-f/--file`. The HTML "full report" (all sections) comes
from combining `--metrics=all` with `-b monthly`.

## Interpreting output

When you summarize, lead with what the user actually cares about:

- **Net lines** = added − deleted. It's a volume signal, not a quality score — say so if a user over-indexes on it.
- **Personal baseline** is the headline comparison: e.g. "127 lines/day this period vs your usual 84 — about 51% above your normal." Frame relative to *their* baseline, never an industry average.
- **Commit size**: lots of large commits can mean infrequent integration; lots of micro commits can mean fix-up churn. Mention the shape, not just numbers.
- **Cadence / lead time**: lower is generally healthier (faster integration). High lead time suggests long-lived branches.
- **Churn**: high churn = lots of recently written code being rewritten, which can signal instability or active iteration. Context-dependent.

Always caveat that lines of code is one lens; reviews, design, and mentoring
don't show up here.

## Recipes

**"How productive was I this quarter?"**
```bash
gitrespect -s 2025-04-01 -u 2025-06-30 --metrics=all
```

**"Did Copilot make me faster?"**
```bash
gitrespect compare --before=2024-07:2024-12 --after=2025-01:2025-06 -a me@x.com
```
(For a whole team, run `compare` once per author and report each delta; `compare`
analyzes one author or all authors, not a custom team list.)

**"Give me a shareable report for my manager."**
```bash
gitrespect --year=2025 -b monthly --metrics=all -o html -f 2025-report.html
```

**"Analyze everything in my projects folder."**
```bash
gitrespect -r ~/projects --per-repo --year=2025
```

## Gotchas

- No author match → empty stats. Confirm the email matches the repo's commits (`git shortlog -sne`).
- **Baseline + `--year` (or any window covering all of a repo's history):** the baseline looks at history *before* the window start, so if the repo's first commit falls inside the analyzed window, the baseline is always "insufficient prior history." Widen the window backward, use a shorter analysis period with `-s/-u`, or fall back to `-b monthly` to show the within-period trend.
- Shallow clones undercount history; fetch full history for accurate baselines/lead time.
- `--metrics` and the personal baseline currently compute from the repo with the most of the author's commits when multiple repos are given.
- Quote relative dates: `-s "30 days ago"`.
