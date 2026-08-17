package git

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type RepoStats struct {
	Path         string
	Author       string
	Since        time.Time
	Until        time.Time
	FirstCommit  time.Time // Actual first commit date in range
	LastCommit   time.Time // Actual last commit date in range
	Added        int
	Deleted      int
	Net          int
	Commits      int
	FilesChanged int
	Monthly      map[string]MonthStats
	Daily        map[string]DayStats
}

type MonthStats struct {
	Year    int
	Month   int
	Added   int
	Deleted int
	Net     int
	Commits int
}

// DayStats aggregates one calendar day. Weekly and daily breakdowns are
// derived from these buckets.
type DayStats struct {
	Date    string // YYYY-MM-DD
	Added   int
	Deleted int
	Net     int
	Commits int
}

type CompareStats struct {
	Before      RepoStats
	After       RepoStats
	BeforeLabel string
	AfterLabel  string
}

// TeamCompareStats holds a before/after comparison for a group of authors,
// used to audit how a tooling change landed across a whole team.
type TeamCompareStats struct {
	Before      TeamStats
	After       TeamStats
	BeforeLabel string
	AfterLabel  string
}

// Members returns the union of author emails seen in either period, sorted.
func (t TeamCompareStats) MemberEmails() []string {
	seen := make(map[string]bool)
	for e := range t.Before.Members {
		seen[e] = true
	}
	for e := range t.After.Members {
		seen[e] = true
	}
	emails := make([]string, 0, len(seen))
	for e := range seen {
		emails = append(emails, e)
	}
	sort.Strings(emails)
	return emails
}

type TeamStats struct {
	Since        time.Time
	Until        time.Time
	Members      map[string]RepoStats
	TotalAdded   int
	TotalDeleted int
	TotalNet     int
	TotalCommits int
	Monthly      map[string]MonthStats
	Daily        map[string]DayStats
}

func Analyze(repoPath, author string, since, until time.Time, excludePatterns []string) (RepoStats, error) {
	stats := RepoStats{
		Path:    repoPath,
		Author:  author,
		Since:   since,
		Until:   until,
		Monthly: make(map[string]MonthStats),
		Daily:   make(map[string]DayStats),
	}

	// Get commit stats with numstat
	args := LogArgs(repoPath)
	args = append(args, AuthorArgs(author)...)
	args = append(args,
		"--since="+TimeArg(since),
		"--until="+TimeArg(until),
		"--pretty=format:%H|%ad",
		"--date=short",
		"--numstat",
	)

	cmd := exec.Command("git", args...)
	output, err := cmd.Output()
	if err != nil {
		return stats, fmt.Errorf("git log failed: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	var currentDate string
	var currentMonth string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Commit header lines are "<40-hex sha>|<date>". Matching on the sha
		// shape rather than a bare "|" keeps filenames containing a pipe from
		// being mistaken for a new commit.
		if date, ok := parseCommitHeader(line); ok {
			currentDate = date
			currentMonth = ""
			if len(currentDate) >= 7 {
				currentMonth = currentDate[:7]
			}
			stats.Commits++

			// Track first and last commit dates
			if commitDate, err := time.Parse("2006-01-02", currentDate); err == nil {
				if stats.FirstCommit.IsZero() || commitDate.Before(stats.FirstCommit) {
					stats.FirstCommit = commitDate
				}
				if stats.LastCommit.IsZero() || commitDate.After(stats.LastCommit) {
					stats.LastCommit = commitDate
				}
			}

			// Count the commit once, here, rather than once per changed file.
			if currentMonth != "" {
				m := stats.Monthly[currentMonth]
				m.Commits++
				y, _ := strconv.Atoi(currentMonth[:4])
				mo, _ := strconv.Atoi(currentMonth[5:7])
				m.Year = y
				m.Month = mo
				stats.Monthly[currentMonth] = m
			}
			if currentDate != "" {
				d := stats.Daily[currentDate]
				d.Date = currentDate
				d.Commits++
				stats.Daily[currentDate] = d
			}
			continue
		}

		// Parse numstat line: added\tdeleted\tfilename
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			// Skip binary files (shown as -)
			if fields[0] == "-" || fields[1] == "-" {
				continue
			}

			// Check if file matches any exclude pattern
			filename := strings.Join(fields[2:], " ") // Handle filenames with spaces
			if ShouldExclude(filename, excludePatterns) {
				continue
			}

			added, err1 := strconv.Atoi(fields[0])
			deleted, err2 := strconv.Atoi(fields[1])

			if err1 == nil && err2 == nil {
				stats.Added += added
				stats.Deleted += deleted
				stats.FilesChanged++

				if currentMonth != "" {
					m := stats.Monthly[currentMonth]
					m.Added += added
					m.Deleted += deleted
					m.Net = m.Added - m.Deleted
					stats.Monthly[currentMonth] = m
				}
				if currentDate != "" {
					d := stats.Daily[currentDate]
					d.Added += added
					d.Deleted += deleted
					d.Net = d.Added - d.Deleted
					stats.Daily[currentDate] = d
				}
			}
		}
	}

	stats.Net = stats.Added - stats.Deleted

	return stats, nil
}

// PeriodRow is one row of a monthly, weekly or daily breakdown.
type PeriodRow struct {
	Key     string // sortable key
	Label   string // display label
	Added   int
	Deleted int
	Net     int
	Commits int
}

// Granularities lists the values accepted by --breakdown.
var Granularities = []string{"monthly", "weekly", "daily"}

// ValidGranularity reports whether g is a supported --breakdown value.
func ValidGranularity(g string) bool {
	for _, v := range Granularities {
		if v == g {
			return true
		}
	}
	return false
}

// Breakdown groups the per-day buckets into the requested granularity and
// returns rows sorted oldest first. An unsupported granularity returns nil.
func Breakdown(stats RepoStats, granularity string) []PeriodRow {
	if !ValidGranularity(granularity) {
		return nil
	}

	grouped := make(map[string]*PeriodRow)
	for date, d := range stats.Daily {
		t, err := time.Parse("2006-01-02", date)
		if err != nil {
			continue
		}

		var key, label string
		switch granularity {
		case "monthly":
			key = t.Format("2006-01")
			label = t.Format("Jan 2006")
		case "weekly":
			// Anchor each week on its Monday so the label names a real date.
			monday := t.AddDate(0, 0, -weekdayOffset(t))
			key = monday.Format("2006-01-02")
			label = "Week of " + monday.Format("Jan 2 2006")
		case "daily":
			key = t.Format("2006-01-02")
			label = t.Format("Jan 2 2006")
		}

		row, ok := grouped[key]
		if !ok {
			row = &PeriodRow{Key: key, Label: label}
			grouped[key] = row
		}
		row.Added += d.Added
		row.Deleted += d.Deleted
		row.Commits += d.Commits
	}

	rows := make([]PeriodRow, 0, len(grouped))
	for _, r := range grouped {
		r.Net = r.Added - r.Deleted
		rows = append(rows, *r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key })
	return rows
}

// weekdayOffset returns days elapsed since Monday for t.
func weekdayOffset(t time.Time) int {
	return (int(t.Weekday()) + 6) % 7
}

// parseCommitHeader recognises a "<sha>|<date>" header emitted by our
// --pretty format and returns the date field.
func parseCommitHeader(line string) (string, bool) {
	sha, date, ok := strings.Cut(line, "|")
	if !ok || len(sha) != 40 {
		return "", false
	}
	for _, r := range sha {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return "", false
		}
	}
	return date, true
}

func GetDefaultAuthor(repoPath string) (string, error) {
	cmd := exec.Command("git", "-C", repoPath, "config", "user.email")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// AuthorArgs builds the git log flags that select commits by author.
//
// git matches --author as a regular expression against "Name <email>", and it
// matches substrings. A bare address like "jo@corp.com" therefore also matches
// "bojo@corp.com", which inflates that member's totals and double-counts the
// team. When the input is a complete address, anchor it on the angle brackets
// git puts around the email. --fixed-strings additionally stops characters
// like "." in an address being treated as regex wildcards.
//
// Matching is case-insensitive. Email domains are case-insensitive by spec and
// local parts are in practice, so a repo whose ident reads "Alice@Corp.com"
// must still be found by someone typing their address in lower case. Without
// this the tool reports a confident zero, which is indistinguishable from
// having done no work.
func AuthorArgs(author string) []string {
	if author == "" {
		return nil
	}
	pattern := author
	if looksLikeEmail(author) {
		pattern = "<" + author + ">"
	}
	return []string{"--fixed-strings", "--regexp-ignore-case", "--author=" + pattern}
}

// looksLikeEmail reports whether s is a complete address rather than a name
// fragment, which callers may legitimately pass to match loosely.
func looksLikeEmail(s string) bool {
	return strings.Contains(s, "@") && !strings.ContainsAny(s, " <>")
}

// TimeArg formats t for git's --since/--until flags.
//
// git parses a bare "2006-01-02" with its approxidate parser, which resolves
// the date in the local timezone and does not reliably include commits made
// during that day. An explicit RFC3339 timestamp carries the offset, so git
// compares the exact instant we mean.
func TimeArg(t time.Time) string {
	return t.Format(time.RFC3339)
}

// dateGranularity records how precise an absolute date string was, so an end
// bound can be widened to cover the whole day, month or year the user named.
type dateGranularity int

const (
	granularityInstant dateGranularity = iota
	granularityDay
	granularityMonth
	granularityYear
)

// parseAbsolute resolves the absolute date forms in the local timezone.
// Dates are what the user sees on their own calendar, so local is the
// intuitive reading and matches how --year is resolved.
func parseAbsolute(dateStr string) (time.Time, dateGranularity, bool) {
	for _, f := range []struct {
		layout      string
		granularity dateGranularity
	}{
		{"2006-01-02", granularityDay},
		{"2006-01", granularityMonth},
		{"2006", granularityYear},
	} {
		if t, err := time.ParseInLocation(f.layout, dateStr, time.Local); err == nil {
			return t, f.granularity, true
		}
	}
	return time.Time{}, granularityInstant, false
}

// endOfRange widens t to the last instant of the unit the user named, so
// "--until 2025-03-05" includes commits made during 5 March.
func endOfRange(t time.Time, g dateGranularity) time.Time {
	switch g {
	case granularityDay:
		return t.AddDate(0, 0, 1).Add(-time.Nanosecond)
	case granularityMonth:
		return t.AddDate(0, 1, 0).Add(-time.Nanosecond)
	case granularityYear:
		return t.AddDate(1, 0, 0).Add(-time.Nanosecond)
	default:
		return t
	}
}

// ParseDate resolves a start-of-range date. Absolute forms (2006-01-02,
// 2006-01, 2006) return the first instant of that day, month or year.
// Relative forms ("30 days ago") return that instant.
func ParseDate(dateStr string) (time.Time, error) {
	if t, _, ok := parseAbsolute(dateStr); ok {
		return t, nil
	}
	return parseRelative(dateStr)
}

// ParseDateEnd resolves an end-of-range date. Absolute forms return the LAST
// instant of the named day, month or year so the bound is inclusive; relative
// forms behave like ParseDate.
func ParseDateEnd(dateStr string) (time.Time, error) {
	if t, g, ok := parseAbsolute(dateStr); ok {
		return endOfRange(t, g), nil
	}
	return parseRelative(dateStr)
}

func parseRelative(dateStr string) (time.Time, error) {
	// Parse relative dates
	dateStr = strings.ToLower(dateStr)

	if strings.Contains(dateStr, "day") {
		parts := strings.Fields(dateStr)
		if len(parts) >= 2 {
			n, err := strconv.Atoi(parts[0])
			if err == nil {
				return time.Now().AddDate(0, 0, -n), nil
			}
		}
	}

	if strings.Contains(dateStr, "week") {
		parts := strings.Fields(dateStr)
		if len(parts) >= 2 {
			n, err := strconv.Atoi(parts[0])
			if err == nil {
				return time.Now().AddDate(0, 0, -n*7), nil
			}
		}
	}

	if strings.Contains(dateStr, "month") {
		parts := strings.Fields(dateStr)
		if len(parts) >= 2 {
			n, err := strconv.Atoi(parts[0])
			if err == nil {
				return time.Now().AddDate(0, -n, 0), nil
			}
		}
	}

	if strings.Contains(dateStr, "year") {
		parts := strings.Fields(dateStr)
		if len(parts) >= 2 {
			n, err := strconv.Atoi(parts[0])
			if err == nil {
				return time.Now().AddDate(-n, 0, 0), nil
			}
		}
	}

	return time.Time{}, fmt.Errorf("could not parse date: %s", dateStr)
}

func CombineStats(stats []RepoStats) RepoStats {
	if len(stats) == 0 {
		return RepoStats{
			Monthly: make(map[string]MonthStats),
			Daily:   make(map[string]DayStats),
		}
	}

	combined := RepoStats{
		Author:  stats[0].Author,
		Since:   stats[0].Since,
		Until:   stats[0].Until,
		Monthly: make(map[string]MonthStats),
		Daily:   make(map[string]DayStats),
	}

	for _, s := range stats {
		combined.Added += s.Added
		combined.Deleted += s.Deleted
		combined.Commits += s.Commits
		combined.FilesChanged += s.FilesChanged

		// Track earliest first commit and latest last commit
		if !s.FirstCommit.IsZero() {
			if combined.FirstCommit.IsZero() || s.FirstCommit.Before(combined.FirstCommit) {
				combined.FirstCommit = s.FirstCommit
			}
		}
		if !s.LastCommit.IsZero() {
			if combined.LastCommit.IsZero() || s.LastCommit.After(combined.LastCommit) {
				combined.LastCommit = s.LastCommit
			}
		}

		// Merge monthly stats
		for month, m := range s.Monthly {
			existing := combined.Monthly[month]
			existing.Added += m.Added
			existing.Deleted += m.Deleted
			existing.Net = existing.Added - existing.Deleted
			existing.Commits += m.Commits
			existing.Year = m.Year
			existing.Month = m.Month
			combined.Monthly[month] = existing
		}

		// Merge daily stats
		for day, d := range s.Daily {
			existing := combined.Daily[day]
			existing.Date = day
			existing.Added += d.Added
			existing.Deleted += d.Deleted
			existing.Net = existing.Added - existing.Deleted
			existing.Commits += d.Commits
			combined.Daily[day] = existing
		}
	}

	combined.Net = combined.Added - combined.Deleted

	if len(stats) > 1 {
		combined.Path = fmt.Sprintf("%d repositories", len(stats))
	} else {
		combined.Path = stats[0].Path
	}

	return combined
}

// WorkingDays calculates approximate working days between two dates
func WorkingDays(since, until time.Time) int {
	days := int(until.Sub(since).Hours() / 24)
	// Approximate: 5/7 of days are working days
	workingDays := (days * 5) / 7
	if workingDays < 1 {
		workingDays = 1
	}
	return workingDays
}

// shouldExclude checks if a filename matches any of the exclude patterns
// RenamePaths expands git's compact rename notation into the concrete paths it
// refers to.
//
// numstat prints a rename as "{vendor => src}/lib.go", "vendor/{a.go => b.go}"
// or "a.go => b.go". None of those match the old or the new path literally, so
// a glob like "vendor/*" silently failed to exclude a renamed file. Both sides
// are returned so a pattern matching either one excludes the change.
func RenamePaths(name string) []string {
	if !strings.Contains(name, " => ") {
		return []string{name}
	}

	if open := strings.Index(name, "{"); open >= 0 {
		if closing := strings.Index(name[open:], "}"); closing >= 0 {
			closing += open
			prefix, suffix := name[:open], name[closing+1:]
			from, to, ok := strings.Cut(name[open+1:closing], " => ")
			if ok {
				return []string{
					cleanRenamePath(prefix + from + suffix),
					cleanRenamePath(prefix + to + suffix),
				}
			}
		}
	}

	// Whole-path rename, printed without braces.
	if from, to, ok := strings.Cut(name, " => "); ok {
		return []string{cleanRenamePath(from), cleanRenamePath(to)}
	}
	return []string{name}
}

// cleanRenamePath tidies a path assembled from a rename, which can pick up a
// doubled or leading separator when a file moves to or from the repo root.
func cleanRenamePath(p string) string {
	return strings.TrimPrefix(path.Clean(p), "/")
}

// LogArgs starts a `git log` argument list for a repository, for callers that
// parse filenames out of --numstat.
//
// core.quotePath defaults to true, which makes git wrap any path containing
// non-ASCII or control characters in double quotes and octal-escape the bytes:
// "caf\303\251.txt". The surrounding quotes make every exclude glob miss, so
// accented and unusual filenames slipped past --exclude entirely.
func LogArgs(repoPath string) []string {
	return []string{"-c", "core.quotePath=false", "-C", repoPath, "log"}
}

// ValidateExcludePatterns rejects globs that filepath.Match cannot compile.
// Match reports a bad pattern as an error rather than a non-match, and every
// call site here ignores that error, so an unusable pattern would otherwise
// exclude nothing while the user believed they had filtered.
func ValidateExcludePatterns(patterns []string) error {
	for _, p := range patterns {
		if _, err := path.Match(normalizePattern(p), ""); err != nil {
			return fmt.Errorf("invalid --exclude pattern %q: %w", p, err)
		}
	}
	return nil
}

// normalizePattern tidies a user-supplied glob. numstat paths are
// repo-relative with no leading "./", so a tab-completed "./vendor/*" would
// never match anything. "**" is folded to "*" because there is no globstar,
// and the directory fast path below already recurses.
//
// This uses path rather than filepath deliberately: git always reports
// forward-slash paths, including on Windows, where filepath.Clean would
// rewrite the separator to a backslash and break every directory pattern.
func normalizePattern(pattern string) string {
	pattern = strings.ReplaceAll(pattern, "**", "*")
	if pattern == "" {
		return pattern
	}
	cleaned := path.Clean(pattern)
	if cleaned == "." {
		return pattern
	}
	return cleaned
}

// ShouldExclude reports whether filename matches any of the glob patterns.
// Renames are matched on both their old and new path.
func ShouldExclude(filename string, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	for _, candidate := range RenamePaths(filename) {
		if matchesAny(candidate, patterns) {
			return true
		}
	}
	return false
}

// matchesAny uses path rather than filepath throughout: git reports
// forward-slash paths on every platform, and filepath's separator is a
// backslash on Windows, which made every directory pattern fail there.
func matchesAny(filename string, patterns []string) bool {
	for _, raw := range patterns {
		pattern := normalizePattern(raw)
		// Try matching the full path
		if matched, _ := path.Match(pattern, filename); matched {
			return true
		}
		// Also try matching just the base name
		if matched, _ := path.Match(pattern, path.Base(filename)); matched {
			return true
		}
		// Handle directory patterns (e.g., "vendor/*"), which exclude the whole
		// subtree rather than only its immediate children.
		if strings.Contains(pattern, "/") {
			// Check if filename starts with the directory prefix
			parts := strings.SplitN(pattern, "/", 2)
			if len(parts) == 2 && strings.HasPrefix(filename, parts[0]+"/") {
				if parts[1] == "*" {
					return true
				}
				if matched, _ := path.Match(parts[1], filename[len(parts[0])+1:]); matched {
					return true
				}
			}
		}
	}
	return false
}

// IsGitRepo checks if a directory is a git repository
func IsGitRepo(dir string) bool {
	gitDir := filepath.Join(dir, ".git")
	info, err := os.Stat(gitDir)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// FindRepos finds all git repositories in a directory (recursively)
func FindRepos(dir string) ([]string, error) {
	var repos []string

	// Recursively scan for git repos (including immediate subdirectories)
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip directories we can't access
		}

		if !d.IsDir() {
			return nil
		}

		// Skip hidden directories (except .git check happens via IsGitRepo)
		if strings.HasPrefix(d.Name(), ".") && p != dir {
			return filepath.SkipDir
		}

		if IsGitRepo(p) {
			// Only add if it has commits (valid working repo)
			if hasCommits(p) {
				repos = append(repos, p)
				return filepath.SkipDir // Don't recurse into valid git repos
			}
			// Invalid/empty git repo at root - continue scanning subdirectories
			if p == dir {
				return nil
			}
			return filepath.SkipDir
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to scan directory: %w", err)
	}

	return repos, nil
}

// hasCommits checks if a git repo has at least one commit
func hasCommits(repoPath string) bool {
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "HEAD")
	err := cmd.Run()
	return err == nil
}
