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

// cachedChart is a chart archive already downloaded, and when.
type cachedChart struct {
	// archive is the .tgz the pull returned, held here rather than as a path into
	// the directory the pull wrote it to.
	//
	// That directory is not a place a key can point at and expect to find its own
	// chart. Helm names a download from the chart's name and version alone —
	// DownloadTo takes filepath.Base of the resolved URL, and for OCI rewrites it
	// to <name>-<tag>.tgz — so the registry host and the org path are dropped, and
	// it writes with AtomicWriteFile, which replaces whatever is already there.
	// Two keys that differ only in where the chart is pulled from therefore name
	// one file, and the second pull becomes what the first key serves for the rest
	// of its life: an origin swapped for a mirror, or either swapped for whatever
	// a re-pointed CR fetched a moment ago. Keeping the bytes is what makes a hit
	// answer with the chart pulled under its own key and nothing else.
	//
	// A chart archive is tens to hundreds of kilobytes and every write prunes what
	// has expired, so what is resident is bounded by the distinct charts one
	// operator pulls inside chartCacheTTL — one or two, in practice.
	archive []byte
	at      time.Time
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
// pulled once a minute — is unauthenticated anyway. The decline is only worth
// anything because a hit answers from bytes this cache kept: a shared download
// directory, which an authenticated pull writes into as well, would hand the
// private chart over by another route. See cachedChart.
//
// A chart re-pushed under a tag already in use is why this cache expires at all
// rather than living as long as the process. Nothing in the key changes when it
// happens, so a hit cannot notice, and the window in which it cannot is exactly
// chartCacheTTL. convergenceTTL bounds the same window on the latch, for the same
// reason and on the same argument.
func chartCacheKey(spec ReleaseSpec) (string, bool) {
	if spec.RegistryAuth != nil || spec.TLSConfig != nil {
		return "", false
	}
	return strings.Join([]string{spec.ChartRef, spec.RepoURL, spec.Version}, "\x00"), true
}

// cachedChart returns a chart parsed from a previously downloaded archive.
//
// Every caller gets its own *chart.Chart. That is the reason to cache the archive
// rather than the loaded chart: Helm mutates the chart it is handed.
// chartutil.ProcessDependenciesWithMerge, which runs on every install and
// upgrade, replaces Chart.Values wholesale and rewrites dependency entries in
// place when they carry an alias. Handing one pointer to the dry-run render and
// then to the upgrade would give the upgrade a chart the render had already
// rewritten. Re-parsing bytes already in hand costs nothing next to a registry
// round trip.
//
// A miss is always safe — it costs the pull that would have happened anyway — so
// an archive that no longer parses is dropped and reported as a miss rather than
// raised as an error.
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

	ch, err := loadArchive(cached.archive)
	if err != nil {
		c.charts.Delete(key)
		return nil, false
	}
	return ch, true
}

// cacheChart records the archive a pull returned, and prunes expired entries on
// the way through. Nothing else removes them: the map is keyed by chart
// reference and version, so it only grows as a cluster re-pins its charts, but
// "slowly" is not "never".
func (c *helmClient) cacheChart(key string, archive []byte) {
	if len(archive) == 0 {
		return
	}

	c.charts.Range(func(k, v interface{}) bool {
		if cached, ok := v.(cachedChart); !ok || time.Since(cached.at) > chartCacheTTL {
			c.charts.Delete(k)
		}
		return true
	})

	c.charts.Store(key, cachedChart{archive: archive, at: time.Now()})
}
