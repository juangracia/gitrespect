package metrics

type Bundle struct {
	Selection       Selection
	Baseline        *Baseline
	CommitSize      *CommitSizeDistribution
	Cadence         *Cadence
	LeadTime        *LeadTime
	Churn           *Churn
	LegacyBenchmark bool
	// ReposAnalyzed is how many repositories the surrounding report covers, set
	// by the caller that assembled the bundle. Each metric carries its own
	// ReposCovered, which is the count that actually fed that metric; the two
	// differ when a repository could not be read or has nothing a given metric
	// can measure. The report needs both to say "3 of 5" rather than implying
	// every number describes every repository.
	ReposAnalyzed int
}
