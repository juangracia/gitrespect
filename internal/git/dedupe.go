package git

import (
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// DuplicateGroup records one set of repository paths that resolve to the same
// upstream project, together with the checkout that was kept.
type DuplicateGroup struct {
	Remote  string   // normalized remote identity shared by the group
	Kept    string   // repo path retained
	Skipped []string // repo paths dropped, sorted
}

// DedupeByRemote drops repositories that are a second checkout of a project
// already present in the list.
//
// A recursive scan over a working machine routinely finds the same project
// twice: an old flat clone next to a newer nested "<service>-group/<service>"
// layout. Both carry the full history, so counting both doubles that project's
// lines and inflates a team total by thousands of lines with nothing in the
// report to suggest anything is wrong.
//
// Repositories with no origin remote, or whose remote cannot be read, are
// always kept. Dropping a repo we failed to identify would understate the
// report, and an understated report is the harder error to notice.
//
// kept preserves the input order of the repositories that survive; dups is
// sorted by remote so the same tree always produces the same summary.
func DedupeByRemote(repos []string) (kept []string, dups []DuplicateGroup) {
	// An identical path listed twice is the same clone, not two checkouts
	// worth reporting on, so collapse those before spending a git call on
	// each entry.
	unique := make([]string, 0, len(repos))
	seenPath := make(map[string]bool, len(repos))
	for _, p := range repos {
		if seenPath[p] {
			continue
		}
		seenPath[p] = true
		unique = append(unique, p)
	}

	remotes := make([]string, len(unique))
	groups := make(map[string][]string)
	for i, p := range unique {
		id := NormalizeRemote(remoteURL(p))
		remotes[i] = id
		if id == "" {
			continue
		}
		groups[id] = append(groups[id], p)
	}

	winner := make(map[string]string, len(groups))
	for id, members := range groups {
		// Most projects appear once, and asking git for a commit date there
		// would double the number of subprocesses a several-hundred-repo scan
		// spawns for nothing.
		if len(members) == 1 {
			winner[id] = members[0]
			continue
		}
		winner[id] = pickCheckout(members)
	}

	kept = make([]string, 0, len(unique))
	for i, p := range unique {
		id := remotes[i]
		if id == "" || winner[id] == p {
			kept = append(kept, p)
		}
	}

	for id, members := range groups {
		if len(members) < 2 {
			continue
		}
		w := winner[id]
		skipped := make([]string, 0, len(members)-1)
		for _, m := range members {
			if m != w {
				skipped = append(skipped, m)
			}
		}
		sort.Strings(skipped)
		dups = append(dups, DuplicateGroup{Remote: id, Kept: w, Skipped: skipped})
	}
	sort.Slice(dups, func(i, j int) bool { return dups[i].Remote < dups[j].Remote })

	return kept, dups
}

// pickCheckout chooses which clone of a project to keep.
//
// The rule is user visible, so it is worth stating plainly: the checkout with
// the most recent commit wins, because that is the one being worked in, and a
// stale clone can be missing months of history. Ties, and repositories whose
// commit date cannot be read, fall back to the alphabetically first path so
// the choice is stable across runs.
func pickCheckout(members []string) string {
	best := ""
	var bestTS int64
	for _, m := range members {
		ts := lastCommitUnix(m)
		if best == "" || ts > bestTS || (ts == bestTS && m < best) {
			best, bestTS = m, ts
		}
	}
	return best
}

// NormalizeRemote reduces a remote URL to a comparable project identity.
//
// The same project gets cloned over HTTPS on one machine, over SSH on another
// and with an embedded access token by CI, and every one of those spellings
// has to collapse to a single identity or the dedupe does nothing. An empty
// result means "no identity", which callers must read as "never merge this".
func NormalizeRemote(url string) string {
	s := strings.TrimSpace(url)
	if s == "" {
		return ""
	}

	// Transport is not part of a project's identity: https, ssh and git all
	// name the same repository.
	scpLike := false
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+len("://"):]
	} else if c := strings.IndexByte(s, ':'); c >= 0 {
		// scp-like "git@host:group/project" has its colon before any slash.
		// That colon separates host from path, not host from port.
		if sl := strings.IndexByte(s, '/'); sl < 0 || c < sl {
			scpLike = true
		}
	}

	// Credentials belong to whoever cloned, not to the project, so an
	// anonymous clone and a token-bearing CI clone must not look different.
	// The delimiter is the last "@" inside the authority, since a path may
	// legitimately contain one.
	authEnd := strings.IndexByte(s, '/')
	if authEnd < 0 {
		authEnd = len(s)
	}
	if at := strings.LastIndex(s[:authEnd], "@"); at >= 0 {
		s = s[at+1:]
	}

	if scpLike {
		if c := strings.IndexByte(s, ':'); c >= 0 {
			s = s[:c] + "/" + s[c+1:]
		}
	}

	host, rest := s, ""
	if sl := strings.IndexByte(s, '/'); sl >= 0 {
		host, rest = s[:sl], s[sl:]
	}
	// Host names are case-insensitive, repository paths are not: GitLab and
	// GitHub both treat Group/Project and group/project as distinct projects,
	// so only the host is folded.
	host = strings.ToLower(host)
	// An explicit port and a bare host name the same server.
	if c := strings.LastIndex(host, ":"); c >= 0 && isAllDigits(host[c+1:]) {
		host = host[:c]
	}
	s = host + rest

	s = strings.TrimRight(s, "/")
	s = strings.TrimSuffix(s, ".git")
	s = strings.TrimRight(s, "/")

	return s
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// remoteURL reads the origin remote, returning "" when the repository has no
// origin or git cannot be run there.
func remoteURL(repoPath string) string {
	cmd := exec.Command("git", "-C", repoPath, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	// A remote configured with several URLs prints one per line; the first is
	// the one git fetches from.
	first, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return strings.TrimSpace(first)
}

// lastCommitUnix returns the committer timestamp of HEAD, or -1 when it
// cannot be read so an unreadable repo always loses the tie-break.
func lastCommitUnix(repoPath string) int64 {
	cmd := exec.Command("git", "-C", repoPath, "log", "-1", "--format=%ct")
	out, err := cmd.Output()
	if err != nil {
		return -1
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return -1
	}
	return ts
}
