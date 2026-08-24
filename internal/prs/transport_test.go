package prs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// fakeTransport replays canned pages and records the queries it was asked for,
// so the pagination loop can be tested without any HTTP at all.
type fakeTransport struct {
	pages   []apiResponse
	queries []url.Values
	err     error
}

func (f *fakeTransport) Get(_ context.Context, _ string, q url.Values) (apiResponse, error) {
	f.queries = append(f.queries, q)
	if f.err != nil {
		return apiResponse{}, f.err
	}
	i := len(f.queries) - 1
	if i >= len(f.pages) {
		return apiResponse{Body: []byte("[]")}, nil
	}
	return f.pages[i], nil
}

// fakeRunner stands in for glab/gh so no test needs either binary installed.
type fakeRunner struct {
	outputs [][]byte
	err     error
	calls   [][]string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if f.err != nil {
		return nil, f.err
	}
	i := len(f.calls) - 1
	if i >= len(f.outputs) {
		return []byte("[]"), nil
	}
	return f.outputs[i], nil
}

func TestNextPageFromXNextPageHeader(t *testing.T) {
	h := http.Header{}
	h.Set("X-Next-Page", "3")
	got := nextPage(h)
	if !got.Known || got.Next != 3 {
		t.Fatalf("nextPage = %+v, want a known next page of 3", got)
	}
}

func TestNextPageEmptyXNextPageMeansExhausted(t *testing.T) {
	h := http.Header{}
	h.Set("X-Next-Page", "")
	got := nextPage(h)
	if !got.Known || got.Next != 0 {
		t.Fatalf("nextPage = %+v, want a known exhausted marker", got)
	}
}

func TestNextPageFromLinkHeader(t *testing.T) {
	h := http.Header{}
	h.Set("Link", `<https://api.github.com/search/issues?q=x&page=2>; rel="next", <https://api.github.com/search/issues?q=x&page=9>; rel="last"`)
	got := nextPage(h)
	if !got.Known || got.Next != 2 {
		t.Fatalf("nextPage = %+v, want a known next page of 2", got)
	}
}

func TestNextPageLinkWithoutNextMeansExhausted(t *testing.T) {
	h := http.Header{}
	h.Set("Link", `<https://api.github.com/search/issues?q=x&page=1>; rel="prev"`)
	got := nextPage(h)
	if !got.Known || got.Next != 0 {
		t.Fatalf("nextPage = %+v, want a known exhausted marker", got)
	}
}

func TestNextPageUnknownWithoutHeaders(t *testing.T) {
	if got := nextPage(http.Header{}); got.Known {
		t.Fatalf("nextPage = %+v, want unknown so the caller falls back to short-page detection", got)
	}
}

func TestPaginateFollowsHeaders(t *testing.T) {
	tr := &fakeTransport{pages: []apiResponse{
		{Body: []byte("p1"), Page: pageInfo{Known: true, Next: 2}},
		{Body: []byte("p2"), Page: pageInfo{Known: true, Next: 3}},
		{Body: []byte("p3"), Page: pageInfo{Known: true, Next: 0}},
	}}

	var seen []string
	requests, hitCap, err := paginate(context.Background(), tr, "path", url.Values{}, 2, 100,
		func(body []byte) (int, error) {
			seen = append(seen, string(body))
			// Deliberately a full page every time: only the headers should
			// decide when to stop.
			return 2, nil
		})
	if err != nil {
		t.Fatalf("paginate failed: %v", err)
	}
	if hitCap {
		t.Fatal("hitCap should be false")
	}
	if requests != 3 || len(seen) != 3 {
		t.Fatalf("requests=%d pages=%v, want 3 of each", requests, seen)
	}
	if got := tr.queries[1].Get("page"); got != "2" {
		t.Fatalf("second request asked for page %q, want 2", got)
	}
}

func TestPaginateStopsOnShortPageWithoutHeaders(t *testing.T) {
	tr := &fakeTransport{pages: []apiResponse{
		{Body: []byte("p1")},
		{Body: []byte("p2")},
	}}

	counts := []int{100, 40}
	call := 0
	requests, _, err := paginate(context.Background(), tr, "path", url.Values{}, 100, 100,
		func([]byte) (int, error) {
			n := counts[call]
			call++
			return n, nil
		})
	if err != nil {
		t.Fatalf("paginate failed: %v", err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2 (stop on the first short page)", requests)
	}
}

func TestPaginateHonoursMaxPages(t *testing.T) {
	tr := &fakeTransport{}
	requests, hitCap, err := paginate(context.Background(), tr, "path", url.Values{}, 10, 3,
		func([]byte) (int, error) { return 10, nil })
	if err != nil {
		t.Fatalf("paginate failed: %v", err)
	}
	if requests != 3 || !hitCap {
		t.Fatalf("requests=%d hitCap=%v, want 3 and true", requests, hitCap)
	}
}

func TestPaginateStopsIfProviderRepeatsAPage(t *testing.T) {
	// A provider that keeps pointing at the current page would otherwise spin
	// forever.
	tr := &fakeTransport{pages: []apiResponse{
		{Body: []byte("p1"), Page: pageInfo{Known: true, Next: 1}},
	}}
	requests, _, err := paginate(context.Background(), tr, "path", url.Values{}, 10, 100,
		func([]byte) (int, error) { return 10, nil })
	if err != nil {
		t.Fatalf("paginate failed: %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestPaginatePropagatesTransportError(t *testing.T) {
	tr := &fakeTransport{err: errors.New("boom")}
	if _, _, err := paginate(context.Background(), tr, "path", url.Values{}, 10, 10,
		func([]byte) (int, error) { return 0, nil }); err == nil {
		t.Fatal("expected the transport error to propagate")
	}
}

func TestIsRateLimitedDistinguishes403Cases(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		headers map[string]string
		body    string
		want    bool
	}{
		{"429 is always throttling", 429, nil, "", true},
		{"403 with exhausted quota", 403, map[string]string{"X-RateLimit-Remaining": "0"}, "", true},
		{"403 with gitlab style quota", 403, map[string]string{"RateLimit-Remaining": "0"}, "", true},
		{"403 with retry-after", 403, map[string]string{"Retry-After": "60"}, "", true},
		{"403 mentioning rate limit", 403, nil, `{"message":"API rate limit exceeded"}`, true},
		{"403 secondary rate limit", 403, nil, `{"message":"You have exceeded a secondary rate limit"}`, true},
		{"403 genuine permission failure", 403, map[string]string{"X-RateLimit-Remaining": "4998"}, `{"message":"Must have admin rights"}`, false},
		{"403 bare permission failure", 403, nil, `{"message":"Forbidden"}`, false},
		{"401 is never throttling", 401, nil, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{StatusCode: tc.status, Header: http.Header{}}
			for k, v := range tc.headers {
				resp.Header.Set(k, v)
			}
			if got := isRateLimited(resp, tc.body); got != tc.want {
				t.Fatalf("isRateLimited = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAPIErrorMessagesAreActionable(t *testing.T) {
	cases := []struct {
		name string
		err  *APIError
		want []string
	}{
		{
			"unauthorized names the fix",
			&APIError{Provider: ProviderGitLab, Path: "groups/x/merge_requests", Status: 401},
			[]string{"GITLAB_TOKEN", "glab"},
		},
		{
			"unauthorized via cli names the cli fix",
			&APIError{Provider: ProviderGitHub, Path: "search/issues", Status: 401, ViaCLI: true},
			[]string{"gh auth login"},
		},
		{
			"rate limit is not phrased as an auth failure",
			&APIError{Provider: ProviderGitHub, Path: "search/issues", Status: 403, RateLimited: true, RetryAfter: "60"},
			[]string{"rate limit", "retry after 60"},
		},
		{
			"forbidden points at scope, not credentials",
			&APIError{Provider: ProviderGitLab, Path: "groups/x/merge_requests", Status: 403, Message: "Forbidden"},
			[]string{"lack access"},
		},
		{
			"not found points at the path",
			&APIError{Provider: ProviderGitLab, Path: "groups/x/merge_requests", Status: 404},
			[]string{"group or org path"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := tc.err.Error()
			for _, want := range tc.want {
				if !strings.Contains(msg, want) {
					t.Errorf("error %q should mention %q", msg, want)
				}
			}
		})
	}
}

func TestRateLimitErrorIsNotConfusedWithAuthFailure(t *testing.T) {
	rate := (&APIError{Provider: ProviderGitHub, Path: "p", Status: 403, RateLimited: true}).Error()
	auth := (&APIError{Provider: ProviderGitHub, Path: "p", Status: 401}).Error()
	if strings.Contains(rate, "credential") || strings.Contains(rate, "token is expired") {
		t.Fatalf("rate limit message %q should not blame credentials", rate)
	}
	if strings.Contains(auth, "rate limit") {
		t.Fatalf("auth failure message %q should not blame throttling", auth)
	}
}

// A leaked token in a log line or an error is a real incident, so assert it.
func TestHTTPTransportNeverLeaksTheToken(t *testing.T) {
	const secret = "glpat-SUPERSECRETVALUE"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("PRIVATE-TOKEN"); got != secret {
			t.Errorf("PRIVATE-TOKEN = %q, want the configured token", got)
		}
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"message":"401 Unauthorized"}`)
	}))
	defer srv.Close()

	tr := &httpTransport{provider: ProviderGitLab, base: srv.URL, token: secret, client: srv.Client()}
	_, err := tr.Get(context.Background(), "groups/x/merge_requests", url.Values{"scope": {"all"}})
	if err == nil {
		t.Fatal("expected a 401 error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error message leaked the token: %q", err)
	}
}

func TestHTTPTransportSetsGitHubAuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q, want Bearer tok", got)
		}
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	tr := &httpTransport{provider: ProviderGitHub, base: srv.URL, token: "tok", client: srv.Client()}
	if _, err := tr.Get(context.Background(), "search/issues", nil); err != nil {
		t.Fatalf("Get failed: %v", err)
	}
}

func TestCLITransportPassesPathAndQuery(t *testing.T) {
	runner := &fakeRunner{outputs: [][]byte{[]byte(`[]`)}}
	tr := &cliTransport{provider: ProviderGitLab, bin: "glab", runner: runner}

	if _, err := tr.Get(context.Background(), "/groups/a%2Fb/merge_requests", url.Values{"state": {"all"}}); err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(runner.calls))
	}
	call := runner.calls[0]
	if call[0] != "glab" || call[1] != "api" {
		t.Fatalf("call = %v, want glab api ...", call)
	}
	if call[2] != "groups/a%2Fb/merge_requests?state=all" {
		t.Fatalf("api path = %q, want the leading slash stripped and the query appended", call[2])
	}
}

func TestCLITransportClassifiesErrors(t *testing.T) {
	cases := []struct {
		name   string
		stderr string
		status int
	}{
		{"unauthorized", "glab failed: HTTP 401: Unauthorized", http.StatusUnauthorized},
		{"forbidden", "glab failed: HTTP 403: Forbidden", http.StatusForbidden},
		{"not found", "glab failed: 404 Not Found", http.StatusNotFound},
		{"rate limited", "gh failed: API rate limit exceeded", http.StatusTooManyRequests},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{err: errors.New(tc.stderr)}
			tr := &cliTransport{provider: ProviderGitLab, bin: "glab", runner: runner}
			_, err := tr.Get(context.Background(), "groups/x/merge_requests", nil)

			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error %v is not an *APIError", err)
			}
			if apiErr.Status != tc.status {
				t.Fatalf("status = %d, want %d", apiErr.Status, tc.status)
			}
			if !apiErr.ViaCLI {
				t.Fatal("ViaCLI should be set so the message names the CLI fix")
			}
		})
	}
}

func TestCLITransportPassesThroughUnclassifiedErrors(t *testing.T) {
	runner := &fakeRunner{err: errors.New("glab failed: connection refused")}
	tr := &cliTransport{provider: ProviderGitLab, bin: "glab", runner: runner}
	_, err := tr.Get(context.Background(), "p", nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Fatalf("a transport level failure should not be dressed up as an API status: %v", err)
	}
}
