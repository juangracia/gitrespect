package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
}

type MonthStats struct {
	Year    int
	Month   int
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

type TeamStats struct {
	Since        time.Time
	Until        time.Time
	Members      map[string]RepoStats
	TotalAdded   int
	TotalDeleted int
	TotalNet     int
	TotalCommits int
}

func Analyze(repoPath, author string, since, until time.Time, excludePatterns []string) (RepoStats, error) {
	stats := RepoStats{
		Path:    repoPath,
		Author:  author,
		Since:   since,
		Until:   until,
		Monthly: make(map[string]MonthStats),
	}

	// Build git log command
	sinceStr := since.Format("2006-01-02")
	untilStr := until.Format("2006-01-02")

	// Get commit stats with numstat
	args := []string{
		"-C", repoPath,
		"log",
		"--author=" + author,
		"--since=" + sinceStr,
		"--until=" + untilStr,
		"--pretty=format:%H|%ad",
		"--date=short",
		"--numstat",
	}

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

		// Check if it's a commit header line
		if strings.Contains(line, "|") {
			parts := strings.Split(line, "|")
			if len(parts) >= 2 {
				currentDate = parts[1]
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

				// Parse month
				if len(currentDate) >= 7 {
					currentMonth = currentDate[:7]
				}
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
			if shouldExclude(filename, excludePatterns) {
				continue
			}

			added, err1 := strconv.Atoi(fields[0])
			deleted, err2 := strconv.Atoi(fields[1])

			if err1 == nil && err2 == nil {
				stats.Added += added
				stats.Deleted += deleted
				stats.FilesChanged++

				// Update monthly stats
				if currentMonth != "" {
					m := stats.Monthly[currentMonth]
					m.Added += added
					m.Deleted += deleted
					m.Net = m.Added - m.Deleted
					m.Commits++

					// Parse year and month
					if len(currentMonth) >= 7 {
						y, _ := strconv.Atoi(currentMonth[:4])
						mo, _ := strconv.Atoi(currentMonth[5:7])
						m.Year = y
						m.Month = mo
					}
					stats.Monthly[currentMonth] = m
				}
			}
		}
	}

	stats.Net = stats.Added - stats.Deleted

	return stats, nil
}

func GetDefaultAuthor(repoPath string) (string, error) {
	cmd := exec.Command("git", "-C", repoPath, "config", "user.email")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func ParseDate(dateStr string) (time.Time, error) {
	// Try parsing as absolute date
	formats := []string{
		"2006-01-02",
		"2006-01",
		"2006",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, dateStr); err == nil {
			return t, nil
		}
	}

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
		return RepoStats{Monthly: make(map[string]MonthStats)}
	}

	combined := RepoStats{
		Author:  stats[0].Author,
		Since:   stats[0].Since,
		Until:   stats[0].Until,
		Monthly: make(map[string]MonthStats),
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

// ActiveWorkingDays calculates working days based on actual commit activity span.
// If no commits exist, falls back to the provided date range.
func ActiveWorkingDays(stats RepoStats) int {
	if stats.FirstCommit.IsZero() || stats.LastCommit.IsZero() {
		return WorkingDays(stats.Since, stats.Until)
	}
	return WorkingDays(stats.FirstCommit, stats.LastCommit)
}

// shouldExclude checks if a filename matches any of the exclude patterns
func shouldExclude(filename string, patterns []string) bool {
	for _, pattern := range patterns {
		// Try matching the full path
		if matched, _ := filepath.Match(pattern, filename); matched {
			return true
		}
		// Also try matching just the base name
		if matched, _ := filepath.Match(pattern, filepath.Base(filename)); matched {
			return true
		}
		// Handle directory patterns (e.g., "vendor/*")
		if strings.Contains(pattern, "/") {
			// Check if filename starts with the directory prefix
			parts := strings.SplitN(pattern, "/", 2)
			if len(parts) == 2 && strings.HasPrefix(filename, parts[0]+"/") {
				if parts[1] == "*" {
					return true
				}
				if matched, _ := filepath.Match(parts[1], filename[len(parts[0])+1:]); matched {
					return true
				}
			}
		}
	}
	return false
}

// IsGitRepo checks if a path is a git repository
func IsGitRepo(path string) bool {
	gitDir := filepath.Join(path, ".git")
	info, err := os.Stat(gitDir)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// FindRepos finds all git repositories in a directory (recursively)
func FindRepos(path string) ([]string, error) {
	var repos []string

	// Recursively scan for git repos (including immediate subdirectories)
	err := filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip directories we can't access
		}

		if !d.IsDir() {
			return nil
		}

		// Skip hidden directories (except .git check happens via IsGitRepo)
		if strings.HasPrefix(d.Name(), ".") && p != path {
			return filepath.SkipDir
		}

		if IsGitRepo(p) {
			// Only add if it has commits (valid working repo)
			if hasCommits(p) {
				repos = append(repos, p)
				return filepath.SkipDir // Don't recurse into valid git repos
			}
			// Invalid/empty git repo at root - continue scanning subdirectories
			if p == path {
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
