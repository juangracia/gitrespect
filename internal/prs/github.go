package prs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	githubDefaultBase = "https://api.github.com"
	githubPerPage     = 100
	// githubSearchCap is the hard limit on results the search API will page
	// through, regardless of what total_count reports.
	githubSearchCap = 1000
	// githubMaxSearchPages follows from the cap at 100 results per page.
	githubMaxSearchPages = githubSearchCap / githubPerPage
	// githubMinSplit is the narrowest window the cap workaround will bisect
	// to. Below a day, splitting stops helping and the result is genuinely
	// truncated.
	githubMinSplit = 24 * time.Hour
	// githubMaxSplitDepth bounds the recursion independently of the window
	// length, so a pathological input cannot fan out without limit.
	githubMaxSplitDepth = 12
	// githubMaxRequests is a hard budget across the whole fetch. Bisection can
	// in principle fan out to thousands of sub-windows, and firing that at a
	// live API with a 30 requests/minute search quota would be worse than
	// admitting the window is too dense to cover.
	githubMaxRequests = 300
)

// githubProvider reads pull requests for a whole org through the search API.
//
// Why search rather than per repo REST: GET /repos/:owner/:repo/pulls has no
// date filter, so covering a window means paging a repo's entire pull request
// history until you fall out of it, once per repo. For an org with hundreds of
// repos that is hundreds to thousands of requests for a single report. The
// search API answers the same question org wide in one paginated query.
//
// The cost of that choice is the search API's 1000 result cap and its stricter
// rate limit (30 requests/minute authenticated). The cap is handled rather than
// ignored: when a window reports more than 1000 matches it is bisected and each
// half queried separately, recursively, down to a one day floor. Only if a
// single day still exceeds the cap is the result reported as truncated, and
// that truncation is carried all the way into the report instead of silently
// under-counting someone's work.
type githubProvider struct {
	scope string
	tr    transport
}

func newGitHubProvider(scope string, tr transport) *githubProvider {
	return &githubProvider{scope: scope, tr: tr}
}

func (g *githubProvider) Name() string  { return ProviderGitHub }
func (g *githubProvider) Scope() string { return g.scope }

func (g *githubProvider) Fetch(ctx context.Context, since, until time.Time) (Fetched, error) {
	var out Fetched
	seen := map[string]bool{}
	err := g.fetchWindow(ctx, since, until, 0, &out, seen)
	if err != nil {
		return out, err
	}
	return out, nil
}

// fetchWindow collects one window, bisecting it when the search API says there
// are more matches than it will hand over.
func (g *githubProvider) fetchWindow(ctx context.Context, since, until time.Time, depth int, out *Fetched, seen map[string]bool) error {
	if out.Requests >= githubMaxRequests {
		out.Truncated = true
		setNote(out, fmt.Sprintf("stopped after %d github api requests; the window is too dense to cover through search, so narrow it with --since/--until",
			githubMaxRequests))
		return nil
	}

	first, err := g.searchPage(ctx, since, until, 1)
	out.Requests++
	if err != nil {
		return err
	}

	if first.TotalCount > githubSearchCap {
		canSplit := depth < githubMaxSplitDepth && until.Sub(since) > githubMinSplit
		if canSplit {
			mid := since.Add(until.Sub(since) / 2)
			// The halves are disjoint because GitHub's created: qualifier is
			// inclusive at both ends, so the first half stops a second early.
			if err := g.fetchWindow(ctx, since, mid.Add(-time.Second), depth+1, out, seen); err != nil {
				return err
			}
			return g.fetchWindow(ctx, mid, until, depth+1, out, seen)
		}
		out.Truncated = true
		setNote(out, fmt.Sprintf("github search returned %d matches for %s to %s but will only page through %d; counts for that window are a floor, not a total",
			first.TotalCount, since.Format("2006-01-02"), until.Format("2006-01-02"), githubSearchCap))
	}
	if first.IncompleteResults {
		out.Truncated = true
		setNote(out, "github reported incomplete search results (the query timed out server side); counts are a floor, not a total")
	}

	collected := g.collect(first.Items, since, until, out, seen)
	if collected == 0 && len(first.Items) == 0 {
		return nil
	}

	for page := 2; page <= githubMaxSearchPages; page++ {
		if (page-1)*githubPerPage >= first.TotalCount {
			break
		}
		if out.Requests >= githubMaxRequests {
			out.Truncated = true
			setNote(out, fmt.Sprintf("stopped after %d github api requests; the window is too dense to cover through search, so narrow it with --since/--until",
				githubMaxRequests))
			break
		}
		resp, err := g.searchPage(ctx, since, until, page)
		out.Requests++
		if err != nil {
			return err
		}
		if len(resp.Items) == 0 {
			break
		}
		g.collect(resp.Items, since, until, out, seen)
		if len(resp.Items) < githubPerPage {
			break
		}
	}
	return nil
}

func (g *githubProvider) collect(items []githubItem, since, until time.Time, out *Fetched, seen map[string]bool) int {
	n := 0
	for _, item := range items {
		mr, ok := item.normalize()
		if !ok {
			continue
		}
		// Enforce the window locally too, so a bisected query that overlaps
		// at the boundary cannot double count or drift.
		if mr.CreatedAt.Before(since) || mr.CreatedAt.After(until) {
			continue
		}
		if seen[mr.ID] {
			continue
		}
		seen[mr.ID] = true
		out.Items = append(out.Items, mr)
		n++
	}
	return n
}

func (g *githubProvider) searchPage(ctx context.Context, since, until time.Time, page int) (githubSearchResponse, error) {
	q := url.Values{}
	q.Set("q", fmt.Sprintf("type:pr org:%s created:%s..%s",
		g.scope,
		since.UTC().Format("2006-01-02T15:04:05Z"),
		until.UTC().Format("2006-01-02T15:04:05Z")))
	q.Set("sort", "created")
	q.Set("order", "asc")
	q.Set("per_page", fmt.Sprintf("%d", githubPerPage))
	q.Set("page", fmt.Sprintf("%d", page))

	resp, err := g.tr.Get(ctx, "search/issues", q)
	if err != nil {
		return githubSearchResponse{}, err
	}
	var parsed githubSearchResponse
	if err := json.Unmarshal(resp.Body, &parsed); err != nil {
		return githubSearchResponse{}, fmt.Errorf("parsing github search response: %w", err)
	}
	return parsed, nil
}

type githubSearchResponse struct {
	TotalCount        int          `json:"total_count"`
	IncompleteResults bool         `json:"incomplete_results"`
	Items             []githubItem `json:"items"`
}

// githubItem is the subset of a search result this report needs. The search
// API returns the issue shape plus a pull_request sub-object, which is where
// merged_at lives.
type githubItem struct {
	Number        int    `json:"number"`
	Title         string `json:"title"`
	HTMLURL       string `json:"html_url"`
	RepositoryURL string `json:"repository_url"`
	State         string `json:"state"`
	CreatedAt     string `json:"created_at"`
	User          struct {
		Login string `json:"login"`
	} `json:"user"`
	PullRequest *struct {
		MergedAt string `json:"merged_at"`
	} `json:"pull_request"`
}

func (i githubItem) normalize() (MergeRequest, bool) {
	created, err := parseAPITime(i.CreatedAt)
	if err != nil {
		return MergeRequest{}, false
	}
	var merged time.Time
	if i.PullRequest != nil {
		merged, _ = parseAPITime(i.PullRequest.MergedAt)
	}

	state := i.State
	if !merged.IsZero() {
		state = "merged"
	}

	return MergeRequest{
		ID:      i.HTMLURL,
		Title:   i.Title,
		URL:     i.HTMLURL,
		Project: i.repoPath(),
		// The search API exposes only a login: no email, and no display name.
		AuthorUser: i.User.Login,
		CreatedAt:  created,
		MergedAt:   merged,
		State:      state,
	}, true
}

// repoPath recovers "owner/name" from the API repository URL, falling back to
// the HTML URL.
func (i githubItem) repoPath() string {
	if i.RepositoryURL != "" {
		if _, rest, ok := strings.Cut(i.RepositoryURL, "/repos/"); ok && rest != "" {
			return rest
		}
	}
	if i.HTMLURL != "" {
		if u, err := url.Parse(i.HTMLURL); err == nil {
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(parts) >= 2 {
				return parts[0] + "/" + parts[1]
			}
		}
	}
	return ""
}

// setNote keeps the first explanation rather than the last. Bisection can hit
// several truncated sub-windows, and the first one names a concrete date range
// the user can act on, where later overwrites would just churn the message.
func setNote(out *Fetched, note string) {
	if out.Note == "" {
		out.Note = note
	}
}

// githubBaseURL normalizes an override so GitHub Enterprise users can pass
// either the host or the full API root.
func githubBaseURL(override string) string {
	override = strings.TrimSpace(strings.TrimSuffix(override, "/"))
	if override == "" {
		return githubDefaultBase
	}
	if strings.Contains(override, "/api/") {
		return override
	}
	return override + "/api/v3"
}
