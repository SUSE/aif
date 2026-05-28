package suse_registry

import "testing"

func TestPickLatestSemver(t *testing.T) {
	cases := []struct {
		name    string
		tags    []string
		wantTag string
		wantOK  bool
	}{
		{"single-semver", []string{"1.0.0"}, "1.0.0", true},
		{"multiple-semver-out-of-order", []string{"1.0.0", "2.0.0", "1.5.0"}, "2.0.0", true},
		{"semver-needs-numeric-sort", []string{"9.0.0", "10.0.0"}, "10.0.0", true},
		{"with-v-prefix", []string{"v1.0.0", "v1.2.0"}, "v1.2.0", true},
		{"mixed-v-and-bare", []string{"1.0.0", "v1.2.0"}, "v1.2.0", true},
		{"prerelease-loses-to-release", []string{"1.0.0-rc1", "1.0.0"}, "1.0.0", true},
		{"prerelease-among-prereleases", []string{"1.0.0-rc1", "1.0.0-rc2"}, "1.0.0-rc2", true},
		{"non-semver-skipped", []string{"1.0.0", "latest", "main"}, "1.0.0", true},
		{"all-non-semver", []string{"latest", "main", "dev"}, "", false},
		{"empty-input", []string{}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := pickLatestSemver(tc.tags)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (got tag %q)", ok, tc.wantOK, got)
			}
			if got != tc.wantTag {
				t.Fatalf("tag = %q, want %q", got, tc.wantTag)
			}
		})
	}
}
