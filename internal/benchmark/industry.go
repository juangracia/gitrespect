package benchmark

// Industry benchmarks for lines of code per day
// Based on research from Fred Brooks, Steve McConnell, and industry surveys
const (
	// SeniorDevLOCPerDay is the typical output for experienced senior developers
	// who focus on quality, architecture, and mentoring
	SeniorDevLOCPerDay = 20

	// AverageDevLOCPerDay is the industry average across all experience levels
	AverageDevLOCPerDay = 50

	// JuniorDevLOCPerDay is typical for newer developers who write more code
	// but may have higher churn rates
	JuniorDevLOCPerDay = 100
)

type Comparison struct {
	Label      string
	Benchmark  int
	Multiplier float64
}

// Compare calculates how the given LOC/day compares to industry benchmarks
func Compare(locPerDay float64) []Comparison {
	return []Comparison{
		{
			Label:      "Senior Dev",
			Benchmark:  SeniorDevLOCPerDay,
			Multiplier: locPerDay / float64(SeniorDevLOCPerDay),
		},
		{
			Label:      "Industry Avg",
			Benchmark:  AverageDevLOCPerDay,
			Multiplier: locPerDay / float64(AverageDevLOCPerDay),
		},
		{
			Label:      "Junior Dev",
			Benchmark:  JuniorDevLOCPerDay,
			Multiplier: locPerDay / float64(JuniorDevLOCPerDay),
		},
	}
}

// CalculateMultiplier returns the productivity multiplier compared to average
func CalculateMultiplier(before, after float64) float64 {
	if before <= 0 {
		return 0
	}
	return after / before
}
