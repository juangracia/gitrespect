package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/juangracia/gitrespect/internal/git"
)

// buildRoster assembles the identity roster from --roster and --alias.
//
// Both sources are optional and compose: a file can carry the standing team
// and an inline --alias can add or correct one person for a single run. The
// combined roster is validated as a whole, because the mistake that matters
// (one address claimed by two people, which double counts them in a team
// total) is only visible once both sources are merged.
func buildRoster(rosterPath string, aliasSpecs []string) (git.Roster, error) {
	var roster git.Roster

	if rosterPath != "" {
		loaded, err := git.LoadRoster(rosterPath)
		if err != nil {
			return nil, err
		}
		roster = loaded
	}

	for _, spec := range aliasSpecs {
		id, err := git.ParseAlias(spec)
		if err != nil {
			return nil, err
		}
		roster = append(roster, id)
	}

	if len(roster) == 0 {
		return nil, nil
	}
	if err := roster.Validate(); err != nil {
		return nil, fmt.Errorf("invalid roster: %w", err)
	}
	return roster, nil
}

// expandIdentity turns one user-supplied token into the full set of addresses
// that person commits under.
//
// A token that the roster does not know is not an error: most runs name people
// who were never registered, and dropping them would silently shrink the team.
// Those fall back to matching the single address as given.
func expandIdentity(token string, roster git.Roster) git.Identity {
	if roster != nil {
		if id, ok := roster.Resolve(token); ok {
			return id
		}
	}
	return git.SoloIdentity(token)
}

// allAuthorsLabel names the unfiltered mode in report output. Without it a
// whole-repo report would be labelled with an empty author and read as though
// it described one anonymous person.
const allAuthorsLabel = "all authors"

// resolveIdentity decides whose commits a single-author run counts.
//
// The three cases are --all-authors (everyone), an explicit --author, and the
// default of the repository's configured user.email. An explicit address is
// expanded through the roster, so naming any one of a person's addresses
// counts all of them.
func resolveIdentity(repoPath string, roster git.Roster) (git.Identity, error) {
	if allAuthors {
		// No addresses means no --author flag, which is how git is asked for
		// every commit. This is deliberate here and refused everywhere else.
		return git.Identity{Name: allAuthorsLabel}, nil
	}

	explicit := author
	if explicit == "" {
		email, err := git.GetDefaultAuthor(repoPath)
		if err != nil || strings.TrimSpace(email) == "" {
			return git.Identity{}, fmt.Errorf(
				"could not determine author: git config user.email is unset; pass --author, or --all-authors to count everyone")
		}
		explicit = strings.TrimSpace(email)
	}
	return expandIdentity(explicit, roster), nil
}

// expandTeam expands every member token through the roster, merging tokens
// that resolve to the same person.
//
// Merging matters because a caller can legitimately pass two addresses that
// the roster knows belong to one human. Without the merge that person is
// analysed twice and counted twice in the team total, which is the same class
// of silent inflation that duplicate repository checkouts cause.
func expandTeam(members []string, roster git.Roster) []git.Identity {
	var identities []git.Identity
	seen := make(map[string]int)

	for _, token := range members {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		id := expandIdentity(token, roster)
		key := identityKey(id)
		if at, dup := seen[key]; dup {
			// Same person named twice. Keep the richer label if the second
			// mention carried a canonical name and the first did not.
			if identities[at].Name == "" && id.Name != "" {
				identities[at].Name = id.Name
			}
			continue
		}
		seen[key] = len(identities)
		identities = append(identities, id)
	}
	return identities
}

// identityKey identifies a person by their address set, so two tokens that
// resolve to the same roster entry collapse regardless of which address the
// caller happened to type.
func identityKey(id git.Identity) string {
	lowered := make([]string, 0, len(id.Emails))
	for _, e := range id.Emails {
		lowered = append(lowered, strings.ToLower(strings.TrimSpace(e)))
	}
	sort.Strings(lowered)
	return strings.Join(lowered, "\x00")
}

// discoverTeam picks the busiest contributors across the scanned repositories,
// so a team report does not have to start with hand-assembled email
// archaeology across hundreds of repos.
//
// Discovered addresses are folded through the roster before ranking, so "top
// N" counts people rather than raw addresses. Without that a contributor split
// across three machines competes with themselves for a slot and can lose one
// to someone who does less work under a single address.
func discoverTeam(paths []string, since, until time.Time, n int, extraExclude []string, roster git.Roster) ([]git.Identity, error) {
	// Scan once and filter in memory. Discovery walks every commit in every
	// repository, so asking for the unfiltered and filtered lists separately
	// would double the cost of the most expensive step in the run.
	all, err := git.ScanContributors(paths, since, until)
	if err != nil {
		return nil, err
	}
	contributors, err := git.FilterBots(all, extraExclude)
	if err != nil {
		return nil, err
	}
	reportExcludedAutomation(all, contributors)

	type bucket struct {
		id      git.Identity
		commits int
		order   int
	}
	buckets := make(map[string]*bucket)
	var order []string

	for i, c := range contributors {
		id := expandIdentity(c.Email, roster)
		// An unregistered contributor is still a person: label them with the
		// name git recorded rather than a bare address, when we have one.
		if id.Name == "" && c.Name != "" {
			id.Name = c.Name
		}
		key := identityKey(id)
		b, ok := buckets[key]
		if !ok {
			b = &bucket{id: id, order: i}
			buckets[key] = b
			order = append(order, key)
		}
		b.commits += c.Commits
	}

	merged := make([]*bucket, 0, len(order))
	for _, k := range order {
		merged = append(merged, buckets[k])
	}
	// Most commits first. Ties fall back to the discovery order, which
	// TopContributors already made deterministic, so repeated runs agree.
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].commits != merged[j].commits {
			return merged[i].commits > merged[j].commits
		}
		return merged[i].order < merged[j].order
	})

	if n > 0 && len(merged) > n {
		merged = merged[:n]
	}

	identities := make([]git.Identity, 0, len(merged))
	for _, b := range merged {
		identities = append(identities, b.id)
	}
	if len(identities) == 0 {
		return nil, fmt.Errorf("--top %d found no contributors in the scanned repositories for this period", n)
	}
	return identities, nil
}

// maxListedAutomation caps how many excluded addresses are named, so a repo
// full of bot traffic does not bury the report under its own warning.
const maxListedAutomation = 8

// reportExcludedAutomation names the identities the bot filter removed.
//
// The filter matches substrings like "renovate" and "dependabot" against both
// address and display name, so a human can in principle be caught by it and
// would otherwise vanish from a team report with nothing to show why. Printing
// what was dropped makes that recoverable: the user can see the name and pass
// it explicitly with --team, which skips discovery entirely.
func reportExcludedAutomation(all, kept []git.Contributor) {
	if len(all) == len(kept) {
		return
	}

	keptSet := make(map[string]bool, len(kept))
	for _, c := range kept {
		keptSet[strings.ToLower(c.Email)] = true
	}

	var dropped []string
	for _, c := range all {
		if !keptSet[strings.ToLower(c.Email)] {
			dropped = append(dropped, c.Email)
		}
	}
	if len(dropped) == 0 {
		return
	}

	shown := dropped
	suffix := ""
	if len(shown) > maxListedAutomation {
		shown = shown[:maxListedAutomation]
		suffix = fmt.Sprintf(", +%d more", len(dropped)-maxListedAutomation)
	}
	fmt.Fprintf(os.Stderr,
		"note: excluded %d automation %s from --top: %s%s\n",
		len(dropped), pluralIdentities(len(dropped)), strings.Join(shown, ", "), suffix)
	fmt.Fprintln(os.Stderr,
		"      if one of those is a person, name them directly with --team, which skips discovery")
}

func pluralIdentities(n int) string {
	if n == 1 {
		return "identity"
	}
	return "identities"
}

// dedupeRepos drops second checkouts of a project already in the list and says
// so on stderr.
//
// Silence would be the wrong default here. A duplicated clone inflates a team
// total by however much history that project carries, and a report consumer
// has no way to notice from the numbers alone, so the skip is announced even
// though it is the correct action.
func dedupeRepos(paths []string) []string {
	if len(paths) < 2 {
		return paths
	}
	kept, dups := git.DedupeByRemote(paths)
	for _, d := range dups {
		fmt.Fprintf(os.Stderr,
			"Warning: %d duplicate %s found and skipped (same remote %s): %s\n",
			len(d.Skipped), pluralRepos(len(d.Skipped)), d.Remote, strings.Join(d.Skipped, ", "))
		fmt.Fprintf(os.Stderr, "         counting %s\n", d.Kept)
	}
	return kept
}

func pluralRepos(n int) string {
	if n == 1 {
		return "repo"
	}
	return "repos"
}
