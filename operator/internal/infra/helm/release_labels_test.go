/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package helm

import (
	"context"
	"testing"
)

const testLabel = "catalog.cattle.io/cluster-repo-name"

func withLabels(spec ReleaseSpec) ReleaseSpec {
	spec.Labels = map[string]string{testLabel: "aif-ui"}
	return spec
}

func assertLabelStored(t *testing.T, c *helmClient, spec ReleaseSpec) *ReleaseInfo {
	t.Helper()

	info, err := c.DeployedRelease(context.Background(), spec.Name)
	if err != nil || info == nil {
		t.Fatalf("DeployedRelease() = %+v, %v", info, err)
	}
	if got := info.Labels[testLabel]; got != "aif-ui" {
		t.Errorf("release label %s = %q, want aif-ui; labels = %v", testLabel, got, info.Labels)
	}
	return info
}

func TestEnsureReleaseInstallsWithLabels(t *testing.T) {
	c, _ := newCountingClient(t)
	spec := withLabels(testSpec("2.1.0", nil))

	if err := c.EnsureRelease(context.Background(), spec); err != nil {
		t.Fatalf("EnsureRelease() error = %v", err)
	}

	assertLabelStored(t, c, spec)
}

// A release installed before the spec asked for a label — every UI-plugin
// release from an operator that predates it — is at the requested version with
// the requested values, so it matches on everything the fast path compares, and
// the manifest it renders is identical, so the diff calls it up-to-date and the
// latch remembers that. Without its own branch the label is never written, and
// the extension stays a "Third-Party" card for as long as its version does not
// change.
func TestEnsureReleaseUpgradesAReleaseMissingARequestedLabel(t *testing.T) {
	c, counter := newCountingClient(t)
	ctx := context.Background()
	plain := testSpec("2.1.0", nil)

	if err := c.EnsureRelease(ctx, plain); err != nil {
		t.Fatalf("install error = %v", err)
	}

	spec := withLabels(plain)
	if err := c.EnsureRelease(ctx, spec); err != nil {
		t.Fatalf("EnsureRelease() error = %v", err)
	}
	info := assertLabelStored(t, c, spec)
	if info.Revision != 2 {
		t.Errorf("revision = %d, want 2: one upgrade to write the label", info.Revision)
	}

	// And it settles: the upgrade wrote what was asked for, so the next passes
	// take the fast path rather than upgrading again.
	afterUpgrade := counter.pulls
	for i := range reconcileTimes {
		if err := c.EnsureRelease(ctx, spec); err != nil {
			t.Fatalf("pass %d error = %v", i+1, err)
		}
	}
	info = assertLabelStored(t, c, spec)
	if info.Revision != 2 {
		t.Errorf("revision = %d after %d steady-state passes, want 2", info.Revision, reconcileTimes)
	}
	if counter.pulls != afterUpgrade {
		t.Errorf("steady state pulled %d more times, want 0", counter.pulls-afterUpgrade)
	}
}

// The labels Helm keeps for itself are not drift. Treating them as such would
// upgrade on every pass.
func TestLabelDrift(t *testing.T) {
	stored := &ReleaseInfo{
		Labels: map[string]string{"owner": "helm", "status": "deployed", testLabel: "aif-ui"},
	}

	tests := []struct {
		name string
		info *ReleaseInfo
		spec ReleaseSpec
		want bool
	}{
		{name: "nothing requested", info: stored, spec: ReleaseSpec{}, want: false},
		{name: "everything requested is present", info: stored, spec: withLabels(ReleaseSpec{}), want: false},
		{name: "no release yet", info: nil, spec: withLabels(ReleaseSpec{}), want: false},
		{name: "label missing", info: &ReleaseInfo{}, spec: withLabels(ReleaseSpec{}), want: true},
		{
			name: "label has another value",
			info: stored,
			spec: ReleaseSpec{Labels: map[string]string{testLabel: "other"}},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := labelDrift(tt.info, tt.spec); got != tt.want {
				t.Errorf("labelDrift() = %v, want %v", got, tt.want)
			}
		})
	}
}
