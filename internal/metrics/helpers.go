package metrics

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// detectMainBranch returns "main", "master", or the resolved origin HEAD name.
// Empty string means no main-like branch found.
func detectMainBranch(repoPath string) string {
	if out, err := exec.Command("git", "-C", repoPath, "symbolic-ref", "refs/remotes/origin/HEAD").Output(); err == nil {
		ref := strings.TrimSpace(string(out))
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

// shouldExcludeFile mirrors git.shouldExclude (internal/git/analyzer.go).
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

// sumNumstat returns (totalAdded, totalDeleted) for the author's commits in the window,
// excluding binary files and excluded patterns.
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
