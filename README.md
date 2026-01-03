# gitrespect

> Respect your git work with real metrics

A fast CLI tool that analyzes git repositories and provides developer productivity metrics. See your lines of code, compare against industry benchmarks, and measure the impact of workflow changes.

## Installation

### Using Go

```bash
go install github.com/juangracia/gitrespect@latest
```

### Download Binary

Download the latest release from [GitHub Releases](https://github.com/juangracia/gitrespect/releases).

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
 gitrespect - juan.gracia@example.com
asset-api (Dec 4 2025 to Jan 3 2026)
──────────────────────────────────────────────────

  Added       Deleted     Net         Commits
  ────────────────────────────────────────────
  2,847       312         2,535       47

  Daily avg: 127 lines/day (22 working days)

  vs Industry:
  ├── Senior Dev (20/day):     6.4x ████████████████████░░░░
  ├── Industry Avg (50/day):   2.5x ████████░░░░░░░░░░░░░░░░
  └── Junior Dev (100/day):    1.3x ████░░░░░░░░░░░░░░░░░░░░
```

### Analyze Specific Path

```bash
gitrespect /path/to/repo
```

### Multiple Repositories

```bash
gitrespect ./api ./frontend ./gateway
```

### Filter by Year

```bash
gitrespect --year=2025
```

### Monthly Breakdown

```bash
gitrespect --year=2025 --breakdown=monthly
```

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

### Export to JSON

```bash
gitrespect --output=json --file=stats.json
```

### Compare Time Periods

Measure the impact of workflow changes, AI adoption, or other productivity improvements:

```bash
gitrespect compare --before=2025-01:2025-07 --after=2025-08:2025-12
```

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

## All Options

```
gitrespect [paths...] [flags]

Flags:
  -a, --author string      Filter by author email (default: git config user.email)
  -s, --since string       Start date (YYYY-MM-DD or "30 days ago") (default: "30 days ago")
  -u, --until string       End date (default: now)
      --year int           Filter by year (e.g., --year=2025)
  -b, --breakdown string   Show breakdown: monthly, weekly, or daily
  -o, --output string      Output format: terminal, json, or html (default: terminal)
  -f, --file string        Output file path (for html/json)
  -h, --help               Show help

Commands:
  gitrespect compare       Compare two time periods
  gitrespect version       Show version info
```

## Industry Benchmarks

gitrespect compares your output against commonly cited industry benchmarks:

| Level | LOC/Day | Source |
|-------|---------|--------|
| Senior Dev | 20 | Fred Brooks, "The Mythical Man-Month" |
| Industry Avg | 50 | Various industry surveys |
| Junior Dev | 100 | New developers often have higher raw output |

**Note:** Lines of code is just one metric. Quality, architecture decisions, code reviews, and mentoring are equally important contributions that aren't captured here.

## How It Works

gitrespect uses `git log --numstat` to count lines added and deleted per commit, filtered by author and date range. It calculates working days (approximately 5/7 of calendar days) for daily averages.

## Contributing

Contributions welcome! Please open an issue or submit a PR.

## Author

Created by [Juan Gracia](https://github.com/juangracia)

## License

MIT License - see [LICENSE](LICENSE) file.

Use it freely, modify it, share it. No attribution required, but a star is always appreciated!
