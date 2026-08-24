package git

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

// --- FilterBots -------------------------------------------------------------

func TestDefaultBotPatternsCompile(t *testing.T) {
	for _, p := range DefaultBotPatterns {
		if _, err := regexp.Compile("(?i)" + p); err != nil {
			t.Errorf("DefaultBotPatterns %q does not compile: %v", p, err)
		}
	}
}

// GitLab mints one bot address per project access token, so an unfiltered scan
// of a large group is mostly CI and the humans it exists to find are buried.
func TestFilterBotsRemovesAutomationIdentities(t *testing.T) {
	bots := []Contributor{
		{Email: "group_12345_bot_a1b2c3@noreply.gitlab.com", Name: "group_12345_bot"},
		{Email: "project_98765_bot_deadbeef@noreply.gitlab.com", Name: "project bot"},
		{Email: "gitlab_bot_something@noreply.gitlab.com", Name: "some bot"},
		{Email: "semantic-release-bot@martynus.net", Name: "semantic-release-bot"},
		{Email: "49699333+dependabot[bot]@users.noreply.github.com", Name: "dependabot[bot]"},
		{Email: "bot@renovateapp.com", Name: "Renovate Bot"},
		{Email: "29139614+renovate[bot]@users.noreply.github.com", Name: "renovate[bot]"},
		{Email: "actions@github.com", Name: "GitHub Actions"},
		// The name carries the marker even when the address does not.
		{Email: "1234@users.noreply.github.com", Name: "some-app[bot]"},
	}
	got, err := FilterBots(bots, nil)
	if err != nil {
		t.Fatalf("FilterBots: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("FilterBots kept %v, want all removed", got)
	}
}

func TestFilterBotsKeepsHumans(t *testing.T) {
	humans := []Contributor{
		{Email: "juan@corp.com", Name: "Juan Gracia"},
		{Email: "wesleyornellas@MacBook---Wesley.local", Name: "Wesley"},
		{Email: "juanmgracia@gmail.com", Name: "Juan Gracia"},
		{Email: "robert@corp.com", Name: "Robert Botello"},
	}
	got, err := FilterBots(humans, nil)
	if err != nil {
		t.Fatalf("FilterBots: %v", err)
	}
	if !reflect.DeepEqual(got, humans) {
		t.Errorf("FilterBots = %v, want all kept", got)
	}
}

func TestFilterBotsAppliesExtraExcludes(t *testing.T) {
	in := []Contributor{
		{Email: "juan@corp.com", Name: "Juan Gracia"},
		{Email: "release@corp.com", Name: "Release Robot"},
	}
	got, err := FilterBots(in, []string{`^release@corp\.com$`})
	if err != nil {
		t.Fatalf("FilterBots: %v", err)
	}
	if len(got) != 1 || got[0].Email != "juan@corp.com" {
		t.Errorf("FilterBots = %v, want only juan@corp.com", got)
	}
}

// Extra excludes are case-insensitive, because someone typing an address does
// not think about the casing git recorded.
func TestFilterBotsExtraExcludesIgnoreCase(t *testing.T) {
	in := []Contributor{{Email: "Release@Corp.com", Name: "Release"}}
	got, err := FilterBots(in, []string{"release@corp.com"})
	if err != nil {
		t.Fatalf("FilterBots: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("FilterBots = %v, want the address excluded", got)
	}
}

// A pattern that cannot compile must be reported. Ignoring it would leave the
// user believing they had filtered an identity that is still in every total.
func TestFilterBotsRejectsInvalidRegex(t *testing.T) {
	_, err := FilterBots(nil, []string{"[unclosed"})
	if err == nil {
		t.Fatal("FilterBots accepted an invalid pattern, want error")
	}
	if !strings.Contains(err.Error(), "[unclosed") {
		t.Errorf("error %q does not name the offending pattern", err)
	}
}

// --- TopContributors --------------------------------------------------------

func TestTopContributorsAggregatesAcrossRepos(t *testing.T) {
	parent := t.TempDir()
	r1 := newRemoteRepo(t, parent, "one", "", stamp(2025, 1, 1))
	writeCommit(t, r1, "a.txt", "a\n", "alice@corp.com", "Alice", stamp(2025, 1, 2))
	writeCommit(t, r1, "b.txt", "b\n", "alice@corp.com", "Alice", stamp(2025, 1, 3))
	writeCommit(t, r1, "c.txt", "c\n", "bob@corp.com", "Bob", stamp(2025, 1, 4))
	writeCommit(t, r1, "ci.txt", "ci\n", "project_9_bot_tok@noreply.gitlab.com", "CI", stamp(2025, 1, 5))

	r2 := newRemoteRepo(t, parent, "two", "", stamp(2025, 1, 1))
	writeCommit(t, r2, "d.txt", "d\n", "alice@corp.com", "Alice", stamp(2025, 1, 6))

	got, err := TopContributors([]string{r1, r2}, stamp(2024, 1, 1), stamp(2026, 1, 1), 0, nil)
	if err != nil {
		t.Fatalf("TopContributors: %v", err)
	}

	// Both repos have a seed commit from newRemoteRepo's dev@example.com.
	want := []Contributor{
		{Email: "alice@corp.com", Name: "Alice", Commits: 3},
		{Email: "dev@example.com", Name: "Dev", Commits: 2},
		{Email: "bob@corp.com", Name: "Bob", Commits: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TopContributors = %+v, want %+v", got, want)
	}
}

func TestTopContributorsHonoursLimit(t *testing.T) {
	parent := t.TempDir()
	r := newRemoteRepo(t, parent, "one", "", stamp(2025, 1, 1))
	writeCommit(t, r, "a.txt", "a\n", "alice@corp.com", "Alice", stamp(2025, 1, 2))
	writeCommit(t, r, "b.txt", "b\n", "alice@corp.com", "Alice", stamp(2025, 1, 3))
	writeCommit(t, r, "c.txt", "c\n", "bob@corp.com", "Bob", stamp(2025, 1, 4))

	got, err := TopContributors([]string{r}, stamp(2024, 1, 1), stamp(2026, 1, 1), 1, nil)
	if err != nil {
		t.Fatalf("TopContributors: %v", err)
	}
	if len(got) != 1 || got[0].Email != "alice@corp.com" {
		t.Errorf("TopContributors = %+v, want only the busiest author", got)
	}
}

// Equal commit counts must not come out in map order, or two runs over the
// same tree would produce different team lists.
func TestTopContributorsBreaksTiesByEmail(t *testing.T) {
	parent := t.TempDir()
	r := newRemoteRepo(t, parent, "one", "", stamp(2025, 1, 1))
	writeCommit(t, r, "z.txt", "z\n", "zoe@corp.com", "Zoe", stamp(2025, 1, 2))
	writeCommit(t, r, "m.txt", "m\n", "mia@corp.com", "Mia", stamp(2025, 1, 3))
	writeCommit(t, r, "a.txt", "a\n", "ana@corp.com", "Ana", stamp(2025, 1, 4))

	got, err := TopContributors([]string{r}, stamp(2024, 1, 1), stamp(2026, 1, 1), 0, []string{`^dev@example\.com$`})
	if err != nil {
		t.Fatalf("TopContributors: %v", err)
	}
	var emails []string
	for _, c := range got {
		emails = append(emails, c.Email)
	}
	want := []string{"ana@corp.com", "mia@corp.com", "zoe@corp.com"}
	if !reflect.DeepEqual(emails, want) {
		t.Errorf("emails = %v, want %v", emails, want)
	}
}

// The same person writing Alice@Corp.com on one machine must not split into
// two contributors, which is what an exact-match count would do.
func TestTopContributorsFoldsEmailCase(t *testing.T) {
	parent := t.TempDir()
	r := newRemoteRepo(t, parent, "one", "", stamp(2025, 1, 1))
	writeCommit(t, r, "a.txt", "a\n", "alice@corp.com", "Alice", stamp(2025, 1, 2))
	writeCommit(t, r, "b.txt", "b\n", "Alice@Corp.com", "Alice", stamp(2025, 1, 3))

	got, err := TopContributors([]string{r}, stamp(2024, 1, 1), stamp(2026, 1, 1), 1, nil)
	if err != nil {
		t.Fatalf("TopContributors: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("TopContributors = %+v, want one contributor", got)
	}
	if got[0].Commits != 2 {
		t.Errorf("Commits = %d, want 2", got[0].Commits)
	}
	if !strings.EqualFold(got[0].Email, "alice@corp.com") {
		t.Errorf("Email = %q, want a spelling of alice@corp.com", got[0].Email)
	}
}

// %aE is mailmap-mapped where %ae is not, so a repo that already maintains a
// .mailmap must show one contributor rather than one per alias.
func TestTopContributorsCollapsesMailmapAliases(t *testing.T) {
	parent := t.TempDir()
	r := newRemoteRepo(t, parent, "one", "", stamp(2025, 1, 1))
	writeCommit(t, r, "a.txt", "a\n", "wesley@corp.com", "Wesley Ornellas", stamp(2025, 1, 2))
	writeCommit(t, r, "b.txt", "b\n", "wesleyornellas@MacBook---Wesley.local", "Wesley", stamp(2025, 1, 3))

	mailmap := "Wesley Ornellas <wesley@corp.com> <wesleyornellas@MacBook---Wesley.local>\n"
	if err := os.WriteFile(filepath.Join(r, ".mailmap"), []byte(mailmap), 0o644); err != nil {
		t.Fatalf("write mailmap: %v", err)
	}

	got, err := TopContributors([]string{r}, stamp(2024, 1, 1), stamp(2026, 1, 1), 1, nil)
	if err != nil {
		t.Fatalf("TopContributors: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("TopContributors = %+v, want one contributor", got)
	}
	if got[0].Email != "wesley@corp.com" || got[0].Commits != 2 {
		t.Errorf("got %+v, want wesley@corp.com with 2 commits", got[0])
	}
}

func TestTopContributorsRespectsDateWindow(t *testing.T) {
	parent := t.TempDir()
	r := newRemoteRepo(t, parent, "one", "", stamp(2024, 1, 1))
	writeCommit(t, r, "old.txt", "old\n", "alice@corp.com", "Alice", stamp(2024, 6, 1))
	writeCommit(t, r, "new.txt", "new\n", "bob@corp.com", "Bob", stamp(2025, 6, 1))

	got, err := TopContributors([]string{r}, stamp(2025, 1, 1), stamp(2026, 1, 1), 0, nil)
	if err != nil {
		t.Fatalf("TopContributors: %v", err)
	}
	if len(got) != 1 || got[0].Email != "bob@corp.com" {
		t.Errorf("TopContributors = %+v, want only bob@corp.com in the window", got)
	}
}

// One broken clone in a tree of hundreds must not cost the whole scan.
func TestTopContributorsSkipsUnreadablePaths(t *testing.T) {
	parent := t.TempDir()
	r := newRemoteRepo(t, parent, "one", "", stamp(2025, 1, 1))
	writeCommit(t, r, "a.txt", "a\n", "alice@corp.com", "Alice", stamp(2025, 1, 2))
	notARepo := filepath.Join(parent, "not-a-repo")
	if err := os.MkdirAll(notARepo, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, err := TopContributors([]string{notARepo, r}, stamp(2024, 1, 1), stamp(2026, 1, 1), 1, nil)
	if err != nil {
		t.Fatalf("TopContributors: %v", err)
	}
	if len(got) != 1 || got[0].Email != "alice@corp.com" {
		t.Errorf("TopContributors = %+v, want alice@corp.com", got)
	}
}

// If nothing could be read the result would be a confident empty list, which
// looks exactly like a team that did no work.
func TestTopContributorsErrorsWhenEveryPathFails(t *testing.T) {
	parent := t.TempDir()
	a := filepath.Join(parent, "a")
	b := filepath.Join(parent, "b")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	if _, err := TopContributors([]string{a, b}, stamp(2024, 1, 1), stamp(2026, 1, 1), 0, nil); err == nil {
		t.Error("TopContributors succeeded with no readable repos, want error")
	}
}

func TestTopContributorsRejectsInvalidExclude(t *testing.T) {
	parent := t.TempDir()
	r := newRemoteRepo(t, parent, "one", "", stamp(2025, 1, 1))

	_, err := TopContributors([]string{r}, stamp(2024, 1, 1), stamp(2026, 1, 1), 0, []string{"[unclosed"})
	if err == nil {
		t.Fatal("TopContributors accepted an invalid pattern, want error")
	}
	if !strings.Contains(err.Error(), "[unclosed") {
		t.Errorf("error %q does not name the offending pattern", err)
	}
}

func TestTopContributorsNoPaths(t *testing.T) {
	got, err := TopContributors(nil, stamp(2024, 1, 1), stamp(2026, 1, 1), 0, nil)
	if err != nil {
		t.Fatalf("TopContributors(nil): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("TopContributors(nil) = %v, want empty", got)
	}
}

// An unset bound must not silently exclude everything: a zero time formats as
// year 1, which as an --until would match no commit at all.
func TestTopContributorsZeroBoundsScanEverything(t *testing.T) {
	parent := t.TempDir()
	r := newRemoteRepo(t, parent, "one", "", stamp(2025, 1, 1))
	writeCommit(t, r, "a.txt", "a\n", "alice@corp.com", "Alice", stamp(2025, 1, 2))

	got, err := TopContributors([]string{r}, time.Time{}, time.Time{}, 0, nil)
	if err != nil {
		t.Fatalf("TopContributors: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("TopContributors = %+v, want both authors", got)
	}
}
