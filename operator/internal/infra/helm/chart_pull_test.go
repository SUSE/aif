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
	"io"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/kube"
	kubefake "helm.sh/helm/v3/pkg/kube/fake"
	"helm.sh/helm/v3/pkg/registry"
	"helm.sh/helm/v3/pkg/release"
)

const (
	testChartRef = "oci://ghcr.io/suse/chart/aif-ui"
	// testRegistry is the host testChartRef resolves to, which is what the
	// counters are labelled with.
	testRegistry = "ghcr.io"
)

// pullCounter is a chart fetcher that serves a chart from memory and records
// every call. It stands in for the registry so a test can assert on how many
// times the operator would have reached out to it.
type pullCounter struct {
	// dir stands in for settings.RepositoryCache: where a pull leaves the
	// artifact it downloaded.
	dir      string
	pulls    int
	versions []string
	// chartVersion is the version recorded in the served chart's Chart.yaml.
	// Empty means serve the version that was requested — a registry whose tags
	// and chart versions agree.
	chartVersion string
}

// fetch mirrors what a real pull does, including the two parts the chart cache
// depends on.
//
// The chart handed back is the one parsed off the artifact just written, not the
// in-memory value it was built from. A cache hit re-parses the same bytes, so
// hits and misses have to produce identical charts, and they only do if a miss
// goes through the archive too.
//
// And the artifact is written under a name taken from the chart and its version,
// which is what Helm does — DownloadTo names a download filepath.Base of the
// resolved URL, dropping the registry and the org path — so two references to one
// chart land on one file and the second pull overwrites the first. Serving those
// two different bytes is the only way a test can tell which pull a hit came from.
func (p *pullCounter) fetch(
	_ func(*registry.Client),
	_ *action.ChartPathOptions,
	spec ReleaseSpec,
) (*chart.Chart, []byte, error) {
	p.pulls++
	p.versions = append(p.versions, spec.Version)
	served := p.chartVersion
	if served == "" {
		served = spec.Version
	}

	pulled := testChart(served)
	pulled.Metadata.Annotations = map[string]string{pulledFromAnnotation: spec.ChartRef}

	path, err := chartutil.Save(pulled, p.dir)
	if err != nil {
		return nil, nil, err
	}
	return loadLocalChart(path)
}

// pulledFromAnnotation records, in the served chart, the reference the pull that
// produced it asked for. Two registries can serve genuinely different charts
// under one name and version — an origin and a stale mirror, most obviously — and
// nothing else about the bytes says which one answered.
const pulledFromAnnotation = "test.aif/pulled-from"

// testChart renders both the chart version and a value, so that a change to
// either moves the stored manifest — the signal EnsureRelease's diff gate reads.
func testChart(version string) *chart.Chart {
	return &chart.Chart{
		Metadata: &chart.Metadata{
			APIVersion: chart.APIVersionV2,
			Name:       testRelName,
			Version:    version,
		},
		Templates: []*chart.File{{
			Name: "templates/configmap.yaml",
			Data: []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: aif-ui
data:
  version: "{{ .Chart.Version }}"
  replicas: "{{ .Values.replicas }}"
`),
		}},
	}
}

// newCountingClient returns a helmClient wired to in-memory storage and an
// in-memory chart, so EnsureRelease can be driven end to end — install, render,
// diff, upgrade — with every chart pull counted and none of them leaving the
// process.
//
// Storage uses the name-ordered driver on purpose. Pull counts are decided by
// which revision the release lookups return, and the memory driver's numeric
// ordering hides the very misordering that caused the over-pulling.
func newCountingClient(t *testing.T, rels ...*release.Release) (*helmClient, *pullCounter) {
	t.Helper()

	return newCountingClientWithKube(t, &kubefake.FailingKubeClient{
		PrintingKubeClient: kubefake.PrintingKubeClient{Out: io.Discard},
	}, rels...)
}

// newCountingClientWithKube is newCountingClient with the cluster side swapped
// out. Only the shutdown-grace tests need that seam — they turn on the one
// property the printing client cannot express, which is that applying takes
// time.
func newCountingClientWithKube(
	t *testing.T,
	kubeClient kube.Interface,
	rels ...*release.Release,
) (*helmClient, *pullCounter) {
	t.Helper()

	cfg := newNameOrderedTestConfig(t, rels...)
	cfg.KubeClient = kubeClient
	cfg.Capabilities = chartutil.DefaultCapabilities
	cfg.Log = func(format string, v ...interface{}) {}

	counter := &pullCounter{dir: t.TempDir()}
	c := &helmClient{
		settings: cli.New(),
		actionConfigFn: func(context.Context, string) (*action.Configuration, error) {
			return cfg, nil
		},
		fetchChartFn: counter.fetch,
	}
	return c, counter
}

func testSpec(version string, values map[string]interface{}) ReleaseSpec {
	return ReleaseSpec{
		Name:      testRelName,
		Namespace: testNamespace,
		ChartRef:  testChartRef,
		Version:   version,
		Values:    values,
	}
}

// The core property: once a release matches what was asked for, reconciling it
// again must not touch the registry. The over-pull defect was never a wrong
// result — every reconcile converged on "up-to-date" — so no assertion on
// release state could catch it. Only the pull count can.
func TestEnsureReleaseDoesNotPullOnceConverged(t *testing.T) {
	c, counter := newCountingClient(t)
	spec := testSpec("2.1.0", map[string]interface{}{"replicas": float64(2)})
	ctx := context.Background()

	if err := c.EnsureRelease(ctx, spec); err != nil {
		t.Fatalf("EnsureRelease() install error = %v", err)
	}
	if counter.pulls != 1 {
		t.Fatalf("install pulled %d times, want 1", counter.pulls)
	}

	// The controller re-reconciles on a health-check timer with an unchanged
	// spec. Ten passes stands in for that steady state.
	for i := range 10 {
		if err := c.EnsureRelease(ctx, spec); err != nil {
			t.Fatalf("EnsureRelease() steady-state pass %d error = %v", i+1, err)
		}
	}

	if counter.pulls != 1 {
		t.Errorf("steady state pulled %d times in total, want 1 (the install); "+
			"versions pulled: %v", counter.pulls, counter.versions)
	}
}

// The regression that motivated the release-lookup fix, expressed as registry
// traffic rather than as a decision enum.
//
// Revision 1 carries 2.0.1 and revision 2 — the deployed one — carries 2.0.0.
// Reading the head of the driver's name-ordered query returns revision 1, so a
// request for the version that is already deployed looked like drift, fell
// through to the manifest diff, and pulled the chart. Forever, once per
// reconcile, because nothing about the cluster ever changed to stop it.
func TestEnsureReleaseDoesNotPullWhenDeployedRevisionMatches(t *testing.T) {
	c, counter := newCountingClient(t,
		testRelease(1, "2.0.1", release.StatusSuperseded),
		testRelease(2, "2.0.0", release.StatusDeployed),
	)

	if err := c.EnsureRelease(context.Background(), testSpec("2.0.0", nil)); err != nil {
		t.Fatalf("EnsureRelease() error = %v", err)
	}

	if counter.pulls != 0 {
		t.Errorf("pulled %d times for a release already deployed at 2.0.0, want 0; "+
			"the requested version is being compared against the wrong revision", counter.pulls)
	}
}

// The counterpart: a version that genuinely differs from the deployed one must
// still pull. Without this, "never pull" would pass the test above.
func TestEnsureReleasePullsWhenDeployedVersionDiffers(t *testing.T) {
	c, counter := newCountingClient(t,
		testRelease(1, "2.0.1", release.StatusSuperseded),
		testRelease(2, "2.0.0", release.StatusDeployed),
	)

	if err := c.EnsureRelease(context.Background(), testSpec("2.0.1", nil)); err != nil {
		t.Fatalf("EnsureRelease() error = %v", err)
	}

	if counter.pulls == 0 {
		t.Fatal("pulled 0 times for an upgrade from 2.0.0 to 2.0.1, want the upgrade to happen")
	}
	for _, v := range counter.versions {
		if v != "2.0.1" {
			t.Errorf("pulled version %q, want the requested 2.0.1", v)
		}
	}
}

// An upgrade renders the candidate manifest for the diff and then applies it,
// and both steps need the chart. They must share one download: the two are
// microseconds apart and asking the registry twice for the same tag is the one
// piece of repeated traffic the convergence latch cannot remove, because both
// pulls belong to the same reconcile.
func TestEnsureReleaseUpgradePullsOnce(t *testing.T) {
	c, counter := newCountingClient(t)
	ctx := context.Background()
	values := map[string]interface{}{"replicas": float64(2)}

	if err := c.EnsureRelease(ctx, testSpec("2.1.0", values)); err != nil {
		t.Fatalf("EnsureRelease() install error = %v", err)
	}
	counter.pulls = 0

	if err := c.EnsureRelease(ctx, testSpec("2.1.1", values)); err != nil {
		t.Fatalf("EnsureRelease() upgrade error = %v", err)
	}
	if counter.pulls != 1 {
		t.Errorf("upgrade pulled %d times, want 1 shared by the render and the upgrade", counter.pulls)
	}

	// And having upgraded, it settles.
	if err := c.EnsureRelease(ctx, testSpec("2.1.1", values)); err != nil {
		t.Fatalf("EnsureRelease() post-upgrade error = %v", err)
	}
	if counter.pulls != 1 {
		t.Errorf("post-upgrade pass pulled again, total %d, want 1", counter.pulls)
	}
}

// The metric has to count the same event the tests above count, or it will
// report a healthy operator while the registry says otherwise.
func TestChartPullsTotalTracksActualPulls(t *testing.T) {
	chartPullsTotal.Reset()
	t.Cleanup(chartPullsTotal.Reset)

	c, counter := newCountingClient(t)
	spec := testSpec("2.1.0", map[string]interface{}{"replicas": float64(1)})
	ctx := context.Background()

	if err := c.EnsureRelease(ctx, spec); err != nil {
		t.Fatalf("EnsureRelease() install error = %v", err)
	}
	if err := c.EnsureRelease(ctx, spec); err != nil {
		t.Fatalf("EnsureRelease() steady-state error = %v", err)
	}

	got := testutil.ToFloat64(chartPullsTotal.WithLabelValues(testRegistry, testChartRef, "2.1.0"))
	if got != float64(counter.pulls) {
		t.Errorf("aif_helm_chart_pulls_total = %v, want %d to match the pulls performed", got, counter.pulls)
	}
	if got != 1 {
		t.Errorf("aif_helm_chart_pulls_total = %v, want 1", got)
	}
}
