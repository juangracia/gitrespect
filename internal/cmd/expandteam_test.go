package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juangracia/gitrespect/internal/git"
)

// rosterOf builds a roster inline, so the tests below read as the team list a
// user would actually write.
func rosterOf(t *testing.T, specs ...string) git.Roster {
	t.Helper()
	var r git.Roster
	for _, s := range specs {
		id, err := git.ParseAlias(s)
		if err != nil {
			t.Fatalf("ParseAlias(%q): %v", s, err)
		}
		r = append(r, id)
	}
	return r
}

func labelsOf(ids []git.Identity) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.Label())
	}
	return out
}

// TestExpandTeamMergesTokensForTheSamePerson is the regression guard for silent
// double counting: naming two of one person's addresses on --team must analyse
// that human once. Counting them twice inflates the team total by everything
// that person wrote, with nothing in the report to suggest anything is wrong.
func TestExpandTeamMergesTokensForTheSamePerson(t *testing.T) {
	roster := rosterOf(t, "Juan Gracia=juan@corp.com,juanmgracia@gmail.com")

	got := expandTeam([]string{"juan@corp.com", "juanmgracia@gmail.com"}, roster)

	if len(got) != 1 {
		t.Fatalf("expandTeam = %v, want one identity: the two addresses are one person", labelsOf(got))
	}
	if got[0].Label() != "Juan Gracia" {
		t.Errorf("label = %q, want %q", got[0].Label(), "Juan Gracia")
	}
	if len(got[0].Emails) != 2 {
		t.Errorf("emails = %v, want both addresses so neither one's commits are missed", got[0].Emails)
	}
}

// The same address typed twice, with no roster in play, is still one person.
func TestExpandTeamMergesARepeatedAddress(t *testing.T) {
	got := expandTeam([]string{"alice@corp.com", "alice@corp.com"}, nil)
	if len(got) != 1 {
		t.Fatalf("expandTeam = %v, want one identity", labelsOf(got))
	}
}

// Addresses are case-insensitive in practice, so a team list that spells one
// person two ways must not analyse them twice.
func TestExpandTeamMergesAddressesDifferingOnlyInCase(t *testing.T) {
	got := expandTeam([]string{"Alice@Corp.com", "alice@corp.com"}, nil)
	if len(got) != 1 {
		t.Fatalf("expandTeam = %v, want one identity", labelsOf(got))
	}
}

// Resolving by canonical name and by one of the addresses must land on the same
// person, or the merge above would depend on which spelling the user typed.
func TestExpandTeamMergesNameAndAddressForOnePerson(t *testing.T) {
	roster := rosterOf(t, "Juan Gracia=juan@corp.com,juanmgracia@gmail.com")

	got := expandTeam([]string{"Juan Gracia", "juanmgracia@gmail.com"}, roster)

	if len(got) != 1 {
		t.Fatalf("expandTeam = %v, want one identity", labelsOf(got))
	}
}

// A second mention that carries the canonical name upgrades a bare address, so
// the report names the human rather than whichever address came first.
func TestExpandTeamKeepsTheRicherLabel(t *testing.T) {
	roster := rosterOf(t, "Alice Smith=alice@corp.com")

	got := expandTeam([]string{"bob@corp.com", "bob@corp.com"}, roster)
	if len(got) != 1 || got[0].Label() != "bob@corp.com" {
		t.Fatalf("unregistered member = %v, want the bare address", labelsOf(got))
	}

	got = expandTeam([]string{"alice@corp.com", "Alice Smith"}, roster)
	if len(got) != 1 {
		t.Fatalf("expandTeam = %v, want one identity", labelsOf(got))
	}
	if got[0].Label() != "Alice Smith" {
		t.Errorf("label = %q, want the canonical name", got[0].Label())
	}
}

// Distinct people must survive: a merge that was too eager would silently drop
// members from the team.
func TestExpandTeamKeepsDistinctPeople(t *testing.T) {
	roster := rosterOf(t, "Juan Gracia=juan@corp.com,juanmgracia@gmail.com")

	got := expandTeam([]string{"juan@corp.com", "alice@corp.com", "bob@corp.com"}, roster)

	if len(got) != 3 {
		t.Fatalf("expandTeam = %v, want three distinct people", labelsOf(got))
	}
}

// Member order is what the report prints, so it must follow the order given.
func TestExpandTeamPreservesInputOrder(t *testing.T) {
	got := labelsOf(expandTeam([]string{"zoe@corp.com", "alice@corp.com", "mike@corp.com"}, nil))
	want := []string{"zoe@corp.com", "alice@corp.com", "mike@corp.com"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expandTeam order = %v, want %v", got, want)
		}
	}
}

func TestExpandTeamSkipsBlankTokens(t *testing.T) {
	got := expandTeam([]string{"alice@corp.com", "", "   ", "bob@corp.com"}, nil)
	if len(got) != 2 {
		t.Fatalf("expandTeam = %v, want the two real addresses", labelsOf(got))
	}
	for _, id := range got {
		for _, e := range id.Emails {
			if strings.TrimSpace(e) == "" {
				// A blank address reaches git as an empty --author, which
				// matches every commit in the repository.
				t.Fatalf("expandTeam kept a blank address in %v", id.Emails)
			}
		}
	}
}

// An unregistered contributor is still a person: dropping them would silently
// shrink the team.
func TestExpandIdentityFallsBackToTheBareAddress(t *testing.T) {
	roster := rosterOf(t, "Alice Smith=alice@corp.com")

	id := expandIdentity("stranger@corp.com", roster)

	if id.Label() != "stranger@corp.com" {
		t.Errorf("label = %q, want the address itself", id.Label())
	}
	if len(id.Emails) != 1 || id.Emails[0] != "stranger@corp.com" {
		t.Errorf("emails = %v, want just the token", id.Emails)
	}
}

func TestExpandIdentityExpandsThroughTheRoster(t *testing.T) {
	roster := rosterOf(t, "Juan Gracia=juan@corp.com,juanmgracia@gmail.com")

	id := expandIdentity("juanmgracia@gmail.com", roster)

	if id.Label() != "Juan Gracia" {
		t.Errorf("label = %q, want the canonical name", id.Label())
	}
	if len(id.Emails) != 2 {
		t.Errorf("emails = %v, want every address the person commits under", id.Emails)
	}
}

func TestIdentityKeyIgnoresAddressOrderAndCase(t *testing.T) {
	a := git.Identity{Name: "A", Emails: []string{"x@corp.com", "Y@corp.com"}}
	b := git.Identity{Name: "B", Emails: []string{"y@corp.com", "X@corp.com"}}

	if identityKey(a) != identityKey(b) {
		t.Errorf("identityKey differs for the same address set: %q vs %q", identityKey(a), identityKey(b))
	}
}

func TestIdentityKeySeparatesDifferentPeople(t *testing.T) {
	a := git.Identity{Emails: []string{"alice@corp.com"}}
	b := git.Identity{Emails: []string{"bob@corp.com"}}

	if identityKey(a) == identityKey(b) {
		t.Error("identityKey collided for two different people")
	}
}

// buildRoster composes --roster and --alias, and must validate the MERGED
// roster: an address claimed by two people is only visible once both sources
// are combined, and left alone it double counts that person in a team total.
func TestBuildRosterRejectsAnAddressClaimedByTwoPeople(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roster.txt")
	if err := os.WriteFile(path, []byte("Alice: alice@corp.com\n"), 0o644); err != nil {
		t.Fatalf("write roster: %v", err)
	}

	_, err := buildRoster(path, []string{"Bob=alice@corp.com"})

	if err == nil {
		t.Fatal("buildRoster accepted an address claimed by two people, want an error")
	}
	if !strings.Contains(err.Error(), "alice@corp.com") {
		t.Errorf("error %q does not name the offending address", err)
	}
}

func TestBuildRosterMergesFileAndAlias(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roster.txt")
	if err := os.WriteFile(path, []byte("Alice: alice@corp.com\n"), 0o644); err != nil {
		t.Fatalf("write roster: %v", err)
	}

	roster, err := buildRoster(path, []string{"Bob=bob@corp.com,bob@personal.com"})
	if err != nil {
		t.Fatalf("buildRoster: %v", err)
	}

	if len(roster) != 2 {
		t.Fatalf("roster = %v entries, want the file entry plus the alias", len(roster))
	}
	if _, ok := roster.Resolve("alice@corp.com"); !ok {
		t.Error("roster lost the entry that came from the file")
	}
	if _, ok := roster.Resolve("bob@personal.com"); !ok {
		t.Error("roster lost the entry that came from --alias")
	}
}

// No roster and no alias must produce nil, not an empty roster: expandIdentity
// treats nil as "no roster in play".
func TestBuildRosterWithNoSourcesIsNil(t *testing.T) {
	roster, err := buildRoster("", nil)
	if err != nil {
		t.Fatalf("buildRoster: %v", err)
	}
	if roster != nil {
		t.Errorf("buildRoster = %v, want nil when neither --roster nor --alias was given", roster)
	}
}

func TestBuildRosterReportsABadAlias(t *testing.T) {
	if _, err := buildRoster("", []string{"no-equals-sign"}); err == nil {
		t.Error("buildRoster accepted an alias with no '=', want an error")
	}
}

func TestBuildRosterReportsAMissingFile(t *testing.T) {
	if _, err := buildRoster(filepath.Join(t.TempDir(), "absent.txt"), nil); err == nil {
		t.Error("buildRoster accepted a missing roster file, want an error")
	}
}

// dedupeRepos must leave a short list untouched without spending a git call.
func TestDedupeReposPassesThroughShortLists(t *testing.T) {
	one := []string{"/only/one"}
	if got := dedupeRepos(one); len(got) != 1 || got[0] != "/only/one" {
		t.Errorf("dedupeRepos = %v, want the input unchanged", got)
	}
	if got := dedupeRepos(nil); len(got) != 0 {
		t.Errorf("dedupeRepos(nil) = %v, want empty", got)
	}
}

func TestPluralHelpers(t *testing.T) {
	if pluralIdentities(1) != "identity" || pluralIdentities(2) != "identities" {
		t.Errorf("pluralIdentities: got %q/%q", pluralIdentities(1), pluralIdentities(2))
	}
	if pluralRepos(1) != "repo" || pluralRepos(2) != "repos" {
		t.Errorf("pluralRepos: got %q/%q", pluralRepos(1), pluralRepos(2))
	}
}
