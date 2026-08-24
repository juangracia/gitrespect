package prs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func gitlabMRJSON(id int, username, created, mergedAt string) string {
	merged := "null"
	if mergedAt != "" {
		merged = fmt.Sprintf("%q", mergedAt)
	}
	return fmt.Sprintf(`{
		"id": %d, "iid": %d, "title": "MR %d",
		"web_url": "https://gitlab.com/g/p/-/merge_requests/%d",
		"state": "opened",
		"created_at": %q,
		"merged_at": %s,
		"project_id": 7,
		"references": {"full": "g/p!%d"},
		"author": {"username": %q, "name": "Someone"}
	}`, id, id, id, id, created, merged, id, username)
}

// gitlabServer serves the group merge request endpoint from canned pages and
// records the paths it was asked for.
func gitlabServer(t *testing.T, pages [][]string) (*httptest.Server, *[]*http.Request) {
	t.Helper()
	var seen []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r)
		page := 1
		fmt.Sscanf(r.URL.Query().Get("page"), "%d", &page)
		if page < 1 || page > len(pages) {
			w.Header().Set("X-Next-Page", "")
			fmt.Fprint(w, "[]")
			return
		}
		if page < len(pages) {
			w.Header().Set("X-Next-Page", fmt.Sprintf("%d", page+1))
		} else {
			w.Header().Set("X-Next-Page", "")
		}
		fmt.Fprint(w, "["+strings.Join(pages[page-1], ",")+"]")
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func gitlabProviderFor(srv *httptest.Server, scope string) *gitlabProvider {
	tr := &httpTransport{
		provider: ProviderGitLab,
		base:     srv.URL,
		token:    "test-token",
		client:   srv.Client(),
	}
	return newGitLabProvider(scope, tr)
}

func fullYear(t *testing.T) (time.Time, time.Time) {
	t.Helper()
	return localTime(t, "2025-01-01 00:00"), localTime(t, "2025-12-31 23:59")
}

func TestGitLabFetchAccumulatesAcrossPages(t *testing.T) {
	srv, seen := gitlabServer(t, [][]string{
		{gitlabMRJSON(1, "alice", "2025-01-05T10:00:00.000Z", ""), gitlabMRJSON(2, "bob", "2025-02-05T10:00:00.000Z", "2025-02-08T10:00:00.000Z")},
		{gitlabMRJSON(3, "alice", "2025-03-05T10:00:00.000Z", "")},
	})

	since, until := fullYear(t)
	got, err := gitlabProviderFor(srv, "bunn-digital/web").Fetch(context.Background(), since, until)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if len(got.Items) != 3 {
		t.Fatalf("got %d merge requests, want 3 accumulated across pages", len(got.Items))
	}
	if got.Truncated {
		t.Fatal("Truncated should be false when pagination completed")
	}
	// Two data pages plus the empty page that terminates the walk is fine;
	// what matters is that it terminated at all.
	if len(*seen) < 2 {
		t.Fatalf("made %d requests, want at least 2", len(*seen))
	}
}

func TestGitLabFetchUsesGroupScopedEndpointWithEscapedPath(t *testing.T) {
	srv, seen := gitlabServer(t, [][]string{{}})

	since, until := fullYear(t)
	if _, err := gitlabProviderFor(srv, "bunn-digital/web").Fetch(context.Background(), since, until); err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if len(*seen) == 0 {
		t.Fatal("no request was made")
	}
	req := (*seen)[0]
	// The group path has to survive as one path segment, or GitLab reads it
	// as a nested route and 404s.
	if !strings.Contains(req.URL.EscapedPath(), "bunn-digital%2Fweb") {
		t.Fatalf("path = %q, want the group path percent-encoded", req.URL.EscapedPath())
	}
	if !strings.Contains(req.URL.Path, "/groups/") || !strings.HasSuffix(req.URL.Path, "/merge_requests") {
		t.Fatalf("path = %q, want the group scoped merge_requests endpoint", req.URL.Path)
	}
	q := req.URL.Query()
	for key, want := range map[string]string{"scope": "all", "state": "all"} {
		if q.Get(key) != want {
			t.Errorf("query %s = %q, want %q", key, q.Get(key), want)
		}
	}
	if q.Get("created_after") == "" || q.Get("created_before") == "" {
		t.Errorf("query should carry created_after/created_before, got %v", q)
	}
}

func TestGitLabFetchEmptyResultIsNotAnError(t *testing.T) {
	srv, _ := gitlabServer(t, [][]string{{}})

	since, until := fullYear(t)
	got, err := gitlabProviderFor(srv, "g").Fetch(context.Background(), since, until)
	if err != nil {
		t.Fatalf("an empty group must not be an error, got: %v", err)
	}
	if len(got.Items) != 0 {
		t.Fatalf("got %d items, want none", len(got.Items))
	}
}

func TestGitLabFetchUnauthorizedIsActionable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"message":"401 Unauthorized"}`)
	}))
	defer srv.Close()

	since, until := fullYear(t)
	_, err := gitlabProviderFor(srv, "g").Fetch(context.Background(), since, until)
	if err == nil {
		t.Fatal("expected an error for a 401")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %v is not an *APIError", err)
	}
	if apiErr.RateLimited {
		t.Fatal("a 401 must not be classified as throttling")
	}
	if !strings.Contains(err.Error(), "GITLAB_TOKEN") {
		t.Fatalf("error %q should name the environment variable to refresh", err)
	}
}

func TestGitLabFetchRateLimitIsDistinctFromAuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("RateLimit-Remaining", "0")
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"message":"Too many requests"}`)
	}))
	defer srv.Close()

	since, until := fullYear(t)
	_, err := gitlabProviderFor(srv, "g").Fetch(context.Background(), since, until)

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %v is not an *APIError", err)
	}
	if !apiErr.RateLimited {
		t.Fatal("a 429 must be classified as throttling, not as bad credentials")
	}
	if strings.Contains(err.Error(), "GITLAB_TOKEN") {
		t.Fatalf("throttling message %q should not tell the user to fix their token", err)
	}
}

func TestGitLabFetchForbiddenIsNotThrottling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("RateLimit-Remaining", "580")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"403 Forbidden"}`)
	}))
	defer srv.Close()

	since, until := fullYear(t)
	_, err := gitlabProviderFor(srv, "g").Fetch(context.Background(), since, until)

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %v is not an *APIError", err)
	}
	if apiErr.RateLimited {
		t.Fatal("a 403 with quota left is a permission problem, not throttling")
	}
	if !strings.Contains(err.Error(), "lack access") {
		t.Fatalf("error %q should point at access, not at throttling", err)
	}
}

func TestGitLabFetchRejectsItemsOutsideTheWindow(t *testing.T) {
	// A server that ignores created_after must not be able to inflate counts.
	srv, _ := gitlabServer(t, [][]string{{
		gitlabMRJSON(1, "alice", "2024-06-05T10:00:00.000Z", ""),
		gitlabMRJSON(2, "alice", "2025-06-05T10:00:00.000Z", ""),
		gitlabMRJSON(3, "alice", "2026-06-05T10:00:00.000Z", ""),
	}})

	since, until := fullYear(t)
	got, err := gitlabProviderFor(srv, "g").Fetch(context.Background(), since, until)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].ID != "2" {
		t.Fatalf("got %+v, want only the merge request inside the window", got.Items)
	}
}

func TestGitLabFetchDeduplicatesRepeatedItems(t *testing.T) {
	repeated := gitlabMRJSON(1, "alice", "2025-01-05T10:00:00.000Z", "")
	srv, _ := gitlabServer(t, [][]string{{repeated}, {repeated}})

	since, until := fullYear(t)
	got, err := gitlabProviderFor(srv, "g").Fetch(context.Background(), since, until)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("got %d items, want the duplicate collapsed", len(got.Items))
	}
}

func TestGitLabFetchViaCLIRunner(t *testing.T) {
	page1, _ := json.Marshal(json.RawMessage("[" + gitlabMRJSON(1, "alice", "2025-01-05T10:00:00.000Z", "") + "]"))
	runner := &fakeRunner{outputs: [][]byte{page1, []byte("[]")}}
	provider := newGitLabProvider("g/p", &cliTransport{provider: ProviderGitLab, bin: "glab", runner: runner})

	since, until := fullYear(t)
	got, err := provider.Fetch(context.Background(), since, until)
	if err != nil {
		t.Fatalf("Fetch via CLI failed: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(got.Items))
	}
	if len(runner.calls) == 0 || runner.calls[0][0] != "glab" {
		t.Fatalf("calls = %v, want the glab binary to be invoked", runner.calls)
	}
}

func TestGitLabNormalizeReadsAuthorAndTimestamps(t *testing.T) {
	var raw gitlabMR
	body := `{"id":9,"iid":3,"title":"t","web_url":"https://gitlab.com/g/p/-/merge_requests/3",
		"state":"merged","created_at":"2025-01-05T10:00:00.000Z","merged_at":"2025-01-07T10:00:00.000Z",
		"references":{"full":"g/p!3"},
		"author":{"username":"jdoe","name":"Jane Doe","public_email":"jane@corp.com"}}`
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	got, ok := raw.normalize()
	if !ok {
		t.Fatal("normalize rejected a well formed merge request")
	}
	if got.ID != "9" || got.Project != "g/p" {
		t.Errorf("ID=%q Project=%q, want 9 and g/p", got.ID, got.Project)
	}
	if got.AuthorUser != "jdoe" || got.AuthorName != "Jane Doe" || got.AuthorEmail != "jane@corp.com" {
		t.Errorf("author = %q/%q/%q, want jdoe/Jane Doe/jane@corp.com", got.AuthorUser, got.AuthorName, got.AuthorEmail)
	}
	if got.MergedAt.IsZero() {
		t.Error("MergedAt should be populated")
	}
}

func TestGitLabNormalizeRejectsMissingCreatedAt(t *testing.T) {
	var raw gitlabMR
	if err := json.Unmarshal([]byte(`{"id":1,"created_at":null}`), &raw); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if _, ok := raw.normalize(); ok {
		t.Fatal("a merge request with no creation time cannot be bucketed and must be dropped")
	}
}

func TestGitLabProjectPathFallbacks(t *testing.T) {
	fromWebURL := gitlabMR{WebURL: "https://gitlab.com/group/sub/proj/-/merge_requests/4"}
	if got := fromWebURL.projectPath(); got != "group/sub/proj" {
		t.Errorf("projectPath from web url = %q, want group/sub/proj", got)
	}
	fromID := gitlabMR{ProjectID: 42}
	if got := fromID.projectPath(); got != "project/42" {
		t.Errorf("projectPath from id = %q, want project/42", got)
	}
	if got := (gitlabMR{}).projectPath(); got != "" {
		t.Errorf("projectPath with nothing to go on = %q, want empty", got)
	}
}

func TestGitLabBaseURL(t *testing.T) {
	cases := map[string]string{
		"":                               gitlabDefaultBase,
		"https://gitlab.corp.com":        "https://gitlab.corp.com/api/v4",
		"https://gitlab.corp.com/":       "https://gitlab.corp.com/api/v4",
		"https://gitlab.corp.com/api/v4": "https://gitlab.corp.com/api/v4",
	}
	for in, want := range cases {
		if got := gitlabBaseURL(in); got != want {
			t.Errorf("gitlabBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseAPITime(t *testing.T) {
	for _, in := range []string{
		"2025-01-05T10:00:00.000Z",
		"2025-01-05T10:00:00Z",
		"2025-01-05T10:00:00+0200",
	} {
		if _, err := parseAPITime(in); err != nil {
			t.Errorf("parseAPITime(%q) failed: %v", in, err)
		}
	}
	for _, in := range []string{"", "null", "yesterday"} {
		if _, err := parseAPITime(in); err == nil {
			t.Errorf("parseAPITime(%q) should have failed", in)
		}
	}
}
