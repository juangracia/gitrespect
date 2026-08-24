package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// DuplicateGroup records one set of repository paths that resolve to the same
// project, together with the checkout that was kept.
type DuplicateGroup struct {
	// Remote is the identity the group was matched on: the normalized origin
	// URL when the repositories have one, and the resolved git directory when
	// they do not.
	Remote  string
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
	unique := collapseSamePath(repos)

	keys := make([]string, len(unique))
	display := make(map[string]string, len(unique))
	groups := make(map[string][]string)
	for i, p := range unique {
		key, shown := repoIdentity(p)
		keys[i] = key
		if key == "" {
			continue
		}
		if _, ok := display[key]; !ok {
			display[key] = shown
		}
		groups[key] = append(groups[key], p)
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
		id := keys[i]
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
		dups = append(dups, DuplicateGroup{Remote: display[id], Kept: w, Skipped: skipped})
	}
	sort.Slice(dups, func(i, j int) bool { return dups[i].Remote < dups[j].Remote })

	return kept, dups
}

// collapseSamePath removes entries that name the same directory on disk.
//
// filepath.Abs in the caller resolves neither symlinks nor the spelling
// differences a case-insensitive filesystem accepts, so "/repo" and "/Repo",
// or a directory and a symlink to it, arrive here as different strings naming
// one repository. Comparing the actual file identity catches both, and doing
// it here means the git calls below are never spent on a path we already have.
//
// A path that cannot be stat'ed is kept and left for the identity check, since
// dropping something we failed to inspect would understate the report.
func collapseSamePath(repos []string) []string {
	out := make([]string, 0, len(repos))
	seenString := make(map[string]bool, len(repos))
	seenFile := make([]os.FileInfo, 0, len(repos))

	for _, p := range repos {
		if seenString[p] {
			continue
		}
		seenString[p] = true

		info, err := os.Stat(p)
		if err != nil {
			out = append(out, p)
			continue
		}
		duplicate := false
		for _, prev := range seenFile {
			if os.SameFile(prev, info) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		seenFile = append(seenFile, info)
		out = append(out, p)
	}
	return out
}

// repoIdentity resolves what a repository IS, for grouping, and returns both
// the grouping key and the identity to show in a duplicate report.
//
// An empty key means "unidentifiable", which callers must treat as "never
// merge this with anything".
func repoIdentity(repoPath string) (key, display string) {
	if remote := NormalizeRemote(remoteURL(repoPath)); remote != "" {
		return "remote:" + remote, remote
	}
	// A repository with no origin still has an identity: the git directory it
	// actually uses. Without this, a remote-less repo reached by two path
	// spellings is counted twice, which is the same silent double count the
	// remote check exists to prevent.
	if dir := canonicalGitDir(repoPath); dir != "" {
		return "gitdir:" + dir, dir
	}
	// The two kinds of identity are namespaced so a local clone whose remote
	// is a filesystem path can never collide with another repo's git dir.
	return "", ""
}

// canonicalGitDir returns the resolved git directory a repository uses, or ""
// when it cannot be determined.
func canonicalGitDir(repoPath string) string {
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "--absolute-git-dir")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return ""
	}
	// git prints the path as reached, so it still contains whatever symlinks
	// the caller walked through. Resolve it before using it as an identity.
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	return dir
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
	// name the same repository. The scheme is still remembered, because it
	// decides which port is redundant below.
	scheme := ""
	scpLike := false
	if i := strings.Index(s, "://"); i >= 0 {
		scheme = strings.ToLower(s[:i])
		s = s[i+len("://"):]
		// "://host/path" names no protocol at all. Returning a plausible
		// looking identity for it would merge two unrelated broken remotes,
		// so malformed input has to mean "no identity" instead.
		if scheme == "" || s == "" {
			return ""
		}
	} else if c := strings.IndexByte(s, ':'); c >= 0 {
		// scp-like "git@host:group/project" has its colon before any slash.
		// That colon separates host from path, not host from port.
		if sl := strings.IndexByte(s, '/'); sl < 0 || c < sl {
			scpLike = true
			scheme = "ssh" // scp syntax is ssh underneath
		}
	}

	// The authority ends at the first "/" in a URL, but at the ":" in an
	// scp-like address. Using the slash for both lets an "@" inside the path
	// swallow the host: git@gitlab.com:my@group/proj would reduce to
	// "group/proj", merging every forge that hosts that path.
	authEnd := len(s)
	if scpLike {
		if c := strings.IndexByte(s, ':'); c >= 0 {
			authEnd = c
		}
	} else if sl := strings.IndexByte(s, '/'); sl >= 0 {
		authEnd = sl
	}
	// Credentials belong to whoever cloned, not to the project, so an
	// anonymous clone and a token-bearing CI clone must not look different.
	// The delimiter is the last "@" inside the authority, since a path may
	// legitimately contain one.
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
	// Host names are case-insensitive.
	host = strings.ToLower(host)
	// Only a protocol's own default port is redundant. Stripping an arbitrary
	// one merges two self-hosted instances that differ only by port, which
	// erases a whole project's work from the total.
	if c := strings.LastIndex(host, ":"); c >= 0 && isAllDigits(host[c+1:]) {
		if host[c+1:] == defaultPort(scheme) {
			host = host[:c]
		}
	}

	if foldsPathCase(host) {
		rest = strings.ToLower(rest)
	}

	out := host + rest
	out = strings.TrimRight(out, "/")
	out = strings.TrimSuffix(out, ".git")
	out = strings.TrimRight(out, "/")

	if out == "" {
		return ""
	}
	// A URL that names a server but no project is not a project identity.
	// Two repos whose remotes are both "https://gitlab.com" are not the same
	// repository, and merging them would delete one from the report.
	if (scheme != "" || scpLike) && out == host {
		return ""
	}
	return out
}

// defaultPort is the port a scheme implies, so that naming it explicitly and
// omitting it produce one identity. An unrecognised scheme returns "", which
// matches no port and therefore strips nothing.
func defaultPort(scheme string) string {
	switch scheme {
	case "ssh", "git+ssh", "ssh+git":
		return "22"
	case "http":
		return "80"
	case "https":
		return "443"
	case "git":
		return "9418"
	}
	return ""
}

// caseInsensitivePathHosts are forges whose repository paths are
// case-insensitive, where /Foo/Bar and /foo/bar are one repository. Folding
// the path for these is what stops two spellings of one clone being counted
// twice.
var caseInsensitivePathHosts = map[string]bool{
	"github.com":     true,
	"www.github.com": true,
}

// caseSensitivePathHosts are forges whose repository paths are case-sensitive,
// where folding would merge two genuinely different projects. Listed
// explicitly rather than left to the default, so the contrast with the map
// above is visible to the next reader instead of implied by absence.
var caseSensitivePathHosts = map[string]bool{
	"gitlab.com":     true,
	"www.gitlab.com": true,
}

// foldsPathCase reports whether host's repository paths should be lowercased.
//
// An unknown host is treated as case-sensitive. The two failure modes are not
// symmetric: folding a case-sensitive host merges two real projects and erases
// one from the total with no trace, while failing to fold a case-insensitive
// one merely counts a project twice, which the duplicate report then makes
// visible. Guess toward the recoverable error.
//
// Self-hosted GitHub Enterprise cannot be recognised by hostname, so it falls
// under the case-sensitive default and its duplicates are reported rather than
// collapsed.
func foldsPathCase(host string) bool {
	if caseSensitivePathHosts[host] {
		return false
	}
	return caseInsensitivePathHosts[host]
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
