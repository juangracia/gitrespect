package metrics

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/juangracia/gitrespect/internal/git"
)

// errNoRepos reports that not one repository could be read.
//
// The multi-repo variants return it instead of an empty result because a
// distribution of zero commits and a run where every repository failed look
// identical once they reach the report, and only one of them means the author
// did no work.
var errNoRepos = errors.New("no repositories could be analyzed")

// coverageErr explains why nothing was measured. The first underlying git
// failure is more useful than the generic message, so it wins when there is one.
func coverageErr(firstErr error) error {
	if firstErr != nil {
		return firstErr
	}
	return errNoRepos
}

// dedupePaths drops repeated repositories.
//
// A repo reachable both as a positional argument and through -r would
// otherwise contribute its commits twice. Doubling a repo does not visibly
// change a total, but it silently doubles that repo's weight in every pooled
// median, which is the kind of skew nobody goes looking for.
//
// Paths are compared after being made absolute; the original string is kept so
// git and any error message still show what the user typed.
func dedupePaths(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		key := p
		if abs, err := filepath.Abs(p); err == nil {
			key = abs
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	return out
}

// joinBranches names the main branch a pooled metric was measured on.
//
// Repositories do not have to agree on that name, so a report covering several
// of them cannot claim a single one. Listing the distinct names keeps the
// sentence honest and stays short in practice, since real answers are drawn
// from main, master and the occasional develop.
func joinBranches(branches []string) string {
	seen := make(map[string]bool, len(branches))
	distinct := make([]string, 0, len(branches))
	for _, b := range branches {
		if b == "" || seen[b] {
			continue
		}
		seen[b] = true
		distinct = append(distinct, b)
	}
	sort.Strings(distinct)
	return strings.Join(distinct, "/")
}

// commitRecord reduces one commit to what the line-counting metrics need: how
// many lines it moved once binary files and --exclude patterns are taken out.
//
// No date is carried. Commit size and churn only ever ask about a window as a
// whole, and the one metric that needs per-commit dates, the baseline, gets
// them from git.AnalyzeMulti instead of a second parser here.
type commitRecord struct {
	Added   int
	Deleted int
}

// Total is the commit's size: churn in both directions, not net lines.
func (r commitRecord) Total() int { return r.Added + r.Deleted }

// scanNumstat reads one repository's commits for the given authors and window.
//
// Both metrics that need per-commit line counts, commit size and churn, derive
// from these records rather than each running their own git log, so the two
// cannot drift apart on what counts as a line.
//
// A commit whose every file was excluded is still returned, with zero lines.
// It happened, and dropping it would quietly shrink the commit count that the
// size distribution is a percentage of.
func scanNumstat(repoPath string, authors []string, since, until time.Time, exclude []string) ([]commitRecord, error) {
	args := git.LogArgs(repoPath)
	args = append(args, git.AuthorArgsMulti(authors)...)
	args = append(args,
		"--since="+git.TimeArg(since),
		"--until="+git.TimeArg(until),
		"--pretty=format:COMMIT",
		"--numstat",
	)
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}

	var (
		records []commitRecord
		current commitRecord
		open    bool
	)
	flush := func() {
		if open {
			records = append(records, current)
			open = false
		}
	}

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		// numstat rows begin with a count or "-", so a header can only be ours.
		if line == "COMMIT" {
			flush()
			current = commitRecord{}
			open = true
			continue
		}
		if !open || line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		// Binary files show "-" for added/deleted counts.
		if fields[0] == "-" || fields[1] == "-" {
			continue
		}
		filename := strings.Join(fields[2:], " ")
		if git.ShouldExclude(filename, exclude) {
			continue
		}
		added, err1 := strconv.Atoi(fields[0])
		deleted, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		current.Added += added
		current.Deleted += deleted
	}
	flush()

	return records, nil
}

// sumNumstat returns (totalAdded, totalDeleted) for the authors' commits in the
// window, excluding binary files and excluded patterns.
func sumNumstat(repoPath string, authors []string, since, until time.Time, exclude []string) (int, int, error) {
	records, err := scanNumstat(repoPath, authors, since, until, exclude)
	if err != nil {
		return 0, 0, err
	}
	totalAdded, totalDeleted := 0, 0
	for _, r := range records {
		totalAdded += r.Added
		totalDeleted += r.Deleted
	}
	return totalAdded, totalDeleted, nil
}
