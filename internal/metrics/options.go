package metrics

import (
	"fmt"
	"strings"
)

type Selection struct {
	CommitSize bool
	Cadence    bool
	LeadTime   bool
	Churn      bool
}

var validMetricNames = []string{"commit-size", "cadence", "lead-time", "churn"}

func ParseSelection(raw string) (Selection, error) {
	var s Selection
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return s, nil
	}
	if raw == "all" {
		return Selection{CommitSize: true, Cadence: true, LeadTime: true, Churn: true}, nil
	}
	for _, part := range strings.Split(raw, ",") {
		name := strings.TrimSpace(part)
		switch name {
		case "commit-size":
			s.CommitSize = true
		case "cadence":
			s.Cadence = true
		case "lead-time":
			s.LeadTime = true
		case "churn":
			s.Churn = true
		default:
			return Selection{}, fmt.Errorf("unknown metric %q (valid: %s, or 'all')", name, strings.Join(validMetricNames, ", "))
		}
	}
	return s, nil
}

func (s Selection) Any() bool {
	return s.CommitSize || s.Cadence || s.LeadTime || s.Churn
}
