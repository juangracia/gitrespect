// Package prs collects pull request / merge request activity from a review
// platform and shapes it like the existing git based reports, so "how many MRs
// did X open per month versus the team" is answerable next to "how many lines
// did X write". Local git history cannot answer that question at all: a merge
// request lives on the platform, not in the object database.
package prs

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// Provider names accepted by --provider.
const (
	ProviderGitLab = "gitlab"
	ProviderGitHub = "github"
)

// Providers lists the values accepted by --provider.
var Providers = []string{ProviderGitLab, ProviderGitHub}

// Granularities lists the values accepted by --breakdown. It mirrors the git
// side so the two reports read the same way.
var Granularities = []string{"monthly", "weekly", "daily"}

// ValidGranularity reports whether g is a supported --breakdown value.
func ValidGranularity(g string) bool {
	for _, v := range Granularities {
		if v == g {
			return true
		}
	}
	return false
}

// MergeRequest is one pull request or merge request, normalized across
// providers. Fields a provider cannot supply are left at their zero value
// rather than guessed, because a wrong author is worse than a missing one.
type MergeRequest struct {
	ID      string
	Title   string
	URL     string
	Project string

	// AuthorEmail is usually empty. Neither the GitLab group merge request
	// list nor the GitHub search API expose a contributor email, which is the
	// root of the identity matching problem documented on Matcher.
	AuthorEmail string
	AuthorUser  string
	AuthorName  string

	CreatedAt time.Time
	MergedAt  time.Time
	State     string
}

// authorLabel is the best human readable handle for the account that opened
// this merge request, used when reporting who could not be matched.
func (m MergeRequest) authorLabel() string {
	switch {
	case m.AuthorEmail != "":
		return m.AuthorEmail
	case m.AuthorUser != "":
		return m.AuthorUser
	case m.AuthorName != "":
		return m.AuthorName
	default:
		return "(unknown)"
	}
}

// Fetched is a provider's raw answer plus what it knows about its own
// completeness. Truncation has to travel with the data: a silently short
// result set under-reports someone's work, which is the one failure mode this
// report must never have.
type Fetched struct {
	Items     []MergeRequest
	Truncated bool
	Note      string
	Requests  int
}

// Provider fetches merge requests for one scope and one window. A third
// platform only has to satisfy this interface to slot in.
type Provider interface {
	// Name is the value accepted by --provider.
	Name() string
	// Scope describes what was queried, for the report header.
	Scope() string
	// Fetch returns every merge request created within [since, until].
	// Implementations paginate internally; callers never loop.
	Fetch(ctx context.Context, since, until time.Time) (Fetched, error)
}

// PeriodCount is one bucket of the breakdown.
type PeriodCount struct {
	Key    string    `json:"-"`
	Label  string    `json:"label"`
	Start  time.Time `json:"start"`
	Opened int       `json:"opened"`
	Merged int       `json:"merged"`
}

// LeadTimeStats is open to merge time measured from real platform timestamps.
// This is a much cleaner DORA style signal than any heuristic over git
// branches, because both ends of the interval are recorded facts rather than
// inferred from commit dates.
type LeadTimeStats struct {
	Samples    int     `json:"samples"`
	MedianDays float64 `json:"median_days"`
	P75Days    float64 `json:"p75_days"`
	MeanDays   float64 `json:"mean_days"`
}

// AuthorStats is one person's activity in the window.
type AuthorStats struct {
	// Identity is the label the user asked about, or the observed platform
	// handle when no --author/--team filter was given.
	Identity string `json:"identity"`
	// Handles lists the distinct platform accounts folded into this identity,
	// so a surprising total can be traced back to a bad match.
	Handles  []string       `json:"handles,omitempty"`
	Opened   int            `json:"opened"`
	Merged   int            `json:"merged"`
	Periods  []PeriodCount  `json:"periods,omitempty"`
	LeadTime *LeadTimeStats `json:"lead_time,omitempty"`
}

// UnmatchedAuthor is a platform account whose merge requests were dropped
// because no requested identity claimed them. Surfacing these is what makes
// identity matching auditable instead of quietly lossy.
type UnmatchedAuthor struct {
	Handle string `json:"handle"`
	Opened int    `json:"opened"`
}

// Result is the whole report. It is deliberately flat and purely additive so
// two Results covering different windows can be compared field by field, which
// is what CompareResults does.
type Result struct {
	Provider    string    `json:"provider"`
	Scope       string    `json:"scope"`
	Since       time.Time `json:"since"`
	Until       time.Time `json:"until"`
	Granularity string    `json:"granularity,omitempty"`

	Opened int `json:"opened"`
	Merged int `json:"merged"`

	Authors []AuthorStats `json:"authors"`
	// AuthorsTotal is how many contributors were seen before --top trimmed
	// the table, so a truncated list never reads as the whole group.
	AuthorsTotal int            `json:"authors_total,omitempty"`
	Periods      []PeriodCount  `json:"periods,omitempty"`
	LeadTime     *LeadTimeStats `json:"lead_time,omitempty"`

	// Filtered reports whether an --author/--team filter was applied. Without
	// one Unmatched is always empty, because every account is its own identity.
	Filtered bool `json:"filtered"`
	// Unmatched is capped for readability, so UnmatchedAccounts carries the
	// real number of distinct accounts and UnmatchedTotal the merge requests
	// they account for.
	Unmatched         []UnmatchedAuthor `json:"unmatched,omitempty"`
	UnmatchedTotal    int               `json:"unmatched_total,omitempty"`
	UnmatchedAccounts int               `json:"unmatched_accounts,omitempty"`

	Truncated bool   `json:"truncated"`
	Note      string `json:"note,omitempty"`
	Requests  int    `json:"api_requests,omitempty"`
}

// Options configures Fetch. The transport seams (HTTPClient, Runner, LookPath,
// Getenv, BaseURL) exist so the whole pipeline is exercisable from tests with
// no network access and no glab/gh installed.
type Options struct {
	Provider string
	// Scope is the GitLab group path or id, or the GitHub org.
	Scope string
	// Authors are the identities to report on, one per string. Empty means
	// every account that opened a merge request in the window.
	Authors []string
	// People is the grouped form of Authors, for callers that already know one
	// person owns several addresses (a roster). It takes precedence over
	// Authors when set. Like Authors, it filters: accounts it does not claim
	// are excluded from the report and reported as unmatched.
	People []Person
	// Roster groups without filtering. A roster describes who people are, not
	// which of them to count, so on its own it folds the accounts it
	// recognises under their canonical names and leaves every other account
	// as its own row. Roster members with no merge requests still get a row,
	// at zero, so a person the platform never saw does not silently vanish.
	// Ignored when Authors or People is set, where the filter is the answer to
	// "who am I counting".
	Roster []Person
	// Mappings are "email=handle" pairs that pin a platform account to an
	// identity when the heuristics in Matcher cannot bridge the two.
	Mappings []string

	Since       time.Time
	Until       time.Time
	Granularity string
	// Top caps the contributor table when no filter was given. Zero shows
	// everyone, which is unreadable for a group with hundreds of accounts.
	Top int

	// Token authenticates the plain HTTP path. Empty falls back to the
	// provider environment variable, then to the local CLI.
	Token string
	// BaseURL overrides the API root, for self-hosted GitLab and for tests.
	BaseURL string

	HTTPClient Doer
	Runner     CommandRunner
	LookPath   func(string) (string, error)
	Getenv     func(string) string
}

func (o Options) validate() error {
	switch o.Provider {
	case ProviderGitLab, ProviderGitHub:
	default:
		return fmt.Errorf("invalid --provider %q: expected one of %s",
			o.Provider, strings.Join(Providers, ", "))
	}
	if strings.TrimSpace(o.Scope) == "" {
		if o.Provider == ProviderGitHub {
			return fmt.Errorf("--org is required for the github provider (for example --org my-org)")
		}
		return fmt.Errorf("--group is required for the gitlab provider (for example --group bunn-digital/web)")
	}
	if o.Granularity != "" && !ValidGranularity(o.Granularity) {
		return fmt.Errorf("invalid --breakdown %q: expected one of %s",
			o.Granularity, strings.Join(Granularities, ", "))
	}
	if o.Top < 0 {
		return fmt.Errorf("invalid --top %d: expected a positive number of contributors", o.Top)
	}
	if o.Since.IsZero() || o.Until.IsZero() {
		return fmt.Errorf("both a start and an end date are required")
	}
	if !o.Until.After(o.Since) {
		return fmt.Errorf("start date (%s) must be before end date (%s)",
			o.Since.Format("2006-01-02"), o.Until.Format("2006-01-02"))
	}
	return nil
}

// people returns the filtering identities in their grouped form: the accounts
// this report is restricted to.
func (o Options) people() []Person {
	if len(o.People) > 0 {
		return o.People
	}
	out := make([]Person, 0, len(o.Authors))
	for _, a := range o.Authors {
		out = append(out, Person{Label: a})
	}
	return out
}

// identities returns the set the matcher is built from, and whether matching
// it also filters. A filter answers "who am I counting"; a bare roster only
// answers "who is this account", so it groups and nothing is dropped.
func (o Options) identities() (people []Person, filtering bool) {
	if p := o.people(); len(p) > 0 {
		return p, true
	}
	return o.Roster, false
}

// getenv reads an environment variable through the injectable hook so tests
// never depend on the ambient environment.
func (o Options) getenv(key string) string {
	if o.Getenv != nil {
		return o.Getenv(key)
	}
	return os.Getenv(key)
}
