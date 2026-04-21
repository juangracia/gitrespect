package metrics

import (
	"strings"
	"testing"
)

func TestParseSelection(t *testing.T) {
	tests := []struct {
		raw          string
		wantCS       bool
		wantCad      bool
		wantLT       bool
		wantChurn    bool
		wantErr      bool
		wantErrMatch string
	}{
		{raw: ""},
		{raw: "all", wantCS: true, wantCad: true, wantLT: true, wantChurn: true},
		{raw: "churn", wantChurn: true},
		{raw: "commit-size,cadence", wantCS: true, wantCad: true},
		{raw: "lead-time,churn,cadence,commit-size", wantCS: true, wantCad: true, wantLT: true, wantChurn: true},
		{raw: " churn , lead-time ", wantChurn: true, wantLT: true},
		{raw: "foo", wantErr: true, wantErrMatch: "foo"},
		{raw: "churn,bogus", wantErr: true, wantErrMatch: "bogus"},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			sel, err := ParseSelection(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tc.wantErrMatch) {
					t.Errorf("error %q does not contain %q", err, tc.wantErrMatch)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if sel.CommitSize != tc.wantCS {
				t.Errorf("CommitSize: got %v want %v", sel.CommitSize, tc.wantCS)
			}
			if sel.Cadence != tc.wantCad {
				t.Errorf("Cadence: got %v want %v", sel.Cadence, tc.wantCad)
			}
			if sel.LeadTime != tc.wantLT {
				t.Errorf("LeadTime: got %v want %v", sel.LeadTime, tc.wantLT)
			}
			if sel.Churn != tc.wantChurn {
				t.Errorf("Churn: got %v want %v", sel.Churn, tc.wantChurn)
			}
		})
	}
}

func TestSelectionAny(t *testing.T) {
	if (Selection{}).Any() {
		t.Error("empty selection should report Any() == false")
	}
	if !(Selection{Churn: true}).Any() {
		t.Error("Churn-only should report Any() == true")
	}
}
