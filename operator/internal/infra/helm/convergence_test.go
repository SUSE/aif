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

	"github.com/prometheus/client_golang/prometheus/testutil"
	"helm.sh/helm/v3/pkg/action"
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
