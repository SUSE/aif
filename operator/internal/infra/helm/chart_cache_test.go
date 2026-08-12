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
	"crypto/tls"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/registry"
)

// backdateChartCache ages every cached entry by d, standing in for time passing.
func backdateChartCache(t *testing.T, c *helmClient, d time.Duration) {
	t.Helper()

	aged := 0
	c.charts.Range(func(k, v interface{}) bool {
		cached, ok := v.(cachedChart)
		if !ok {
			return true
		}
		cached.at = cached.at.Add(-d)
		c.charts.Store(k, cached)
		aged++
		return true
	})
	if aged == 0 {
		t.Fatal("nothing was cached, so ageing the cache proves nothing")
	}
}

// noRegistry stands in for the action's SetRegistryClient. Only LocateChart ever
// reads what it sets, and these tests never reach it.
func noRegistry(*registry.Client) {}

// loadChartOnce drives a single fetch through loadChart, the way install,
// upgrade and the dry-run render all do.
func loadChartOnce(t *testing.T, c *helmClient, spec ReleaseSpec) {
	t.Helper()

	if _, err := c.loadChart(noRegistry, &action.ChartPathOptions{}, spec); err != nil {
		t.Fatalf("loadChart() error = %v", err)
	}
}

// loadChartTwice is the shape of one upgrade: render, then apply.
func loadChartTwice(t *testing.T, c *helmClient, spec ReleaseSpec) {
	t.Helper()

	loadChartOnce(t, c, spec)
	loadChartOnce(t, c, spec)
}

// The whole point: two loads of the same chart, one download.
func TestChartCacheServesTheSecondLoadOfTheSameChart(t *testing.T) {
	c, counter := newCountingClient(t)
	spec := testSpec("2.1.0", nil)

	loadChartTwice(t, c, spec)

	if counter.pulls != 1 {
		t.Errorf("pulled %d times for two loads of the same chart, want 1", counter.pulls)
	}
}

// Helm rewrites the chart it is handed — ProcessDependenciesWithMerge replaces
// Values on every run — so handing the same pointer to the render and then to
// the upgrade would feed the second a chart the first had already modified.
// Caching the artifact rather than the chart is what avoids that, and this is
// the property that makes the difference observable.
func TestChartCacheReturnsAnIndependentChartEachTime(t *testing.T) {
	c, counter := newCountingClient(t)
	spec := testSpec("2.1.0", nil)
	opts := &action.ChartPathOptions{}

	// Prime the cache, then compare two loads that are both hits. Comparing a
	// miss against a hit would prove nothing: those differ even if every hit
	// after them shares one chart.
	loadChartOnce(t, c, spec)

	first, err := c.loadChart(noRegistry, opts, spec)
	if err != nil {
		t.Fatalf("first cached load error = %v", err)
	}
	// Stand in for what Helm does to a chart it renders.
	first.Values = map[string]interface{}{"mutated": true}
	first.Metadata.Version = "clobbered"

	second, err := c.loadChart(noRegistry, opts, spec)
	if err != nil {
		t.Fatalf("second cached load error = %v", err)
	}
	if counter.pulls != 1 {
		t.Fatalf("pulled %d times, want 1; the loads compared below were not both cache hits", counter.pulls)
	}

	if second == first {
		t.Fatal("the cache handed back the same *chart.Chart; a consumer that mutates it corrupts the next one")
	}
	if second.Metadata.Version != "2.1.0" {
		t.Errorf("second load version = %q, want 2.1.0; the first consumer's mutation leaked", second.Metadata.Version)
	}
	if _, mutated := second.Values["mutated"]; mutated {
		t.Error("the first consumer's values leaked into the second load")
	}
}

// An artifact must not be reused forever. The window is short by design: it
// covers one reconcile's render-then-upgrade pair and little else.
func TestChartCacheExpires(t *testing.T) {
	c, counter := newCountingClient(t)
	spec := testSpec("2.1.0", nil)

	loadChartTwice(t, c, spec)
	if counter.pulls != 1 {
		t.Fatalf("pulled %d times before expiry, want 1", counter.pulls)
	}

	backdateChartCache(t, c, chartCacheTTL+time.Minute)

	loadChartOnce(t, c, spec)
	if counter.pulls != 2 {
		t.Errorf("pulled %d times in total, want 2; an expired artifact was reused", counter.pulls)
	}
}

// The cache directory is an emptyDir the operator does not own exclusively, and
// a missing artifact is not an error condition — it is a cache miss, which costs
// exactly the pull that would have happened without a cache at all.
func TestChartCacheFallsBackWhenTheArtifactIsGone(t *testing.T) {
	c, counter := newCountingClient(t)
	spec := testSpec("2.1.0", nil)

	loadChartOnce(t, c, spec)

	key, ok := chartCacheKey(spec)
	if !ok {
		t.Fatal("chartCacheKey() reported this spec uncacheable")
	}
	entry, ok := c.charts.Load(key)
	if !ok {
		t.Fatal("the install cached nothing")
	}
	if err := os.Remove(entry.(cachedChart).path); err != nil {
		t.Fatalf("removing the artifact: %v", err)
	}

	loadChartOnce(t, c, spec)
	if counter.pulls != 2 {
		t.Errorf("pulled %d times, want 2; a vanished artifact was reported as a hit", counter.pulls)
	}
	if _, stillCached := c.charts.Load(key); !stillCached {
		t.Error("the re-pull did not repopulate the cache")
	}
}

// Everything that decides which bytes come back has to be part of the key.
func TestChartCacheKeyCoversEverythingThatChangesTheDownload(t *testing.T) {
	base := testSpec("2.1.0", nil)

	tests := []struct {
		name string
		next ReleaseSpec
	}{
		{
			name: "version",
			next: testSpec("2.1.1", nil),
		},
		{
			name: "chart reference",
			next: func() ReleaseSpec {
				s := testSpec("2.1.0", nil)
				s.ChartRef = "oci://registry.example.com/aif-ui"
				return s
			}(),
		},
		{
			name: "repo url",
			next: func() ReleaseSpec {
				s := testSpec("2.1.0", nil)
				s.RepoURL = "https://charts.example.com"
				return s
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, counter := newCountingClient(t)

			loadChartOnce(t, c, base)
			loadChartOnce(t, c, tt.next)

			if counter.pulls != 2 {
				t.Errorf("pulled %d times, want 2; a change of %s was served from the cache",
					counter.pulls, tt.name)
			}
		})
	}
}

// Values are applied when the chart is rendered, not when it is downloaded, so
// they must not split the cache.
func TestChartCacheIgnoresValues(t *testing.T) {
	c, counter := newCountingClient(t)

	loadChartOnce(t, c, testSpec("2.1.0", map[string]interface{}{"replicas": float64(1)}))
	loadChartOnce(t, c, testSpec("2.1.0", map[string]interface{}{"replicas": float64(9)}))

	if counter.pulls != 1 {
		t.Errorf("pulled %d times for two value sets over one chart, want 1", counter.pulls)
	}
}

// A hit skips the fetch and the authentication the fetch performs. Rather than
// reason about which credentials a cached artifact was pulled with, a spec that
// carries its own is never cached.
func TestChartCacheDeclinesCredentialedPulls(t *testing.T) {
	authed := testSpec("2.1.0", nil)
	authed.RegistryAuth = &RegistryAuth{Username: "u", Password: "p"}

	withTLS := testSpec("2.1.0", nil)
	withTLS.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}

	for name, spec := range map[string]ReleaseSpec{"registry auth": authed, "tls": withTLS} {
		t.Run(name, func(t *testing.T) {
			if _, ok := chartCacheKey(spec); ok {
				t.Fatal("chartCacheKey() reported a credentialed spec cacheable")
			}

			c, counter := newCountingClient(t)
			loadChartTwice(t, c, spec)

			if counter.pulls != 2 {
				t.Errorf("pulled %d times, want 2; a credentialed pull was served from the cache", counter.pulls)
			}
		})
	}
}

// A fetch that leaves nothing on disk — the in-memory TLS path — has no artifact
// to reuse, and must not be recorded as though it did.
func TestChartCacheIgnoresAFetchWithNoArtifact(t *testing.T) {
	c, _ := newCountingClient(t)
	key, ok := chartCacheKey(testSpec("2.1.0", nil))
	if !ok {
		t.Fatal("chartCacheKey() reported this spec uncacheable")
	}

	c.cacheChart(key, "")
	if _, cached := c.charts.Load(key); cached {
		t.Error("cached an entry with no artifact behind it")
	}
}

// Nothing sweeps the cache on a timer, so a put has to carry expired entries out
// or a long-lived operator accumulates one per version it has ever been pinned to.
func TestChartCachePrunesExpiredEntriesOnWrite(t *testing.T) {
	c, _ := newCountingClient(t)

	loadChartOnce(t, c, testSpec("2.1.0", nil))
	backdateChartCache(t, c, chartCacheTTL+time.Minute)
	loadChartOnce(t, c, testSpec("2.1.1", nil))

	entries := 0
	c.charts.Range(func(interface{}, interface{}) bool {
		entries++
		return true
	})
	if entries != 1 {
		t.Errorf("%d entries cached, want 1; the expired one was left behind", entries)
	}
}

// The counters have to disagree about a hit, or a flat pull count can no longer
// be told apart from a cache quietly absorbing the traffic.
func TestChartCacheHitsAreCountedSeparatelyFromPulls(t *testing.T) {
	chartPullsTotal.Reset()
	chartCacheHitsTotal.Reset()
	t.Cleanup(chartPullsTotal.Reset)
	t.Cleanup(chartCacheHitsTotal.Reset)

	c, _ := newCountingClient(t)
	loadChartTwice(t, c, testSpec("2.1.0", nil))

	if got := testutil.ToFloat64(chartPullsTotal.WithLabelValues(testRegistry, testChartRef, "2.1.0")); got != 1 {
		t.Errorf("aif_helm_chart_pulls_total = %v, want 1", got)
	}
	if got := testutil.ToFloat64(chartCacheHitsTotal.WithLabelValues(testRegistry, testChartRef, "2.1.0")); got != 1 {
		t.Errorf("aif_helm_chart_cache_hits_total = %v, want 1", got)
	}
}

// A cached chart still has to render what the freshly pulled one rendered, or
// the diff that gates the upgrade would report a change on every cache hit.
func TestCachedChartRendersTheSameManifest(t *testing.T) {
	c, counter := newCountingClient(t)
	ctx := context.Background()
	spec := testSpec("2.1.0", map[string]interface{}{"replicas": float64(2)})

	if err := c.EnsureRelease(ctx, spec); err != nil {
		t.Fatalf("install error = %v", err)
	}

	// Force the render: a values key the chart ignores keeps storage disagreeing
	// with the spec, so EnsureRelease renders instead of taking the fast path,
	// and it renders from the artifact the install cached.
	unused := testSpec("2.1.0", map[string]interface{}{"replicas": float64(2), "unusedByChart": "x"})
	if err := c.EnsureRelease(ctx, unused); err != nil {
		t.Fatalf("render pass error = %v", err)
	}

	if counter.pulls != 1 {
		t.Fatalf("pulled %d times, want 1; the render did not come from the cache", counter.pulls)
	}

	cfg := configOf(t, c)
	deployed, err := cfg.Releases.Deployed(testRelName)
	if err != nil {
		t.Fatalf("Deployed() error = %v", err)
	}
	if deployed.Version != 1 {
		t.Errorf("deployed revision = %d, want 1; the cached chart rendered a different manifest "+
			"and triggered an upgrade that changes nothing", deployed.Version)
	}
}
