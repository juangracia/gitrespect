package prs

import (
	"strings"
	"testing"
)

func mustMatcher(t *testing.T, requested, mappings []string) *Matcher {
	t.Helper()
	m, err := NewMatcher(requested, mappings)
	if err != nil {
		t.Fatalf("NewMatcher(%v, %v) failed: %v", requested, mappings, err)
	}
	return m
}

func TestMatcherExactEmailWins(t *testing.T) {
	m := mustMatcher(t, []string{"jane@corp.com"}, nil)

	got, ok := m.Match(MergeRequest{AuthorEmail: "JANE@CORP.COM", AuthorUser: "someoneelse"})
	if !ok || got != "jane@corp.com" {
		t.Fatalf("Match by email = (%q, %v), want (jane@corp.com, true)", got, ok)
	}
}

func TestMatcherExactUsername(t *testing.T) {
	m := mustMatcher(t, []string{"jdoe"}, nil)

	got, ok := m.Match(MergeRequest{AuthorUser: "JDoe"})
	if !ok || got != "jdoe" {
		t.Fatalf("Match by username = (%q, %v), want (jdoe, true)", got, ok)
	}
}

// The motivating case: gitrespect knows an email, the platform knows a handle.
func TestMatcherEmailLocalPartFindsHandle(t *testing.T) {
	m := mustMatcher(t, []string{"jane.doe@corp.com"}, nil)

	cases := []struct {
		name string
		mr   MergeRequest
	}{
		{"username with dot", MergeRequest{AuthorUser: "jane.doe"}},
		{"username with hyphen", MergeRequest{AuthorUser: "jane-doe"}},
		{"username squashed", MergeRequest{AuthorUser: "janedoe"}},
		{"display name", MergeRequest{AuthorUser: "jd7", AuthorName: "Jane Doe"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := m.Match(tc.mr)
			if !ok || got != "jane.doe@corp.com" {
				t.Fatalf("Match = (%q, %v), want (jane.doe@corp.com, true)", got, ok)
			}
		})
	}
}

func TestMatcherDoesNotMatchUnrelatedAccount(t *testing.T) {
	m := mustMatcher(t, []string{"jane.doe@corp.com"}, nil)

	if got, ok := m.Match(MergeRequest{AuthorUser: "renovate-bot", AuthorName: "Renovate"}); ok {
		t.Fatalf("Match = (%q, true), want no match", got)
	}
}

// A very short handle must never drive a fuzzy match: "jg" would collide with
// half an organisation.
func TestMatcherIgnoresShortFuzzyKeys(t *testing.T) {
	m := mustMatcher(t, []string{"jg@corp.com"}, nil)

	if got, ok := m.Match(MergeRequest{AuthorName: "J G"}); ok {
		t.Fatalf("Match on a two character key = (%q, true), want no match", got)
	}
	// The exact form still works.
	if _, ok := m.Match(MergeRequest{AuthorUser: "jg"}); !ok {
		t.Fatal("exact username match should still succeed for a short handle")
	}
}

func TestMatcherMappingPinsAccount(t *testing.T) {
	m := mustMatcher(t, []string{"j.gracia@corp.com"}, []string{"j.gracia@corp.com=jgracia42"})

	got, ok := m.Match(MergeRequest{AuthorUser: "jgracia42", AuthorName: "JG"})
	if !ok || got != "j.gracia@corp.com" {
		t.Fatalf("Match via --map = (%q, %v), want (j.gracia@corp.com, true)", got, ok)
	}
}

func TestMatcherMappingUnknownIdentityIsRejected(t *testing.T) {
	_, err := NewMatcher([]string{"jane@corp.com"}, []string{"other@corp.com=handle"})
	if err == nil {
		t.Fatal("expected an error for a --map that refers to nobody in --team")
	}
	if !strings.Contains(err.Error(), "not in --author/--team") {
		t.Fatalf("error %q should say the mapping refers to an unknown identity", err)
	}
}

func TestMatcherMappingWithoutFilterCreatesIdentity(t *testing.T) {
	m := mustMatcher(t, nil, []string{"jane@corp.com=jdoe"})
	if !m.Filtering() {
		t.Fatal("a mapping without --team should still establish an identity to filter on")
	}
	got, ok := m.Match(MergeRequest{AuthorUser: "jdoe"})
	if !ok || got != "jane@corp.com" {
		t.Fatalf("Match = (%q, %v), want (jane@corp.com, true)", got, ok)
	}
}

func TestMatcherRejectsMalformedMapping(t *testing.T) {
	for _, bad := range []string{"nohandle", "=handle", "email=", ""} {
		if _, err := NewMatcher(nil, []string{bad}); err == nil {
			t.Fatalf("NewMatcher accepted malformed --map %q", bad)
		}
	}
}

func TestMatcherRejectsAmbiguousIdentities(t *testing.T) {
	// Two people whose normalized names collide must be reported, not
	// silently attributed to whoever was listed first.
	_, err := NewMatcher([]string{"jane.doe@a.com", "janedoe@b.com"}, nil)
	if err == nil {
		t.Fatal("expected an error for two identities that normalize to the same key")
	}
	if !strings.Contains(err.Error(), "--map") {
		t.Fatalf("error %q should point at --map as the fix", err)
	}
}

func TestMatcherNoIdentitiesNeverMatches(t *testing.T) {
	m := mustMatcher(t, nil, nil)
	if m.Filtering() {
		t.Fatal("Filtering() should be false with no identities")
	}
	if _, ok := m.Match(MergeRequest{AuthorUser: "anyone"}); ok {
		t.Fatal("a matcher with no identities must not claim anything")
	}
}

func TestMatcherLabelsPreserveInputOrder(t *testing.T) {
	m := mustMatcher(t, []string{"b@corp.com", "a@corp.com"}, nil)
	got := m.Labels()
	if len(got) != 2 || got[0] != "b@corp.com" || got[1] != "a@corp.com" {
		t.Fatalf("Labels() = %v, want the order they were given", got)
	}
}

func TestNormalizeHandle(t *testing.T) {
	cases := map[string]string{
		"Jane Doe":  "janedoe",
		"jane.doe":  "janedoe",
		"jane-doe":  "janedoe",
		"jane_doe":  "janedoe",
		"  JANE  ":  "jane",
		"":          "",
		"a.b-c_d e": "abcde",
	}
	for in, want := range cases {
		if got := normalizeHandle(in); got != want {
			t.Errorf("normalizeHandle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEmailLocalPart(t *testing.T) {
	cases := []struct {
		in    string
		local string
		ok    bool
	}{
		{"jane@corp.com", "jane", true},
		{"jane.doe@sub.corp.co.uk", "jane.doe", true},
		{"jdoe", "", false},
		{"@corp.com", "", false},
		{"jane@localhost", "", false},
	}
	for _, tc := range cases {
		local, ok := emailLocalPart(tc.in)
		if local != tc.local || ok != tc.ok {
			t.Errorf("emailLocalPart(%q) = (%q, %v), want (%q, %v)", tc.in, local, ok, tc.local, tc.ok)
		}
	}
}
