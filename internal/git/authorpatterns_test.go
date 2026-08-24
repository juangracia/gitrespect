package git

import (
	"strings"
	"testing"
)

// A blank --author matches every commit in the repository, so one blank entry
// in a team list would silently turn one person's report into the whole repo's.
// AuthorArgsMulti must drop blanks rather than pass them through.
func TestAuthorArgsMultiDropsBlankEntries(t *testing.T) {
	args := AuthorArgsMulti([]string{"alice@corp.com", "", "   ", "\t"})

	authors := 0
	for _, a := range args {
		if !strings.HasPrefix(a, "--author=") {
			continue
		}
		authors++
		if v := strings.TrimPrefix(a, "--author="); strings.TrimSpace(v) == "" || v == "<>" {
			t.Errorf("AuthorArgsMulti emitted a blank author %q, which matches every commit in the repo", a)
		}
	}
	if authors != 1 {
		t.Errorf("AuthorArgsMulti produced %d --author flags, want 1: %v", authors, args)
	}
}

// An all-blank list must produce no --author at all, which is the documented
// "match everything" case, rather than an empty pattern that means the same
// thing by accident.
func TestAuthorArgsMultiAllBlankEmitsNoAuthorFlag(t *testing.T) {
	for _, a := range AuthorArgsMulti([]string{"", "  "}) {
		if strings.HasPrefix(a, "--author=") {
			t.Errorf("AuthorArgsMulti(all blank) emitted %q, want no --author flag", a)
		}
	}
}

func TestAuthorArgsMultiDropsRepeatedPatterns(t *testing.T) {
	args := AuthorArgsMulti([]string{"alice@corp.com", "alice@corp.com", "ALICE@corp.com"})

	seen := make(map[string]int)
	for _, a := range args {
		if strings.HasPrefix(a, "--author=") {
			seen[a]++
		}
	}
	for pattern, n := range seen {
		if n > 1 {
			t.Errorf("--author=%s repeated %d times", pattern, n)
		}
	}
}

// Each default bot pattern must be load-bearing on its own. Several of them
// overlap (a GitHub app carries both "dependabot" and the "[bot]" marker), so a
// test that only uses the fully decorated spelling stays green even if a
// pattern is deleted. These are the bare spellings that only one pattern
// catches.
func TestDefaultBotPatternsCatchBareSpellings(t *testing.T) {
	tests := []struct {
		name    string
		contrib Contributor
	}{
		{"bare dependabot address", Contributor{Email: "dependabot@example.com", Name: "Dependabot"}},
		{"bare renovate address", Contributor{Email: "renovate@example.com", Name: "Renovate"}},
		{"renovatebot spelling", Contributor{Email: "renovatebot@example.com", Name: "Renovatebot"}},
		{"semantic release bot", Contributor{Email: "sr@example.com", Name: "semantic-release-bot"}},
		{"github actions", Contributor{Email: "actions@github.com", Name: "GitHub Actions"}},
		{"gitlab group access token", Contributor{
			Email: "group_12345_bot_a1b2c3@noreply.gitlab.com", Name: "group token"}},
		{"gitlab project access token", Contributor{
			Email: "project_98765_bot_deadbeef@noreply.gitlab.com", Name: "project token"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FilterBots([]Contributor{tt.contrib}, nil)
			if err != nil {
				t.Fatalf("FilterBots: %v", err)
			}
			if len(got) != 0 {
				t.Errorf("FilterBots kept %+v, want it filtered as automation", got)
			}
		})
	}
}

// The filter must not swallow humans whose name merely contains a bot-ish
// substring, or a real contributor vanishes from the report with no explanation.
func TestDefaultBotPatternsKeepHumansWithBotishNames(t *testing.T) {
	humans := []Contributor{
		{Email: "robert@corp.com", Name: "Robert Botello"},
		{Email: "abbot@corp.com", Name: "Anna Abbot"},
		{Email: "talbot@corp.com", Name: "Chris Talbot"},
	}

	got, err := FilterBots(humans, nil)
	if err != nil {
		t.Fatalf("FilterBots: %v", err)
	}
	if len(got) != len(humans) {
		t.Errorf("FilterBots kept %d of %d humans: %+v", len(got), len(humans), got)
	}
}

// A user pattern that does not compile must be an error naming the pattern.
// Ignoring it would leave the user believing an identity was excluded while it
// stayed in every total.
func TestFilterBotsRejectsAnUncompilablePattern(t *testing.T) {
	_, err := FilterBots([]Contributor{{Email: "a@b.com"}}, []string{"([unclosed"})
	if err == nil {
		t.Fatal("FilterBots accepted an invalid regexp, want an error")
	}
	if !strings.Contains(err.Error(), "([unclosed") {
		t.Errorf("error %q does not name the offending pattern", err)
	}
}
