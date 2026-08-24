package prs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func envFrom(pairs map[string]string) func(string) string {
	return func(key string) string { return pairs[key] }
}

// foundBinary stands in for exec.LookPath so no test depends on glab or gh
// actually being installed.
func foundBinary(string) (string, error) { return "/usr/local/bin/stub", nil }

func missingBinary(name string) (string, error) {
	return "", errors.New("exec: \"" + name + "\": executable file not found in $PATH")
}

func TestNewTransportPrefersAnExplicitToken(t *testing.T) {
	tr, err := newTransport(Options{
		Provider: ProviderGitLab,
		Token:    "explicit",
		Getenv:   envFrom(map[string]string{"GITLAB_TOKEN": "from-env"}),
		LookPath: foundBinary,
	})
	if err != nil {
		t.Fatalf("newTransport failed: %v", err)
	}
	http, ok := tr.(*httpTransport)
	if !ok {
		t.Fatalf("transport = %T, want *httpTransport", tr)
	}
	if http.token != "explicit" {
		t.Fatalf("token = %q, want the --token value to win over the environment", http.token)
	}
}

func TestNewTransportFallsBackToTheEnvironment(t *testing.T) {
	tr, err := newTransport(Options{
		Provider: ProviderGitLab,
		Getenv:   envFrom(map[string]string{"GITLAB_TOKEN": "from-env"}),
		LookPath: foundBinary,
	})
	if err != nil {
		t.Fatalf("newTransport failed: %v", err)
	}
	http, ok := tr.(*httpTransport)
	if !ok {
		t.Fatalf("transport = %T, want *httpTransport", tr)
	}
	if http.token != "from-env" {
		t.Fatalf("token = %q, want the environment value", http.token)
	}
}

func TestNewTransportAcceptsGHTokenForGitHub(t *testing.T) {
	tr, err := newTransport(Options{
		Provider: ProviderGitHub,
		Getenv:   envFrom(map[string]string{"GH_TOKEN": "gh-cli-token"}),
		LookPath: foundBinary,
	})
	if err != nil {
		t.Fatalf("newTransport failed: %v", err)
	}
	if got := tr.(*httpTransport).token; got != "gh-cli-token" {
		t.Fatalf("token = %q, want GH_TOKEN to be honoured", got)
	}
}

func TestNewTransportUsesTheLocalCLIWithoutAToken(t *testing.T) {
	tr, err := newTransport(Options{
		Provider: ProviderGitLab,
		Getenv:   envFrom(nil),
		LookPath: foundBinary,
		Runner:   &fakeRunner{},
	})
	if err != nil {
		t.Fatalf("newTransport failed: %v", err)
	}
	cli, ok := tr.(*cliTransport)
	if !ok {
		t.Fatalf("transport = %T, want *cliTransport", tr)
	}
	if cli.bin != "glab" {
		t.Fatalf("bin = %q, want glab", cli.bin)
	}
}

func TestNewTransportMissingCLINamesBothOptions(t *testing.T) {
	_, err := newTransport(Options{
		Provider: ProviderGitHub,
		Getenv:   envFrom(nil),
		LookPath: missingBinary,
	})
	if err == nil {
		t.Fatal("expected an error when there is neither a token nor a CLI")
	}
	for _, want := range []string{"GITHUB_TOKEN", "--token", "gh auth login"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

func TestNewTransportHonoursSelfHostedBaseURL(t *testing.T) {
	tr, err := newTransport(Options{
		Provider: ProviderGitLab,
		Token:    "t",
		BaseURL:  "https://gitlab.corp.com",
	})
	if err != nil {
		t.Fatalf("newTransport failed: %v", err)
	}
	if got := tr.(*httpTransport).base; got != "https://gitlab.corp.com/api/v4" {
		t.Fatalf("base = %q, want the API root appended", got)
	}
}

func TestFetchEndToEnd(t *testing.T) {
	srv, _ := gitlabServer(t, [][]string{{
		gitlabMRJSON(1, "alice", "2025-01-05T10:00:00.000Z", "2025-01-08T10:00:00.000Z"),
		gitlabMRJSON(2, "alice", "2025-02-05T10:00:00.000Z", ""),
		gitlabMRJSON(3, "bob", "2025-02-06T10:00:00.000Z", ""),
	}})

	since, until := fullYear(t)
	res, err := Fetch(context.Background(), Options{
		Provider:    ProviderGitLab,
		Scope:       "bunn-digital/web",
		Since:       since,
		Until:       until,
		Granularity: "monthly",
		Token:       "t",
		BaseURL:     srv.URL,
		HTTPClient:  srv.Client(),
	})
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if res.Opened != 3 || res.Merged != 1 {
		t.Fatalf("Opened=%d Merged=%d, want 3 and 1", res.Opened, res.Merged)
	}
	if len(res.Authors) != 2 || res.Authors[0].Identity != "alice" {
		t.Fatalf("authors = %+v, want alice first with 2", res.Authors)
	}
	if len(res.Periods) != 2 {
		t.Fatalf("got %d periods, want Jan and Feb", len(res.Periods))
	}
	if res.LeadTime == nil || res.LeadTime.Samples != 1 {
		t.Fatalf("LeadTime = %+v, want one sample", res.LeadTime)
	}
}

func TestFetchAppliesTeamFilter(t *testing.T) {
	srv, _ := gitlabServer(t, [][]string{{
		gitlabMRJSON(1, "alice", "2025-01-05T10:00:00.000Z", ""),
		gitlabMRJSON(2, "renovate", "2025-01-06T10:00:00.000Z", ""),
	}})

	since, until := fullYear(t)
	res, err := Fetch(context.Background(), Options{
		Provider:   ProviderGitLab,
		Scope:      "g",
		Authors:    []string{"alice@corp.com"},
		Since:      since,
		Until:      until,
		Token:      "t",
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if res.Opened != 1 {
		t.Fatalf("Opened = %d, want only the matched author", res.Opened)
	}
	if res.UnmatchedTotal != 1 || res.Unmatched[0].Handle != "renovate" {
		t.Fatalf("Unmatched = %+v, want renovate reported", res.Unmatched)
	}
}

// A bad --map must fail before any API call is spent.
func TestFetchValidatesIdentityFlagsBeforeCallingTheAPI(t *testing.T) {
	srv, seen := gitlabServer(t, [][]string{{}})

	since, until := fullYear(t)
	_, err := Fetch(context.Background(), Options{
		Provider:   ProviderGitLab,
		Scope:      "g",
		Mappings:   []string{"nonsense"},
		Since:      since,
		Until:      until,
		Token:      "t",
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	})
	if err == nil {
		t.Fatal("expected a validation error")
	}
	if len(*seen) != 0 {
		t.Fatalf("made %d API calls, want none before validation passes", len(*seen))
	}
}

func TestFetchComparison(t *testing.T) {
	srv, _ := gitlabServer(t, [][]string{{
		gitlabMRJSON(1, "alice", "2025-01-05T10:00:00.000Z", ""),
		gitlabMRJSON(2, "alice", "2025-07-05T10:00:00.000Z", ""),
		gitlabMRJSON(3, "alice", "2025-07-06T10:00:00.000Z", ""),
		gitlabMRJSON(4, "alice", "2025-07-07T10:00:00.000Z", ""),
	}})

	opts := Options{
		Provider:   ProviderGitLab,
		Scope:      "g",
		Token:      "t",
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	}
	before := Window{Label: "H1", Since: localTime(t, "2025-01-01 00:00"), Until: localTime(t, "2025-06-30 23:59")}
	after := Window{Label: "H2", Since: localTime(t, "2025-07-01 00:00"), Until: localTime(t, "2025-12-31 23:59")}

	c, err := FetchComparison(context.Background(), opts, before, after)
	if err != nil {
		t.Fatalf("FetchComparison failed: %v", err)
	}
	if c.Before.Opened != 1 || c.After.Opened != 3 {
		t.Fatalf("before=%d after=%d, want 1 and 3", c.Before.Opened, c.After.Opened)
	}
	if c.OpenedMultiplier != 3 {
		t.Fatalf("OpenedMultiplier = %v, want 3", c.OpenedMultiplier)
	}
	if c.BeforeLabel != "H1" || c.AfterLabel != "H2" {
		t.Fatalf("labels = %q/%q, want H1/H2", c.BeforeLabel, c.AfterLabel)
	}
}

func TestOptionsValidate(t *testing.T) {
	valid := Options{
		Provider: ProviderGitLab,
		Scope:    "g",
		Since:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.Local),
		Until:    time.Date(2025, 2, 1, 0, 0, 0, 0, time.Local),
	}
	if err := valid.validate(); err != nil {
		t.Fatalf("a valid Options was rejected: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*Options)
		want   string
	}{
		{"unknown provider", func(o *Options) { o.Provider = "bitbucket" }, "--provider"},
		{"missing group", func(o *Options) { o.Scope = "" }, "--group"},
		{"missing org", func(o *Options) { o.Provider = ProviderGitHub; o.Scope = "" }, "--org"},
		{"bad breakdown", func(o *Options) { o.Granularity = "hourly" }, "--breakdown"},
		{"inverted window", func(o *Options) { o.Until = o.Since.AddDate(0, -1, 0) }, "before end date"},
		{"missing dates", func(o *Options) { o.Since = time.Time{} }, "start and an end date"},
		{"negative top", func(o *Options) { o.Top = -1 }, "--top"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := valid
			tc.mutate(&opts)
			err := opts.validate()
			if err == nil {
				t.Fatalf("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q should mention %q", err, tc.want)
			}
		})
	}
}

func TestNewProviderPicksTheRightPlatform(t *testing.T) {
	gl, err := NewProvider(Options{Provider: ProviderGitLab, Scope: "g", Token: "t"})
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}
	if gl.Name() != ProviderGitLab || gl.Scope() != "g" {
		t.Fatalf("provider = %q/%q, want gitlab/g", gl.Name(), gl.Scope())
	}

	gh, err := NewProvider(Options{Provider: ProviderGitHub, Scope: "o", Token: "t"})
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}
	if gh.Name() != ProviderGitHub {
		t.Fatalf("provider = %q, want github", gh.Name())
	}
}
