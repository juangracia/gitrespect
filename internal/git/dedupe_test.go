package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// --- NormalizeRemote --------------------------------------------------------

func TestNormalizeRemoteCollapsesEquivalentURLs(t *testing.T) {
	const want = "gitlab.com/tag-api-group/tag-api-helm"
	equivalent := []string{
		"https://gitlab.com/tag-api-group/tag-api-helm.git",
		"https://gitlab.com/tag-api-group/tag-api-helm",
		"https://gitlab.com/tag-api-group/tag-api-helm/",
		"https://gitlab.com/tag-api-group/tag-api-helm.git/",
		"https://user:glpat-secret@gitlab.com/tag-api-group/tag-api-helm.git",
		"https://oauth2:token@gitlab.com/tag-api-group/tag-api-helm",
		"git@gitlab.com:tag-api-group/tag-api-helm.git",
		"git@gitlab.com:tag-api-group/tag-api-helm",
		"ssh://git@gitlab.com/tag-api-group/tag-api-helm.git",
		"ssh://git@gitlab.com:22/tag-api-group/tag-api-helm.git",
		"GITLAB.COM/tag-api-group/tag-api-helm.git",
		"  https://gitlab.com/tag-api-group/tag-api-helm.git  ",
	}
	for _, in := range equivalent {
		if got := NormalizeRemote(in); got != want {
			t.Errorf("NormalizeRemote(%q) = %q, want %q", in, got, want)
		}
	}
}

// Host names are case-insensitive but repository paths are not: folding the
// path would merge two genuinely different GitLab projects into one.
func TestNormalizeRemotePreservesPathCase(t *testing.T) {
	got := NormalizeRemote("https://GitHub.com/Acme/WidgetService.git")
	want := "github.com/Acme/WidgetService"
	if got != want {
		t.Errorf("NormalizeRemote() = %q, want %q", got, want)
	}
	if NormalizeRemote("https://github.com/acme/widget") == NormalizeRemote("https://github.com/Acme/Widget") {
		t.Error("paths differing only in case collapsed, want distinct identities")
	}
}

func TestNormalizeRemoteDistinguishesDifferentProjects(t *testing.T) {
	tests := [][2]string{
		{"https://gitlab.com/g/a.git", "https://gitlab.com/g/b.git"},
		{"https://gitlab.com/g1/p.git", "https://gitlab.com/g2/p.git"},
		{"https://gitlab.com/g/p.git", "https://github.com/g/p.git"},
	}
	for _, tc := range tests {
		if NormalizeRemote(tc[0]) == NormalizeRemote(tc[1]) {
			t.Errorf("NormalizeRemote collapsed %q and %q", tc[0], tc[1])
		}
	}
}

func TestNormalizeRemoteEdgeCases(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"   ", ""},
		{"/home/dev/projects/widget", "/home/dev/projects/widget"},
		{"file:///home/dev/projects/widget.git", "/home/dev/projects/widget"},
		// An "@" in the path is not a credential delimiter.
		{"https://gitlab.com/g/p@2.git", "gitlab.com/g/p@2"},
	}
	for _, tc := range tests {
		if got := NormalizeRemote(tc.in); got != tc.want {
			t.Errorf("NormalizeRemote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// --- DedupeByRemote ---------------------------------------------------------

// The tie-break is user visible, so it is asserted rather than assumed: the
// checkout committed to most recently is the one that survives.
func TestDedupeByRemoteKeepsMostRecentCheckout(t *testing.T) {
	parent := t.TempDir()
	stale := newRemoteRepo(t, parent, "tag-api-helm", "https://gitlab.com/g/tag-api-helm.git", stamp(2024, 1, 15))
	fresh := newRemoteRepo(t, parent, "tag-api-group-tag-api-helm", "git@gitlab.com:g/tag-api-helm.git", stamp(2025, 6, 1))

	kept, dups := DedupeByRemote([]string{stale, fresh})

	if !reflect.DeepEqual(kept, []string{fresh}) {
		t.Fatalf("kept = %v, want [%s]", kept, fresh)
	}
	if len(dups) != 1 {
		t.Fatalf("dups = %v, want 1 group", dups)
	}
	if dups[0].Remote != "gitlab.com/g/tag-api-helm" {
		t.Errorf("Remote = %q, want gitlab.com/g/tag-api-helm", dups[0].Remote)
	}
	if dups[0].Kept != fresh {
		t.Errorf("Kept = %q, want %q", dups[0].Kept, fresh)
	}
	if !reflect.DeepEqual(dups[0].Skipped, []string{stale}) {
		t.Errorf("Skipped = %v, want [%s]", dups[0].Skipped, stale)
	}
}

func TestDedupeByRemoteBreaksTiesAlphabetically(t *testing.T) {
	parent := t.TempDir()
	ts := stamp(2025, 3, 10)
	// Created in reverse so a stable result cannot come from input order.
	second := newRemoteRepo(t, parent, "b-clone", "https://gitlab.com/g/p.git", ts)
	first := newRemoteRepo(t, parent, "a-clone", "https://gitlab.com/g/p.git", ts)

	kept, dups := DedupeByRemote([]string{second, first})

	if !reflect.DeepEqual(kept, []string{first}) {
		t.Fatalf("kept = %v, want [%s]", kept, first)
	}
	if len(dups) != 1 || dups[0].Kept != first {
		t.Fatalf("dups = %v, want kept %s", dups, first)
	}
}

// A repo we cannot identify must never be dropped: understating a report is
// harder to notice than reporting a project twice.
func TestDedupeByRemoteKeepsReposWithoutRemote(t *testing.T) {
	parent := t.TempDir()
	a := newRemoteRepo(t, parent, "local-a", "", stamp(2025, 1, 1))
	b := newRemoteRepo(t, parent, "local-b", "", stamp(2025, 1, 2))

	kept, dups := DedupeByRemote([]string{a, b})

	if !reflect.DeepEqual(kept, []string{a, b}) {
		t.Errorf("kept = %v, want both repos", kept)
	}
	if len(dups) != 0 {
		t.Errorf("dups = %v, want none", dups)
	}
}

func TestDedupeByRemoteKeepsDistinctProjects(t *testing.T) {
	parent := t.TempDir()
	a := newRemoteRepo(t, parent, "alpha", "https://gitlab.com/g/alpha.git", stamp(2025, 1, 1))
	b := newRemoteRepo(t, parent, "beta", "https://gitlab.com/g/beta.git", stamp(2025, 1, 2))

	kept, dups := DedupeByRemote([]string{a, b})

	if !reflect.DeepEqual(kept, []string{a, b}) {
		t.Errorf("kept = %v, want both repos", kept)
	}
	if len(dups) != 0 {
		t.Errorf("dups = %v, want none", dups)
	}
}

func TestDedupeByRemotePreservesInputOrder(t *testing.T) {
	parent := t.TempDir()
	solo1 := newRemoteRepo(t, parent, "zulu", "https://gitlab.com/g/zulu.git", stamp(2025, 1, 1))
	stale := newRemoteRepo(t, parent, "mike-old", "https://gitlab.com/g/mike.git", stamp(2024, 1, 1))
	solo2 := newRemoteRepo(t, parent, "alpha", "https://gitlab.com/g/alpha.git", stamp(2025, 1, 1))
	fresh := newRemoteRepo(t, parent, "mike-new", "https://gitlab.com/g/mike.git", stamp(2025, 8, 1))

	kept, _ := DedupeByRemote([]string{solo1, stale, solo2, fresh})

	if !reflect.DeepEqual(kept, []string{solo1, solo2, fresh}) {
		t.Errorf("kept = %v, want input order [%s %s %s]", kept, solo1, solo2, fresh)
	}
}

func TestDedupeByRemoteSortsGroupsByRemote(t *testing.T) {
	parent := t.TempDir()
	z1 := newRemoteRepo(t, parent, "z1", "https://gitlab.com/g/zebra.git", stamp(2025, 1, 1))
	z2 := newRemoteRepo(t, parent, "z2", "https://gitlab.com/g/zebra.git", stamp(2025, 2, 1))
	a1 := newRemoteRepo(t, parent, "a1", "https://gitlab.com/g/apple.git", stamp(2025, 1, 1))
	a2 := newRemoteRepo(t, parent, "a2", "https://gitlab.com/g/apple.git", stamp(2025, 2, 1))

	_, dups := DedupeByRemote([]string{z1, z2, a1, a2})

	if len(dups) != 2 {
		t.Fatalf("dups = %v, want 2 groups", dups)
	}
	if dups[0].Remote >= dups[1].Remote {
		t.Errorf("dups not sorted by remote: %q then %q", dups[0].Remote, dups[1].Remote)
	}
}

func TestDedupeByRemoteCollapsesRepeatedPaths(t *testing.T) {
	parent := t.TempDir()
	a := newRemoteRepo(t, parent, "solo", "https://gitlab.com/g/solo.git", stamp(2025, 1, 1))

	kept, dups := DedupeByRemote([]string{a, a})

	if !reflect.DeepEqual(kept, []string{a}) {
		t.Errorf("kept = %v, want [%s]", kept, a)
	}
	if len(dups) != 0 {
		t.Errorf("dups = %v, want none for an identical path", dups)
	}
}

func TestDedupeByRemoteEmptyInput(t *testing.T) {
	kept, dups := DedupeByRemote(nil)
	if len(kept) != 0 || len(dups) != 0 {
		t.Errorf("DedupeByRemote(nil) = %v, %v, want empty", kept, dups)
	}
}

// --- helpers ----------------------------------------------------------------

// newRemoteRepo builds a repository under parent with one commit dated ts and,
// when remote is non-empty, an origin pointing at it.
func newRemoteRepo(t *testing.T, parent, name, remote string, ts time.Time) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	execGit(t, dir, "init", "-q", "-b", "main")
	execGit(t, dir, "config", "user.email", "dev@example.com")
	execGit(t, dir, "config", "user.name", "Dev")
	execGit(t, dir, "config", "commit.gpgsign", "false")
	if remote != "" {
		execGit(t, dir, "remote", "add", "origin", remote)
	}
	writeCommit(t, dir, "README.md", "hello\n", "dev@example.com", "Dev", ts)
	return dir
}

func execGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

// writeCommit commits body to name with a fixed author identity and date, so
// tie-breaks and date windows are deterministic.
func writeCommit(t *testing.T, dir, name, body, email, author string, ts time.Time) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	execGit(t, dir, "add", "-A")
	cmd := exec.Command("git", "-C", dir, "commit", "-q", "--no-gpg-sign", "-m", "c "+name)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME="+author, "GIT_AUTHOR_EMAIL="+email,
		"GIT_COMMITTER_NAME="+author, "GIT_COMMITTER_EMAIL="+email,
		"GIT_AUTHOR_DATE="+ts.Format(time.RFC3339),
		"GIT_COMMITTER_DATE="+ts.Format(time.RFC3339),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit %s: %v\n%s", name, err, out)
	}
}

func stamp(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 12, 0, 0, 0, time.UTC)
}
