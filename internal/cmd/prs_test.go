package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/juangracia/gitrespect/internal/git"
	"github.com/juangracia/gitrespect/internal/prs"
	"github.com/spf13/cobra"
)

func TestResolvePRScopeMapsProviderToItsFlag(t *testing.T) {
	got, err := resolvePRScope(prs.ProviderGitLab, "bunn-digital/web", "")
	if err != nil || got != "bunn-digital/web" {
		t.Fatalf("gitlab scope = (%q, %v), want the group path", got, err)
	}
	got, err = resolvePRScope(prs.ProviderGitHub, "", "my-org")
	if err != nil || got != "my-org" {
		t.Fatalf("github scope = (%q, %v), want the org", got, err)
	}
}

// Passing the other platform's flag is almost always a copy/paste mistake, and
// silently ignoring it would query the wrong thing or nothing at all.
func TestResolvePRScopeRejectsTheWrongPlatformFlag(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		group    string
		org      string
		want     string
	}{
		{"org with gitlab", prs.ProviderGitLab, "g", "my-org", "--org is a github flag"},
		{"group with github", prs.ProviderGitHub, "g", "my-org", "--group is a gitlab flag"},
		{"gitlab without group", prs.ProviderGitLab, "", "", "--group is required"},
		{"github without org", prs.ProviderGitHub, "", "", "--org is required"},
		{"unknown provider", "bitbucket", "g", "", "invalid --provider"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolvePRScope(tc.provider, tc.group, tc.org)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q should mention %q", err, tc.want)
			}
		})
	}
}

func TestResolvePRScopeTrimsWhitespace(t *testing.T) {
	got, err := resolvePRScope(prs.ProviderGitLab, "  g/p  ", "")
	if err != nil || got != "g/p" {
		t.Fatalf("scope = (%q, %v), want the trimmed path", got, err)
	}
}

func TestResolvePRIdentities(t *testing.T) {
	people, grouping, err := resolvePRIdentities("me@corp.com", nil, nil)
	if err != nil || len(people) != 1 || people[0].Label != "me@corp.com" {
		t.Fatalf("--author = (%v, %v), want a one element filter", people, err)
	}
	if grouping != nil {
		t.Fatalf("grouping = %v, want nil when a filter is given", grouping)
	}

	people, _, err = resolvePRIdentities("", []string{"a@x.com", "b@x.com"}, nil)
	if err != nil || len(people) != 2 {
		t.Fatalf("--team = (%v, %v), want both members", people, err)
	}

	// No filter is legitimate: it reports the whole group, which is what
	// makes a team average meaningful.
	people, grouping, err = resolvePRIdentities("", nil, nil)
	if err != nil || len(people) != 0 || len(grouping) != 0 {
		t.Fatalf("no filter = (%v, %v, %v), want empty and no error", people, grouping, err)
	}
}

func TestResolvePRIdentitiesRejectsAuthorWithTeam(t *testing.T) {
	_, _, err := resolvePRIdentities("me@corp.com", []string{"a@x.com"}, nil)
	if err == nil {
		t.Fatal("expected --author and --team to be mutually exclusive")
	}
}

// The whole point of a roster is that one human with several addresses is
// counted once. Two artefacts in the same release disagreeing about who
// someone is would be worse than having no roster at all.
func TestResolvePRIdentitiesFoldsRosterAddressesIntoOnePerson(t *testing.T) {
	roster := git.Roster{{Name: "Jane Doe", Emails: []string{"jane@corp.com", "j.doe@personal.com"}}}

	// Naming either address, or the canonical name, yields the same person.
	for _, token := range []string{"jane@corp.com", "j.doe@personal.com", "Jane Doe"} {
		people, _, err := resolvePRIdentities("", []string{token}, roster)
		if err != nil {
			t.Fatalf("resolvePRIdentities(%q) failed: %v", token, err)
		}
		if len(people) != 1 {
			t.Fatalf("token %q produced %d identities, want 1", token, len(people))
		}
		if people[0].Label != "Jane Doe" {
			t.Errorf("token %q labelled %q, want the canonical name", token, people[0].Label)
		}
		if len(people[0].Keys) != 2 {
			t.Errorf("token %q carried %d keys, want both addresses", token, len(people[0].Keys))
		}
	}

	// Naming both addresses must not produce two rows for one human.
	people, _, err := resolvePRIdentities("", []string{"jane@corp.com", "j.doe@personal.com"}, roster)
	if err != nil {
		t.Fatalf("resolvePRIdentities failed: %v", err)
	}
	if len(people) != 1 {
		t.Fatalf("got %d identities, want one person merged from two addresses", len(people))
	}
}

// A bare roster says who an account belongs to, not who to count, so it groups
// without filtering. Making it a selector would silently shrink the group and
// destroy the denominator a team average needs.
func TestResolvePRIdentitiesBareRosterGroupsWithoutFiltering(t *testing.T) {
	roster := git.Roster{
		{Name: "Jane Doe", Emails: []string{"jane@corp.com"}},
		{Name: "Bob Smith", Emails: []string{"bob@corp.com"}},
	}

	people, grouping, err := resolvePRIdentities("", nil, roster)
	if err != nil {
		t.Fatalf("resolvePRIdentities failed: %v", err)
	}
	if len(people) != 0 {
		t.Fatalf("people = %v, want no filter from a bare roster", people)
	}
	if len(grouping) != 2 {
		t.Fatalf("grouping = %v, want both roster entries", grouping)
	}
}

func TestResolvePRIdentitiesUnknownTokenFallsBackToItself(t *testing.T) {
	roster := git.Roster{{Name: "Jane Doe", Emails: []string{"jane@corp.com"}}}

	people, _, err := resolvePRIdentities("stranger@corp.com", nil, roster)
	if err != nil {
		t.Fatalf("resolvePRIdentities failed: %v", err)
	}
	// Dropping an unregistered person would silently shrink the report.
	if len(people) != 1 || people[0].Label != "stranger@corp.com" {
		t.Fatalf("people = %v, want the token kept as its own identity", people)
	}
}

func TestResolvePRWindowFromYear(t *testing.T) {
	cmd := newPRsTestCommand(t, "--year=2020")

	since, until, err := resolvePRWindow(cmd)
	if err != nil {
		t.Fatalf("resolvePRWindow failed: %v", err)
	}
	if since.Year() != 2020 || since.Month() != 1 || since.Day() != 1 {
		t.Fatalf("since = %v, want 2020-01-01", since)
	}
	if until.Year() != 2020 || until.Month() != 12 || until.Day() != 31 {
		t.Fatalf("until = %v, want 2020-12-31", until)
	}
}

func TestResolvePRWindowRejectsBadYear(t *testing.T) {
	cmd := newPRsTestCommand(t, "--year=0")
	if _, _, err := resolvePRWindow(cmd); err == nil {
		t.Fatal("expected --year=0 to be rejected")
	}

	cmd = newPRsTestCommand(t, "--year=3000")
	if _, _, err := resolvePRWindow(cmd); err == nil {
		t.Fatal("expected a future --year to be rejected")
	}
}

func TestResolvePRWindowFromSinceUntil(t *testing.T) {
	cmd := newPRsTestCommand(t, "--since=2025-01-01", "--until=2025-03-05")

	since, until, err := resolvePRWindow(cmd)
	if err != nil {
		t.Fatalf("resolvePRWindow failed: %v", err)
	}
	if since.Format("2006-01-02") != "2025-01-01" {
		t.Fatalf("since = %v, want 2025-01-01", since)
	}
	// The end bound must cover the whole day, not stop at its midnight.
	if until.Format("2006-01-02") != "2025-03-05" || until.Hour() != 23 {
		t.Fatalf("until = %v, want the end of 2025-03-05", until)
	}
}

func TestResolvePRWindowRejectsInvertedRange(t *testing.T) {
	cmd := newPRsTestCommand(t, "--since=2025-06-01", "--until=2025-01-01")
	if _, _, err := resolvePRWindow(cmd); err == nil {
		t.Fatal("expected an inverted date range to be rejected")
	}
}

func TestPRsCommandHelpRenders(t *testing.T) {
	var buf bytes.Buffer
	root := &cobra.Command{Use: "gitrespect"}
	clone := *prsCmd
	root.AddCommand(&clone)
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"prs", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("prs --help failed: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"--provider", "--group", "--org", "--team", "--breakdown", "GITLAB_TOKEN", "glab"} {
		if !strings.Contains(out, want) {
			t.Errorf("prs --help should mention %q:\n%s", want, out)
		}
	}
}

func TestPRsCommandRejectsPositionalArgs(t *testing.T) {
	// Unlike the analyze command, prs takes no repository paths: it reads the
	// platform, not the working copy. A stray path is a misunderstanding worth
	// reporting.
	if err := prsCmd.Args(prsCmd, []string{"."}); err == nil {
		t.Fatal("expected prs to reject positional arguments")
	}
}

// newPRsTestCommand builds a throwaway command carrying the prs flag set, so
// flag parsing and Changed() behave exactly as they do in production without
// mutating the shared command.
func newPRsTestCommand(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "prs", RunE: func(*cobra.Command, []string) error { return nil }}

	prsYear, prsSince, prsUntil = 0, "30 days ago", ""
	cmd.Flags().IntVar(&prsYear, "year", 0, "")
	cmd.Flags().StringVar(&prsSince, "since", "30 days ago", "")
	cmd.Flags().StringVar(&prsUntil, "until", "", "")

	if err := cmd.Flags().Parse(args); err != nil {
		t.Fatalf("parsing %v: %v", args, err)
	}
	return cmd
}
