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
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/kube"
	kubefake "helm.sh/helm/v3/pkg/kube/fake"
	"helm.sh/helm/v3/pkg/registry"
	"helm.sh/helm/v3/pkg/release"
)

// applyDelay is how long the fake cluster takes to apply a manifest.
//
// It exists because Helm decides a cancelled write by racing the apply against
// the context: performInstallCtx and performUpgrade both select over ctx.Done()
// and the apply's result channel. With an instantaneous fake cluster that race
// has no defined winner, so a test built on it would pass or fail on scheduler
// luck rather than on whether the grace is wired up.
//
// A cancelled context is ready in nanoseconds. Fifty milliseconds is six orders
// of magnitude of margin, and it is also the shape of the real thing: the
// production apply was measured at roughly two seconds against a cancellation
// that arrives instantly.
const applyDelay = 50 * time.Millisecond

// slowKubeClient is Helm's printing fake with the one property it lacks —
// applying a manifest takes time.
//
// The delay is atomic because a test changes it between calls while Helm's own
// goroutines are still reading it.
type slowKubeClient struct {
	kubefake.PrintingKubeClient
	delay atomic.Int64
}

func (s *slowKubeClient) setDelay(d time.Duration) { s.delay.Store(int64(d)) }

func (s *slowKubeClient) Create(resources kube.ResourceList) (*kube.Result, error) {
	time.Sleep(time.Duration(s.delay.Load()))
	return s.PrintingKubeClient.Create(resources)
}

func (s *slowKubeClient) Update(original, target kube.ResourceList, force bool) (*kube.Result, error) {
	time.Sleep(time.Duration(s.delay.Load()))
	return s.PrintingKubeClient.Update(original, target, force)
}

// hookChart is testChart with a pre-install hook bolted on.
//
// The hook is what makes the install measurable at all. Helm builds the release
// manifest through KubeClient.Build, and the printing fake returns an empty
// resource list from it — so the apply is skipped and performInstall does
// literally nothing, leaving the cancelled-context race with no defined winner.
// Hook resources go through a separate Create inside performInstall that is not
// conditioned on that list, so a chart with one hook is the smallest thing that
// puts real work on the side of the race the grace is supposed to protect.
//
// It is not a contrivance for its own sake: an extension chart with a
// pre-install hook is ordinary, and the hook runs inside the same window a
// SIGTERM lands in.
func hookChart(version string) *chart.Chart {
	ch := testChart(version)
	ch.Templates = append(ch.Templates, &chart.File{
		Name: "templates/pre-install-hook.yaml",
		Data: []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: aif-ui-pre-install
  annotations:
    "helm.sh/hook": pre-install
data:
  version: "{{ .Chart.Version }}"
`),
	})
	return ch
}

// hookFetcher serves hookChart in place of the registry.
type hookFetcher struct{ dir string }

func (h *hookFetcher) fetch(
	_ func(*registry.Client),
	_ *action.ChartPathOptions,
	spec ReleaseSpec,
) (*chart.Chart, []byte, error) {
	path, err := chartutil.Save(hookChart(spec.Version), h.dir)
	if err != nil {
		return nil, nil, err
	}
	return loadLocalChart(path)
}

func newSlowClient(t *testing.T, delay time.Duration, rels ...*release.Release) (*helmClient, *slowKubeClient) {
	t.Helper()

	kubeClient := &slowKubeClient{
		PrintingKubeClient: kubefake.PrintingKubeClient{Out: io.Discard},
	}
	kubeClient.setDelay(delay)

	c, _ := newCountingClientWithKube(t, kubeClient, rels...)
	c.fetchChartFn = (&hookFetcher{dir: t.TempDir()}).fetch
	return c, kubeClient
}

// cancelledCtx stands in for the reconcile context at the moment the operator
// Pod is signalled: controller-runtime cancels it the instant SIGTERM lands,
// before any of the drain has run.
func cancelledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// TestUpgradeSurvivesACancelledReconcileContext is the end-to-end pin for the
// grace, as opposed to the unit tests for the helper itself.
//
// withShutdownGrace is correct in isolation and separately tested, but nothing
// checked that upgrade actually calls it — delete the two lines in action.go and
// every other test in this package still passes. That is the whole defect
// restored: the reconcile context is the manager's, SIGTERM cancels it, and
// Helm's performUpgrade answers a cancelled context by calling failRelease,
// which stamps `failed: context canceled` on a revision whose apply was fine.
//
// Driven through EnsureRelease rather than upgrade directly, so the assertion
// covers the call as it is really made.
func TestUpgradeSurvivesACancelledReconcileContext(t *testing.T) {
	noGoroutineLeak(t)

	c, _ := newSlowClient(t, applyDelay)
	values := map[string]interface{}{"replicas": float64(1)}

	if err := c.EnsureRelease(context.Background(), testSpec("2.1.0", values)); err != nil {
		t.Fatalf("EnsureRelease() install error = %v", err)
	}

	err := c.EnsureRelease(cancelledCtx(), testSpec("2.1.1", values))

	if errors.Is(err, context.Canceled) {
		t.Fatalf("EnsureRelease() error = %v; the upgrade was resolved into failRelease by a "+
			"cancellation that arrived after the apply had already started, which is the "+
			"failed revision this branch exists to prevent", err)
	}
	if err != nil {
		t.Fatalf("EnsureRelease() error = %v", err)
	}

	last, err := lastRelease(c.mustConfig(t), testRelName)
	if err != nil {
		t.Fatalf("lastRelease() error = %v", err)
	}
	if last.Status != StatusDeployed {
		t.Errorf("release status = %s, want %s; the upgrade completed on borrowed time but "+
			"was not recorded as having done so", last.Status, StatusDeployed)
	}
}

// The same for install. It reaches failRelease through its own select in
// performInstallCtx rather than through upgrade's handleContext, so sharing the
// upgrade's evidence would be an argument by analogy across two different code
// paths in Helm.
func TestInstallSurvivesACancelledReconcileContext(t *testing.T) {
	noGoroutineLeak(t)

	c, _ := newSlowClient(t, applyDelay)

	err := c.EnsureRelease(cancelledCtx(), testSpec("2.1.0", nil))

	if errors.Is(err, context.Canceled) {
		t.Fatalf("EnsureRelease() error = %v; the install was failed by a cancellation "+
			"rather than being allowed to finish", err)
	}
	if err != nil {
		t.Fatalf("EnsureRelease() error = %v", err)
	}

	last, err := lastRelease(c.mustConfig(t), testRelName)
	if err != nil {
		t.Fatalf("lastRelease() error = %v", err)
	}
	if last.Status != StatusDeployed {
		t.Errorf("release status = %s, want %s", last.Status, StatusDeployed)
	}
}

// The bound, wired rather than in isolation. The grace is not "ignore
// cancellation": the two tests above are also satisfied by
// context.WithoutCancel, and that is the variant the helper's own doc comment
// argues against — an apply detached outright is killed with the process and
// leaves the release *pending*, which Helm refuses to act on and no reconcile
// here recovers from. Failed is retried; pending is a wedge.
//
// Elapsed time is the assertion, because the returned error cannot be: an
// expired grace cancels, so it is context.Canceled either way. What separates
// the three candidate implementations is when it arrives — immediately with no
// wrapper, at the grace with this one, never with WithoutCancel. So the apply is
// made to take twice the grace and the window between them is the pass
// condition.
//
// Upgrade rather than install, and not by preference. Abandoning either write
// leaves Helm's own goroutine running, and only Upgrade guards the release
// object it is still touching (u.Lock, around failRelease and every
// reportToPerformUpgrade). Install has no such lock, so the equivalent test
// there trips the race detector inside helm.sh/helm/v3/pkg/action — an upstream
// defect, reachable in production whenever an install outlasts the grace, but
// not one this repo can fix or usefully assert on.
//
// It costs ShutdownGrace of wall clock. That is the price of testing a duration
// that production reads from a const, and it buys the one property no unit test
// of the helper can reach: that the call site passes that const.
//
// No noGoroutineLeak here, unlike the two above. Abandoning the write is the
// point of the test, so Helm's apply goroutine is still in flight when it ends
// — by construction, and in production too. A goleak guard would either fail on
// the thing being asserted or need an ignore broad enough to hide a real leak.
func TestAWriteThatOutlastsTheGraceIsStillGivenUp(t *testing.T) {
	c, kubeClient := newSlowClient(t, 0)

	if err := c.EnsureRelease(context.Background(), testSpec("2.1.0", nil)); err != nil {
		t.Fatalf("EnsureRelease() install error = %v", err)
	}
	kubeClient.setDelay(2 * ShutdownGrace)

	start := time.Now()
	err := c.EnsureRelease(cancelledCtx(), testSpec("2.1.1", nil))
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("EnsureRelease() error = %v, want context.Canceled; an apply that outlives "+
			"the grace has to be abandoned, not waited out", err)
	}
	if elapsed < ShutdownGrace {
		t.Errorf("gave up after %v, want at least the %v grace; the write was abandoned at "+
			"cancellation, which is the failed revision the grace exists to prevent",
			elapsed, ShutdownGrace)
	}
	if elapsed >= 2*ShutdownGrace {
		t.Errorf("gave up after %v, want less than the %v apply; the write was detached "+
			"outright and would be killed with the process, leaving the release pending",
			elapsed, 2*ShutdownGrace)
	}
}

// mustConfig reaches the same action.Configuration the client just wrote
// through, so a test can read the release state back out of storage.
func (c *helmClient) mustConfig(t *testing.T) *action.Configuration {
	t.Helper()

	cfg, err := c.actionConfig(context.Background(), testNamespace)
	if err != nil {
		t.Fatalf("actionConfig() error = %v", err)
	}
	return cfg
}
