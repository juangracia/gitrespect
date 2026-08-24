package prs

import (
	"fmt"
	"sort"
	"strings"
)

// minFuzzyKey is the shortest string allowed to drive a name based match.
// A one or two character key ("jg", "a") would collide with unrelated people,
// and a wrong attribution is worse than a missing one.
const minFuzzyKey = 3

// Matcher resolves a platform account to one of the identities the user asked
// about.
//
// This is the weakest part of the feature and it is weak for a structural
// reason: gitrespect identifies people by git commit email, while review
// platforms identify them by account. Neither the GitLab group merge request
// list nor the GitHub search API returns a contributor email, so an email
// given on --team almost never has an authoritative counterpart to compare
// against. The rules, strongest first:
//
//  1. Exact, case insensitive match of the requested string against the
//     account email (when the provider supplies one) or the account username.
//  2. Exact match against any handle pinned with --map email=handle.
//  3. Normalized match (lowercased, with dots, underscores, hyphens and spaces
//     removed) of the requested string, or of an email's local part, against
//     the account username or display name. This is what lets
//     jane.doe@corp.com find the account @jane-doe or "Jane Doe".
//
// Rule 3 is a heuristic. It misses anyone whose handle does not resemble their
// email local part (j.gracia@corp.com versus @jgracia42), and in principle it
// can over-match two people whose normalized names collide. Ambiguous keys are
// rejected at construction rather than silently resolved, and every account
// that matched nobody is reported back so the gap is visible and fixable with
// --map.
type Matcher struct {
	identities []string
	exact      map[string]int
	fuzzy      map[string]int
}

// Person is one identity expressed as a label plus every string that should
// resolve to it. A roster produces exactly this shape: one human, several
// email addresses. Flattening those into separate identities would split one
// person's work across several rows, so the grouping has to survive.
type Person struct {
	Label string
	Keys  []string
}

// NewMatcher builds a matcher for identities given as plain strings, each its
// own person. mappings are "email=handle" pairs; the left side must be one of
// the requested identities when a filter is in play, otherwise the mapping
// would silently do nothing.
func NewMatcher(requested []string, mappings []string) (*Matcher, error) {
	people := make([]Person, 0, len(requested))
	for _, raw := range requested {
		people = append(people, Person{Label: raw})
	}
	return NewMatcherFor(people, mappings)
}

// NewMatcherFor builds a matcher from pre-grouped identities, so one person
// with several email addresses stays one row in the report.
func NewMatcherFor(people []Person, mappings []string) (*Matcher, error) {
	m := &Matcher{
		exact: make(map[string]int),
		fuzzy: make(map[string]int),
	}

	for _, p := range people {
		label := strings.TrimSpace(p.Label)
		if label == "" {
			continue
		}
		idx := len(m.identities)
		m.identities = append(m.identities, label)
		// The label is a match key in its own right: a roster label like
		// "Jane Doe" is exactly what a platform display name looks like.
		if err := m.addKeys(label, idx); err != nil {
			return nil, err
		}
		for _, key := range p.Keys {
			if err := m.addKeys(key, idx); err != nil {
				return nil, err
			}
		}
	}
	filtering := len(m.identities) > 0

	for _, raw := range mappings {
		left, right, ok := strings.Cut(raw, "=")
		left, right = strings.TrimSpace(left), strings.TrimSpace(right)
		if !ok || left == "" || right == "" {
			return nil, fmt.Errorf("invalid --map %q: expected email=handle, for example --map jane@corp.com=jdoe", raw)
		}
		idx, known := m.exact[strings.ToLower(left)]
		if !known {
			if filtering {
				return nil, fmt.Errorf("--map %q refers to %q, which is not in --author/--team", raw, left)
			}
			idx = len(m.identities)
			m.identities = append(m.identities, left)
			if err := m.addKeys(left, idx); err != nil {
				return nil, err
			}
		}
		if err := m.addExact(right, idx); err != nil {
			return nil, err
		}
		if err := m.addFuzzy(normalizeHandle(right), idx); err != nil {
			return nil, err
		}
	}

	return m, nil
}

// addKeys registers every match key derivable from one string: the string
// itself, its normalized form, and, when it is an email, its local part.
func (m *Matcher) addKeys(raw string, idx int) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if err := m.addExact(raw, idx); err != nil {
		return err
	}
	if err := m.addFuzzy(normalizeHandle(raw), idx); err != nil {
		return err
	}
	if local, ok := emailLocalPart(raw); ok {
		if err := m.addExact(local, idx); err != nil {
			return err
		}
		if err := m.addFuzzy(normalizeHandle(local), idx); err != nil {
			return err
		}
	}
	return nil
}

func (m *Matcher) addExact(key string, idx int) error {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return nil
	}
	if prev, ok := m.exact[key]; ok && prev != idx {
		return fmt.Errorf("identities %q and %q both match %q: disambiguate with --map",
			m.identities[prev], m.identities[idx], key)
	}
	m.exact[key] = idx
	return nil
}

func (m *Matcher) addFuzzy(key string, idx int) error {
	if len(key) < minFuzzyKey {
		return nil
	}
	if prev, ok := m.fuzzy[key]; ok && prev != idx {
		return fmt.Errorf("identities %q and %q normalize to the same name %q: disambiguate with --map",
			m.identities[prev], m.identities[idx], key)
	}
	m.fuzzy[key] = idx
	return nil
}

// Filtering reports whether any identity was requested. When false, Match
// never claims anything and callers group by the observed account instead.
func (m *Matcher) Filtering() bool { return len(m.identities) > 0 }

// Labels returns the requested identities in the order they were given.
func (m *Matcher) Labels() []string {
	out := make([]string, len(m.identities))
	copy(out, m.identities)
	return out
}

// Match returns the label of the identity that owns mr's author, and whether
// anything claimed it.
func (m *Matcher) Match(mr MergeRequest) (string, bool) {
	if !m.Filtering() {
		return "", false
	}
	// Exact first: an email or username the user typed verbatim is a fact,
	// while the normalized forms below are only a resemblance.
	for _, cand := range []string{mr.AuthorEmail, mr.AuthorUser} {
		key := strings.ToLower(strings.TrimSpace(cand))
		if key == "" {
			continue
		}
		if idx, ok := m.exact[key]; ok {
			return m.identities[idx], true
		}
	}
	if local, ok := emailLocalPart(mr.AuthorEmail); ok {
		if idx, found := m.exact[strings.ToLower(local)]; found {
			return m.identities[idx], true
		}
	}
	for _, cand := range []string{mr.AuthorUser, mr.AuthorName} {
		key := normalizeHandle(cand)
		if len(key) < minFuzzyKey {
			continue
		}
		if idx, ok := m.fuzzy[key]; ok {
			return m.identities[idx], true
		}
	}
	return "", false
}

// normalizeHandle reduces a username or display name to a comparable core:
// lowercase, with the separators people vary between removed. "Jane Doe",
// "jane.doe" and "jane-doe" all become "janedoe".
func normalizeHandle(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch r {
		case '.', '_', '-', ' ', '\t':
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// emailLocalPart returns the part before the @ when s looks like an email.
func emailLocalPart(s string) (string, bool) {
	s = strings.TrimSpace(s)
	local, domain, ok := strings.Cut(s, "@")
	if !ok || local == "" || !strings.Contains(domain, ".") {
		return "", false
	}
	return local, true
}

// sortedHandles returns the distinct handles folded into an identity, sorted
// for a stable report.
func sortedHandles(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for h := range set {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}
