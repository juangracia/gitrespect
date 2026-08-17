package cmd

import "testing"

func TestResolveVersion(t *testing.T) {
	tests := []struct {
		name                               string
		version, commit, date              string
		moduleVersion, revision, buildTime string
		fromVCS                            bool
		wantVersion, wantCommit            string
	}{
		{
			name:          "go install from proxy adopts the module version",
			version:       "dev",
			commit:        "none",
			date:          "unknown",
			moduleVersion: "v0.5.0",
			fromVCS:       false,
			wantVersion:   "0.5.0",
			wantCommit:    "none",
		},
		{
			name:          "local checkout stays dev rather than claiming the last tag",
			version:       "dev",
			commit:        "none",
			date:          "unknown",
			moduleVersion: "v0.4.1+dirty",
			revision:      "1d18acfeddfd8ca4122dc64c9ebe8fde76842aea",
			buildTime:     "2026-06-24T15:06:28Z",
			fromVCS:       true,
			wantVersion:   "dev",
			wantCommit:    "1d18acf",
		},
		{
			name:          "goreleaser stamped values win",
			version:       "0.5.0",
			commit:        "96180d4",
			date:          "2026-08-17T19:48:23Z",
			moduleVersion: "v0.4.1",
			fromVCS:       false,
			wantVersion:   "0.5.0",
			wantCommit:    "96180d4",
		},
		{
			name:          "devel module version is not adopted",
			version:       "dev",
			commit:        "none",
			date:          "unknown",
			moduleVersion: "(devel)",
			fromVCS:       false,
			wantVersion:   "dev",
			wantCommit:    "none",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotVersion, gotCommit, _ := resolveVersion(
				tc.version, tc.commit, tc.date,
				tc.moduleVersion, tc.revision, tc.buildTime, tc.fromVCS)
			if gotVersion != tc.wantVersion {
				t.Errorf("version = %q, want %q", gotVersion, tc.wantVersion)
			}
			if gotCommit != tc.wantCommit {
				t.Errorf("commit = %q, want %q", gotCommit, tc.wantCommit)
			}
		})
	}
}

func TestValidateOutputFlags(t *testing.T) {
	if err := validateOutputFlags("weekly", "json", "light"); err != nil {
		t.Errorf("valid combination rejected: %v", err)
	}
	if err := validateOutputFlags("", "", ""); err != nil {
		t.Errorf("empty defaults rejected: %v", err)
	}
	for _, tc := range []struct{ breakdown, output, theme string }{
		{"hourly", "", ""},
		{"", "yaml", ""},
		{"", "", "neon"},
	} {
		if err := validateOutputFlags(tc.breakdown, tc.output, tc.theme); err == nil {
			t.Errorf("validateOutputFlags(%q,%q,%q) accepted an invalid value",
				tc.breakdown, tc.output, tc.theme)
		}
	}
}
