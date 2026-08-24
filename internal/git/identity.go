package git

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// Identity is one human, with every address they commit under.
//
// Real contributors accumulate addresses: a corporate one, a personal one, and
// per-machine addresses that an unconfigured git invents from the hostname
// ("someone@MacBook---Someone.local"). Counting each address as a separate
// contributor undercounts the person by whatever share of their work landed
// under the other addresses, which in practice runs from a few percent to a
// quarter of their output.
//
// A repository can already solve this on its own with a .mailmap file, and
// nothing here is needed there: git resolves --author through .mailmap because
// log.mailmap defaults to true. Verified on git 2.50.1, where --author with
// the canonical address returned commits authored under an alternate address
// as well. This roster exists for the common case of a repository that has no
// .mailmap and where nobody is going to add one.
type Identity struct {
	Name   string   // canonical display name, may be empty
	Emails []string // at least one
}

// Label is what a report should print for this person.
func (i Identity) Label() string {
	if i.Name != "" {
		return i.Name
	}
	if len(i.Emails) > 0 {
		return i.Emails[0]
	}
	return ""
}

// SoloIdentity wraps a single address, for the ordinary case of a caller that
// was given an email and no roster.
func SoloIdentity(email string) Identity {
	return Identity{Emails: []string{strings.TrimSpace(email)}}
}

// AuthorArgsFor builds the git log author filter for one person.
//
// The matching rules (anchored addresses, literal comparison, case-insensitive)
// live in AuthorArgsMulti and are deliberately not restated here. The name is
// not used as a filter: names are not unique and matching on one would pull in
// unrelated contributors.
func AuthorArgsFor(i Identity) []string {
	return AuthorArgsMulti(i.Emails)
}

// AnalyzeIdentity analyses one person across every address they commit under
// and labels the result with their canonical name, so a report names the human
// rather than whichever address happened to be listed first.
func AnalyzeIdentity(repoPath string, i Identity, since, until time.Time, excludePatterns []string) (RepoStats, error) {
	stats, err := AnalyzeMulti(repoPath, i.Emails, since, until, excludePatterns)
	if err != nil {
		return stats, err
	}
	stats.Author = i.Label()
	return stats, nil
}

// Roster maps people to the set of addresses they commit under.
type Roster []Identity

// ParseAlias reads one inline alias spec, the form accepted on the command
// line: "Juan Gracia=a@x.com,b@x.com".
func ParseAlias(spec string) (Identity, error) {
	name, rest, ok := strings.Cut(spec, "=")
	if !ok {
		return Identity{}, fmt.Errorf("invalid alias %q: expected NAME=email[,email...]", spec)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Identity{}, fmt.Errorf("invalid alias %q: missing name before the '='", spec)
	}
	emails := splitEmails(rest)
	if len(emails) == 0 {
		return Identity{}, fmt.Errorf("invalid alias %q: %s has no email addresses", spec, name)
	}
	return Identity{Name: name, Emails: emails}, nil
}

// Resolve looks a token up as either a canonical name or one of the addresses
// under it. Comparison is case-insensitive on both, matching how AuthorArgs
// treats addresses, so a roster written in one casing still answers a user who
// types another.
func (r Roster) Resolve(token string) (Identity, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Identity{}, false
	}
	for _, id := range r {
		if strings.EqualFold(id.Name, token) && id.Name != "" {
			return id, true
		}
		for _, e := range id.Emails {
			if strings.EqualFold(e, token) {
				return id, true
			}
		}
	}
	return Identity{}, false
}

// Validate reports the roster mistakes that would produce a wrong report
// rather than an obvious failure.
func (r Roster) Validate() error {
	return r.validate(nil)
}

// validate optionally takes the source line each identity came from, so a
// roster read from a file can point at the offending line. lines may be nil.
func (r Roster) validate(lines []int) error {
	at := func(i int) string {
		if i < len(lines) && lines[i] > 0 {
			return fmt.Sprintf("line %d: ", lines[i])
		}
		return ""
	}

	seenEmail := make(map[string]int, len(r))
	seenName := make(map[string]int, len(r))
	for i, id := range r {
		if len(id.Emails) == 0 {
			return fmt.Errorf("%s%q has no email addresses", at(i), id.Label())
		}
		if id.Name != "" {
			key := strings.ToLower(id.Name)
			if _, dup := seenName[key]; dup {
				return fmt.Errorf("%s%q is listed twice: merge the entries, or one set of addresses will be ignored",
					at(i), id.Name)
			}
			seenName[key] = i
		}
		for _, e := range id.Emails {
			key := strings.ToLower(e)
			if prev, dup := seenEmail[key]; dup {
				// Left alone this address would be counted once for each
				// identity claiming it, silently inflating the team total.
				return fmt.Errorf("%s%s is listed under both %q and %q: an address can belong to only one person, or team totals double-count it",
					at(i), e, r[prev].Label(), id.Label())
			}
			seenEmail[key] = i
		}
	}
	return nil
}

// LoadRoster reads a roster file in either of two formats, chosen by the first
// non-whitespace byte:
//
//	{"Juan Gracia": ["a@x.com", "b@x.com"]}   JSON, when the file starts with "{"
//	Juan Gracia: a@x.com, b@x.com             one person per line, otherwise
//
// The line format is also valid YAML, which is why a .yaml roster written this
// way works and why this package needs no YAML dependency. Blank lines are
// ignored and "#" starts a comment.
func LoadRoster(path string) (Roster, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read roster %s: %w", path, err)
	}

	var roster Roster
	var lines []int
	if isJSONObject(data) {
		roster, err = parseJSONRoster(data, path)
	} else {
		roster, lines, err = parseLineRoster(data, path)
	}
	if err != nil {
		return nil, err
	}

	if len(roster) == 0 {
		return nil, fmt.Errorf("roster %s lists no identities", path)
	}
	if err := roster.validate(lines); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return roster, nil
}

func isJSONObject(data []byte) bool {
	for _, b := range data {
		switch b {
		case ' ', '\t', '\r', '\n':
			continue
		default:
			return b == '{'
		}
	}
	return false
}

// parseJSONRoster reads the {"name": ["email", ...]} form. Object key order is
// not preserved by the decoder, so identities come back sorted by name to keep
// reports stable between runs.
func parseJSONRoster(data []byte, path string) (Roster, error) {
	var raw map[string][]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("could not parse roster %s as JSON: %w (expected {\"Name\": [\"a@x.com\", \"b@x.com\"]})", path, err)
	}

	names := make([]string, 0, len(raw))
	for name := range raw {
		names = append(names, name)
	}
	sort.Strings(names)

	roster := make(Roster, 0, len(names))
	for _, name := range names {
		emails := make([]string, 0, len(raw[name]))
		for _, e := range raw[name] {
			if e = strings.TrimSpace(e); e != "" {
				emails = append(emails, e)
			}
		}
		roster = append(roster, Identity{Name: strings.TrimSpace(name), Emails: emails})
	}
	return roster, nil
}

// parseLineRoster reads the "Name: a@x.com, b@x.com" form and returns the
// source line each identity came from, so later validation can name it.
func parseLineRoster(data []byte, path string) (Roster, []int, error) {
	raw := strings.Split(string(data), "\n")

	// Comments are stripped up front so the block-style check below can look
	// ahead to the next line that actually carries content.
	content := make([]string, len(raw))
	for i, line := range raw {
		if h := strings.IndexByte(line, '#'); h >= 0 {
			line = line[:h]
		}
		content[i] = line
	}

	var roster Roster
	var lines []int

	for i, line := range content {
		lineNo := i + 1
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// A bare "- address" is a YAML sequence item, never a roster entry.
		if strings.HasPrefix(trimmed, "-") && !strings.Contains(trimmed, ":") {
			return nil, nil, blockStyleError(path, lineNo)
		}

		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			return nil, nil, fmt.Errorf("%s line %d: %q is missing a ':'; expected \"Name: a@x.com, b@x.com\"", path, lineNo, strings.TrimSpace(raw[i]))
		}
		name = unquote(strings.TrimSpace(name))
		if name == "" {
			return nil, nil, fmt.Errorf("%s line %d: missing name before the ':'", path, lineNo)
		}
		emails := splitEmails(rest)
		if len(emails) == 0 {
			// "Name:" with the addresses indented underneath is ordinary
			// block-style YAML, which is what a user told to write a .yaml
			// roster will reach for first. Reporting "no email addresses"
			// here is true but reads as "your addresses were ignored", which
			// sends the reader hunting for the wrong problem.
			if nextContentIsListItem(content, i) {
				return nil, nil, blockStyleError(path, lineNo)
			}
			return nil, nil, fmt.Errorf("%s line %d: %q has no email addresses", path, lineNo, name)
		}

		roster = append(roster, Identity{Name: name, Emails: emails})
		lines = append(lines, lineNo)
	}

	return roster, lines, nil
}

// nextContentIsListItem reports whether the next line after i carrying content
// opens a YAML sequence.
func nextContentIsListItem(content []string, i int) bool {
	for _, line := range content[i+1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		return strings.HasPrefix(trimmed, "-")
	}
	return false
}

func blockStyleError(path string, lineNo int) error {
	return fmt.Errorf("%s line %d: block-style YAML lists are not supported; write \"Juan Gracia: juan.gracia@bunn.com, other@x.com\" on one line", path, lineNo)
}

// splitEmails accepts the comma-separated address list used by both the inline
// alias form and the line file format.
func splitEmails(s string) []string {
	parts := strings.Split(s, ",")
	emails := make([]string, 0, len(parts))
	for _, p := range parts {
		if e := unquote(strings.TrimSpace(p)); e != "" {
			emails = append(emails, e)
		}
	}
	return emails
}

// unquote drops the surrounding quotes a YAML-minded author may have written,
// since the line format is meant to be readable as YAML.
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
