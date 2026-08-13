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
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/registry"
)

const (
	// reconcileTimes drives the number of steady-state passes the tests below
	// use. Large enough that a per-pass pull is unmistakable, small enough to
	// stay fast.
	reconcileTimes = 10

	// mismatchedChartVersion is what the registry records in Chart.yaml while
	// the CR pins testSpec's version. The disagreement is the point: Helm stores
	// this value, so the release can never compare equal to the spec.
	mismatchedChartVersion = "2.1.0-build.7"
)

// noChartCache makes every fetch advertise no reusable artifact, the way the
// in-memory TLS pull path does, which leaves the chart cache empty.
//
// The tests below count pulls to detect whether a render happened at all, and
// that is the one thing the cache is built to hide: with it in play they would
// report a flat pull count whether the latch held or not. Two mechanisms remove
// the same traffic for different reasons and each needs measuring on its own —
// the cache has its own tests, in chart_cache_test.go.
func noChartCache(c *helmClient, counter *pullCounter) {
	c.fetchChartFn = func(
		setRegistry func(*registry.Client),
		opts *action.ChartPathOptions,
		spec ReleaseSpec,
	) (*chart.Chart, string, error) {
		ch, _, err := counter.fetch(setRegistry, opts, spec)
		return ch, "", err
	}
}

// backdateConvergence ages every latched verdict by d, standing in for time
// passing.
func backdateConvergence(t *testing.T, c *helmClient, d time.Duration) {
	t.Helper()

	aged := 0
	c.converged.Range(func(k, v interface{}) bool {
		at, ok := v.(convergedAt)
		if !ok {
			return true
		}
		at.provenAt = at.provenAt.Add(-d)
		c.converged.Store(k, at)
		aged++
		return true
	})
	if aged == 0 {
		t.Fatal("nothing was latched, so ageing the latch proves nothing")
	}
}

func configOf(t *testing.T, c *helmClient) *action.Configuration {
	t.Helper()
	cfg, err := c.actionConfigFn(context.Background(), testNamespace)
	if err != nil {
		t.Fatalf("actionConfig: %v", err)
	}
	return cfg
}

// The defect that survives the release-lookup fix.
//
// The registry serves a chart whose Chart.yaml says 2.1.0-build.7 while the CR
// pins the tag 2.1.0. Helm stores the chart's version, so the deployed release
// reads 2.1.0-build.7 forever and never compares equal to the spec. Every
// reconcile therefore decides an upgrade is needed, pulls the chart to render
// it, finds the manifest identical, and skips — having changed nothing, so the
// next pass repeats it. Once per health check, for the life of the CR.
func TestChartVersionMismatchPullsOnceNotEveryPass(t *testing.T) {
	c, counter := newCountingClient(t)
	noChartCache(c, counter)
	counter.chartVersion = mismatchedChartVersion
	spec := testSpec("2.1.0", map[string]interface{}{"replicas": float64(1)})
	ctx := context.Background()

	if err := c.EnsureRelease(ctx, spec); err != nil {
		t.Fatalf("install error = %v", err)
	}
	afterInstall := counter.pulls

	for i := range reconcileTimes {
		if err := c.EnsureRelease(ctx, spec); err != nil {
			t.Fatalf("pass %d error = %v", i+1, err)
		}
	}

	// One render to prove the release is up-to-date despite the version
	// disagreement, then never again while nothing changes.
	steadyState := counter.pulls - afterInstall
	if steadyState > 1 {
		t.Errorf("%d pulls across %d steady-state passes, want at most 1; "+
			"a release that cannot converge is being re-verified on every pass",
			steadyState, reconcileTimes)
	}
}

// The other way a release cannot converge: a values key the chart never
// references. Nothing renders differently, so the upgrade that would have
// written the values into storage is skipped, so storage keeps disagreeing.
func TestUnusedValuesKeyPullsOnceNotEveryPass(t *testing.T) {
	c, counter := newCountingClient(t)
	noChartCache(c, counter)
	ctx := context.Background()

	installed := testSpec("2.1.0", map[string]interface{}{"replicas": float64(1)})
	if err := c.EnsureRelease(ctx, installed); err != nil {
		t.Fatalf("install error = %v", err)
	}
	afterInstall := counter.pulls

	// testChart's template reads only .Values.replicas, so this key changes
	// nothing about the rendered manifest.
	withUnused := testSpec("2.1.0", map[string]interface{}{
		"replicas":      float64(1),
		"unusedByChart": "x",
	})
	for i := range reconcileTimes {
		if err := c.EnsureRelease(ctx, withUnused); err != nil {
			t.Fatalf("pass %d error = %v", i+1, err)
		}
	}

	steadyState := counter.pulls - afterInstall
	if steadyState > 1 {
		t.Errorf("%d pulls across %d steady-state passes, want at most 1", steadyState, reconcileTimes)
	}
}

// The latch must not become a way to miss real work. Everything that can change
// what the chart renders has to invalidate it.
func TestConvergenceLatchInvalidation(t *testing.T) {
	base := testSpec("2.1.0", map[string]interface{}{"replicas": float64(1)})

	tests := []struct {
		name string
		next ReleaseSpec
	}{
		{
			name: "requested version changes",
			next: testSpec("2.2.0", map[string]interface{}{"replicas": float64(1)}),
		},
		{
			name: "values change",
			next: testSpec("2.1.0", map[string]interface{}{"replicas": float64(3)}),
		},
		{
			name: "chart reference changes",
			next: func() ReleaseSpec {
				s := testSpec("2.1.0", map[string]interface{}{"replicas": float64(1)})
				s.ChartRef = "oci://registry.example.com/aif-ui"
				return s
			}(),
		},
		{
			name: "repo url changes",
			next: func() ReleaseSpec {
				s := testSpec("2.1.0", map[string]interface{}{"replicas": float64(1)})
				s.RepoURL = "https://charts.example.com"
				return s
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, counter := newCountingClient(t)
			noChartCache(c, counter)
			counter.chartVersion = mismatchedChartVersion // forces the latch to engage
			ctx := context.Background()

			if err := c.EnsureRelease(ctx, base); err != nil {
				t.Fatalf("install error = %v", err)
			}
			// Latch it, then confirm it is actually holding.
			for range 2 {
				if err := c.EnsureRelease(ctx, base); err != nil {
					t.Fatalf("latching pass error = %v", err)
				}
			}
			latched := counter.pulls
			if err := c.EnsureRelease(ctx, base); err != nil {
				t.Fatalf("post-latch error = %v", err)
			}
			if counter.pulls != latched {
				t.Fatalf("the latch is not holding, so this test proves nothing")
			}

			if err := c.EnsureRelease(ctx, tt.next); err != nil {
				t.Fatalf("changed-spec error = %v", err)
			}
			if counter.pulls == latched {
				t.Error("the changed spec was not pulled; a stale verdict is suppressing real work")
			}
		})
	}
}

// A release changed underneath the operator — a rollback, or a human running
// helm by hand — moves the deployed revision. The verdict was proven against
// the old one and says nothing about the new one.
func TestConvergenceLatchInvalidatedByANewDeployedRevision(t *testing.T) {
	c, counter := newCountingClient(t)
	noChartCache(c, counter)
	counter.chartVersion = mismatchedChartVersion
	spec := testSpec("2.1.0", map[string]interface{}{"replicas": float64(1)})
	ctx := context.Background()

	if err := c.EnsureRelease(ctx, spec); err != nil {
		t.Fatalf("install error = %v", err)
	}
	for range 2 {
		if err := c.EnsureRelease(ctx, spec); err != nil {
			t.Fatalf("latching pass error = %v", err)
		}
	}
	latched := counter.pulls

	// Someone else deploys a new revision.
	cfg := configOf(t, c)
	current, err := cfg.Releases.Deployed(testRelName)
	if err != nil {
		t.Fatalf("Deployed() error = %v", err)
	}
	next := testRelease(current.Version+1, mismatchedChartVersion, "deployed")
	next.Manifest = "totally different"
	next.Config = spec.Values
	if err := cfg.Releases.Create(next); err != nil {
		t.Fatalf("seeding a new revision: %v", err)
	}

	if err := c.EnsureRelease(ctx, spec); err != nil {
		t.Fatalf("post-change error = %v", err)
	}
	if counter.pulls == latched {
		t.Error("the new revision was not re-verified; the verdict outlived the release it was proven against")
	}
}

// The one way a release is replaced without its revision moving: helm uninstall
// followed by helm install starts again at revision 1. A latch keyed on the
// revision alone matches, so the operator goes on skipping upgrades for a
// release it never verified — and this is exactly the case where it must not,
// because the chart now deployed is not the one the CR asks for. Before the
// latch existed the render would have caught it on the next pass, so this is a
// regression the latch could introduce rather than a pre-existing gap.
func TestConvergenceLatchInvalidatedByAReinstallAtTheSameRevision(t *testing.T) {
	c, counter := newCountingClient(t)
	noChartCache(c, counter)
	counter.chartVersion = mismatchedChartVersion
	spec := testSpec("2.1.0", map[string]interface{}{"replicas": float64(1)})
	ctx := context.Background()

	if err := c.EnsureRelease(ctx, spec); err != nil {
		t.Fatalf("install error = %v", err)
	}
	for range 2 {
		if err := c.EnsureRelease(ctx, spec); err != nil {
			t.Fatalf("latching pass error = %v", err)
		}
	}
	latched := counter.pulls
	if err := c.EnsureRelease(ctx, spec); err != nil {
		t.Fatalf("post-latch error = %v", err)
	}
	if counter.pulls != latched {
		t.Fatalf("the latch is not holding, so this test proves nothing")
	}

	// Someone uninstalls the release and installs a different chart under the
	// same name. Storage is back to a single revision 1, as it was when the
	// verdict was proven.
	cfg := configOf(t, c)
	current, err := cfg.Releases.Deployed(testRelName)
	if err != nil {
		t.Fatalf("Deployed() error = %v", err)
	}
	if _, err := cfg.Releases.Delete(testRelName, current.Version); err != nil {
		t.Fatalf("uninstalling: %v", err)
	}
	replacement := testRelease(current.Version, mismatchedChartVersion, "deployed")
	replacement.Chart.Metadata.Name = "a-completely-different-chart"
	replacement.Manifest = "totally different"
	replacement.Config = spec.Values
	if err := cfg.Releases.Create(replacement); err != nil {
		t.Fatalf("seeding the reinstalled release: %v", err)
	}

	if err := c.EnsureRelease(ctx, spec); err != nil {
		t.Fatalf("post-reinstall error = %v", err)
	}
	if counter.pulls == latched {
		t.Error("the reinstalled release was not re-verified; a verdict proven against the old release is suppressing the upgrade that would restore the requested chart")
	}
}

// The falsifier no key can cover: a chart re-pushed under a tag already in use.
// Storage, the CR and the requested version are all byte-identical before and
// after, so every fingerprint the latch holds still matches and no invalidation
// can fire. Expiry is the only thing that brings the operator back to the
// registry, which is why the latch has to have one at all.
func TestConvergenceLatchIsRederivedOnceTheVerdictExpires(t *testing.T) {
	c, counter := newCountingClient(t)
	noChartCache(c, counter)
	counter.chartVersion = mismatchedChartVersion
	spec := testSpec("2.1.0", map[string]interface{}{"replicas": float64(1)})
	ctx := context.Background()

	if err := c.EnsureRelease(ctx, spec); err != nil {
		t.Fatalf("install error = %v", err)
	}
	for range 2 {
		if err := c.EnsureRelease(ctx, spec); err != nil {
			t.Fatalf("latching pass error = %v", err)
		}
	}
	latched := counter.pulls
	if err := c.EnsureRelease(ctx, spec); err != nil {
		t.Fatalf("post-latch error = %v", err)
	}
	if counter.pulls != latched {
		t.Fatalf("the latch is not holding, so this test proves nothing")
	}

	backdateConvergence(t, c, convergenceTTL+time.Minute)

	if err := c.EnsureRelease(ctx, spec); err != nil {
		t.Fatalf("post-expiry error = %v", err)
	}
	if counter.pulls != latched+1 {
		t.Fatalf("%d pulls after the verdict expired, want %d; an expired verdict is still suppressing the render that would notice a re-pushed chart",
			counter.pulls-latched, 1)
	}

	// Re-derived, not abandoned: the fresh verdict has to latch in its turn, or
	// expiry would put the release back to pulling on every single pass.
	afterExpiry := counter.pulls
	for i := range reconcileTimes {
		if err := c.EnsureRelease(ctx, spec); err != nil {
			t.Fatalf("pass %d after expiry error = %v", i+1, err)
		}
	}
	if counter.pulls != afterExpiry {
		t.Errorf("%d pulls across %d passes after the verdict was re-derived, want 0; the re-derived verdict did not latch",
			counter.pulls-afterExpiry, reconcileTimes)
	}
}

// Silencing the pull loop must not silence the cause. The gauge is the only
// outward sign left once the chart stops being pulled every minute.
func TestUnconvergedGaugeReportsAChartVersionMismatch(t *testing.T) {
	releaseUnconverged.Reset()
	t.Cleanup(releaseUnconverged.Reset)

	c, counter := newCountingClient(t)
	counter.chartVersion = mismatchedChartVersion
	spec := testSpec("2.1.0", map[string]interface{}{"replicas": float64(1)})
	ctx := context.Background()

	if err := c.EnsureRelease(ctx, spec); err != nil {
		t.Fatalf("install error = %v", err)
	}
	if err := c.EnsureRelease(ctx, spec); err != nil {
		t.Fatalf("verify error = %v", err)
	}

	if got := testutil.ToFloat64(releaseUnconverged.WithLabelValues(testRelName)); got != 1 {
		t.Errorf("aif_helm_release_unconverged = %v, want 1", got)
	}
}

// The third way a release stays unconverged, and the one neither named cause
// covers: the chart now pulled matches the CR, but it renders exactly what is
// already deployed, so no upgrade runs and storage keeps the older version it
// was deployed from. A version-only chart bump leaves this behind.
//
// The pass that renders must agree with the passes that follow it. The latch
// reports 1 for this release on every later pass, so a 0 here would make the one
// pass able to explain the cause the only one denying there is one — the gauge
// contradicting itself, which is worse than either answer alone.
func TestUnconvergedGaugeReportsAStoredVersionNoUpgradeCanUpdate(t *testing.T) {
	releaseUnconverged.Reset()
	t.Cleanup(releaseUnconverged.Reset)

	c, counter := newCountingClient(t)
	noChartCache(c, counter)
	spec := testSpec("2.1.0", map[string]interface{}{"replicas": float64(1)})
	ctx := context.Background()

	if err := c.EnsureRelease(ctx, spec); err != nil {
		t.Fatalf("install error = %v", err)
	}

	// Rewrite the stored version, keeping the manifest. That is the state a
	// version-only bump produces: the deployed manifest is already what 2.1.0
	// renders, but storage still records the version it was installed from.
	cfg := configOf(t, c)
	stored, err := cfg.Releases.Deployed(testRelName)
	if err != nil {
		t.Fatalf("Deployed() error = %v", err)
	}
	stored.Chart.Metadata.Version = "2.0.0"
	if err := cfg.Releases.Update(stored); err != nil {
		t.Fatalf("rewriting the stored version: %v", err)
	}

	// The pass that renders and reaches reportUnconverged.
	if err := c.EnsureRelease(ctx, spec); err != nil {
		t.Fatalf("render pass error = %v", err)
	}
	rendered := testutil.ToFloat64(releaseUnconverged.WithLabelValues(testRelName))
	if rendered != 1 {
		t.Errorf("after the render pass: aif_helm_release_unconverged = %v, want 1", rendered)
	}

	// The pass that takes the latch, which has always reported 1.
	if err := c.EnsureRelease(ctx, spec); err != nil {
		t.Fatalf("latch pass error = %v", err)
	}
	latched := testutil.ToFloat64(releaseUnconverged.WithLabelValues(testRelName))
	if latched != rendered {
		t.Errorf("the gauge contradicts itself: %v on the render pass, %v on the latch pass",
			rendered, latched)
	}
}

// A healthy release must never raise the gauge, or it is noise rather than a
// signal. This one takes the actionSkip fast path and never renders at all.
func TestUnconvergedGaugeStaysDownForAHealthyRelease(t *testing.T) {
	releaseUnconverged.Reset()
	t.Cleanup(releaseUnconverged.Reset)

	c, _ := newCountingClient(t)
	spec := testSpec("2.1.0", map[string]interface{}{"replicas": float64(1)})
	ctx := context.Background()

	for range 3 {
		if err := c.EnsureRelease(ctx, spec); err != nil {
			t.Fatalf("EnsureRelease() error = %v", err)
		}
	}

	if got := testutil.ToFloat64(releaseUnconverged.WithLabelValues(testRelName)); got != 0 {
		t.Errorf("aif_helm_release_unconverged = %v, want 0 for a converged release", got)
	}
}

// A gauge that only rises is an alert nobody can clear: it goes on reporting a
// misconfiguration for as long as the process lives, however promptly someone
// fixes it. Nothing else can lower this one — a converged release returns from
// the actionSkip fast path long before reaching the render that raised it.
func TestUnconvergedGaugeFallsBackToZeroOnceTheCauseIsRemoved(t *testing.T) {
	releaseUnconverged.Reset()
	t.Cleanup(releaseUnconverged.Reset)

	c, counter := newCountingClient(t)
	noChartCache(c, counter)
	ctx := context.Background()

	installed := testSpec("2.1.0", map[string]interface{}{"replicas": float64(1)})
	if err := c.EnsureRelease(ctx, installed); err != nil {
		t.Fatalf("install error = %v", err)
	}

	withUnused := testSpec("2.1.0", map[string]interface{}{
		"replicas":      float64(1),
		"unusedByChart": "x",
	})
	if err := c.EnsureRelease(ctx, withUnused); err != nil {
		t.Fatalf("unconverged pass error = %v", err)
	}
	if got := testutil.ToFloat64(releaseUnconverged.WithLabelValues(testRelName)); got != 1 {
		t.Fatalf("aif_helm_release_unconverged = %v while the values disagree, want 1", got)
	}

	// The CR is corrected to match what storage already holds.
	if err := c.EnsureRelease(ctx, installed); err != nil {
		t.Fatalf("converged pass error = %v", err)
	}
	if got := testutil.ToFloat64(releaseUnconverged.WithLabelValues(testRelName)); got != 0 {
		t.Errorf("aif_helm_release_unconverged = %v after the disagreement was "+
			"removed, want 0; the gauge cannot be cleared once raised", got)
	}
}

// Re-entering a disagreement the latch has already ruled on must still report
// it. The latch memoizes the verdict, not the disagreement: the release is as
// unconverged on the tenth pass as on the first, and the pass that skips the
// render is the only one left to say so.
//
// Observed on a live cluster, where a release spent four minutes unconverged
// while the gauge read 0 — a silent false negative, strictly worse than the
// stale 1 that prompted this work.
func TestUnconvergedGaugeRisesAgainWhenTheLatchHits(t *testing.T) {
	releaseUnconverged.Reset()
	t.Cleanup(releaseUnconverged.Reset)

	c, counter := newCountingClient(t)
	noChartCache(c, counter)
	ctx := context.Background()

	installed := testSpec("2.1.0", map[string]interface{}{"replicas": float64(1)})
	if err := c.EnsureRelease(ctx, installed); err != nil {
		t.Fatalf("install error = %v", err)
	}
	unconverged := testSpec("2.1.0", map[string]interface{}{
		"replicas":      float64(1),
		"unusedByChart": "x",
	})

	// First encounter: rendered, reported, and latched.
	if err := c.EnsureRelease(ctx, unconverged); err != nil {
		t.Fatalf("first unconverged pass error = %v", err)
	}
	if got := testutil.ToFloat64(releaseUnconverged.WithLabelValues(testRelName)); got != 1 {
		t.Fatalf("gauge = %v on the first disagreement, want 1", got)
	}
	rendersAfterFirst := counter.pulls

	// Corrected, so the gauge drops.
	if err := c.EnsureRelease(ctx, installed); err != nil {
		t.Fatalf("corrected pass error = %v", err)
	}
	if got := testutil.ToFloat64(releaseUnconverged.WithLabelValues(testRelName)); got != 0 {
		t.Fatalf("gauge = %v once corrected, want 0", got)
	}

	// Re-entered. Same spec, same revision, so the latch answers without a
	// render — and that is exactly the path that used to report nothing.
	if err := c.EnsureRelease(ctx, unconverged); err != nil {
		t.Fatalf("second unconverged pass error = %v", err)
	}
	if got := counter.pulls; got != rendersAfterFirst {
		t.Errorf("%d extra pull(s) on the re-entry; the latch was expected to answer "+
			"without rendering, so this test is no longer covering the latch path",
			got-rendersAfterFirst)
	}
	if got := testutil.ToFloat64(releaseUnconverged.WithLabelValues(testRelName)); got != 1 {
		t.Errorf("gauge = %v on a re-entered disagreement, want 1; a latched release "+
			"reports converged while it is not", got)
	}
}

// The log line is the only thing that turns the gauge into something actionable,
// and naming the wrong keys is worse than naming none: it sends whoever is
// debugging at the wrong side of the disagreement.
func TestValuesKeyDiffNamesEachSideAlone(t *testing.T) {
	stored := map[string]interface{}{
		"appCatalog": map[string]interface{}{"useStaticCatalog": true},
		"replicas":   float64(1),
	}
	requested := map[string]interface{}{
		"replicas":      float64(1),
		"unusedByChart": "x",
		"alsoUnused":    "y",
	}

	onlyStored, onlyRequested := valuesKeyDiff(stored, requested)

	if len(onlyStored) != 1 || onlyStored[0] != "appCatalog" {
		t.Errorf("keys only in storage = %v, want [appCatalog]", onlyStored)
	}
	// Sorted, so the line reads the same on every pass rather than reshuffling
	// with Go's map iteration order.
	if len(onlyRequested) != 2 || onlyRequested[0] != "alsoUnused" || onlyRequested[1] != "unusedByChart" {
		t.Errorf("keys only in the request = %v, want [alsoUnused unusedByChart]", onlyRequested)
	}
}

// Left behind, a verdict would apply to whatever release next takes the name.
func TestDeleteReleaseDropsTheConvergenceVerdict(t *testing.T) {
	c, _ := newCountingClient(t)
	spec := testSpec("2.1.0", map[string]interface{}{"replicas": float64(1)})

	c.latchConvergence(spec, &ReleaseInfo{Revision: 1})
	if _, ok := c.converged.Load(spec.Name); !ok {
		t.Fatal("latchConvergence stored nothing")
	}

	if err := c.DeleteRelease(context.Background(), spec.Name); err != nil {
		t.Fatalf("DeleteRelease() error = %v", err)
	}
	if _, ok := c.converged.Load(spec.Name); ok {
		t.Error("the verdict survived the release being deleted")
	}
}

// An unmarshallable spec must not latch. Two such specs would produce the same
// empty fingerprint and compare equal, skipping an upgrade never verified.
func TestUnmarshallableValuesNeverLatch(t *testing.T) {
	c, _ := newCountingClient(t)
	spec := testSpec("2.1.0", map[string]interface{}{"bad": make(chan int)})
	deployed := &ReleaseInfo{Revision: 1}

	c.latchConvergence(spec, deployed)
	if _, ok := c.converged.Load(spec.Name); ok {
		t.Error("latched a spec whose values cannot be fingerprinted")
	}
	if c.convergenceHolds(spec, deployed) {
		t.Error("convergenceHolds() = true for a spec that was never latched")
	}
}

func TestSpecFingerprintDistinguishesAdjacentFields(t *testing.T) {
	// Without a separator, ChartRef "ab" + Version "c" and "a" + "bc" collide.
	a := ReleaseSpec{ChartRef: "ab", Version: "c"}
	b := ReleaseSpec{ChartRef: "a", Version: "bc"}

	fa, ok := specFingerprint(a)
	if !ok {
		t.Fatal("specFingerprint(a) not ok")
	}
	fb, ok := specFingerprint(b)
	if !ok {
		t.Fatal("specFingerprint(b) not ok")
	}
	if fa == fb {
		t.Errorf("distinct specs share fingerprint %q", fa)
	}
}
