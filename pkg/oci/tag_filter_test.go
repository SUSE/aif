package oci

import "testing"

func TestIsSigstoreTag(t *testing.T) {
	cases := []struct {
		name string
		tag  string
		want bool
	}{
		// Cosign-shaped tags MUST be dropped.
		{"sig", "sha256-2da536d9d3e093af219f235df2dbdac6d948f4536f11f69342f78ee6c2f7d911.sig", true},
		{"att", "sha256-2da536d9d3e093af219f235df2dbdac6d948f4536f11f69342f78ee6c2f7d911.att", true},
		{"sbom", "sha256-2da536d9d3e093af219f235df2dbdac6d948f4536f11f69342f78ee6c2f7d911.sbom", true},

		// Real chart tags MUST NOT be dropped.
		{"semver", "1.0.0", false},
		{"semver-with-v", "v1.0.0", false},
		{"semver-prerelease", "1.0.0-rc1", false},
		{"appco-chart-tag", "1.55.0-13.1", false},

		// Boundary cases.
		{"short-hex", "sha256-abc.sig", false},
		{"uppercase-hex", "sha256-2DA536D9D3E093AF219F235DF2DBDAC6D948F4536F11F69342F78EE6C2F7D911.sig", false},
		{"wrong-prefix", "md5-2da536d9d3e093af219f235df2dbdac6d948f4536f11f69342f78ee6c2f7d911.sig", false},
		{"wrong-suffix", "sha256-2da536d9d3e093af219f235df2dbdac6d948f4536f11f69342f78ee6c2f7d911.txt", false},
		{"no-suffix", "sha256-2da536d9d3e093af219f235df2dbdac6d948f4536f11f69342f78ee6c2f7d911", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSigstoreTag(tc.tag); got != tc.want {
				t.Fatalf("isSigstoreTag(%q) = %v, want %v", tc.tag, got, tc.want)
			}
		})
	}
}
