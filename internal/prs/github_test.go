package prs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

type githubFixture struct {
	login   string
	created time.Time
	merged  time.Time
	repo    string
}

// githubStub serves search/issues from fixtures, honouring the created:A..B
// qualifier so window bisection can be exercised for real.
type githubStub struct {
	fixtures []githubFixture
	// totalOverride lets a test claim more matches than it will hand over,
	// which is exactly what the 1000 result cap looks like in production.
	totalOverride func(since, until time.Time) int
	queries       []string
}

func (s *githubStub) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		s.queries = append(s.queries, q)

		since, until, err := parseCreatedRange(q)
		if err != nil {
			t.Errorf("query %q has no parsable created range: %v", q, err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}

		// Keep each fixture's global index so its html_url is stable across
		// windows. Re-numbering per window would make two different pull
		// requests look identical to the caller's deduplication.
		var matched []int
		for i, f := range s.fixtures {
			if !f.created.Before(since) && !f.created.After(until) {
				matched = append(matched, i)
			}
		}

		total := len(matched)
		if s.totalOverride != nil {
			total = s.totalOverride(since, until)
		}

		start := (page - 1) * githubPerPage
		end := start + githubPerPage
		if start > len(matched) {
			start = len(matched)
		}
		if end > len(matched) {
			end = len(matched)
		}

		items := make([]string, 0, end-start)
		for _, idx := range matched[start:end] {
			items = append(items, s.fixtures[idx].json(idx))
		}
		fmt.Fprintf(w, `{"total_count": %d, "incomplete_results": false, "items": [%s]}`,
			total, strings.Join(items, ","))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (f githubFixture) json(i int) string {
	repo := f.repo
	if repo == "" {
		repo = "my-org/app"
	}
	pr := `null`
	if !f.merged.IsZero() {
		pr = fmt.Sprintf(`{"merged_at": %q}`, f.merged.UTC().Format(time.RFC3339))
	}
	return fmt.Sprintf(`{
		"number": %d, "title": "PR %d",
		"html_url": "https://github.com/%s/pull/%d",
		"repository_url": "https://api.github.com/repos/%s",
		"state": "open",
		"created_at": %q,
		"user": {"login": %q},
		"pull_request": %s
	}`, i, i, repo, i, repo, f.created.UTC().Format(time.RFC3339), f.login, pr)
}

// parseCreatedRange pulls the window back out of the search query so the stub
// can behave like the real API.
func parseCreatedRange(q string) (time.Time, time.Time, error) {
	for _, field := range strings.Fields(q) {
		rest, ok := strings.CutPrefix(field, "created:")
		if !ok {
			continue
		}
		lo, hi, ok := strings.Cut(rest, "..")
		if !ok {
			return time.Time{}, time.Time{}, fmt.Errorf("malformed range %q", rest)
		}
		since, err := time.Parse("2006-01-02T15:04:05Z", lo)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		until, err := time.Parse("2006-01-02T15:04:05Z", hi)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		return since, until, nil
	}
	return time.Time{}, time.Time{}, fmt.Errorf("no created qualifier")
}

func githubProviderFor(srv *httptest.Server, org string) *githubProvider {
	tr := &httpTransport{
		provider: ProviderGitHub,
		base:     srv.URL,
		token:    "test-token",
		client:   srv.Client(),
	}
	return newGitHubProvider(org, tr)
}

func TestGitHubSearchQueryScopesToOrgAndPullRequests(t *testing.T) {
	stub := &githubStub{}
	srv := stub.server(t)

	since, until := fullYear(t)
	if _, err := githubProviderFor(srv, "my-org").Fetch(context.Background(), since, until); err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if len(stub.queries) == 0 {
		t.Fatal("no search query was issued")
	}
	q := stub.queries[0]
	for _, want := range []string{"type:pr", "org:my-org", "created:"} {
		if !strings.Contains(q, want) {
			t.Errorf("query %q should contain %q", q, want)
		}
	}
}

func TestGitHubSearchPaginatesToTheEnd(t *testing.T) {
	base := localTime(t, "2025-03-01 12:00")
	stub := &githubStub{}
	for i := 0; i < 250; i++ {
		stub.fixtures = append(stub.fixtures, githubFixture{
			login:   "alice",
			created: base.Add(time.Duration(i) * time.Minute),
		})
	}
	srv := stub.server(t)

	since, until := fullYear(t)
	got, err := githubProviderFor(srv, "my-org").Fetch(context.Background(), since, until)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if len(got.Items) != 250 {
		t.Fatalf("got %d pull requests, want all 250 across 3 pages", len(got.Items))
	}
	if got.Truncated {
		t.Fatal("Truncated should be false when the whole result set fits under the cap")
	}
	if got.Requests != 3 {
		t.Fatalf("Requests = %d, want exactly 3 pages", got.Requests)
	}
}

func TestGitHubEmptySearchIsNotAnError(t *testing.T) {
	stub := &githubStub{}
	srv := stub.server(t)

	since, until := fullYear(t)
	got, err := githubProviderFor(srv, "my-org").Fetch(context.Background(), since, until)
	if err != nil {
		t.Fatalf("an org with no pull requests must not be an error, got: %v", err)
	}
	if len(got.Items) != 0 || got.Truncated {
		t.Fatalf("got %+v, want an empty, untruncated result", got)
	}
}

// The 1000 result cap is the whole reason the search API needs special
// handling: an oversized window has to be split, not silently under-reported.
func TestGitHubBisectsWindowOverTheResultCap(t *testing.T) {
	early := localTime(t, "2025-02-01 12:00")
	late := localTime(t, "2025-11-01 12:00")
	stub := &githubStub{
		fixtures: []githubFixture{
			{login: "alice", created: early},
			{login: "bob", created: late},
		},
		totalOverride: func(since, until time.Time) int {
			// Only the full year claims to blow past the cap.
			if until.Sub(since) > 200*24*time.Hour {
				return githubSearchCap + 500
			}
			return 2
		},
	}
	srv := stub.server(t)

	since, until := fullYear(t)
	got, err := githubProviderFor(srv, "my-org").Fetch(context.Background(), since, until)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if got.Truncated {
		t.Fatalf("splitting should have avoided truncation, note: %q", got.Note)
	}
	if len(got.Items) != 2 {
		t.Fatalf("got %d pull requests, want both halves collected: %+v", len(got.Items), got.Items)
	}
	if len(stub.queries) < 3 {
		t.Fatalf("made %d queries, want the original plus both halves", len(stub.queries))
	}
}

func TestGitHubBisectionDoesNotDoubleCount(t *testing.T) {
	stub := &githubStub{
		fixtures: []githubFixture{
			{login: "alice", created: localTime(t, "2025-06-15 12:00")},
		},
		totalOverride: func(since, until time.Time) int {
			if until.Sub(since) > 200*24*time.Hour {
				return githubSearchCap + 1
			}
			return 1
		},
	}
	srv := stub.server(t)

	since, until := fullYear(t)
	got, err := githubProviderFor(srv, "my-org").Fetch(context.Background(), since, until)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("got %d pull requests, want 1: overlapping halves must not double count", len(got.Items))
	}
}

func TestGitHubReportsTruncationWhenTheWindowCannotSplitFurther(t *testing.T) {
	start := localTime(t, "2025-06-01 00:00")
	stub := &githubStub{
		totalOverride: func(since, until time.Time) int { return githubSearchCap + 700 },
	}
	srv := stub.server(t)

	// 36 hours splits once into two sub-day windows and then has to give up.
	got, err := githubProviderFor(srv, "my-org").Fetch(context.Background(), start, start.Add(36*time.Hour))
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if !got.Truncated {
		t.Fatal("a window that still exceeds the cap after splitting must report truncation")
	}
	if !strings.Contains(got.Note, "floor") {
		t.Fatalf("note %q should say the counts are a floor, not a total", got.Note)
	}
}

func TestGitHubReportsIncompleteResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"total_count": 3, "incomplete_results": true, "items": []}`)
	}))
	defer srv.Close()

	since, until := fullYear(t)
	got, err := githubProviderFor(srv, "my-org").Fetch(context.Background(), since, until)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if !got.Truncated {
		t.Fatal("incomplete_results means the server gave up mid-query and must be surfaced")
	}
}

func TestGitHubFetchUnauthorizedIsActionable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"message":"Bad credentials"}`)
	}))
	defer srv.Close()

	since, until := fullYear(t)
	_, err := githubProviderFor(srv, "my-org").Fetch(context.Background(), since, until)
	if err == nil {
		t.Fatal("expected an error for a 401")
	}
	if !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("error %q should name the environment variable to refresh", err)
	}
}

// GitHub signals an exhausted search quota with a 403, which must not be read
// as "your token is wrong".
func TestGitHubSecondaryRateLimitIsNotAnAuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"API rate limit exceeded for user"}`)
	}))
	defer srv.Close()

	since, until := fullYear(t)
	_, err := githubProviderFor(srv, "my-org").Fetch(context.Background(), since, until)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("error %q should be reported as throttling", err)
	}
	if strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("throttling message %q should not tell the user to fix their token", err)
	}
}

func TestGitHubFetchViaCLIRunner(t *testing.T) {
	body := fmt.Sprintf(`{"total_count":1,"incomplete_results":false,"items":[%s]}`,
		githubFixture{login: "alice", created: localTime(t, "2025-05-01 10:00")}.json(1))
	runner := &fakeRunner{outputs: [][]byte{[]byte(body)}}
	provider := newGitHubProvider("my-org", &cliTransport{provider: ProviderGitHub, bin: "gh", runner: runner})

	since, until := fullYear(t)
	got, err := provider.Fetch(context.Background(), since, until)
	if err != nil {
		t.Fatalf("Fetch via CLI failed: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(got.Items))
	}
	if len(runner.calls) == 0 || runner.calls[0][0] != "gh" {
		t.Fatalf("calls = %v, want the gh binary to be invoked", runner.calls)
	}
}

func TestGitHubItemNormalize(t *testing.T) {
	var item githubItem
	body := `{"number":12,"title":"t",
		"html_url":"https://github.com/my-org/app/pull/12",
		"repository_url":"https://api.github.com/repos/my-org/app",
		"state":"closed","created_at":"2025-01-05T10:00:00Z",
		"user":{"login":"jdoe"},
		"pull_request":{"merged_at":"2025-01-07T10:00:00Z"}}`
	if err := json.Unmarshal([]byte(body), &item); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	got, ok := item.normalize()
	if !ok {
		t.Fatal("normalize rejected a well formed pull request")
	}
	if got.Project != "my-org/app" {
		t.Errorf("Project = %q, want my-org/app", got.Project)
	}
	if got.AuthorUser != "jdoe" {
		t.Errorf("AuthorUser = %q, want jdoe", got.AuthorUser)
	}
	// The search API gives no email and no display name; pretending otherwise
	// would produce false identity matches.
	if got.AuthorEmail != "" || got.AuthorName != "" {
		t.Errorf("author email/name = %q/%q, want both empty", got.AuthorEmail, got.AuthorName)
	}
	if got.State != "merged" {
		t.Errorf("State = %q, want merged once merged_at is set", got.State)
	}
}

func TestGitHubItemNormalizeUnmergedPullRequest(t *testing.T) {
	var item githubItem
	body := `{"html_url":"https://github.com/o/r/pull/1","state":"open",
		"created_at":"2025-01-05T10:00:00Z","user":{"login":"a"},
		"pull_request":{"merged_at":null}}`
	if err := json.Unmarshal([]byte(body), &item); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	got, ok := item.normalize()
	if !ok {
		t.Fatal("normalize rejected an open pull request")
	}
	if !got.MergedAt.IsZero() {
		t.Errorf("MergedAt = %v, want zero for an unmerged pull request", got.MergedAt)
	}
}

func TestGitHubRepoPathFallsBackToHTMLURL(t *testing.T) {
	item := githubItem{HTMLURL: "https://github.com/my-org/app/pull/9"}
	if got := item.repoPath(); got != "my-org/app" {
		t.Fatalf("repoPath = %q, want my-org/app", got)
	}
	if got := (githubItem{}).repoPath(); got != "" {
		t.Fatalf("repoPath with nothing to go on = %q, want empty", got)
	}
}

// Bisection can in principle fan out to thousands of sub-windows. A hard
// request budget is what stops a dense window from turning into a request
// storm against a live API.
func TestGitHubStopsAtTheRequestBudget(t *testing.T) {
	stub := &githubStub{
		totalOverride: func(since, until time.Time) int { return githubSearchCap + 1 },
	}
	srv := stub.server(t)

	since, until := fullYear(t)
	got, err := githubProviderFor(srv, "my-org").Fetch(context.Background(), since, until)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if !got.Truncated {
		t.Fatal("giving up on the budget must be reported as truncation")
	}
	if got.Requests > githubMaxRequests+githubMaxSearchPages {
		t.Fatalf("made %d requests, want the budget of %d roughly respected", got.Requests, githubMaxRequests)
	}
}

func TestGitHubKeepsTheFirstTruncationNote(t *testing.T) {
	out := &Fetched{}
	setNote(out, "first")
	setNote(out, "second")
	if out.Note != "first" {
		t.Fatalf("Note = %q, want the first explanation kept", out.Note)
	}
}

func TestGitHubBaseURL(t *testing.T) {
	cases := map[string]string{
		"":                            githubDefaultBase,
		"https://ghe.corp.com":        "https://ghe.corp.com/api/v3",
		"https://ghe.corp.com/":       "https://ghe.corp.com/api/v3",
		"https://ghe.corp.com/api/v3": "https://ghe.corp.com/api/v3",
	}
	for in, want := range cases {
		if got := githubBaseURL(in); got != want {
			t.Errorf("githubBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}
