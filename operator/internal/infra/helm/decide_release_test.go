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
	"errors"
	"testing"

	"helm.sh/helm/v3/pkg/release"
)

func info(revision int, version string, status ReleaseStatus) *ReleaseInfo {
	return &ReleaseInfo{Version: version, Status: status, Revision: revision}
}

func staticDeployed(deployed *ReleaseInfo) func() (*ReleaseInfo, error) {
	return func() (*ReleaseInfo, error) { return deployed, nil }
}

func TestDecideRelease(t *testing.T) {
	spec := ReleaseSpec{Name: testRelName, Version: "2.0.1"}

	tests := []struct {
		name     string
		last     *ReleaseInfo
		deployed *ReleaseInfo
		spec     ReleaseSpec
		want     releaseAction
	}{
		{
			name: "never installed",
			last: nil,
			spec: spec,
			want: actionInstall,
		},
		{
			// The regression this whole change exists for: install 2.0.1, downgrade
			// to 2.0.0, ask for 2.0.1 again. Reading the oldest revision made this
			// compare equal and skip.
			name:     "upgrade after downgrade",
			last:     info(2, "2.0.0", StatusDeployed),
			deployed: info(2, "2.0.0", StatusDeployed),
			spec:     spec,
			want:     actionUpgrade,
		},
		{
			// A failed revision carries the version Helm *attempted*. Deciding from
			// it would report the upgrade as already applied and never retry.
			name:     "failed revision above a deployed one is retried",
			last:     info(2, "2.0.1", StatusFailed),
			deployed: info(1, "2.0.0", StatusDeployed),
			spec:     spec,
			want:     actionUpgrade,
		},
		{
			name:     "deployed matches requested version",
			last:     info(1, "2.0.1", StatusDeployed),
			deployed: info(1, "2.0.1", StatusDeployed),
			spec:     spec,
			want:     actionSkip,
		},
		{
			name:     "superseded revision above a matching deployed one",
			last:     info(3, "2.0.1", StatusSuperseded),
			deployed: info(3, "2.0.1", StatusDeployed),
			spec:     spec,
			want:     actionSkip,
		},
		{
			name:     "values differ at the same version",
			last:     info(1, "2.0.1", StatusDeployed),
			deployed: &ReleaseInfo{Version: "2.0.1", Status: StatusDeployed, Revision: 1, Values: map[string]interface{}{"replicas": 1}},
			spec:     ReleaseSpec{Name: testRelName, Version: "2.0.1", Values: map[string]interface{}{"replicas": 2}},
			want:     actionUpgrade,
		},
		{
			name:     "first install failed, nothing ever deployed",
			last:     info(1, "2.0.1", StatusFailed),
			deployed: nil,
			spec:     spec,
			want:     actionUpgrade,
		},
		{
			name: "pending install",
			last: info(1, "2.0.1", StatusPendingInstall),
			spec: spec,
			want: actionPending,
		},
		{
			name: "pending upgrade",
			last: info(2, "2.0.1", StatusPendingUpgrade),
			spec: spec,
			want: actionPending,
		},
		{
			name: "pending rollback",
			last: info(3, "2.0.0", StatusPendingRollback),
			spec: spec,
			want: actionPending,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := decideRelease(tt.last, staticDeployed(tt.deployed), tt.spec)
			if err != nil {
				t.Fatalf("decideRelease() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("decideRelease() = %s, want %s", got, tt.want)
			}
		})
	}
}

// Install and pending are decided from `last` alone. Reading deployed state there
// would be a wasted API call on every poll of a wedged release.
func TestDecideReleaseSkipsDeployedLookupWhenUnusable(t *testing.T) {
	tests := []struct {
		name string
		last *ReleaseInfo
	}{
		{"never installed", nil},
		{"pending", info(1, "2.0.1", StatusPendingUpgrade)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			_, _, err := decideRelease(tt.last, func() (*ReleaseInfo, error) {
				called = true
				return nil, nil
			}, ReleaseSpec{Name: testRelName, Version: "2.0.1"})
			if err != nil {
				t.Fatalf("decideRelease() error = %v", err)
			}
			if called {
				t.Error("deployedFn was called; want the decision made from last alone")
			}
		})
	}
}

func TestDecideReleasePropagatesDeployedLookupError(t *testing.T) {
	wantErr := errors.New("storage unavailable")

	_, _, err := decideRelease(info(1, "2.0.0", StatusDeployed), func() (*ReleaseInfo, error) {
		return nil, wantErr
	}, ReleaseSpec{Name: testRelName, Version: "2.0.1"})

	if !errors.Is(err, wantErr) {
		t.Errorf("decideRelease() error = %v, want %v", err, wantErr)
	}
}

// End-to-end over real storage in the cluster's query order: install 2.0.1,
// downgrade to 2.0.0, then ask for 2.0.1 again. Also pins WHY it used to fail —
// deciding from `last` skips, deciding from `deployed` upgrades — so the two
// cannot be quietly swapped back.
func TestDecideReleaseUpgradeDowngradeUpgradeSequence(t *testing.T) {
	cfg := newNameOrderedTestConfig(t,
		testRelease(1, "2.0.1", release.StatusSuperseded),
		testRelease(2, "2.0.0", release.StatusDeployed),
	)
	spec := ReleaseSpec{Name: testRelName, Version: "2.0.1"}

	last, err := lastRelease(cfg, testRelName)
	if err != nil {
		t.Fatalf("lastRelease() error = %v", err)
	}

	got, deployed, err := decideRelease(last, func() (*ReleaseInfo, error) {
		return deployedRelease(cfg, testRelName)
	}, spec)
	if err != nil {
		t.Fatalf("decideRelease() error = %v", err)
	}
	if got != actionUpgrade {
		t.Errorf("decideRelease() = %s, want %s", got, actionUpgrade)
	}
	if deployed == nil || deployed.Revision != 2 {
		t.Fatalf("decideRelease() deployed = %+v, want revision 2", deployed)
	}

	// The old code's comparison, kept as a guard: revision 1 still carries 2.0.1,
	// so anything deciding from the oldest revision reports "nothing to do".
	oldest, err := cfg.Releases.Get(testRelName, 1)
	if err != nil {
		t.Fatalf("Get(revision 1) error = %v", err)
	}
	if releaseNeedsUpgrade(releaseInfoFrom(oldest), spec) {
		t.Error("revision 1 no longer compares equal to the request; " +
			"this test no longer reproduces the original bug")
	}
}
