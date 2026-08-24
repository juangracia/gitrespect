package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// --- ParseAlias -------------------------------------------------------------

func TestParseAliasAcceptsInlineSpec(t *testing.T) {
	got, err := ParseAlias("Juan Gracia=a@x.com, b@x.com ")
	if err != nil {
		t.Fatalf("ParseAlias: %v", err)
	}
	want := Identity{Name: "Juan Gracia", Emails: []string{"a@x.com", "b@x.com"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseAlias = %+v, want %+v", got, want)
	}
}

func TestParseAliasRejectsMalformedSpecs(t *testing.T) {
	tests := []string{
		"Juan Gracia",       // no separator
		"=a@x.com",          // no name
		"Juan Gracia=",      // no addresses
		"Juan Gracia= , , ", // only empty addresses
	}
	for _, spec := range tests {
		if _, err := ParseAlias(spec); err == nil {
			t.Errorf("ParseAlias(%q) succeeded, want error", spec)
		}
	}
}

// --- Identity ---------------------------------------------------------------

func TestIdentityLabelPrefersName(t *testing.T) {
	if got := (Identity{Name: "Juan Gracia", Emails: []string{"a@x.com"}}).Label(); got != "Juan Gracia" {
		t.Errorf("Label = %q, want the name", got)
	}
	if got := (Identity{Emails: []string{"a@x.com"}}).Label(); got != "a@x.com" {
		t.Errorf("Label = %q, want the first address", got)
	}
	// An empty identity must not panic; reports call Label unconditionally.
	if got := (Identity{}).Label(); got != "" {
		t.Errorf("Label = %q, want empty", got)
	}
}

func TestSoloIdentity(t *testing.T) {
	got := SoloIdentity("  a@x.com ")
	if got.Name != "" || !reflect.DeepEqual(got.Emails, []string{"a@x.com"}) {
		t.Errorf("SoloIdentity = %+v, want the trimmed address only", got)
	}
	if got.Label() != "a@x.com" {
		t.Errorf("Label = %q, want a@x.com", got.Label())
	}
}

func TestAuthorArgsForMatchesAuthorArgsMulti(t *testing.T) {
	id := Identity{Name: "Juan Gracia", Emails: []string{"a@x.com", "b@x.com"}}
	got := AuthorArgsFor(id)
	want := AuthorArgsMulti(id.Emails)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AuthorArgsFor = %v, want %v", got, want)
	}
	// The display name must not become a filter: names are not unique and
	// matching on one would pull in unrelated contributors.
	for _, a := range got {
		if strings.Contains(a, "Juan Gracia") {
			t.Errorf("AuthorArgsFor leaked the display name into %q", a)
		}
	}
}

// --- Roster.Resolve ---------------------------------------------------------

func TestRosterResolveByNameOrEmail(t *testing.T) {
	r := Roster{
		{Name: "Juan Gracia", Emails: []string{"juan@corp.com", "juanmgracia@gmail.com"}},
		{Name: "Wesley Ornellas", Emails: []string{"wesley@corp.com"}},
	}
	tests := []struct {
		token string
		want  string
	}{
		{"Juan Gracia", "Juan Gracia"},
		{"juan gracia", "Juan Gracia"},
		{"juan@corp.com", "Juan Gracia"},
		{"JUANMGRACIA@GMAIL.COM", "Juan Gracia"},
		{" wesley@corp.com ", "Wesley Ornellas"},
	}
	for _, tc := range tests {
		id, ok := r.Resolve(tc.token)
		if !ok {
			t.Errorf("Resolve(%q) not found", tc.token)
			continue
		}
		if id.Name != tc.want {
			t.Errorf("Resolve(%q) = %q, want %q", tc.token, id.Name, tc.want)
		}
	}

	if _, ok := r.Resolve("nobody@corp.com"); ok {
		t.Error("Resolve found an unknown address")
	}
	// An empty token must not match the first entry by accident, which would
	// silently report one person's work as somebody else's.
	if _, ok := r.Resolve("  "); ok {
		t.Error("Resolve matched an empty token")
	}
}

// --- Roster.Validate --------------------------------------------------------

func TestRosterValidateRejectsBadRosters(t *testing.T) {
	tests := []struct {
		name    string
		roster  Roster
		wantSub string
	}{
		{
			name:    "no addresses",
			roster:  Roster{{Name: "Juan Gracia"}},
			wantSub: "no email addresses",
		},
		{
			name: "shared address",
			roster: Roster{
				{Name: "Juan Gracia", Emails: []string{"shared@corp.com"}},
				{Name: "Wesley Ornellas", Emails: []string{"SHARED@corp.com"}},
			},
			// The message names the address as written on the offending line,
			// so the reader can search the file for it.
			wantSub: "SHARED@corp.com",
		},
		{
			name: "duplicate person",
			roster: Roster{
				{Name: "Juan Gracia", Emails: []string{"a@corp.com"}},
				{Name: "juan gracia", Emails: []string{"b@corp.com"}},
			},
			wantSub: "listed twice",
		},
	}
	for _, tc := range tests {
		err := tc.roster.Validate()
		if err == nil {
			t.Errorf("%s: Validate succeeded, want error", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantSub) {
			t.Errorf("%s: error %q does not mention %q", tc.name, err, tc.wantSub)
		}
	}
}

func TestRosterValidateAcceptsGoodRoster(t *testing.T) {
	r := Roster{
		{Name: "Juan Gracia", Emails: []string{"juan@corp.com", "juanmgracia@gmail.com"}},
		{Name: "Wesley Ornellas", Emails: []string{"wesley@corp.com"}},
	}
	if err := r.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// --- LoadRoster -------------------------------------------------------------

func TestLoadRosterLineFormat(t *testing.T) {
	path := writeRoster(t, "team.yaml", `
# the team, one person per line
Juan Gracia: juan@corp.com, juanmgracia@gmail.com

Wesley Ornellas: wesley@corp.com, wesleyornellas@MacBook---Wesley.local  # laptop default
`)
	got, err := LoadRoster(path)
	if err != nil {
		t.Fatalf("LoadRoster: %v", err)
	}
	want := Roster{
		{Name: "Juan Gracia", Emails: []string{"juan@corp.com", "juanmgracia@gmail.com"}},
		{Name: "Wesley Ornellas", Emails: []string{"wesley@corp.com", "wesleyornellas@MacBook---Wesley.local"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LoadRoster = %+v, want %+v", got, want)
	}
}

// Quoted keys keep the line format readable as YAML, which is the whole reason
// this parser exists instead of a YAML dependency.
func TestLoadRosterLineFormatAcceptsQuotedNames(t *testing.T) {
	path := writeRoster(t, "team.yaml", "\"Juan Gracia\": juan@corp.com\n")
	got, err := LoadRoster(path)
	if err != nil {
		t.Fatalf("LoadRoster: %v", err)
	}
	if got[0].Name != "Juan Gracia" {
		t.Errorf("Name = %q, want Juan Gracia", got[0].Name)
	}
}

func TestLoadRosterJSONFormat(t *testing.T) {
	path := writeRoster(t, "team.json", `{
  "Wesley Ornellas": ["wesley@corp.com"],
  "Juan Gracia": ["juan@corp.com", "juanmgracia@gmail.com"]
}`)
	got, err := LoadRoster(path)
	if err != nil {
		t.Fatalf("LoadRoster: %v", err)
	}
	// Object key order is not preserved by the decoder, so identities come
	// back sorted by name and reports stay stable between runs.
	want := Roster{
		{Name: "Juan Gracia", Emails: []string{"juan@corp.com", "juanmgracia@gmail.com"}},
		{Name: "Wesley Ornellas", Emails: []string{"wesley@corp.com"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LoadRoster = %+v, want %+v", got, want)
	}
}

func TestLoadRosterRejectsBadFiles(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		file    string
		wantSub string
	}{
		{
			name:    "missing colon",
			file:    "team.yaml",
			body:    "Juan Gracia juan@corp.com\n",
			wantSub: "line 1",
		},
		{
			name:    "missing colon on a later line",
			file:    "team.yaml",
			body:    "Juan Gracia: juan@corp.com\n# comment\nWesley Ornellas wesley@corp.com\n",
			wantSub: "line 3",
		},
		{
			name:    "no addresses",
			file:    "team.yaml",
			body:    "Juan Gracia:\n",
			wantSub: "no email addresses",
		},
		{
			name: "address shared by two people",
			file: "team.yaml",
			body: "Juan Gracia: shared@corp.com\nWesley Ornellas: SHARED@corp.com\n",
			// The message names the address as written on the offending line,
			// so the reader can search the file for it.
			wantSub: "SHARED@corp.com",
		},
		{
			name:    "empty roster",
			file:    "team.yaml",
			body:    "# nobody here\n\n",
			wantSub: "no identities",
		},
		{
			name:    "malformed JSON",
			file:    "team.json",
			body:    `{"Juan Gracia": "juan@corp.com"}`,
			wantSub: "JSON",
		},
		{
			name:    "JSON identity with no addresses",
			file:    "team.json",
			body:    `{"Juan Gracia": []}`,
			wantSub: "no email addresses",
		},
	}
	for _, tc := range tests {
		path := writeRoster(t, tc.file, tc.body)
		_, err := LoadRoster(path)
		if err == nil {
			t.Errorf("%s: LoadRoster succeeded, want error", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantSub) {
			t.Errorf("%s: error %q does not mention %q", tc.name, err, tc.wantSub)
		}
	}
}

// The duplicate-address error has to point at the line, because a roster for a
// real team runs to dozens of entries.
func TestLoadRosterDuplicateErrorNamesTheLine(t *testing.T) {
	path := writeRoster(t, "team.yaml", "Juan Gracia: shared@corp.com\n\nWesley Ornellas: shared@corp.com\n")
	_, err := LoadRoster(path)
	if err == nil {
		t.Fatal("LoadRoster succeeded, want error")
	}
	if !strings.Contains(err.Error(), "line 3") {
		t.Errorf("error %q does not name line 3", err)
	}
}

func TestLoadRosterMissingFile(t *testing.T) {
	if _, err := LoadRoster(filepath.Join(t.TempDir(), "absent.yaml")); err == nil {
		t.Error("LoadRoster on a missing file succeeded, want error")
	}
}

// A user told to write a "--roster roster.yaml" file reaches for ordinary
// block-style YAML first. The error has to name that as the problem: saying
// the addresses are missing reads as "my emails were not picked up", which
// sends the reader hunting for a bug that is not there.
func TestLoadRosterRejectsBlockStyleYAML(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantLine string
	}{
		{
			name:     "first entry",
			body:     "Juan Gracia:\n  - juan.gracia@bunn.com\n  - other@x.com\n",
			wantLine: "line 1",
		},
		{
			name:     "later entry",
			body:     "Juan Gracia: juan@bunn.com\nWesley Ornellas:\n  - wesley@bunn.com\n",
			wantLine: "line 2",
		},
		{
			name:     "blank and comment lines in between",
			body:     "Juan Gracia:\n\n  # work address\n  - juan@bunn.com\n",
			wantLine: "line 1",
		},
		{
			name:     "sequence item with no name above it",
			body:     "- juan@bunn.com\n",
			wantLine: "line 1",
		},
	}
	for _, tc := range tests {
		path := writeRoster(t, "roster.yaml", tc.body)
		_, err := LoadRoster(path)
		if err == nil {
			t.Errorf("%s: LoadRoster succeeded, want error", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), "block-style YAML lists are not supported") {
			t.Errorf("%s: error %q does not name the block-style format", tc.name, err)
		}
		if !strings.Contains(err.Error(), tc.wantLine) {
			t.Errorf("%s: error %q does not name %s", tc.name, err, tc.wantLine)
		}
	}
}

// "Name:" with nothing indented under it is a plain empty entry, not
// block-style YAML, and must keep the specific message.
func TestLoadRosterEmptyEntryIsNotReportedAsBlockStyle(t *testing.T) {
	path := writeRoster(t, "roster.yaml", "Juan Gracia:\nWesley Ornellas: wesley@bunn.com\n")
	_, err := LoadRoster(path)
	if err == nil {
		t.Fatal("LoadRoster succeeded, want error")
	}
	if !strings.Contains(err.Error(), "no email addresses") {
		t.Errorf("error %q, want the empty-entry message", err)
	}
	if strings.Contains(err.Error(), "block-style") {
		t.Errorf("error %q wrongly blames block-style YAML", err)
	}
}

// --- end to end -------------------------------------------------------------

// The point of the roster: in a repository with no .mailmap, one person's
// split addresses have to count as one contributor. Without the merge each
// address is a separate author and the person is undercounted.
func TestAuthorArgsForCountsSplitAddressesAsOnePerson(t *testing.T) {
	dir := newRemoteRepo(t, t.TempDir(), "aliases", "", stamp(2025, 1, 1))
	writeCommit(t, dir, "a.txt", "a\n", "wesley@corp.com", "Wesley Ornellas", stamp(2025, 1, 2))
	writeCommit(t, dir, "b.txt", "b\n", "wesleyornellas@MacBook---Wesley.local", "Wesley", stamp(2025, 1, 3))
	writeCommit(t, dir, "c.txt", "c\n", "wesley.ornellas@gmail.com", "Wesley O", stamp(2025, 1, 4))

	if got := countCommitsFor(t, dir, SoloIdentity("wesley@corp.com")); got != 1 {
		t.Fatalf("single address matched %d commits, want 1", got)
	}

	merged := Identity{Name: "Wesley Ornellas", Emails: []string{
		"wesley@corp.com",
		"wesleyornellas@MacBook---Wesley.local",
		"wesley.ornellas@gmail.com",
	}}
	if got := countCommitsFor(t, dir, merged); got != 3 {
		t.Errorf("merged identity matched %d commits, want 3", got)
	}
}

// Looping Analyze over a person's addresses and adding the results is not the
// same as one multi-author log. git ORs the --author patterns, so a commit
// that two patterns both match is counted twice by the loop, and overlapping
// patterns are the norm in the aliasing case this exists for.
func TestAnalyzeMultiDoesNotDoubleCountOverlappingPatterns(t *testing.T) {
	dir := newRemoteRepo(t, t.TempDir(), "overlap", "", stamp(2025, 1, 1))
	writeCommit(t, dir, "a.txt", "1\n2\n3\n", "juan@corp.com", "Juan Gracia", stamp(2025, 1, 2))

	since, until := stamp(2024, 1, 1), stamp(2026, 1, 1)
	// A name fragment and a full address that both match the same commit.
	authors := []string{"Juan Gracia", "juan@corp.com"}

	var summed RepoStats
	for _, a := range authors {
		s, err := Analyze(dir, a, since, until, nil)
		if err != nil {
			t.Fatalf("Analyze(%q): %v", a, err)
		}
		summed.Commits += s.Commits
		summed.Added += s.Added
	}
	if summed.Commits != 2 {
		t.Fatalf("looping Analyze counted %d commits; the test no longer demonstrates the hazard", summed.Commits)
	}

	got, err := AnalyzeMulti(dir, authors, since, until, nil)
	if err != nil {
		t.Fatalf("AnalyzeMulti: %v", err)
	}
	if got.Commits != 1 {
		t.Errorf("AnalyzeMulti Commits = %d, want 1", got.Commits)
	}
	if got.Added != 3 {
		t.Errorf("AnalyzeMulti Added = %d, want 3", got.Added)
	}
}

func TestAnalyzeMultiMergesDistinctAddresses(t *testing.T) {
	dir := newRemoteRepo(t, t.TempDir(), "merge", "", stamp(2025, 1, 1))
	writeCommit(t, dir, "a.txt", "1\n2\n", "wesley@corp.com", "Wesley Ornellas", stamp(2025, 1, 2))
	writeCommit(t, dir, "b.txt", "1\n2\n2\n", "wesleyornellas@MacBook---Wesley.local", "Wesley", stamp(2025, 1, 3))

	id := Identity{Name: "Wesley Ornellas", Emails: []string{
		"wesley@corp.com",
		"wesleyornellas@MacBook---Wesley.local",
	}}
	got, err := AnalyzeIdentity(dir, id, stamp(2024, 1, 1), stamp(2026, 1, 1), nil)
	if err != nil {
		t.Fatalf("AnalyzeIdentity: %v", err)
	}
	if got.Commits != 2 || got.Added != 5 {
		t.Errorf("got %d commits / %d added, want 2 / 5", got.Commits, got.Added)
	}
	// The report should name the human, not whichever address came first.
	if got.Author != "Wesley Ornellas" {
		t.Errorf("Author = %q, want the canonical name", got.Author)
	}
}

// Analyze must behave exactly as it did before AnalyzeMulti was factored out
// of it, including its empty-author "everyone" case.
func TestAnalyzeUnchangedByMultiRefactor(t *testing.T) {
	dir := newRemoteRepo(t, t.TempDir(), "unchanged", "", stamp(2025, 1, 1))
	writeCommit(t, dir, "a.txt", "1\n", "alice@corp.com", "Alice", stamp(2025, 1, 2))
	writeCommit(t, dir, "b.txt", "1\n", "bob@corp.com", "Bob", stamp(2025, 1, 3))

	since, until := stamp(2024, 1, 1), stamp(2026, 1, 1)

	one, err := Analyze(dir, "alice@corp.com", since, until, nil)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if one.Commits != 1 || one.Author != "alice@corp.com" {
		t.Errorf("Analyze = %d commits, author %q, want 1 and the address", one.Commits, one.Author)
	}

	all, err := Analyze(dir, "", since, until, nil)
	if err != nil {
		t.Fatalf("Analyze(all): %v", err)
	}
	if all.Commits != 3 {
		t.Errorf("Analyze with an empty author = %d commits, want all 3", all.Commits)
	}
}

// --- helpers ----------------------------------------------------------------

func writeRoster(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write roster: %v", err)
	}
	return path
}

// countCommitsFor runs git log with the identity's author filter and counts
// what came back, which is exactly how the analyzer uses these args.
func countCommitsFor(t *testing.T, dir string, id Identity) int {
	t.Helper()
	args := LogArgs(dir)
	args = append(args, AuthorArgsFor(id)...)
	args = append(args, "--pretty=format:%H")
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	n := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}
