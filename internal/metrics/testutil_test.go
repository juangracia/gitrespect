package metrics

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type testRepo struct {
	t    *testing.T
	path string
}

func newTestRepo(t *testing.T) *testRepo {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "git", "init", "-q", "-b", "main")
	run(t, dir, "git", "config", "user.email", "test@example.com")
	run(t, dir, "git", "config", "user.name", "Test")
	run(t, dir, "git", "config", "commit.gpgsign", "false")
	return &testRepo{t: t, path: dir}
}

func (r *testRepo) writeFile(name, content string) {
	r.t.Helper()
	p := filepath.Join(r.path, name)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		r.t.Fatalf("writeFile: %v", err)
	}
	run(r.t, r.path, "git", "add", name)
}

func (r *testRepo) commit(msg, author string, ts time.Time) {
	r.t.Helper()
	name := parseName(author)
	email := parseEmail(author)
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME="+name,
		"GIT_AUTHOR_EMAIL="+email,
		"GIT_COMMITTER_NAME="+name,
		"GIT_COMMITTER_EMAIL="+email,
		"GIT_AUTHOR_DATE="+ts.Format(time.RFC3339),
		"GIT_COMMITTER_DATE="+ts.Format(time.RFC3339),
	)
	cmd := exec.Command("git", "-C", r.path, "commit", "-q", "--allow-empty", "-m", msg)
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		r.t.Fatalf("commit failed: %v\n%s", err, out)
	}
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
}

func parseName(author string) string {
	if i := strings.Index(author, "<"); i >= 0 {
		return strings.TrimSpace(author[:i])
	}
	return author
}

func parseEmail(author string) string {
	if i := strings.Index(author, "<"); i >= 0 {
		if j := strings.Index(author, ">"); j > i {
			return author[i+1 : j]
		}
	}
	return author
}
