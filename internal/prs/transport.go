package prs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// maxBodyBytes caps how much of a response is read. A runaway or hostile
// endpoint should not be able to exhaust memory.
const maxBodyBytes = 32 << 20

// Doer is the subset of *http.Client this package uses. Tests point it at an
// httptest.Server or a stub, so no test needs network access.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// CommandRunner runs a local CLI (glab or gh) and returns its stdout. Tests
// inject a fake, so no test needs glab or gh installed.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// execRunner is the production CommandRunner.
type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s failed: %s", name, msg)
	}
	return out, nil
}

// pageInfo is what a transport learned about pagination from response headers.
// Known is false for the CLI transport, which only sees a response body, so
// the pagination loop falls back to stopping on the first short page.
type pageInfo struct {
	Known bool
	Next  int
}

// apiResponse is one API call's result, however it was made.
type apiResponse struct {
	Body []byte
	Page pageInfo
}

// transport performs one authenticated GET against a provider's REST API.
// path is relative to the API root, for example "groups/x/merge_requests".
type transport interface {
	Get(ctx context.Context, path string, query url.Values) (apiResponse, error)
}

// APIError carries enough detail to tell an expired token apart from a missing
// permission apart from a rate limit, because the fix differs in each case.
type APIError struct {
	Provider    string
	Path        string
	Status      int
	Message     string
	RateLimited bool
	RetryAfter  string
	// ViaCLI records that the call went through glab/gh rather than a token,
	// so the remedy named in the message is the right one.
	ViaCLI bool
}

func (e *APIError) Error() string {
	switch {
	case e.RateLimited:
		msg := fmt.Sprintf("%s rate limit hit on %s", e.Provider, e.Path)
		if e.RetryAfter != "" {
			msg += fmt.Sprintf(" (retry after %s)", e.RetryAfter)
		}
		return msg + ": wait and retry, or narrow the window with --since/--until"
	case e.Status == http.StatusUnauthorized:
		return fmt.Sprintf("%s rejected the credentials on %s: %s", e.Provider, e.Path, e.credentialHint())
	case e.Status == http.StatusForbidden:
		return fmt.Sprintf("%s returned 403 on %s: the credentials are valid but lack access to that scope%s",
			e.Provider, e.Path, detailSuffix(e.Message))
	case e.Status == http.StatusNotFound:
		return fmt.Sprintf("%s returned 404 on %s: check the group or org path, and that the credentials can see it%s",
			e.Provider, e.Path, detailSuffix(e.Message))
	default:
		return fmt.Sprintf("%s returned HTTP %d on %s%s", e.Provider, e.Status, e.Path, detailSuffix(e.Message))
	}
}

func (e *APIError) credentialHint() string {
	if e.ViaCLI {
		return fmt.Sprintf("re-authenticate the local CLI (%s auth login) or pass --token", cliBinary(e.Provider))
	}
	return fmt.Sprintf("the token is expired or lacks the read scope; refresh %s, or unset it to use the locally authenticated %s CLI",
		tokenEnvName(e.Provider), cliBinary(e.Provider))
}

func detailSuffix(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}
	const limit = 200
	if len(msg) > limit {
		msg = msg[:limit] + "..."
	}
	return ": " + msg
}

func tokenEnvName(provider string) string {
	if provider == ProviderGitHub {
		return "GITHUB_TOKEN"
	}
	return "GITLAB_TOKEN"
}

func cliBinary(provider string) string {
	if provider == ProviderGitHub {
		return "gh"
	}
	return "glab"
}

// credentialHeaders are the headers that must never survive a redirect to a
// different host.
var credentialHeaders = []string{"PRIVATE-TOKEN", "Authorization"}

// maxRedirects matches net/http's own default.
const maxRedirects = 10

// httpTransport talks to the REST API directly with a token.
type httpTransport struct {
	provider string
	base     string
	token    string
	client   Doer

	guard   sync.Once
	guarded Doer
}

// safeClient returns the client with a redirect policy that will not hand our
// credentials to another host.
//
// Go strips Authorization on a cross-domain redirect but knows nothing about
// GitLab's PRIVATE-TOKEN, so without this an API host that redirects (a
// hijacked --api-url, a plain-http endpoint bounced by a machine in the
// middle, a compromised instance) would hand the token straight to wherever it
// pointed. The credential is this transport's, so the rule about where it may
// travel belongs here rather than in whatever client was injected.
func (t *httpTransport) safeClient() Doer {
	t.guard.Do(func() {
		t.guarded = withSafeRedirects(t.client)
	})
	return t.guarded
}

func withSafeRedirects(d Doer) Doer {
	hc, ok := d.(*http.Client)
	if !ok {
		// A caller that injected something other than *http.Client controls
		// its own redirect handling; there is nothing to clone.
		return d
	}
	clone := *hc
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		if !sameHost(req.URL.Host, via[0].URL.Host) {
			for _, h := range credentialHeaders {
				req.Header.Del(h)
			}
		}
		return nil
	}
	return &clone
}

// sameHost compares hostnames, ignoring port and case. A redirect that only
// changes the port is still the same server; anything else is not.
func sameHost(a, b string) bool {
	return strings.EqualFold(hostOnly(a), hostOnly(b))
}

func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}

func (t *httpTransport) Get(ctx context.Context, path string, query url.Values) (apiResponse, error) {
	full := strings.TrimSuffix(t.base, "/") + "/" + strings.TrimPrefix(path, "/")
	if encoded := query.Encode(); encoded != "" {
		full += "?" + encoded
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
	if err != nil {
		return apiResponse{}, fmt.Errorf("building request for %s: %w", path, err)
	}
	req.Header.Set("Accept", "application/json")
	// The token only ever exists in a header. It is kept out of error text by
	// redactSecret, and off other hosts by safeClient's redirect policy.
	if t.provider == ProviderGitHub {
		req.Header.Set("Authorization", "Bearer "+t.token)
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	} else {
		req.Header.Set("PRIVATE-TOKEN", t.token)
	}

	resp, err := t.safeClient().Do(req)
	if err != nil {
		return apiResponse{}, fmt.Errorf("calling %s %s: %w", t.provider, path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return apiResponse{}, fmt.Errorf("reading %s response for %s: %w", t.provider, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return apiResponse{}, newAPIError(t.provider, path, resp, body, false, t.token)
	}
	return apiResponse{Body: body, Page: nextPage(resp.Header)}, nil
}

// cliTransport shells out to an already authenticated glab or gh. This is the
// zero setup path: no token to mint, store or leak.
type cliTransport struct {
	provider string
	bin      string
	runner   CommandRunner
}

func (t *cliTransport) Get(ctx context.Context, path string, query url.Values) (apiResponse, error) {
	arg := strings.TrimPrefix(path, "/")
	if encoded := query.Encode(); encoded != "" {
		arg += "?" + encoded
	}
	out, err := t.runner.Run(ctx, t.bin, "api", arg)
	if err != nil {
		if apiErr := cliAPIError(t.provider, path, err); apiErr != nil {
			return apiResponse{}, apiErr
		}
		return apiResponse{}, err
	}
	// Leaving Page zeroed is deliberate, not an oversight: `glab api` and
	// `gh api` print the response body and nothing else, so X-Next-Page and
	// Link are simply not available on this path and paginate falls back to
	// stopping on the first short page. Recovering the headers would mean
	// parsing the raw HTTP dump from `-i`, which is fragile and cannot be
	// tested without the real binaries installed. Do not "fix" this by
	// pretending the headers exist.
	return apiResponse{Body: out}, nil
}

// cliAPIError maps the CLI's error text back onto the status codes the HTTP
// path reports, so both auth paths produce the same actionable messages.
func cliAPIError(provider, path string, err error) *APIError {
	msg := err.Error()
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "429") || strings.Contains(lower, "rate limit"):
		return &APIError{Provider: provider, Path: path, Status: http.StatusTooManyRequests, Message: msg, RateLimited: true, ViaCLI: true}
	case strings.Contains(lower, "401") || strings.Contains(lower, "unauthorized") || strings.Contains(lower, "authentication required"):
		return &APIError{Provider: provider, Path: path, Status: http.StatusUnauthorized, Message: msg, ViaCLI: true}
	case strings.Contains(lower, "403") || strings.Contains(lower, "forbidden"):
		return &APIError{Provider: provider, Path: path, Status: http.StatusForbidden, Message: msg, ViaCLI: true}
	case strings.Contains(lower, "404") || strings.Contains(lower, "not found"):
		return &APIError{Provider: provider, Path: path, Status: http.StatusNotFound, Message: msg, ViaCLI: true}
	}
	return nil
}

// newAPIError classifies a failed response. Separating a rate limit from a
// genuine auth failure matters: one means wait, the other means fix your
// credentials, and both arrive as a 403 on GitHub.
func newAPIError(provider, path string, resp *http.Response, body []byte, viaCLI bool, secret string) *APIError {
	e := &APIError{
		Provider:   provider,
		Path:       path,
		Status:     resp.StatusCode,
		Message:    redactSecret(apiMessage(body), secret),
		RetryAfter: resp.Header.Get("Retry-After"),
		ViaCLI:     viaCLI,
	}
	e.RateLimited = isRateLimited(resp, e.Message)
	return e
}

// isRateLimited reports whether a rejection is throttling rather than a
// credential problem. 429 is unambiguous; a 403 needs the headers or the body
// to say so, which is how GitHub signals an exhausted quota.
func isRateLimited(resp *http.Response, message string) bool {
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if resp.StatusCode != http.StatusForbidden {
		return false
	}
	for _, h := range []string{"X-RateLimit-Remaining", "RateLimit-Remaining"} {
		if v := resp.Header.Get(h); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n <= 0 {
				return true
			}
		}
	}
	if resp.Header.Get("Retry-After") != "" {
		return true
	}
	lower := strings.ToLower(message)
	return strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "secondary rate") ||
		strings.Contains(lower, "abuse detection")
}

// redactSecret strips a credential out of text that is about to reach a user,
// a log, or a support ticket.
//
// A hostile server can echo the token straight back in its error body, and a
// merely careless one can include it in a diagnostic. Either way that body
// ends up inside our error message, so the redaction has to happen where the
// message is built rather than being left to whoever prints it.
func redactSecret(s, secret string) string {
	if secret == "" {
		return s
	}
	return strings.ReplaceAll(s, secret, "[redacted]")
}

// apiMessage pulls the human readable part out of an error body without
// pretending to know the exact schema of every provider.
func apiMessage(body []byte) string {
	s := strings.TrimSpace(string(body))
	const limit = 400
	if len(s) > limit {
		s = s[:limit]
	}
	return s
}

// nextPage reads pagination out of response headers. GitLab sends
// X-Next-Page; GitHub sends only a Link header, so both are handled.
func nextPage(h http.Header) pageInfo {
	if v := strings.TrimSpace(h.Get("X-Next-Page")); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			return pageInfo{Known: true, Next: n}
		}
	}
	// An empty X-Next-Page is GitLab explicitly saying "that was the last
	// page", which is information, not an absence of it.
	if _, ok := h["X-Next-Page"]; ok {
		return pageInfo{Known: true, Next: 0}
	}
	if link := h.Get("Link"); link != "" {
		if n, ok := linkNextPage(link); ok {
			return pageInfo{Known: true, Next: n}
		}
		// A Link header without rel="next" means this was the last page.
		return pageInfo{Known: true, Next: 0}
	}
	return pageInfo{}
}

// linkNextPage extracts the page number from the rel="next" entry of an
// RFC 5988 Link header.
func linkNextPage(header string) (int, bool) {
	for _, part := range strings.Split(header, ",") {
		segments := strings.Split(part, ";")
		if len(segments) < 2 {
			continue
		}
		isNext := false
		for _, seg := range segments[1:] {
			if strings.Contains(strings.ToLower(seg), `rel="next"`) {
				isNext = true
				break
			}
		}
		if !isNext {
			continue
		}
		raw := strings.Trim(strings.TrimSpace(segments[0]), "<>")
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		n, err := strconv.Atoi(u.Query().Get("page"))
		if err != nil || n <= 0 {
			continue
		}
		return n, true
	}
	return 0, false
}

// paginate walks a paginated list endpoint. It trusts the transport's page
// headers when it has them, and otherwise stops on the first short page, which
// is the only signal available through glab/gh. onPage reports how many items
// that page held. hitCap is true when maxPages was reached with more data
// still available, which callers must surface rather than silently drop.
func paginate(
	ctx context.Context,
	tr transport,
	path string,
	base url.Values,
	perPage, maxPages int,
	onPage func(body []byte) (int, error),
) (requests int, hitCap bool, err error) {
	page := 1
	for {
		if maxPages > 0 && page > maxPages {
			return requests, true, nil
		}
		q := url.Values{}
		for k, vs := range base {
			q[k] = append([]string(nil), vs...)
		}
		q.Set("per_page", strconv.Itoa(perPage))
		q.Set("page", strconv.Itoa(page))

		resp, err := tr.Get(ctx, path, q)
		requests++
		if err != nil {
			return requests, false, err
		}
		n, err := onPage(resp.Body)
		if err != nil {
			return requests, false, err
		}

		if resp.Page.Known {
			if resp.Page.Next <= 0 {
				return requests, false, nil
			}
			if resp.Page.Next <= page {
				// Defensive: a provider that keeps pointing at the same page
				// would otherwise loop forever.
				return requests, false, nil
			}
			page = resp.Page.Next
			continue
		}
		if n < perPage {
			return requests, false, nil
		}
		page++
	}
}

// httpClient returns the client to use, defaulting to one with a timeout so a
// hung endpoint cannot wedge the CLI forever.
func httpClient(d Doer) Doer {
	if d != nil {
		return d
	}
	return &http.Client{Timeout: 60 * time.Second}
}
