package prs

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestCheckAPIURLAcceptsHTTPS(t *testing.T) {
	plaintext, err := checkAPIURL("https://gitlab.corp.com")
	if err != nil {
		t.Fatalf("checkAPIURL failed: %v", err)
	}
	if plaintext {
		t.Fatal("https must not be reported as plaintext")
	}
}

func TestCheckAPIURLEmptyIsFine(t *testing.T) {
	plaintext, err := checkAPIURL("")
	if err != nil || plaintext {
		t.Fatalf("checkAPIURL(\"\") = (%v, %v), want (false, nil): the defaults are https", plaintext, err)
	}
}

func TestCheckAPIURLFlagsPlaintextHTTP(t *testing.T) {
	plaintext, err := checkAPIURL("http://gitlab.corp.com")
	if err != nil {
		t.Fatalf("http should be allowed, got: %v", err)
	}
	if !plaintext {
		t.Fatal("http must be reported as plaintext so the user can be warned")
	}
}

// Each of these used to fail somewhere downstream with a message that did not
// name the real problem.
func TestCheckAPIURLRejectsUnusableValues(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"schemeless host", "gitlab.corp.com", "missing scheme"},
		{"file scheme", "file:///etc/passwd", "not supported"},
		{"ftp scheme", "ftp://gitlab.corp.com", "not supported"},
		{"no host", "https://", "no host"},
		{"embedded credentials", "https://evil.com@gitlab.corp.com", "remove the credentials"},
		{"dot segments", "https://gitlab.corp.com/api/v4/../../..", "not allowed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := checkAPIURL(tc.in)
			if err == nil {
				t.Fatalf("checkAPIURL(%q) was accepted", tc.in)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q should mention %q", err, tc.want)
			}
		})
	}
}

// The error names the URL, so it must not echo whatever was sitting in the
// userinfo.
func TestCheckAPIURLDoesNotEchoEmbeddedPassword(t *testing.T) {
	const secret = "hunter2-DO-NOT-ECHO"
	_, err := checkAPIURL("https://user:" + secret + "@gitlab.corp.com")
	if err == nil {
		t.Fatal("expected embedded credentials to be rejected")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("the error echoed the embedded password: %v", err)
	}
}

func TestCheckAPIURLCaseInsensitiveScheme(t *testing.T) {
	if _, err := checkAPIURL("HTTPS://gitlab.corp.com"); err != nil {
		t.Fatalf("an uppercase scheme should be accepted: %v", err)
	}
}

// A bare ".." escapes correctly as a segment but still produces
// /groups/../merge_requests, which a server normalizes into a different valid
// endpoint. A confident answer about the wrong scope is worse than an error.
func TestCheckScopeRejectsDotSegments(t *testing.T) {
	cases := []struct {
		provider string
		scope    string
		wantFlag string
	}{
		{ProviderGitLab, "..", "--group"},
		{ProviderGitLab, "../../admin", "--group"},
		{ProviderGitLab, "group/../other", "--group"},
		{ProviderGitLab, ".", "--group"},
		{ProviderGitHub, "..", "--org"},
	}
	for _, tc := range cases {
		err := checkScope(tc.provider, tc.scope)
		if err == nil {
			t.Errorf("checkScope(%q, %q) was accepted", tc.provider, tc.scope)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantFlag) {
			t.Errorf("error %q should name %s", err, tc.wantFlag)
		}
	}
}

func TestCheckScopeAcceptsOrdinaryPaths(t *testing.T) {
	for _, scope := range []string{"bunn-digital/web", "group/sub/project", "my-org", "..hidden", "a..b"} {
		if err := checkScope(ProviderGitLab, scope); err != nil {
			t.Errorf("checkScope rejected the legitimate scope %q: %v", scope, err)
		}
	}
}

func TestOptionsValidateRejectsBadScopeAndAPIURL(t *testing.T) {
	base := Options{
		Provider: ProviderGitLab,
		Scope:    "g",
		Since:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.Local),
		Until:    time.Date(2025, 2, 1, 0, 0, 0, 0, time.Local),
	}

	traversal := base
	traversal.Scope = ".."
	if err := traversal.validate(); err == nil {
		t.Error("validate accepted a traversal scope")
	}

	badURL := base
	badURL.BaseURL = "gitlab.corp.com"
	if err := badURL.validate(); err == nil {
		t.Error("validate accepted a schemeless --api-url")
	}
}

// The warning has to reach stderr before the token goes anywhere, and it has to
// name both ways out or it is just a nag.
func TestPlaintextAPIURLWarnsOnce(t *testing.T) {
	var stderr bytes.Buffer
	srv, _ := gitlabServer(t, [][]string{{}})

	since, until := fullYear(t)
	_, err := Fetch(t.Context(), Options{
		Provider:   ProviderGitLab,
		Scope:      "g",
		Since:      since,
		Until:      until,
		Token:      "glpat-secret",
		BaseURL:    srv.URL, // httptest serves plain http
		HTTPClient: srv.Client(),
		Stderr:     &stderr,
	})
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	out := stderr.String()
	if !strings.Contains(out, "plaintext") {
		t.Fatalf("stderr = %q, want a plaintext warning", out)
	}
	for _, want := range []string{"https", "GITLAB_TOKEN", "glab"} {
		if !strings.Contains(out, want) {
			t.Errorf("warning %q should mention %q so it is actionable", out, want)
		}
	}
	if n := strings.Count(out, "warning:"); n != 1 {
		t.Fatalf("emitted %d warnings, want exactly one per run", n)
	}
	if strings.Contains(out, "glpat-secret") {
		t.Fatalf("the warning leaked the token: %q", out)
	}
}

func TestHTTPSAPIURLDoesNotWarn(t *testing.T) {
	var stderr bytes.Buffer
	opts := Options{
		Provider: ProviderGitLab,
		Scope:    "g",
		Token:    "t",
		BaseURL:  "https://gitlab.corp.com",
		Stderr:   &stderr,
	}
	if _, err := newTransport(opts); err != nil {
		t.Fatalf("newTransport failed: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("https should be silent, got: %q", stderr.String())
	}
}

// Without a token nothing crosses the network from us, so there is nothing to
// warn about even on plain http.
func TestPlaintextWithoutTokenDoesNotWarn(t *testing.T) {
	var stderr bytes.Buffer
	opts := Options{
		Provider: ProviderGitLab,
		Scope:    "g",
		BaseURL:  "http://gitlab.corp.com",
		Getenv:   envFrom(nil),
		LookPath: foundBinary,
		Runner:   &fakeRunner{},
		Stderr:   &stderr,
	}
	if _, err := newTransport(opts); err != nil {
		t.Fatalf("newTransport failed: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("the CLI path has no token of ours to leak, got: %q", stderr.String())
	}
}
