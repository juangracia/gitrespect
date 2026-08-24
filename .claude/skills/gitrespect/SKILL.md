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
| A team with per-member metrics | `gitrespect -t a@x.com,b@x.com --year=2025 --metrics=all -b monthly` |
| A team before/after audit | `gitrespect compare -t a@x.com,b@x.com --before=2025-01:2025-06 --after=2025-07:2025-12` |
| A weekly or daily trend | `gitrespect --year=2025 -b weekly` |
| Several repos at once | `gitrespect ./api ./web ./gateway` |
| Every repo under a folder | `gitrespect -r ~/projects` |
| Per-repo breakdown (works in team mode too) | `gitrespect -r ~/projects --per-repo` |
| "Who is even on this team?" | `gitrespect -r ~/projects --top 10 --year=2025` |
| One person with several email addresses | `gitrespect --alias "Alice=a@corp.com,a@personal.com" -a Alice --year=2025` |
| A standing team roster | `gitrespect -r ~/projects --roster team.yaml --top 10 --year=2025` |
| Everyone's commits, unfiltered | `gitrespect -r ~/projects --all-authors --year=2025` |
| A trend chart in HTML | `gitrespect --year=2025 -b monthly -o html --chart -f report.html` |
| A weekly or daily trend chart | `gitrespect --year=2025 -b weekly -o html --chart -f report.html` |
| One member vs the team average | `gitrespect -t a@x.com,b@x.com --year=2025 -b monthly -o html --chart --highlight a@x.com -f t.html` |
| When inside each period it changed | `gitrespect compare --before=2025-01:2025-06 --after=2025-07:2025-12 -b monthly` |
| MR/PR volume per person | `gitrespect prs --provider gitlab --group my-group/web --year=2025 -b monthly` |
| MR volume before/after | `gitrespect compare --data=prs --group my-group/web --before=2025-01:2025-06 --after=2025-07:2025-12` |
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

Periods are `YYYY-MM:YYYY-MM` (a full `YYYY-MM-DD` also works). Output reports
net lines/day for each period and the multiplier between them. `compare` takes
`[paths...]`, `-a/--author`, `-t/--team`, `--all-authors`, `-r/--recursive`,
`-e/--exclude`, `-b/--breakdown`, `--roster`/`--alias`, `--data`, `-o/--output`,
`-f/--file`, and `--theme`.

`-b/--breakdown` shows each period's shape. The two periods are broken down
**separately**, since they can differ in length and need not be adjacent.

`--data=prs` compares merge request volume instead of lines, giving the same Nx
multiplier (add `--group` for GitLab or `--org` for GitHub).

For a whole team, pass `-t` and get the team total plus a per-member table:

```bash
gitrespect compare -t a@x.com,b@x.com,c@x.com --before=2025-01:2025-06 --after=2025-07:2025-12
```

Members with no output in the before period show `n/a` instead of a ratio.
Without `-a`, `-t` or `--all-authors`, compare uses the repo's configured
`user.email`.

## Merge requests and pull requests (`gitrespect prs`)

Git history cannot answer "how many MRs did X open per month". This subcommand
asks GitLab or GitHub directly and shapes the answer like the git reports.

```bash
gitrespect prs --provider gitlab --group my-group/web --year=2025 -b monthly
gitrespect prs --provider github --org my-org -a me@x.com --year=2025
```

| Flag | Meaning |
|------|---------|
| `--provider` | `gitlab` (default) or `github` |
| `--group` | GitLab group path or id; one query covers every project under it |
| `--org` | GitHub organization (use instead of `--group`) |
| `--token` | API token; prefer the `GITLAB_TOKEN` / `GITHUB_TOKEN` env var |
| `--api-url` | API root for self-hosted instances, e.g. `https://gitlab.corp.com` |
| `--map` | Pin a platform account to a person: `--map you@corp.com=handle` (repeatable) |
| `--top N` | Trim the contributor table to N rows; never trims the totals |

It also takes `-a/--author`, `-t/--team`, `--roster`/`--alias`, `-s/--since`,
`-u/--until`, `--year`, `-b/--breakdown`, `-o/--output`, `-f/--file`, `--theme`.

**Auth:** a token from `--token` or the env var, otherwise it shells out to a
locally authenticated `glab` or `gh`, which needs no token at all. Needs network
access, unlike every other command here.

**Identity matching is the weak seam.** Neither the GitLab group MR list nor the
GitHub search API returns a contributor email, so an email on `-t` often has no
authoritative counterpart. Matching is exact email/username first, then `--map`
pins, then a guarded name heuristic. Accounts that cannot be matched are counted
and named in the output rather than dropped, so if you see unmatched accounts,
resolve them with `--map` before quoting per-person numbers.

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
**"Did Copilot make the team faster?"**
```bash
gitrespect compare -t a@x.com,b@x.com,c@x.com --before=2024-07:2024-12 --after=2025-01:2025-06
```

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
- `-a/--author` resolves through a repo's `.mailmap` (git's `log.mailmap` defaults on), so an author's alternate addresses are merged automatically wherever a repo ships one. Usually desirable, but it means totals can differ per repo under `-r` depending on which repos carry a mailmap. Use `--roster`/`--alias` to apply the same identities everywhere.
- **Mailmap can also produce a confident zero.** A `.mailmap` maps addresses *away* as well as together, so asking for someone's OLD address in a repo that rewrites it returns 0 commits, not their work. If an active contributor reports nothing, run `git shortlog -sne` to see the mapped identities and use the canonical address. Don't report that zero as "they shipped nothing".
- **Baseline + `--year` (or any window covering all of a repo's history):** the baseline looks at history *before* the window start, so if the repo's first commit falls inside the analyzed window, the baseline is always "insufficient prior history." Widen the window backward, use a shorter analysis period with `-s/-u`, or fall back to `-b monthly` to show the within-period trend.
- Shallow clones undercount history; fetch full history for accurate baselines/lead time.
- `--metrics` and the personal baseline pool their samples across every repo analyzed. Medians pool the underlying samples rather than averaging per-repo medians, so the numbers describe the same work the totals do.
- Team mode (`-t`) honors `--metrics` (computed per member) and `--breakdown` at any granularity (team-wide). The personal baseline is single-author only and is not shown in team reports.
- `-a`, `-t`, `--all-authors` and `--top` are mutually exclusive: each selects who to count, so passing two is an error, not a silent winner.
- `--top` filters bot/CI identities by regex against both address and display name. Excluded identities are listed on stderr; if a real person is caught by it, name them with `-t`, which skips discovery. Add patterns with `--exclude-authors`.
- `-r` skips repos that share an `origin` remote with one already counted, warning on stderr. Repos with no readable remote are always kept.
- A roster (`--roster` / `--alias`) applies the same identities to every repo, unlike `.mailmap` which is per-repo. Listing one address under two people is rejected.
- `--chart` needs `-b/--breakdown` and `-o html`. It plots whatever granularity `--breakdown` names (monthly, weekly or daily), drawn from the same rows as the table so the two cannot disagree. When it cannot be drawn the report says why, rather than the chart being silently absent. `--highlight` needs a team.
- `prs` needs network access and either a token (`GITLAB_TOKEN`/`GITHUB_TOKEN`) or an authenticated `glab`/`gh`. Prefer the env var: `--token` is visible to anyone who can run `ps`.
- `prs` lead time uses real MR open→merge timestamps, which is a cleaner signal than the commit-graph heuristic behind `--metrics=lead-time`.
- `--breakdown` accepts `monthly`, `weekly` and `daily`; weeks are anchored on Monday.
- `--until` is inclusive of the unit named: `--until=2025-03-05` covers all of 5 March.
- Lead time needs merge commits, or a rebase/patch workflow that preserves author dates. Squash-merge repos rewrite both timestamps and report no signal; that is a git-history limitation, not a failure. Don't present it as zero.
- Quote relative dates: `-s "30 days ago"`.
