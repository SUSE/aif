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
	"strings"
	"time"

	"helm.sh/helm/v3/pkg/chart"
)

// chartCacheTTL bounds how long a downloaded chart artifact is reused before the
// registry is consulted again.
//
// It has to comfortably span a single reconcile, because the traffic it exists
// to remove is the pair of pulls one upgrade performs: one to render the
// candidate manifest for the diff, one to apply it, with a Helm upgrade timeout
// of ten minutes between them at worst. Past that the value falls away — the
// convergence latch already stops a settled release from rendering at all — so
// there is little reason to hold artifacts longer and one reason not to, below.
const chartCacheTTL = 10 * time.Minute

// cachedChart is a chart already downloaded to disk, and when.
type cachedChart struct {
	// path is the local artifact the pull wrote, under settings.RepositoryCache.
	// The operator's deployment mounts an emptyDir over that directory, so the
	// artifacts live exactly as long as the process that cached them.
	path string
	at   time.Time
}

// chartCacheKey identifies a chart artifact by everything that decides which
// bytes a pull returns. Values are deliberately excluded: they are applied when
// the chart is rendered and change nothing about the download.
//
// It reports false for a spec that carries credentials or TLS trust of its own.
// A cache hit skips the fetch, and with it the authentication that fetch would
// have performed, so a spec whose credentials do not actually grant access could
// be served a chart pulled on behalf of one whose credentials do. That is narrow
// in a single-tenant operator, but it applies to precisely the private charts
// where it would matter, and the traffic worth removing — a public registry
// pulled once a minute — is unauthenticated anyway.
//
// This is also why the key can ignore the TTL question for mutable tags: a chart
// re-pushed under a tag already in use is invisible to the operator today, since
// a release whose version and values match its spec takes decideRelease's
// actionSkip path and never pulls at all. The cache only shortens the window in
// paths that do pull, and only by chartCacheTTL.
func chartCacheKey(spec ReleaseSpec) (string, bool) {
	if spec.RegistryAuth != nil || spec.TLSConfig != nil {
		return "", false
	}
	return strings.Join([]string{spec.ChartRef, spec.RepoURL, spec.Version}, "\x00"), true
}

// cachedChart returns a chart parsed from a previously downloaded artifact.
//
// Every caller gets its own *chart.Chart. That is the reason to cache the
// artifact rather than the loaded chart: Helm mutates the chart it is handed.
// chartutil.ProcessDependenciesWithMerge, which runs on every install and
// upgrade, replaces Chart.Values wholesale and rewrites dependency entries in
// place when they carry an alias. Handing one pointer to the dry-run render and
// then to the upgrade would give the upgrade a chart the render had already
// rewritten. Re-parsing a local file costs nothing next to a registry round trip.
//
// A miss is always safe — it costs the pull that would have happened anyway — so
// an artifact that has been evicted from the cache directory, or that no longer
// loads, is dropped and reported as a miss rather than raised as an error.
func (c *helmClient) cachedChart(key string) (*chart.Chart, bool) {
	entry, ok := c.charts.Load(key)
	if !ok {
		return nil, false
	}

	cached, ok := entry.(cachedChart)
	if !ok || time.Since(cached.at) > chartCacheTTL {
		c.charts.Delete(key)
		return nil, false
	}

	ch, err := loadLocalChart(cached.path)
	if err != nil {
		c.charts.Delete(key)
		return nil, false
	}
	return ch, true
}

// cacheChart records the artifact a pull wrote, and prunes expired entries on
// the way through. Nothing else removes them: the map is keyed by chart
// reference and version, so it only grows as a cluster re-pins its charts, but
// "slowly" is not "never".
func (c *helmClient) cacheChart(key, path string) {
	if path == "" {
		return
	}

	c.charts.Range(func(k, v interface{}) bool {
		if cached, ok := v.(cachedChart); !ok || time.Since(cached.at) > chartCacheTTL {
			c.charts.Delete(k)
		}
		return true
	})

	c.charts.Store(key, cachedChart{path: path, at: time.Now()})
}
