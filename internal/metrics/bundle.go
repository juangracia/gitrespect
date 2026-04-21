package metrics

type Bundle struct {
	Selection       Selection
	Baseline        *Baseline
	CommitSize      *CommitSizeDistribution
	Cadence         *Cadence
	LeadTime        *LeadTime
	Churn           *Churn
	LegacyBenchmark bool
}
