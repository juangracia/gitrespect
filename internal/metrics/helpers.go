package metrics

import (
	"os/exec"
	"sort"
	"strings"
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

// isReadableRepo reports whether git can resolve path as a repository.
//
// The branch-based metrics need to tell "a repo we read, which has no
// main-like branch" apart from "not a repo at all". The first is a finding to
// report, the second is a failure that must not be counted as coverage, and
// detectMainBranch answers the empty string to both.
//
// This asks git rather than looking for a .git directory, because in a worktree
// .git is a file and an on-disk check would reject a perfectly readable repo.
func isReadableRepo(repoPath string) bool {
	return exec.Command("git", "-C", repoPath, "rev-parse", "--git-dir").Run() == nil
}

// median is where pooled samples land. Every multi-repo metric concatenates the
// raw samples from each repository and calls this once. Taking each repo's
// median and averaging those would weight a repo with three commits the same as
// one with three hundred, and the result would not be a median of anything.
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
