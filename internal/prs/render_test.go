package prs

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func sampleResult(t *testing.T) Result {
	t.Helper()
	opts := window(t, "2025-01-01 00:00", "2025-04-01 00:00")
	opts.Granularity = "monthly"
	opts.Authors = []string{"alice@corp.com", "bob@corp.com"}

	f := Fetched{Items: []MergeRequest{
		merged(t, mr(t, "alice", "2025-01-05 10:00"), "2025-01-07 10:00"),
		merged(t, mr(t, "alice", "2025-02-05 10:00"), "2025-02-06 10:00"),
		mr(t, "alice", "2025-03-05 10:00"),
		merged(t, mr(t, "bob", "2025-02-10 10:00"), "2025-02-20 10:00"),
		mr(t, "renovate", "2025-02-11 10:00"),
	}}

	res, err := Aggregate(f, opts)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}
	return res
}

func TestWriteTerminalRendersTotalsAndContributors(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteTerminal(&buf, sampleResult(t)); err != nil {
		t.Fatalf("WriteTerminal failed: %v", err)
	}
	out := buf.String()

	for _, want := range []string{
		"gitrespect",
		"Merge Requests",
		"gitlab group/x",
		"Contributors",
		"alice@corp.com",
		"bob@corp.com",
		"Monthly Breakdown",
		"Jan 2025",
		"Mar 2025",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("terminal output should contain %q:\n%s", want, out)
		}
	}
}

func TestWriteTerminalSurfacesUnmatchedAccounts(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteTerminal(&buf, sampleResult(t)); err != nil {
		t.Fatalf("WriteTerminal failed: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "renovate") {
		t.Errorf("an unmatched account must be named so the gap is fixable:\n%s", out)
	}
	if !strings.Contains(out, "--map") {
		t.Errorf("the report should name --map as the fix:\n%s", out)
	}
}

// A roster folds accounts under canonical names, which is exactly the change
// that could quietly swallow an account nobody claimed. The warning has to
// survive it, or identity matching stops being auditable.
func TestWriteTerminalSurfacesUnmatchedAccountsUnderARoster(t *testing.T) {
	opts := window(t, "2025-01-01 00:00", "2025-02-01 00:00")
	opts.People = []Person{{Label: "Jane Doe", Keys: []string{"jane@corp.com", "j.doe@personal.com"}}}

	res, err := Aggregate(Fetched{Items: []MergeRequest{
		mrFrom(t, "jane", "jane@corp.com", "2025-01-05 10:00"),
		mrFrom(t, "jdoe-personal", "j.doe@personal.com", "2025-01-06 10:00"),
		mr(t, "renovate", "2025-01-07 10:00"),
	}}, opts)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}

	var buf bytes.Buffer
	if err := WriteTerminal(&buf, res); err != nil {
		t.Fatalf("WriteTerminal failed: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "Jane Doe") {
		t.Errorf("the canonical name should label the row:\n%s", out)
	}
	if !strings.Contains(out, "renovate") {
		t.Errorf("the unmatched account must still be named under a roster:\n%s", out)
	}
	if !strings.Contains(out, "--map") {
		t.Errorf("the report should still name --map as the fix:\n%s", out)
	}
}

func TestWriteTerminalReportsTruncation(t *testing.T) {
	res := sampleResult(t)
	res.Truncated = true
	res.Note = "github search returned more than it will page through"

	var buf bytes.Buffer
	if err := WriteTerminal(&buf, res); err != nil {
		t.Fatalf("WriteTerminal failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "incomplete results") || !strings.Contains(out, res.Note) {
		t.Errorf("truncation must be visible in the report:\n%s", out)
	}
}

func TestWriteTerminalSaysWhenTheContributorTableIsTrimmed(t *testing.T) {
	res := sampleResult(t)
	res.AuthorsTotal = len(res.Authors) + 7

	var buf bytes.Buffer
	if err := WriteTerminal(&buf, res); err != nil {
		t.Fatalf("WriteTerminal failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "7 more not listed") {
		t.Errorf("a trimmed table must say how many contributors are hidden:\n%s", out)
	}
}

func TestWriteTerminalEmptyResultReadsSensibly(t *testing.T) {
	res, err := Aggregate(Fetched{}, window(t, "2025-01-01 00:00", "2025-02-01 00:00"))
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}

	var buf bytes.Buffer
	if err := WriteTerminal(&buf, res); err != nil {
		t.Fatalf("WriteTerminal failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "No merge requests were created") {
		t.Errorf("an empty window should say so plainly:\n%s", out)
	}
}

func TestWriteComparisonTerminal(t *testing.T) {
	before := Result{
		Provider: ProviderGitLab, Scope: "g",
		Since: localTime(t, "2025-01-01 00:00"), Until: localTime(t, "2025-02-01 00:00"),
		Opened: 4, Authors: []AuthorStats{{Identity: "alice", Opened: 4}},
	}
	after := Result{
		Provider: ProviderGitLab, Scope: "g",
		Since: localTime(t, "2025-02-01 00:00"), Until: localTime(t, "2025-03-01 00:00"),
		Opened: 12, Authors: []AuthorStats{{Identity: "alice", Opened: 8}, {Identity: "carol", Opened: 4}},
	}
	c := CompareResults(before, after)
	c.BeforeLabel, c.AfterLabel = "2025-01", "2025-02"

	var buf bytes.Buffer
	if err := WriteComparisonTerminal(&buf, c); err != nil {
		t.Fatalf("WriteComparisonTerminal failed: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"2025-01", "2025-02", "alice", "carol"} {
		if !strings.Contains(out, want) {
			t.Errorf("comparison output should contain %q:\n%s", want, out)
		}
	}
	// January and February are different lengths, so the headline is the per
	// month rate. The raw volume multiplier has to be visible too, or the
	// reader cannot tell where the number came from.
	if !strings.Contains(out, "per month") {
		t.Errorf("the headline should say it is a rate:\n%s", out)
	}
	if !strings.Contains(out, "volume 4 → 12") || !strings.Contains(out, "+3.0x") {
		t.Errorf("the raw volume and its multiplier should both be shown:\n%s", out)
	}
	// carol has no before baseline; reporting 0.0x for her would read as a
	// collapse rather than as a new contributor.
	if !strings.Contains(out, "no baseline") {
		t.Errorf("a contributor with no before volume should be labelled, not given a ratio:\n%s", out)
	}
}

func TestRenderJSONWritesAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prs.json")
	if err := RenderJSON(sampleResult(t), path); err != nil {
		t.Fatalf("RenderJSON failed: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	var decoded Result
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if decoded.Opened != 4 || len(decoded.Authors) != 2 {
		t.Fatalf("decoded = %+v, want 4 opened across 2 authors", decoded)
	}
	if decoded.UnmatchedTotal != 1 {
		t.Fatalf("UnmatchedTotal = %d, want the unmatched account carried into JSON", decoded.UnmatchedTotal)
	}
}

// Nothing credential shaped may reach a file the user might share.
func TestRenderJSONCarriesNoCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prs.json")
	if err := RenderJSON(sampleResult(t), path); err != nil {
		t.Fatalf("RenderJSON failed: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	for _, forbidden := range []string{"token", "TOKEN", "PRIVATE-TOKEN", "Authorization"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("JSON output contains %q:\n%s", forbidden, raw)
		}
	}
}

func TestRenderHTMLWritesASelfContainedPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prs.html")
	if err := RenderHTML(sampleResult(t), path, "dark"); err != nil {
		t.Fatalf("RenderHTML failed: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	out := string(raw)
	for _, want := range []string{"<!DOCTYPE html>", "alice@corp.com", "Monthly Breakdown", "Contributors"} {
		if !strings.Contains(out, want) {
			t.Errorf("HTML should contain %q", want)
		}
	}
	// No external fetches: the report has to work from a file:// URL.
	for _, forbidden := range []string{"<script src=", "http://", "cdn."} {
		if strings.Contains(out, forbidden) {
			t.Errorf("HTML should be self-contained but references %q", forbidden)
		}
	}
}

func TestRenderHTMLLightTheme(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prs.html")
	if err := RenderHTML(sampleResult(t), path, "light"); err != nil {
		t.Fatalf("RenderHTML failed: %v", err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "#ffffff") {
		t.Error("the light theme should use the light palette")
	}
}

func TestRenderHTMLEscapesUntrustedText(t *testing.T) {
	// Titles and handles come from a remote API, so they are untrusted input.
	res := sampleResult(t)
	res.Authors[0].Identity = `<img src=x onerror=alert(1)>`

	path := filepath.Join(t.TempDir(), "prs.html")
	if err := RenderHTML(res, path, "dark"); err != nil {
		t.Fatalf("RenderHTML failed: %v", err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "<img src=x onerror") {
		t.Fatal("author names from the API must be escaped, not injected raw")
	}
}

// A GitLab or GitHub display name can hold any byte, unlike a git ident, so an
// MR author can otherwise erase lines of a report someone is reading or retitle
// their terminal. Proven end to end through the real render path.
func TestWriteTerminalStripsTerminalEscapesFromPlatformNames(t *testing.T) {
	const hostile = "Jane\x1b[2K\x1b]0;pwned\x07\rDoe\x00"

	opts := window(t, "2025-01-01 00:00", "2025-02-01 00:00")
	opts.Granularity = "monthly"

	f := Fetched{Items: []MergeRequest{
		{ID: "1", AuthorUser: hostile, AuthorName: hostile, CreatedAt: localTime(t, "2025-01-05 10:00")},
		{ID: "2", AuthorUser: "ordinary", CreatedAt: localTime(t, "2025-01-06 10:00")},
	}}

	res, err := Aggregate(f, opts)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}

	var buf bytes.Buffer
	if err := WriteTerminal(&buf, res); err != nil {
		t.Fatalf("WriteTerminal failed: %v", err)
	}
	assertNoTerminalEscapes(t, buf.String())
}

func TestWriteTerminalStripsEscapesFromUnmatchedHandles(t *testing.T) {
	const hostile = "renovate\x1b[2K\x1b]0;pwned\x07"

	opts := window(t, "2025-01-01 00:00", "2025-02-01 00:00")
	opts.Authors = []string{"alice@corp.com"}

	res, err := Aggregate(Fetched{Items: []MergeRequest{
		{ID: "1", AuthorUser: hostile, CreatedAt: localTime(t, "2025-01-05 10:00")},
	}}, opts)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}
	if res.UnmatchedTotal != 1 {
		t.Fatalf("UnmatchedTotal = %d, want the hostile account reported", res.UnmatchedTotal)
	}

	var buf bytes.Buffer
	if err := WriteTerminal(&buf, res); err != nil {
		t.Fatalf("WriteTerminal failed: %v", err)
	}
	assertNoTerminalEscapes(t, buf.String())
}

func TestWriteComparisonTerminalStripsEscapes(t *testing.T) {
	const hostile = "Bob\x1b[2K\x1b]0;pwned\x07"

	before := Result{Provider: ProviderGitLab, Scope: "g",
		Since: localTime(t, "2025-01-01 00:00"), Until: localTime(t, "2025-02-01 00:00"),
		Opened: 2, Authors: []AuthorStats{{Identity: hostile, Opened: 2}}}
	after := Result{Provider: ProviderGitLab, Scope: "g",
		Since: localTime(t, "2025-02-01 00:00"), Until: localTime(t, "2025-03-01 00:00"),
		Opened: 4, Authors: []AuthorStats{{Identity: hostile, Opened: 4}}}

	var buf bytes.Buffer
	if err := WriteComparisonTerminal(&buf, CompareResults(before, after)); err != nil {
		t.Fatalf("WriteComparisonTerminal failed: %v", err)
	}
	assertNoTerminalEscapes(t, buf.String())
}

// assertNoTerminalEscapes checks that nothing beyond the reporter's own styling
// reached the output. The reporter blanks its ANSI codes when stdout is not a
// terminal, which it is not under `go test`, so any escape byte here came from
// the data.
func assertNoTerminalEscapes(t *testing.T, out string) {
	t.Helper()
	for _, bad := range []struct {
		name string
		seq  string
	}{
		{"ESC", "\x1b"},
		{"erase line", "\x1b[2K"},
		{"set window title", "\x1b]0;"},
		{"BEL", "\x07"},
		{"NUL", "\x00"},
		{"carriage return", "\r"},
	} {
		if strings.Contains(out, bad.seq) {
			t.Errorf("output contains a %s sequence from platform data:\n%q", bad.name, out)
		}
	}
}

func TestSanitize(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"ordinary text untouched", "Jane Doe", "Jane Doe"},
		{"tab survives as whitespace", "a\tb", "a\tb"},
		{"escape replaced", "a\x1bb", "a�b"},
		{"newline replaced", "a\nb", "a�b"},
		{"carriage return replaced", "a\rb", "a�b"},
		{"nul replaced", "a\x00b", "a�b"},
		{"del replaced", "a\x7fb", "a�b"},
		{"c1 replaced", "ab", "a�b"},
		{"non-ascii text preserved", "José Ñuñez", "José Ñuñez"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitize(tc.in); got != tc.want {
				t.Fatalf("sanitize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// Sanitizing must stay in the display layer: a crafted name that changed the
// matching keys could redirect someone else's work to the attacker's row.
func TestSanitizeDoesNotAffectIdentityMatching(t *testing.T) {
	opts := window(t, "2025-01-01 00:00", "2025-02-01 00:00")
	opts.Authors = []string{"jane\x1b@corp.com"}

	res, err := Aggregate(Fetched{Items: []MergeRequest{
		{ID: "1", AuthorEmail: "jane\x1b@corp.com", CreatedAt: localTime(t, "2025-01-05 10:00")},
	}}, opts)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}
	// The raw value still matched, so nothing was dropped.
	if res.Opened != 1 || res.UnmatchedTotal != 0 {
		t.Fatalf("Opened=%d Unmatched=%d, want the raw value to still match", res.Opened, res.UnmatchedTotal)
	}
	// But it is cleaned on the way out.
	var buf bytes.Buffer
	if err := WriteTerminal(&buf, res); err != nil {
		t.Fatalf("WriteTerminal failed: %v", err)
	}
	assertNoTerminalEscapes(t, buf.String())
}

func TestDisplayWidthCountsRunes(t *testing.T) {
	// fmt pads %-*s by runes, so measuring bytes would misalign every row
	// containing a non-ASCII name.
	if got := displayWidth("José"); got != 4 {
		t.Fatalf("displayWidth(\"José\") = %d, want 4", got)
	}
}

func TestTruncateIsRuneAware(t *testing.T) {
	// Cutting on a byte boundary would emit a broken rune.
	got := truncate("Ñuñez-Fernández-Extra", 10)
	if !utf8.ValidString(got) {
		t.Fatalf("truncate produced invalid UTF-8: %q", got)
	}
	if displayWidth(got) != 10 {
		t.Fatalf("truncate = %q (%d runes), want 10 runes", got, displayWidth(got))
	}
}

func TestFormatNumber(t *testing.T) {
	cases := map[int]string{
		0:        "0",
		42:       "42",
		999:      "999",
		1000:     "1,000",
		1234:     "1,234",
		999999:   "999,999",
		1000000:  "1,000,000",
		12345678: "12,345,678",
		-4200:    "-4,200",
	}
	for in, want := range cases {
		if got := formatNumber(in); got != want {
			t.Errorf("formatNumber(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestMergeRate(t *testing.T) {
	if got := mergeRate(0, 0); got != "-" {
		t.Errorf("mergeRate with no merge requests = %q, want -", got)
	}
	if got := mergeRate(4, 3); got != "75%" {
		t.Errorf("mergeRate(4, 3) = %q, want 75%%", got)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate = %q, want the original", got)
	}
	if got := truncate("averyverylongidentity", 10); got != "averyve..." {
		t.Errorf("truncate = %q, want a 10 character ellipsis form", got)
	}
}

func TestBreakdownTitle(t *testing.T) {
	cases := map[string]string{
		"monthly": "Monthly Breakdown",
		"weekly":  "Weekly Breakdown",
		"daily":   "Daily Breakdown",
	}
	for in, want := range cases {
		if got := breakdownTitle(in); got != want {
			t.Errorf("breakdownTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
