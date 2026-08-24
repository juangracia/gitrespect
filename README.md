# gitrespect

> Respect your git work with real metrics

[![CI](https://github.com/juangracia/gitrespect/actions/workflows/ci.yml/badge.svg)](https://github.com/juangracia/gitrespect/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/juangracia/gitrespect)](https://goreportcard.com/report/github.com/juangracia/gitrespect)
[![Latest release](https://img.shields.io/github/v/release/juangracia/gitrespect)](https://github.com/juangracia/gitrespect/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A fast CLI tool that analyzes git repositories and provides comprehensive developer productivity metrics. **Measure the real impact of AI tools on your productivity**, track team contributions, and benchmark against your own personal baseline.

No agent, no sign-up, no instrumentation: gitrespect reads plain git history, so it works **retroactively** on any repository you already have.

![gitrespect report](screenshots/report-full.png)

## Table of Contents

- [Why gitrespect?](#why-gitrespect)
- [Features](#features)
- [Installation](#installation)
- [Usage](#usage)
  - [Basic Analysis](#basic-analysis)
  - [Measure AI Impact (Before/After Comparison)](#measure-ai-impact-beforeafter-comparison)
    - [Team AI Adoption Audit](#team-ai-adoption-audit)
    - [When Inside Each Period Did It Change?](#when-inside-each-period-did-it-change)
  - [Team Analysis](#team-analysis)
  - [Build the Team Automatically](#build-the-team-automatically)
  - [Merging One Person's Multiple Addresses](#merging-one-persons-multiple-addresses)
  - [Per-Repository Team Breakdown](#per-repository-team-breakdown)
  - [Duplicate Repository Detection](#duplicate-repository-detection)
  - [Analyze Specific Path](#analyze-specific-path)
  - [Multiple Repositories](#multiple-repositories)
  - [Scan Directory for Repos](#scan-directory-for-repos)
  - [Filter by Year](#filter-by-year)
  - [Breakdowns](#breakdowns)
  - [Custom Date Range](#custom-date-range)
  - [Filter by Author](#filter-by-author)
  - [Export to HTML](#export-to-html)
  - [HTML Theme Options](#html-theme-options)
  - [Opt-in Metrics](#opt-in-metrics)
  - [Export to JSON](#export-to-json)
  - [Team HTML Report](#team-html-report)
  - [Trend Chart](#trend-chart)
- [Merge Requests and Pull Requests](#merge-requests-and-pull-requests)
- [All Options](#all-options)
- [Personal Baseline](#personal-baseline)
- [For AI Agents](#for-ai-agents)
- [How It Works](#how-it-works)
- [Use Cases](#use-cases)
- [Contributing](#contributing)
- [Author](#author)
- [License](#license)

## Why gitrespect?

**Measure AI Impact on Productivity**

The rise of AI coding assistants (Copilot, Claude, Cursor) is changing how we write code. But how do you know if it's actually making you more productive? gitrespect lets you:

- Compare your output before vs after adopting AI tools
- Quantify the productivity multiplier with real data
- Track team-wide AI adoption impact
- Generate shareable reports for stakeholders

## Features

- **AI Productivity Comparison** - Measure before/after impact of AI tools on your workflow
- **Personal Baseline** - Compare this period against your own normal output (no arbitrary industry numbers)
- **Flow & Quality Metrics** (opt-in) - Commit size distribution, integration cadence, lead time (branch → main), and churn
- **Team Analysis** - Analyze multiple contributors as a team or organization, with a per-repository rollup
- **Automatic Team Discovery** - `--top N` ranks real contributors and filters out CI and bot accounts
- **Identity Merging** - One person, many email addresses, counted once, via `.mailmap` or a roster
- **Merge Request / Pull Request Metrics** - GitLab and GitHub MR volume and true open-to-merge lead time
- **Lines of Code** - Track added, deleted, and net lines across repositories
- **Multi-repo Support** - Analyze many repositories at once, with duplicate clones detected and skipped
- **Multiple Output Formats** - Terminal, HTML reports (dark/light themes, optional trend chart), JSON export
- **AI Agent Skill** - Bundled [skill](.claude/skills/gitrespect/SKILL.md) so Claude Code / Codex can run gitrespect for you

## Installation

### Homebrew (macOS/Linux)

```bash
brew install juangracia/gitrespect/gitrespect
```

### Using Go

```bash
go install github.com/juangracia/gitrespect/cmd/gitrespect@latest
```

### Download Binary

Download the latest release from [GitHub Releases](https://github.com/juangracia/gitrespect/releases).

#### macOS

```bash
# Apple Silicon (M1/M2/M3)
curl -L https://github.com/juangracia/gitrespect/releases/latest/download/gitrespect-darwin-arm64.tar.gz | tar xz
sudo mv gitrespect /usr/local/bin/

# Intel Mac
curl -L https://github.com/juangracia/gitrespect/releases/latest/download/gitrespect-darwin-amd64.tar.gz | tar xz
sudo mv gitrespect /usr/local/bin/
```

#### Linux

```bash
# x86_64
curl -L https://github.com/juangracia/gitrespect/releases/latest/download/gitrespect-linux-amd64.tar.gz | tar xz
sudo mv gitrespect /usr/local/bin/

# ARM64
curl -L https://github.com/juangracia/gitrespect/releases/latest/download/gitrespect-linux-arm64.tar.gz | tar xz
sudo mv gitrespect /usr/local/bin/
```

#### Windows

```powershell
# Download from GitHub Releases
# Extract gitrespect-windows-amd64.zip
# Add to PATH or move to a directory in your PATH
```

### Build from Source

```bash
git clone https://github.com/juangracia/gitrespect.git
cd gitrespect
go build -o gitrespect ./cmd/gitrespect
```

## Usage

### Basic Analysis

Run in any git repository to see your contribution stats for the last 30 days:

```bash
gitrespect
```

Output:
```
 gitrespect - developer@example.com
my-project (Dec 4 2025 to Jan 3 2026)
──────────────────────────────────────────────────

  Added       Deleted     Net         Commits
  ────────────────────────────────────────────
  2,847       312         2,535       47

  Daily avg: 127 lines/day (22 working days)

  Baseline (90d prior):
  └── Your normal: 84 lines/day → this period: 127 (+51% ↑)
```

By default gitrespect compares this period against **your own baseline** computed
from the prior 90 days of history (configurable via `--baseline-window`). Add
`--metrics` to opt into deeper flow and quality metrics (see [Opt-in Metrics](#opt-in-metrics)).

### Measure AI Impact (Before/After Comparison)

The killer feature: measure how AI tools have changed your productivity.

```bash
gitrespect compare --before=2025-01:2025-07 --after=2025-08:2025-12
```

![gitrespect compare](screenshots/compare-report.png)

Output:
```
 gitrespect - Period Comparison
──────────────────────────────────────────────────

  Period           Net Lines   Days    Per Day
  ─────────────    ──────────  ──────  ────────
  2025-01:2025-07  6,308       154     41
  2025-08:2025-12  32,164      110     292

  Change: +7.1x productivity increase 🚀
```

**Use cases:**
- Before/after adopting GitHub Copilot
- Before/after switching to Claude or Cursor
- Comparing productivity across different project phases
- Quantifying the ROI of AI tools for your team

#### Team AI Adoption Audit

Add `--team` to compare a whole group across the same two periods. You get
the team total plus each member's individual change, which is what an
adoption audit actually needs:

```bash
gitrespect compare --team=dev1@company.com,dev2@company.com,dev3@company.com \
  --before=2025-01:2025-06 --after=2025-07:2025-12
```

```
 gitrespect - Team Period Comparison
──────────────────────────────────────────────────────────────────

  Period          Net Lines   Days    Per Day
  ────────────────────────────────────────────
  2025-01:2025-06 18,204      129     141
  2025-07:2025-12 41,880      131     320

  Team change: +2.3x productivity 📈

  Per Member
  Contributor          Before     After      Change
  ──────────────────────────────────────────────────
  dev1@company.com     9,120      21,400     +2.3x
  dev2@company.com     6,984      15,300     +2.2x
  dev3@company.com     2,100      5,180      +2.5x
```

Members with no output in the "before" period report `n/a` rather than a
meaningless ratio.

#### When Inside Each Period Did It Change?

`compare` takes `-b/--breakdown` to show the shape of each period, not just its
total:

```bash
gitrespect compare --before=2025-01:2025-06 --after=2025-07:2025-12 -b monthly
```

The two periods are broken down **separately**, under their own headings. They
can be different lengths and need not be adjacent, so a single spanning series
would either invent empty buckets for the gap or imply the periods are
contiguous when they are not.

`compare` also accepts `--all-authors` to compare a whole repository or
organisation without listing every contributor, `-r` to scan recursively, and
`--roster`/`--alias` to merge split identities the same way the main command
does.

### Team Analysis

Analyze contributions across your entire team:

```bash
gitrespect --team=dev1@company.com,dev2@company.com,dev3@company.com --year=2025
```

Output:
```
 gitrespect - Team Report
Jan 1 2025 to Dec 31 2025
────────────────────────────────────────────────────────────

  Team Totals
  Added       Deleted     Net         Commits
  ────────────────────────────────────────────
  45,230      3,127       42,103      312

  Team daily avg: 162 lines/day (260 working days)

  Team Members
  Contributor                         Net       Commits  /day
  ────────────────────────────────────────────────────────
  dev1@company.com                    18,450    128      71
  dev2@company.com                    15,230    98       59
  dev3@company.com                    8,423     86       32
```

Team mode also honors `--metrics` and `--breakdown`: add `--metrics=all` to get
each member's commit-size distribution and flow metrics (cadence, lead time,
churn) computed individually, and `--breakdown=monthly` for a team-wide monthly
table. This works in terminal, HTML, and JSON:

```bash
gitrespect repo1 repo2 --team=dev1@company.com,dev2@company.com \
  --year=2025 --breakdown=monthly --metrics=all --output=html --file=team.html
```

### Build the Team Automatically

Assembling a `--team` list by hand means running `git log` across every repo,
counting addresses and eyeballing the result to strip CI. `--top` does that:

```bash
gitrespect -r ~/projects --top 10 --year=2025
```

It ranks contributors by commit count across every scanned repo and filters
automation (GitLab project and group access tokens, `semantic-release-bot`,
`dependabot`, `renovate`, GitHub `[bot]` accounts). Excluded identities are
named on stderr, because the filter matches substrings and a human whose name
contains one of them would otherwise vanish without explanation. Add your own
patterns with `--exclude-authors`, or name people directly with `--team`, which
skips discovery entirely.

### Merging One Person's Multiple Addresses

Real contributors commit under several addresses: a corporate one, a personal
one, and per-machine addresses an unconfigured git invents from the hostname.
Counting each separately undercuts that person by whatever share of their work
landed under the others.

If a repository has a `.mailmap`, this is already handled (see the note under
[All Options](#all-options)). For everything else, use a roster:

```bash
gitrespect -r ~/projects --roster team.yaml --top 10 --year=2025
```

```yaml
# team.yaml - one person per line, canonical name then every address they use
Wesley Ornellas: wesley.ornellas@corp.com, wesleyornellas@MacBook---Wesley.local
Juan Gracia: juan.gracia@corp.com, juanmgracia@gmail.com
```

A JSON roster (`{"Wesley Ornellas": ["a@x.com", "b@x.com"]}`) works too. For a
one-off, skip the file entirely:

```bash
gitrespect --alias "Wesley Ornellas=wesley.ornellas@corp.com,wesleyornellas@MacBook---Wesley.local" ...
```

Reports are then labelled with the canonical name rather than whichever address
came first, and `--top` ranks people rather than addresses. Listing one address
under two people is rejected, since that would double-count them in a team
total.

### Per-Repository Team Breakdown

`--per-repo` works in team mode, reporting one row per repository with every
member's work folded in:

```bash
gitrespect -r ~/projects --top 10 --year=2025 --per-repo
```

```
  By Repository (3)
  Repository            Net        Commits  People
  ──────────────────────────────────────────────────
  tag-api-helm          11         11       1
  billing-service       6          6        1
  scratch-notes         1          1        1
```

One aggregated table rather than one table per member, which stops being
readable past a handful of repos. Per-contributor attribution for each
repository is available in `--output json`.

### Duplicate Repository Detection

A recursive scan over a working machine routinely finds the same project twice:
an old flat clone next to a newer nested layout. Both carry the full history, so
counting both inflates the total with nothing in the report to suggest anything
is wrong.

Repositories are grouped by their `origin` remote and duplicates skipped, with
the skip announced:

```
Warning: 1 duplicate repo found and skipped (same remote gitlab.com/acme/tag-api-helm): /projects/tag-api-helm
         counting /projects/tag-api-group/tag-api-helm
```

HTTPS, SSH, credential-bearing and ported URLs for the same project all collapse
to one identity. The most recently committed checkout is kept, ties break
alphabetically. A repository with no readable remote is always kept, since
dropping an unidentified repo would understate the report.

### Analyze Specific Path

```bash
gitrespect /path/to/repo
```

### Multiple Repositories

```bash
gitrespect ./api ./frontend ./gateway
```

### Scan Directory for Repos

Analyze all git repositories in a folder:

```bash
gitrespect -r ~/projects
```

### Filter by Year

```bash
gitrespect --year=2025
```

### Breakdowns

Group output by month, week or day. Weeks are anchored on Monday.

```bash
gitrespect --year=2025 --breakdown=monthly
gitrespect --year=2025 --breakdown=weekly
gitrespect --since=2025-06-01 --breakdown=daily
```

Works with `--team` and with every output format.

### Custom Date Range

```bash
gitrespect --since=2025-01-01 --until=2025-06-30
```

### Filter by Author

```bash
gitrespect --author="developer@example.com"
```

### Export to HTML

```bash
gitrespect --year=2025 --breakdown=monthly --output=html --file=report.html
```

### HTML Theme Options

Choose between dark (default) and light themes:

**Dark theme (default):**
```bash
gitrespect --output=html --theme=dark --file=report.html
```

![Dark Theme](screenshots/report-dark.png)

**Light theme:**
```bash
gitrespect --output=html --theme=light --file=report.html
```

![Light Theme](screenshots/report-light.png)

### Opt-in Metrics

Beyond lines of code and the personal baseline, gitrespect can compute deeper
flow and quality metrics. These are **opt-in** (they run extra git queries) via
the `--metrics` flag, which takes a comma-separated list or `all`:

```bash
# Everything
gitrespect --metrics=all

# Just the ones you want
gitrespect --metrics=commit-size,churn

# Full HTML report with every section
gitrespect --year=2025 --breakdown=monthly --metrics=all --output=html --file=report.html
```

| Metric | Flag value | What it shows |
|--------|-----------|---------------|
| Commit size distribution | `commit-size` | % of commits that are micro (<10), small (10-99), medium (100-499), large (500+) |
| Integration cadence | `cadence` | Median days between commits on the main branch |
| Lead time | `lead-time` | Median days from a feature branch's first commit to it landing on main |
| Churn | `churn` | % of recently added lines rewritten within the churn window (`--churn-window`, default 30d) |

The personal baseline window is controlled with `--baseline-window` (e.g. `30d`,
`90d`, `6m`, `1y`). To bring back the deprecated Senior/Avg/Junior comparison,
pass `--legacy-benchmark`.

#### A note on lead time and squash merges

Lead time is measured from merge commits where they exist. Where they don't,
gitrespect falls back to the gap between when a commit was authored and when
it landed on main, which rebase and patch-based workflows preserve.

**Squash merges rewrite both timestamps**, so a squash-merging repository
leaves nothing in git history to measure and gitrespect will say so rather
than report a misleading zero. This is a limitation of git history, not a bug.
The other metrics are unaffected.

The fallback deliberately discards gaps longer than the period being analysed
and needs several samples before reporting, because a cherry-pick or a
history rewrite such as `filter-repo` also moves the committer date and would
otherwise masquerade as a very long lead time.

### Export to JSON

```bash
gitrespect --output=json --file=stats.json
```

### Team HTML Report

```bash
gitrespect --team=dev1@example.com,dev2@example.com --output=html --file=team-report.html
```

### Trend Chart

The HTML report can plot the breakdown it already computes as a line chart:

```bash
gitrespect -r ~/projects --top 10 --year=2025 -b monthly \
  -o html --chart -f team.html
```

One line per member, or add `--highlight` for the "you versus the team average"
view, which draws that person against a derived average of everyone else:

```bash
gitrespect -r ~/projects --top 10 --year=2025 -b monthly \
  -o html --chart --highlight "Wesley Ornellas" -f team.html
```

The chart is inline SVG in the same self-contained file, with no external
requests, and its palette is checked for colour-vision-deficiency safety and
contrast in both the light and dark themes. `--chart` needs `--breakdown` to
have periods to plot, and only applies to `--output html`.

## Merge Requests and Pull Requests

Local git history cannot answer "how many MRs did this person open per month
versus the team", because a merge request lives on the review platform, not in
the object database. The `prs` subcommand asks the platform directly and shapes
the answer like the git-based reports:

```bash
# GitLab, group-scoped: one query covers every project under the group
gitrespect prs --provider gitlab --group my-group/web --year=2025 -b monthly

# GitHub
gitrespect prs --provider github --org my-org -a me@example.com --year=2025
```

```
  Opened      Merged      Merge rate  Lead time
  ──────────────────────────────────────────────────
  14          14          100%        1.0d median

  Contributors
  Contributor Opened   Merged   /month
  ─────────────────────────────────────
  wornellas   8        8        0.7
  jgracia     6        6        0.5
```

Because merge timestamps are real, lead time here is measured from MR opened to
MR merged, which is a cleaner DORA-style signal than the commit-graph heuristic
`--metrics=lead-time` uses on local history.

**Authentication**, tried in this order:

1. `--token`, or the `GITLAB_TOKEN` / `GITHUB_TOKEN` environment variable.
2. The locally authenticated `glab` or `gh` CLI, which needs no token at all.

Prefer the environment variable: a token passed as `--token` is visible to
anyone who can list processes.

**Before/after on MR volume.** `compare` takes `--data=prs`, giving the same Nx
multiplier for merge request volume that it already gives for lines of code:

```bash
gitrespect compare --data=prs --group my-group/web \
  --before=2025-01:2025-06 --after=2025-07:2025-12
```

```
  Period          Opened   Merged   /month
  ─────────────────────────────────────────
  2025-01:2025-06 5        5        0.8
  2025-07:2025-12 9        9        1.5

  Change: +1.8x per month  (volume 5 → 9, +1.8x)
```

Platform accounts are matched to people by email where the API exposes one, and
otherwise by username and display name. When an account cannot be matched to a
requested identity it is reported rather than silently dropped. Use `--map
you@corp.com=handle` to pin an account explicitly.

## All Options

```
gitrespect [paths...] [flags]

Flags:
  -a, --author string        Filter by author email (default: git config user.email)
  -t, --team strings         Team mode: analyze multiple authors (comma-separated emails)
      --all-authors          Analyze every author's commits, unfiltered
      --top int              Auto-discover the top N contributors and run team mode on them
      --exclude-authors str  Extra regexes excluded from --top, on top of the bot filter
      --roster string        Roster file mapping a canonical name to a person's addresses
      --alias stringArray    Inline identity: 'Name=a@x.com,b@x.com' (repeatable)
  -r, --recursive            Scan subdirectories for git repositories
      --per-repo             Show breakdown by repository (works in team mode too)
      --chart                Include a trend chart in the HTML report (needs --breakdown)
      --highlight string     In team mode, emphasise one member against the team average
  -s, --since string         Start date (YYYY-MM-DD or "30 days ago") (default: "30 days ago")
  -u, --until string         End date (default: now)
      --year int             Filter by year (e.g., --year=2025)
  -b, --breakdown string     Show breakdown: monthly, weekly, or daily
  -e, --exclude strings      Exclude files matching glob patterns (e.g. -e 'vendor/*')
      --metrics string       Opt-in metrics: comma list of churn,lead-time,commit-size,cadence, or 'all'
      --baseline-window str  Personal baseline window (e.g. 30d, 90d, 6m, 1y) (default: "90d")
      --churn-window string  Churn detection window (default: "30d")
      --legacy-benchmark     Show deprecated Senior/Avg/Junior comparison instead of personal baseline
  -o, --output string        Output format: terminal, json, or html (default: terminal)
  -f, --file string          Output file path (for html/json)
      --theme string         HTML theme: dark or light (default: dark)
  -h, --help                 Show help

Commands:
  gitrespect compare       Compare two time periods (add --team for a group,
                           -b for a per-period breakdown, --data=prs for MR volume)
  gitrespect prs           Merge request / pull request activity from GitLab or GitHub
  gitrespect version       Show version info
```

`--author`, `--team`, `--all-authors` and `--top` each answer "whose commits am
I counting", so exactly one of them may be given. Passing two is an error rather
than a silent winner.

Dates accept `YYYY-MM-DD`, `YYYY-MM`, `YYYY`, or relative forms like
`"30 days ago"`. `--until` is inclusive: `--until=2025-03-05` covers all of
5 March, and `--until=2025-03` covers all of March.

Author matching is case-insensitive, and a full address is matched exactly, so
`-a jo@corp.com` will not also pick up `bojo@corp.com`. A bare name fragment
like `-a alice` still matches loosely. `--author` and `--team` are mutually
exclusive.

Author matching also resolves through `.mailmap`, because git's `log.mailmap`
setting defaults to on and gitrespect inherits it. If a repository carries a
`.mailmap` unifying `alice@personal.example` into `alice@corp.com`, then
`-a alice@corp.com` returns the commits made under both addresses. That is
almost always what you want. The catch is that the result then depends on each
repository's `.mailmap`, so with `-r` across many repos the same person can be
counted differently per repo depending on which of them happens to carry an
entry. Use `--roster` or `--alias` when you need the same identities applied
uniformly across every repository regardless of what each one ships.

`--exclude` patterns are matched against renamed files on both their old and
their new path, so `-e 'vendor/*'` still excludes a file that was moved into or
out of `vendor/`. A directory pattern excludes the whole subtree, `**` is
treated as `*`, a leading `./` is ignored, and an uncompilable pattern is
rejected rather than silently matching nothing.

Colour is disabled automatically when output is piped or redirected, and when
[`NO_COLOR`](https://no-color.org) is set.

## Personal Baseline

Instead of comparing you against arbitrary industry numbers, gitrespect compares
this period against **your own normal output**. It computes a baseline from the
prior `--baseline-window` (default 90 days) of your commit history and reports how
this period stacks up:

```
Baseline (90d prior):
└── Your normal: 84 lines/day → this period: 127 (+51% ↑)
```

If there isn't enough prior history (under ~30 days of activity in the window),
gitrespect says so rather than inventing a comparison.

> The old Senior/Avg/Junior industry benchmark is deprecated but still available
> via `--legacy-benchmark` for anyone who relied on it.

**Note:** Lines of code is just one metric. Quality, architecture decisions, code reviews, and mentoring are equally important contributions that aren't captured here. The opt-in flow metrics (cadence, lead time, churn) give a fuller picture.

## For AI Agents

gitrespect ships with a [skill](.claude/skills/gitrespect/SKILL.md) that teaches AI
coding agents (Claude Code, Codex, and compatible tools) how to run it: which
flags to use, how to opt into metrics, and how to read the output. Clone the repo
and the skill is picked up automatically, or point your agent at
`.claude/skills/gitrespect/SKILL.md`.

## How It Works

gitrespect uses `git log --numstat` to count lines added and deleted per commit, filtered by author and date range. It calculates working days (approximately 5/7 of calendar days) for daily averages.

## Use Cases

### For Individual Developers
- Track your personal productivity trends
- Measure impact of new tools or workflows
- Generate reports for performance reviews
- Compare productivity across different projects

### For Engineering Managers
- Understand team contribution patterns
- Measure team-wide AI tool adoption impact
- Identify productivity trends
- Generate reports for stakeholders

### For Organizations
- Quantify ROI of AI coding tools
- Compare team productivity metrics
- Track productivity before/after process changes

## Contributing

Contributions welcome! Please open an issue or submit a PR.

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## Author

Created by [Juan Gracia](https://github.com/juangracia)

## License

MIT License - see [LICENSE](LICENSE) file.

Use it freely, modify it, share it. No attribution required, but a star is always appreciated!
