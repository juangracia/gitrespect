package git

import "sort"

// RepoRollup is one repository's total across a whole team, with the per-member
// rows that add up to it.
type RepoRollup struct {
	Path         string
	Added        int
	Deleted      int
	Net          int
	Commits      int
	Contributors []RepoStats // this repo's per-member rows
}

// RollupByRepo inverts a per-member view of a team into a per-repository one.
//
// Team mode analyses each member across every path, which answers "how much did
// each person write" but not "where did the work land". Flattening those results
// with CombineStats discards the path entirely, and printing one table per
// member stops being readable past a handful of repositories. Grouping by path
// keeps both: one row per repository, with the members who touched it beneath.
//
// perMember is keyed by the label the caller reports each member under. Rows
// where a member did nothing are dropped, and a repository nobody touched does
// not appear at all: analysing a few hundred repositories would otherwise bury
// the real work under hundreds of zero rows. A repository with commits but no
// net lines is kept, because a refactor that deletes as much as it adds is real
// work and reporting it as absent would be wrong.
//
// Ordering is total, because the input is a map and map iteration order varies
// between runs: repositories by net lines descending then path ascending, and
// contributors within a repository by net lines descending then author
// ascending. Without that, the same team would print in a different order every
// time it was run.
//
// The caller's RepoStats are not modified.
func RollupByRepo(perMember map[string][]RepoStats) []RepoRollup {
	byPath := make(map[string]*RepoRollup, len(perMember))

	for label, stats := range perMember {
		for _, s := range stats {
			// A member who never touched a repository contributes no row. Net
			// alone cannot decide this: a pure refactor nets zero and still
			// belongs in the table, so the test is on activity, not size.
			if s.Commits == 0 && s.Added == 0 && s.Deleted == 0 {
				continue
			}

			row := s
			// The map key is the label this member is reported under. A row
			// built without an author would otherwise print as a blank name.
			if row.Author == "" {
				row.Author = label
			}
			// Keep each row's arithmetic consistent with the totals below, so
			// what is displayed matches what was sorted on.
			row.Net = row.Added - row.Deleted

			r, ok := byPath[s.Path]
			if !ok {
				r = &RepoRollup{Path: s.Path}
				byPath[s.Path] = r
			}
			r.Added += row.Added
			r.Deleted += row.Deleted
			r.Commits += row.Commits
			r.Contributors = append(r.Contributors, row)
		}
	}

	rollups := make([]RepoRollup, 0, len(byPath))
	for _, r := range byPath {
		r.Net = r.Added - r.Deleted
		contribs := r.Contributors
		sort.Slice(contribs, func(i, j int) bool {
			if contribs[i].Net != contribs[j].Net {
				return contribs[i].Net > contribs[j].Net
			}
			return contribs[i].Author < contribs[j].Author
		})
		rollups = append(rollups, *r)
	}

	sort.Slice(rollups, func(i, j int) bool {
		if rollups[i].Net != rollups[j].Net {
			return rollups[i].Net > rollups[j].Net
		}
		return rollups[i].Path < rollups[j].Path
	})

	return rollups
}
