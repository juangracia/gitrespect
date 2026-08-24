package prs

import (
	"fmt"
	"testing"
	"time"
)

// localTime builds a fixture in the local zone, so bucketing (which works in
// local time, like the git side) is not sensitive to the test machine's TZ.
func localTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02 15:04", value, time.Local)
	if err != nil {
		t.Fatalf("bad fixture time %q: %v", value, err)
	}
	return parsed
}

func mr(t *testing.T, user, created string) MergeRequest {
	t.Helper()
	return MergeRequest{
		ID:         user + created,
		AuthorUser: user,
		CreatedAt:  localTime(t, created),
	}
}

func merged(t *testing.T, m MergeRequest, at string) MergeRequest {
	t.Helper()
	m.MergedAt = localTime(t, at)
	m.State = "merged"
	return m
}

func window(t *testing.T, since, until string) Options {
	t.Helper()
	return Options{
		Provider: ProviderGitLab,
		Scope:    "group/x",
		Since:    localTime(t, since),
		Until:    localTime(t, until),
	}
}

func TestAggregateBucketsByMonthOfCreation(t *testing.T) {
	opts := window(t, "2025-01-01 00:00", "2025-12-31 23:59")
	opts.Granularity = "monthly"

	f := Fetched{Items: []MergeRequest{
		mr(t, "alice", "2025-01-05 10:00"),
		mr(t, "alice", "2025-01-28 10:00"),
		mr(t, "bob", "2025-03-02 10:00"),
	}}

	res, err := Aggregate(f, opts)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}
	if res.Opened != 3 {
		t.Fatalf("Opened = %d, want 3", res.Opened)
	}
	if len(res.Periods) != 2 {
		t.Fatalf("got %d periods, want 2 (only months with activity)", len(res.Periods))
	}
	if res.Periods[0].Label != "Jan 2025" || res.Periods[0].Opened != 2 {
		t.Errorf("period 0 = %+v, want Jan 2025 with 2 opened", res.Periods[0])
	}
	if res.Periods[1].Label != "Mar 2025" || res.Periods[1].Opened != 1 {
		t.Errorf("period 1 = %+v, want Mar 2025 with 1 opened", res.Periods[1])
	}
}

func TestAggregateWeeklyBucketsAnchorOnMonday(t *testing.T) {
	opts := window(t, "2025-01-01 00:00", "2025-01-31 23:59")
	opts.Granularity = "weekly"

	// 2025-01-15 is a Wednesday; its week starts Monday 2025-01-13.
	f := Fetched{Items: []MergeRequest{mr(t, "alice", "2025-01-15 09:00")}}

	res, err := Aggregate(f, opts)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}
	if len(res.Periods) != 1 {
		t.Fatalf("got %d periods, want 1", len(res.Periods))
	}
	if res.Periods[0].Label != "Week of Jan 13 2025" {
		t.Fatalf("weekly label = %q, want %q", res.Periods[0].Label, "Week of Jan 13 2025")
	}
}

func TestAggregateDailyBuckets(t *testing.T) {
	opts := window(t, "2025-01-01 00:00", "2025-01-31 23:59")
	opts.Granularity = "daily"

	f := Fetched{Items: []MergeRequest{
		mr(t, "alice", "2025-01-15 09:00"),
		mr(t, "alice", "2025-01-15 23:30"),
	}}

	res, err := Aggregate(f, opts)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}
	if len(res.Periods) != 1 || res.Periods[0].Label != "Jan 15 2025" || res.Periods[0].Opened != 2 {
		t.Fatalf("daily periods = %+v, want one Jan 15 2025 bucket with 2", res.Periods)
	}
}

func TestAggregateSeedsRequestedIdentitiesWithZero(t *testing.T) {
	opts := window(t, "2025-01-01 00:00", "2025-02-01 00:00")
	opts.Authors = []string{"alice@corp.com", "ghost@corp.com"}

	f := Fetched{Items: []MergeRequest{mr(t, "alice", "2025-01-05 10:00")}}

	res, err := Aggregate(f, opts)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}
	if len(res.Authors) != 2 {
		t.Fatalf("got %d authors, want both requested identities", len(res.Authors))
	}
	ghost, ok := res.Author("ghost@corp.com")
	if !ok {
		t.Fatal("a requested identity with no merge requests should still appear")
	}
	if ghost.Opened != 0 {
		t.Fatalf("ghost.Opened = %d, want 0", ghost.Opened)
	}
}

func TestAggregateReportsUnmatchedAccounts(t *testing.T) {
	opts := window(t, "2025-01-01 00:00", "2025-02-01 00:00")
	opts.Authors = []string{"alice@corp.com"}

	f := Fetched{Items: []MergeRequest{
		mr(t, "alice", "2025-01-05 10:00"),
		mr(t, "renovate", "2025-01-06 10:00"),
		mr(t, "renovate", "2025-01-07 10:00"),
		mr(t, "dependabot", "2025-01-08 10:00"),
	}}

	res, err := Aggregate(f, opts)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}
	if res.Opened != 1 {
		t.Fatalf("Opened = %d, want only the matched merge request", res.Opened)
	}
	if res.UnmatchedTotal != 3 {
		t.Fatalf("UnmatchedTotal = %d, want 3", res.UnmatchedTotal)
	}
	if len(res.Unmatched) != 2 || res.Unmatched[0].Handle != "renovate" || res.Unmatched[0].Opened != 2 {
		t.Fatalf("Unmatched = %+v, want renovate first with 2", res.Unmatched)
	}
}

func TestAggregateWithoutFilterGroupsByAccount(t *testing.T) {
	opts := window(t, "2025-01-01 00:00", "2025-02-01 00:00")

	f := Fetched{Items: []MergeRequest{
		mr(t, "alice", "2025-01-05 10:00"),
		mr(t, "alice", "2025-01-06 10:00"),
		mr(t, "bob", "2025-01-07 10:00"),
	}}

	res, err := Aggregate(f, opts)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}
	if res.Filtered {
		t.Fatal("Filtered should be false without --author/--team")
	}
	if len(res.Authors) != 2 {
		t.Fatalf("got %d authors, want 2", len(res.Authors))
	}
	// Sorted by volume descending.
	if res.Authors[0].Identity != "alice" || res.Authors[0].Opened != 2 {
		t.Fatalf("top author = %+v, want alice with 2", res.Authors[0])
	}
	if res.UnmatchedTotal != 0 {
		t.Fatalf("UnmatchedTotal = %d, want 0 when there is no filter", res.UnmatchedTotal)
	}
}

func TestAggregateAuthorPeriodsAlignWithTeamPeriods(t *testing.T) {
	opts := window(t, "2025-01-01 00:00", "2025-04-01 00:00")
	opts.Granularity = "monthly"

	f := Fetched{Items: []MergeRequest{
		mr(t, "alice", "2025-01-05 10:00"),
		mr(t, "bob", "2025-03-05 10:00"),
	}}

	res, err := Aggregate(f, opts)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}
	alice, _ := res.Author("alice")
	if len(alice.Periods) != len(res.Periods) {
		t.Fatalf("alice has %d periods, want %d so the columns line up", len(alice.Periods), len(res.Periods))
	}
	if alice.Periods[1].Opened != 0 {
		t.Fatalf("alice's March bucket = %d, want an explicit zero", alice.Periods[1].Opened)
	}
}

func TestAggregateLeadTimeFromRealTimestamps(t *testing.T) {
	opts := window(t, "2025-01-01 00:00", "2025-02-01 00:00")

	f := Fetched{Items: []MergeRequest{
		merged(t, mr(t, "alice", "2025-01-01 00:00"), "2025-01-03 00:00"), // 2 days
		merged(t, mr(t, "alice", "2025-01-05 00:00"), "2025-01-09 00:00"), // 4 days
		merged(t, mr(t, "alice", "2025-01-10 00:00"), "2025-01-16 00:00"), // 6 days
		mr(t, "alice", "2025-01-20 00:00"),                                // never merged
	}}

	res, err := Aggregate(f, opts)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}
	if res.Merged != 3 {
		t.Fatalf("Merged = %d, want 3", res.Merged)
	}
	if res.LeadTime == nil {
		t.Fatal("LeadTime should be present when merge requests were merged")
	}
	if res.LeadTime.Samples != 3 {
		t.Fatalf("LeadTime.Samples = %d, want 3", res.LeadTime.Samples)
	}
	if res.LeadTime.MedianDays != 4 {
		t.Fatalf("LeadTime.MedianDays = %v, want 4", res.LeadTime.MedianDays)
	}
	if res.LeadTime.MeanDays != 4 {
		t.Fatalf("LeadTime.MeanDays = %v, want 4", res.LeadTime.MeanDays)
	}
}

func TestAggregateLeadTimeNilWithoutMerges(t *testing.T) {
	opts := window(t, "2025-01-01 00:00", "2025-02-01 00:00")
	f := Fetched{Items: []MergeRequest{mr(t, "alice", "2025-01-05 10:00")}}

	res, err := Aggregate(f, opts)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}
	if res.LeadTime != nil {
		t.Fatalf("LeadTime = %+v, want nil so 'no merges' is distinguishable from 'instant'", res.LeadTime)
	}
}

func TestAggregateEmptyResultIsNotAnError(t *testing.T) {
	opts := window(t, "2025-01-01 00:00", "2025-02-01 00:00")

	res, err := Aggregate(Fetched{}, opts)
	if err != nil {
		t.Fatalf("an empty result set must not be an error, got: %v", err)
	}
	if res.Opened != 0 || len(res.Authors) != 0 || len(res.Periods) != 0 {
		t.Fatalf("empty aggregate = %+v, want zeroes", res)
	}
}

func TestAggregateSkipsItemsWithoutCreationTime(t *testing.T) {
	opts := window(t, "2025-01-01 00:00", "2025-02-01 00:00")
	f := Fetched{Items: []MergeRequest{{AuthorUser: "alice"}}}

	res, err := Aggregate(f, opts)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}
	if res.Opened != 0 {
		t.Fatalf("Opened = %d, want 0 for an item with no created timestamp", res.Opened)
	}
}

func TestAggregatePropagatesTruncation(t *testing.T) {
	opts := window(t, "2025-01-01 00:00", "2025-02-01 00:00")
	f := Fetched{Truncated: true, Note: "capped", Requests: 7}

	res, err := Aggregate(f, opts)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}
	if !res.Truncated || res.Note != "capped" || res.Requests != 7 {
		t.Fatalf("truncation metadata lost: %+v", res)
	}
}

func TestAggregateRejectsBadMapping(t *testing.T) {
	opts := window(t, "2025-01-01 00:00", "2025-02-01 00:00")
	opts.Mappings = []string{"garbage"}

	if _, err := Aggregate(Fetched{}, opts); err == nil {
		t.Fatal("expected Aggregate to reject a malformed --map")
	}
}

func TestResultOpenedPerMonth(t *testing.T) {
	res := Result{
		Since:  localTime(t, "2025-01-01 00:00"),
		Until:  localTime(t, "2025-03-02 00:00"), // 60 days
		Opened: 20,
	}
	got := res.OpenedPerMonth()
	if got < 9.9 || got > 10.1 {
		t.Fatalf("OpenedPerMonth() = %v, want about 10", got)
	}
}

func TestCompareResults(t *testing.T) {
	before := Result{
		Since:  localTime(t, "2025-01-01 00:00"),
		Until:  localTime(t, "2025-02-01 00:00"),
		Opened: 10,
		Authors: []AuthorStats{
			{Identity: "alice", Opened: 8},
			{Identity: "bob", Opened: 2},
		},
	}
	after := Result{
		Since:  localTime(t, "2025-02-01 00:00"),
		Until:  localTime(t, "2025-03-01 00:00"),
		Opened: 30,
		Authors: []AuthorStats{
			{Identity: "alice", Opened: 24},
			{Identity: "carol", Opened: 6},
		},
	}

	c := CompareResults(before, after)
	if c.OpenedMultiplier != 3 {
		t.Fatalf("OpenedMultiplier = %v, want 3", c.OpenedMultiplier)
	}
	if c.OpenedUndefined {
		t.Fatal("OpenedUndefined should be false when the before window has volume")
	}

	byID := map[string]AuthorComparison{}
	for _, a := range c.Authors {
		byID[a.Identity] = a
	}
	if len(byID) != 3 {
		t.Fatalf("got %d authors, want the union of both windows", len(byID))
	}
	if got := byID["alice"]; got.Multiplier != 3 || got.Before != 8 || got.After != 24 {
		t.Fatalf("alice = %+v, want 8 -> 24 at 3x", got)
	}
	if got := byID["bob"]; got.After != 0 || got.Multiplier != 0 {
		t.Fatalf("bob = %+v, want a 2 -> 0 collapse", got)
	}
	if got := byID["carol"]; !got.Undefined {
		t.Fatalf("carol = %+v, want Undefined because there is no before baseline", got)
	}
}

func TestCompareResultsZeroBeforeIsUndefinedNotZero(t *testing.T) {
	before := Result{Since: localTime(t, "2025-01-01 00:00"), Until: localTime(t, "2025-02-01 00:00")}
	after := Result{Since: localTime(t, "2025-02-01 00:00"), Until: localTime(t, "2025-03-01 00:00"), Opened: 12}

	c := CompareResults(before, after)
	if !c.OpenedUndefined {
		t.Fatal("going from zero to twelve must be flagged undefined, not reported as 0.0x")
	}
}

func TestPercentile(t *testing.T) {
	odd := []float64{1, 2, 3}
	if got := percentile(odd, 0.5); got != 2 {
		t.Errorf("median of %v = %v, want 2", odd, got)
	}
	even := []float64{1, 2, 3, 4}
	if got := percentile(even, 0.5); got != 2.5 {
		t.Errorf("median of %v = %v, want 2.5", even, got)
	}
	if got := percentile(even, 0.75); got != 4 {
		t.Errorf("p75 of %v = %v, want 4", even, got)
	}
	if got := percentile(nil, 0.5); got != 0 {
		t.Errorf("percentile of empty = %v, want 0", got)
	}
}

// mrFrom builds a merge request whose platform account carries an email, which
// is what a roster has to match against.
func mrFrom(t *testing.T, user, email, created string) MergeRequest {
	t.Helper()
	m := mr(t, user, created)
	m.AuthorEmail = email
	m.ID = email + created
	return m
}

// The headline roster guarantee: one human with two addresses is one row.
// Two of them would double the row count and halve each number, which is
// exactly the miscount the roster exists to prevent.
func TestAggregateRosterPersonWithTwoAddressesIsOneRow(t *testing.T) {
	opts := window(t, "2025-01-01 00:00", "2025-02-01 00:00")
	opts.People = []Person{{
		Label: "Jane Doe",
		Keys:  []string{"jane@corp.com", "j.doe@personal.com"},
	}}

	f := Fetched{Items: []MergeRequest{
		mrFrom(t, "jane", "jane@corp.com", "2025-01-05 10:00"),
		mrFrom(t, "jdoe-personal", "j.doe@personal.com", "2025-01-06 10:00"),
		mrFrom(t, "jane", "jane@corp.com", "2025-01-07 10:00"),
	}}

	res, err := Aggregate(f, opts)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}
	if len(res.Authors) != 1 {
		t.Fatalf("got %d rows, want 1: a roster person's addresses must not split: %+v", len(res.Authors), res.Authors)
	}
	if res.Authors[0].Identity != "Jane Doe" {
		t.Fatalf("row labelled %q, want the canonical name", res.Authors[0].Identity)
	}
	if res.Authors[0].Opened != 3 {
		t.Fatalf("Opened = %d, want all 3 merge requests under one person", res.Authors[0].Opened)
	}
	// Both accounts should be traceable from the row.
	if len(res.Authors[0].Handles) != 2 {
		t.Fatalf("Handles = %v, want both accounts listed", res.Authors[0].Handles)
	}
}

// The roster must not become a way for accounts to disappear quietly. This is
// the property that makes the identity matching auditable rather than lossy.
func TestAggregateRosterStillReportsUnmatchedAccounts(t *testing.T) {
	opts := window(t, "2025-01-01 00:00", "2025-02-01 00:00")
	opts.People = []Person{{Label: "Jane Doe", Keys: []string{"jane@corp.com"}}}

	f := Fetched{Items: []MergeRequest{
		mrFrom(t, "jane", "jane@corp.com", "2025-01-05 10:00"),
		mr(t, "renovate", "2025-01-06 10:00"),
		mr(t, "dependabot", "2025-01-07 10:00"),
	}}

	res, err := Aggregate(f, opts)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}
	if res.UnmatchedTotal != 2 || res.UnmatchedAccounts != 2 {
		t.Fatalf("unmatched = %d/%d accounts, want 2 and 2", res.UnmatchedTotal, res.UnmatchedAccounts)
	}
	handles := map[string]bool{}
	for _, u := range res.Unmatched {
		handles[u.Handle] = true
	}
	if !handles["renovate"] || !handles["dependabot"] {
		t.Fatalf("Unmatched = %+v, want both bot accounts named", res.Unmatched)
	}
}

// A bare roster answers "who is this account", not "who am I counting", so
// nothing is dropped and the group total stays intact.
func TestAggregateBareRosterGroupsWithoutFiltering(t *testing.T) {
	opts := window(t, "2025-01-01 00:00", "2025-02-01 00:00")
	opts.Roster = []Person{
		{Label: "Jane Doe", Keys: []string{"jane@corp.com", "j.doe@personal.com"}},
		{Label: "Bob Smith", Keys: []string{"bob@corp.com"}},
	}

	f := Fetched{Items: []MergeRequest{
		mrFrom(t, "jane", "jane@corp.com", "2025-01-05 10:00"),
		mrFrom(t, "jdoe-personal", "j.doe@personal.com", "2025-01-06 10:00"),
		mr(t, "renovate", "2025-01-07 10:00"),
	}}

	res, err := Aggregate(f, opts)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}
	if res.Filtered {
		t.Fatal("a bare roster must not filter: it says who an account is, not who to count")
	}
	if res.Opened != 3 {
		t.Fatalf("Opened = %d, want every account still counted", res.Opened)
	}
	if res.UnmatchedTotal != 0 {
		t.Fatalf("UnmatchedTotal = %d, want 0: nothing was dropped", res.UnmatchedTotal)
	}

	byID := map[string]AuthorStats{}
	for _, a := range res.Authors {
		byID[a.Identity] = a
	}
	if got := byID["Jane Doe"]; got.Opened != 2 {
		t.Errorf("Jane Doe = %d, want her two addresses folded together", got.Opened)
	}
	// An account the roster does not know keeps its own row rather than
	// vanishing from the group.
	if got, ok := byID["renovate"]; !ok || got.Opened != 1 {
		t.Errorf("renovate = %+v, want its own row", got)
	}
	// A roster member the platform never saw must show as zero, not vanish.
	bob, ok := byID["Bob Smith"]
	if !ok {
		t.Fatal("a roster member with no merge requests must still appear")
	}
	if bob.Opened != 0 {
		t.Errorf("Bob Smith = %d, want an explicit zero", bob.Opened)
	}
}

func TestAggregateFilterTakesPrecedenceOverRoster(t *testing.T) {
	opts := window(t, "2025-01-01 00:00", "2025-02-01 00:00")
	opts.People = []Person{{Label: "Jane Doe", Keys: []string{"jane@corp.com"}}}
	opts.Roster = []Person{{Label: "Bob Smith", Keys: []string{"bob@corp.com"}}}

	f := Fetched{Items: []MergeRequest{
		mrFrom(t, "jane", "jane@corp.com", "2025-01-05 10:00"),
		mrFrom(t, "bob", "bob@corp.com", "2025-01-06 10:00"),
	}}

	res, err := Aggregate(f, opts)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}
	if !res.Filtered {
		t.Fatal("an explicit filter must still filter when a roster is also present")
	}
	if len(res.Authors) != 1 || res.Authors[0].Identity != "Jane Doe" {
		t.Fatalf("authors = %+v, want only the filtered person", res.Authors)
	}
	if res.UnmatchedTotal != 1 {
		t.Fatalf("UnmatchedTotal = %d, want bob reported as excluded", res.UnmatchedTotal)
	}
}

func TestAggregateTopTrimsTheContributorTable(t *testing.T) {
	opts := window(t, "2025-01-01 00:00", "2025-02-01 00:00")
	opts.Top = 2

	var items []MergeRequest
	// alice 3, bob 2, carol 1.
	for _, spec := range []struct {
		user string
		n    int
	}{{"alice", 3}, {"bob", 2}, {"carol", 1}} {
		for i := 0; i < spec.n; i++ {
			items = append(items, mr(t, spec.user, fmt.Sprintf("2025-01-%02d 10:00", i+1)))
		}
	}

	res, err := Aggregate(Fetched{Items: items}, opts)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}
	if len(res.Authors) != 2 || res.Authors[0].Identity != "alice" || res.Authors[1].Identity != "bob" {
		t.Fatalf("authors = %+v, want the top two by volume", res.Authors)
	}
	if res.AuthorsTotal != 3 {
		t.Fatalf("AuthorsTotal = %d, want the untrimmed count so the report can say what is hidden", res.AuthorsTotal)
	}
	// Totals must still cover everyone, or the team denominator is wrong.
	if res.Opened != 6 {
		t.Fatalf("Opened = %d, want 6: --top trims the table, not the totals", res.Opened)
	}
}

// A named team must never be trimmed: a missing row would read as "they opened
// nothing", which is a different claim entirely.
func TestAggregateTopDoesNotTrimAnExplicitTeam(t *testing.T) {
	opts := window(t, "2025-01-01 00:00", "2025-02-01 00:00")
	opts.Top = 1
	opts.Authors = []string{"alice@corp.com", "bob@corp.com", "carol@corp.com"}

	res, err := Aggregate(Fetched{Items: []MergeRequest{mr(t, "alice", "2025-01-05 10:00")}}, opts)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}
	if len(res.Authors) != 3 {
		t.Fatalf("got %d authors, want all three named team members", len(res.Authors))
	}
}

// The displayed list is capped, so the count of distinct accounts has to be
// tracked separately or the report understates how wide the gap is.
func TestAggregateCountsEveryUnmatchedAccountEvenWhenTheListIsCapped(t *testing.T) {
	opts := window(t, "2025-01-01 00:00", "2025-02-01 00:00")
	opts.Authors = []string{"alice@corp.com"}

	var items []MergeRequest
	const accounts = maxUnmatchedReported + 4
	for i := 0; i < accounts; i++ {
		items = append(items, mr(t, fmt.Sprintf("bot%02d", i), "2025-01-05 10:00"))
	}

	res, err := Aggregate(Fetched{Items: items}, opts)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}
	if res.UnmatchedAccounts != accounts {
		t.Fatalf("UnmatchedAccounts = %d, want %d", res.UnmatchedAccounts, accounts)
	}
	if len(res.Unmatched) != maxUnmatchedReported {
		t.Fatalf("displayed %d accounts, want the list capped at %d", len(res.Unmatched), maxUnmatchedReported)
	}
	if res.UnmatchedTotal != accounts {
		t.Fatalf("UnmatchedTotal = %d, want %d", res.UnmatchedTotal, accounts)
	}
}

func TestTopUnmatchedIsCapped(t *testing.T) {
	counts := map[string]int{}
	for i := 0; i < maxUnmatchedReported+5; i++ {
		counts[string(rune('a'+i))] = i + 1
	}
	got := topUnmatched(counts)
	if len(got) != maxUnmatchedReported {
		t.Fatalf("got %d unmatched entries, want them capped at %d", len(got), maxUnmatchedReported)
	}
	if got[0].Opened <= got[1].Opened {
		t.Fatal("unmatched accounts should be sorted by volume descending")
	}
}
