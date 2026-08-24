package cmd

import (
	"strings"
	"testing"

	"github.com/juangracia/gitrespect/internal/git"
)

// TestDisambiguateLabelsSeparatesPeopleSharingADisplayName is the guard for the
// bug that made a team report contradict itself.
//
// Two identities sharing a label collide in the map team stats are keyed by, so
// one row silently overwrote the other while the totals kept counting both. The
// printed rows then summed to less than the total printed above them.
func TestDisambiguateLabelsSeparatesPeopleSharingADisplayName(t *testing.T) {
	in := []git.Identity{
		{Name: "Juan Gracia", Emails: []string{"juan@corp.com"}},
		{Name: "Juan Gracia", Emails: []string{"juan@personal.com"}},
		{Name: "Bob Other", Emails: []string{"bob@corp.com"}},
	}

	got := disambiguateLabels(in)
	if len(got) != len(in) {
		t.Fatalf("disambiguateLabels returned %d identities, want %d", len(got), len(in))
	}

	seen := make(map[string]bool, len(got))
	for _, id := range got {
		label := id.Label()
		if seen[label] {
			t.Errorf("label %q is used twice; colliding labels overwrite each other in "+
				"the team stats map, which makes member rows stop summing to the team total", label)
		}
		seen[label] = true
	}

	// The unaffected person keeps a clean label: disambiguation must not make
	// every report uglier to fix a collision between two other people.
	if !seen["Bob Other"] {
		t.Errorf("Bob Other was renamed despite having no collision; got labels %v", keysOf(seen))
	}
	// Each colliding identity must remain identifiable by its address.
	for _, want := range []string{"juan@corp.com", "juan@personal.com"} {
		if !anyContains(keysOf(seen), want) {
			t.Errorf("no label distinguishes %s; got %v", want, keysOf(seen))
		}
	}
}

// TestDisambiguateLabelsLeavesDistinctLabelsAlone pins that the common case
// stays untouched, so the fix costs nothing when there is no collision.
func TestDisambiguateLabelsLeavesDistinctLabelsAlone(t *testing.T) {
	in := []git.Identity{
		{Name: "Alice", Emails: []string{"alice@corp.com"}},
		{Name: "Bob", Emails: []string{"bob@corp.com"}},
		{Emails: []string{"carol@corp.com"}},
	}
	for i, id := range disambiguateLabels(in) {
		if id.Label() != in[i].Label() {
			t.Errorf("label %d changed from %q to %q with no collision present",
				i, in[i].Label(), id.Label())
		}
	}
}

// TestUniqueLabelNeverOverwritesAnExistingMember covers the backstop in
// buildTeamStats for callers that hand over colliding labels anyway.
func TestUniqueLabelNeverOverwritesAnExistingMember(t *testing.T) {
	members := map[string]git.RepoStats{}
	for i := 0; i < 3; i++ {
		label := uniqueLabel("Same Name", members)
		if _, taken := members[label]; taken {
			t.Fatalf("uniqueLabel returned %q, which is already a member key", label)
		}
		members[label] = git.RepoStats{Net: 10}
	}
	if len(members) != 3 {
		t.Errorf("got %d member rows, want 3: a collision silently dropped a person's row", len(members))
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func anyContains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if strings.Contains(h, needle) {
			return true
		}
	}
	return false
}
