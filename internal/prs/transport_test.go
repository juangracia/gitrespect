package prs

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
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

// hostRoutingClient dials named hostnames onto local test servers, so a
// redirect genuinely crosses a hostname boundary. Two httptest servers both on
// 127.0.0.1 look like the same host and would prove nothing.
func hostRoutingClient(routes map[string]string) *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, _, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				target, ok := routes[host]
				if !ok {
					return nil, fmt.Errorf("test dialed unexpected host %q", host)
				}
				var d net.Dialer
				return d.DialContext(ctx, network, target)
			},
		},
	}
}

func hostPort(serverURL string) string {
	return strings.TrimPrefix(serverURL, "http://")
}

// Go strips Authorization across a cross-host redirect but knows nothing about
// GitLab's PRIVATE-TOKEN, so without an explicit policy a redirecting API host
// hands the token to whatever it points at. That is credential exfiltration,
// not a cosmetic issue.
func TestHTTPTransportDropsCredentialsOnCrossHostRedirect(t *testing.T) {
	for _, tc := range []struct {
		provider string
		token    string
		header   string
	}{
		{ProviderGitLab, "glpat-SUPERSECRET", "PRIVATE-TOKEN"},
		{ProviderGitHub, "ghp_SUPERSECRET", "Authorization"},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			var received http.Header
			attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				received = r.Header.Clone()
				fmt.Fprint(w, `{"total_count":0,"items":[]}`)
			}))
			defer attacker.Close()

			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "http://evil.example/stolen", http.StatusFound)
			}))
			defer origin.Close()

			client := hostRoutingClient(map[string]string{
				"origin.example": hostPort(origin.URL),
				"evil.example":   hostPort(attacker.URL),
			})
			tr := &httpTransport{provider: tc.provider, base: "http://origin.example/api", token: tc.token, client: client}
			if _, err := tr.Get(context.Background(), "groups/x/merge_requests", url.Values{}); err != nil {
				t.Fatalf("Get failed: %v", err)
			}

			if got := received.Get(tc.header); got != "" {
				t.Fatalf("%s leaked to another host across a redirect: %q", tc.header, got)
			}
			for _, h := range credentialHeaders {
				if v := received.Get(h); strings.Contains(v, tc.token) {
					t.Fatalf("token leaked in header %s: %q", h, v)
				}
			}
		})
	}
}

// The guard must not break ordinary redirects, or a self-hosted instance that
// bounces http to https stops working.
func TestHTTPTransportKeepsCredentialsOnSameHostRedirect(t *testing.T) {
	var received http.Header
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/final") {
			received = r.Header.Clone()
			fmt.Fprint(w, "[]")
			return
		}
		http.Redirect(w, r, srv.URL+"/final", http.StatusFound)
	}))
	defer srv.Close()

	tr := &httpTransport{provider: ProviderGitLab, base: srv.URL, token: "tok", client: httpClient(nil)}
	if _, err := tr.Get(context.Background(), "x", url.Values{}); err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got := received.Get("PRIVATE-TOKEN"); got != "tok" {
		t.Fatalf("PRIVATE-TOKEN = %q on a same-host redirect, want it preserved", got)
	}
}

func TestSameHost(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"gitlab.com", "gitlab.com", true},
		{"gitlab.com:443", "gitlab.com", true},
		{"GitLab.com", "gitlab.com", true},
		{"gitlab.com", "evil.com", false},
		{"gitlab.com", "gitlab.com.evil.com", false},
		{"gitlab.com", "sub.gitlab.com", false},
	}
	for _, tc := range cases {
		if got := sameHost(tc.a, tc.b); got != tc.want {
			t.Errorf("sameHost(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// A hostile or merely careless server can echo the credential back in its
// error body, and that body goes straight into our error message.
func TestAPIErrorRedactsATokenEchoedByTheServer(t *testing.T) {
	const secret = "glpat-DO-NOT-LEAK-ME"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprintf(w, `{"message":"bad token %s"}`, r.Header.Get("PRIVATE-TOKEN"))
	}))
	defer srv.Close()

	tr := &httpTransport{provider: ProviderGitLab, base: srv.URL, token: secret, client: srv.Client()}
	_, err := tr.Get(context.Background(), "groups/x/merge_requests", url.Values{})
	if err == nil {
		t.Fatal("expected a 403 error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("a token echoed by the server reached the error text: %v", err)
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("error %q should show the redaction rather than dropping the detail silently", err)
	}
}

func TestRedactSecret(t *testing.T) {
	if got := redactSecret("token abc here", "abc"); got != "token [redacted] here" {
		t.Errorf("redactSecret = %q, want the secret replaced", got)
	}
	// An empty secret must not turn every empty string match into a redaction.
	if got := redactSecret("nothing to hide", ""); got != "nothing to hide" {
		t.Errorf("redactSecret with no secret = %q, want the text unchanged", got)
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
