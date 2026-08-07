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
	"fmt"
	"sort"
	"testing"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
)

const (
	testNamespace = "cattle-ui-plugin-system"
	testRelName   = "aif-ui-server"
)

// newTestConfig returns an action.Configuration backed by Helm's in-memory
// storage driver, seeded with the supplied revisions.
//
// The memory driver sorts query results numerically by revision (records.Less),
// which is NOT what a real cluster does. That is fine for tests that do not
// depend on query order; anything that does must use newNameOrderedTestConfig.
func newTestConfig(t *testing.T, rels ...*release.Release) *action.Configuration {
	t.Helper()

	mem := driver.NewMemory()
	mem.SetNamespace(testNamespace)
	return seedConfig(t, mem, rels...)
}

// nameOrderedDriver reproduces the query order a real cluster returns.
//
// Helm stores each revision as a Secret named sh.helm.release.v1.<release>.v<N>,
// and the Secret driver's Query is a List, so the API server returns them sorted
// lexicographically BY NAME: .v1, .v10, .v11, .v2. The memory driver sorts
// numerically by revision instead, so a test built on it cannot tell "sorts
// correctly" apart from "was handed an already-sorted slice" — precisely the
// confusion that let this bug through in the first place.
type nameOrderedDriver struct {
	*driver.Memory
}

func (d *nameOrderedDriver) Query(labels map[string]string) ([]*release.Release, error) {
	rels, err := d.Memory.Query(labels)
	if err != nil {
		return nil, err
	}
	sort.Slice(rels, func(i, j int) bool {
		return secretName(rels[i]) < secretName(rels[j])
	})
	return rels, nil
}

// secretName mirrors the release Secret naming the Secret driver uses (makeKey).
func secretName(rel *release.Release) string {
	return fmt.Sprintf("sh.helm.release.v1.%s.v%d", rel.Name, rel.Version)
}

// newNameOrderedTestConfig seeds storage that hands back revisions in the Secret
// driver's lexicographic-by-name order rather than sorted by revision.
func newNameOrderedTestConfig(t *testing.T, rels ...*release.Release) *action.Configuration {
	t.Helper()

	mem := driver.NewMemory()
	mem.SetNamespace(testNamespace)
	return seedConfig(t, &nameOrderedDriver{Memory: mem}, rels...)
}

func seedConfig(t *testing.T, d driver.Driver, rels ...*release.Release) *action.Configuration {
	t.Helper()

	store := storage.Init(d)
	for _, rel := range rels {
		if err := store.Create(rel); err != nil {
			t.Fatalf("seeding revision %d: %v", rel.Version, err)
		}
	}
	return &action.Configuration{Releases: store}
}

func testRelease(revision int, chartVersion string, status release.Status) *release.Release {
	return &release.Release{
		Name:      testRelName,
		Namespace: testNamespace,
		Version:   revision,
		Info:      &release.Info{Status: status},
		Chart: &chart.Chart{
			Metadata: &chart.Metadata{Name: testRelName, Version: chartVersion},
		},
	}
}

// Reproduces the upgrade -> downgrade -> upgrade failure. After installing 2.0.1
// and downgrading to 2.0.0, revision 1 still carries chart version 2.0.1. Reading
// the head of the unsorted history returned revision 1, so re-requesting 2.0.1
// compared equal, EnsureRelease reported "version and values unchanged" and
// skipped the upgrade — leaving the cluster on 2.0.0.
func TestLastReleaseReturnsNewestRevisionAfterDowngrade(t *testing.T) {
	cfg := newTestConfig(t,
		testRelease(1, "2.0.1", release.StatusSuperseded),
		testRelease(2, "2.0.0", release.StatusDeployed),
	)

	info, err := lastRelease(cfg, testRelName)
	if err != nil {
		t.Fatalf("lastRelease() error = %v", err)
	}
	if info == nil {
		t.Fatal("lastRelease() = nil, want the newest revision")
	}
	if info.Revision != 2 || info.Version != "2.0.0" {
		t.Errorf("lastRelease() = revision %d version %q, want revision 2 version \"2.0.0\"",
			info.Revision, info.Version)
	}
}

// Release Secrets sort lexicographically, so .v10 and .v11 precede .v2. Guards
// against a fix that only works while revision counts stay in single digits.
//
// Uses the name-ordered driver: on the memory driver's numeric ordering this
// assertion would hold even for code that just takes the last element of the
// query result, so it would not pin anything.
func TestLastReleaseWithDoubleDigitRevisions(t *testing.T) {
	rels := make([]*release.Release, 0, 11)
	for i := 1; i <= 11; i++ {
		status := release.StatusSuperseded
		if i == 11 {
			status = release.StatusDeployed
		}
		rels = append(rels, testRelease(i, fmt.Sprintf("2.0.%d", i), status))
	}

	cfg := newNameOrderedTestConfig(t, rels...)

	// Pin the premise: if storage ever starts handing back revision-sorted
	// results, the rest of this test silently stops proving anything.
	raw, err := cfg.Releases.History(testRelName)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	gotOrder := make([]int, 0, len(raw))
	for _, rel := range raw {
		gotOrder = append(gotOrder, rel.Version)
	}
	wantOrder := []int{1, 10, 11, 2, 3, 4, 5, 6, 7, 8, 9}
	if fmt.Sprint(gotOrder) != fmt.Sprint(wantOrder) {
		t.Fatalf("query order = %v, want lexicographic %v", gotOrder, wantOrder)
	}

	info, err := lastRelease(cfg, testRelName)
	if err != nil {
		t.Fatalf("lastRelease() error = %v", err)
	}
	if info.Revision != 11 || info.Version != "2.0.11" {
		t.Errorf("lastRelease() = revision %d version %q, want revision 11 version \"2.0.11\"",
			info.Revision, info.Version)
	}
}

// The head of the query result is revision 1, not the newest — the exact trap the
// old GetRelease fell into. Pins it independently of any single lookup helper.
func TestDeployedReleaseWithNameOrderedStorage(t *testing.T) {
	cfg := newNameOrderedTestConfig(t,
		testRelease(1, "2.0.1", release.StatusSuperseded),
		testRelease(2, "2.0.0", release.StatusDeployed),
	)

	deployed, err := deployedRelease(cfg, testRelName)
	if err != nil {
		t.Fatalf("deployedRelease() error = %v", err)
	}
	if deployed.Revision != 2 || deployed.Version != "2.0.0" {
		t.Errorf("deployedRelease() = revision %d version %q, want revision 2 version \"2.0.0\"",
			deployed.Revision, deployed.Version)
	}
}

func TestLastReleaseAbsentReleaseReturnsNil(t *testing.T) {
	info, err := lastRelease(newTestConfig(t), testRelName)
	if err != nil {
		t.Fatalf("lastRelease() error = %v, want nil for an absent release", err)
	}
	if info != nil {
		t.Errorf("lastRelease() = %+v, want nil", info)
	}
}

// A failed upgrade leaves the newer revision carrying the requested version while
// the cluster still runs the previous one. Drift must be measured against what is
// deployed, or the retry is skipped and the extension stays pinned to the old
// version while the operator reports success.
func TestDeployedReleaseIgnoresFailedNewerRevision(t *testing.T) {
	cfg := newTestConfig(t,
		testRelease(1, "2.0.0", release.StatusDeployed),
		testRelease(2, "2.0.1", release.StatusFailed),
	)

	last, err := lastRelease(cfg, testRelName)
	if err != nil {
		t.Fatalf("lastRelease() error = %v", err)
	}
	if last.Version != "2.0.1" {
		t.Errorf("lastRelease() version = %q, want \"2.0.1\" (the failed attempt)", last.Version)
	}

	deployed, err := deployedRelease(cfg, testRelName)
	if err != nil {
		t.Fatalf("deployedRelease() error = %v", err)
	}
	if deployed == nil {
		t.Fatal("deployedRelease() = nil, want revision 1")
	}
	if deployed.Revision != 1 || deployed.Version != "2.0.0" {
		t.Errorf("deployedRelease() = revision %d version %q, want revision 1 version \"2.0.0\"",
			deployed.Revision, deployed.Version)
	}

	// The whole point: against the deployed release, 2.0.1 is still outstanding.
	if !releaseNeedsUpgrade(deployed, ReleaseSpec{Version: "2.0.1"}) {
		t.Error("releaseNeedsUpgrade(deployed, 2.0.1) = false, want true so the failed upgrade is retried")
	}
}

// The manifest diff is the authoritative upgrade gate, so it has to compare
// against the deployed revision for the same reason the drift check does. A
// failed revision records the manifest Helm attempted; diffing against that
// would report "up-to-date" for an upgrade that never reached the cluster.
func TestDeployedManifestIgnoresFailedNewerRevision(t *testing.T) {
	deployedRev := testRelease(1, "2.0.0", release.StatusDeployed)
	deployedRev.Manifest = "image: aif:2.0.0"
	failedRev := testRelease(2, "2.0.1", release.StatusFailed)
	failedRev.Manifest = "image: aif:2.0.1"

	manifest, err := deployedManifest(newTestConfig(t, deployedRev, failedRev), testRelName)
	if err != nil {
		t.Fatalf("deployedManifest() error = %v", err)
	}
	if manifest != deployedRev.Manifest {
		t.Errorf("deployedManifest() = %q, want %q", manifest, deployedRev.Manifest)
	}
}

func TestDeployedReleaseNoneDeployedReturnsNil(t *testing.T) {
	cfg := newTestConfig(t, testRelease(1, "2.0.0", release.StatusFailed))

	deployed, err := deployedRelease(cfg, testRelName)
	if err != nil {
		t.Fatalf("deployedRelease() error = %v, want nil when nothing is deployed", err)
	}
	if deployed != nil {
		t.Errorf("deployedRelease() = %+v, want nil", deployed)
	}
}

func TestDeployedReleaseAbsentReleaseReturnsNil(t *testing.T) {
	deployed, err := deployedRelease(newTestConfig(t), testRelName)
	if err != nil {
		t.Fatalf("deployedRelease() error = %v, want nil for an absent release", err)
	}
	if deployed != nil {
		t.Errorf("deployedRelease() = %+v, want nil", deployed)
	}
}

func TestReleaseStatusIsPending(t *testing.T) {
	tests := []struct {
		status ReleaseStatus
		want   bool
	}{
		{StatusPendingInstall, true},
		{StatusPendingUpgrade, true},
		{StatusPendingRollback, true},
		{StatusDeployed, false},
		{StatusFailed, false},
		{StatusSuperseded, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.IsPending(); got != tt.want {
				t.Errorf("ReleaseStatus(%q).IsPending() = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}
