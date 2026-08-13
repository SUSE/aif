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
	"net/url"

	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// registryUnknown labels a pull whose destination host could not be determined,
// so that such a pull is still counted rather than dropped or, worse, attributed
// to a host it never reached.
const registryUnknown = "unknown"

// chartUnknown labels a pull whose chart reference could not be parsed, and so
// could not be shown to carry no credential. Fewer pulls are attributable; none
// carry a secret.
const chartUnknown = "unknown"

// chartPullsTotal counts chart fetches that leave the process for a registry.
//
// A steady-state operator should leave this flat: charts are pulled to install
// or to upgrade, and neither happens once a release has converged. A counter
// that keeps climbing against an idle cluster means some reconcile path is
// deciding to fetch on every pass — which is invisible in logs (each pull looks
// like ordinary work) but shows up in registry egress and, on a public
// registry, in rate limits.
//
// The registry label is what makes that egress attributable. The destination is
// not a property of this operator — source.helm.chartURL is required, has no
// default, and is only constrained to start with oci:// or https:// — so the
// same build pulls from ghcr.io in one cluster and from a private mirror in the
// next, and a Git source pulls from raw.githubusercontent.com instead. Naming a
// specific host in the metric would therefore be wrong in exactly the clusters
// that are not the default one. Carrying it as a label keeps the question
// answerable — sum by (registry), or filter registry="ghcr.io" for rate-limit
// work — without asserting anything that stops being true when someone repoints
// the CR.
//
// Cardinality is bounded by the number of InstallAIExtension resources, which is
// one per cluster in practice.
var chartPullsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "aif_helm_chart_pulls_total",
		Help: "Number of Helm chart pulls that left the process for a registry, by destination registry host, chart reference and requested version.",
	},
	[]string{"registry", "chart", "version"},
)

// pullRegistry reports the host a pull for spec reaches.
//
// The two source kinds name their chart differently and neither the metric nor
// whoever reads it should have to know which is in play. A Helm source puts a
// full oci:// or https:// URL in ChartRef, so the host is already there. A Git
// source puts a bare chart name in ChartRef and the raw.githubusercontent.com
// base in RepoURL, so without the fallback every git-source pull would be
// labelled with no destination at all.
//
// Only the host is taken, and url.Host excludes userinfo. That is deliberate: a
// credential embedded in a chart URL must not reach a metric, which is readable
// by anything that can scrape the endpoint and is retained long after the
// credential is rotated. See pullChart, which carries the same rule to the label
// that carries the whole reference.
func pullRegistry(spec ReleaseSpec) string {
	for _, raw := range []string{spec.ChartRef, spec.RepoURL} {
		if u, err := url.Parse(raw); err == nil && u.Host != "" {
			return u.Host
		}
	}
	return registryUnknown
}

// pullChart reports the chart a pull is for, with any embedded credential
// removed.
//
// The registry label gets this for free, because url.Host excludes userinfo.
// This label does not: it carries the whole reference, which is the point — it
// is what distinguishes two charts on one host — and a chart URL is a place a
// credential can legitimately be written. The CRD constrains source.helm.chartURL
// only to start with oci:// or https://, so https://user:token@host/chart.tgz is
// a valid CR, and unredacted it would put that token in the time series database,
// in every dashboard reading from it, and in whatever long-term storage sits
// behind it — outliving the rotation of the secret it came from by however long
// retention runs.
//
// The query goes with it. A presigned archive URL — Azure SAS, S3, GCS — carries
// its authorization there rather than in userinfo, and the CRD permits one on
// exactly this path: the pattern only requires an oci:// or https:// prefix, and
// the rule demanding a .tgz suffix applies solely when tls is set. Such a URL is
// a bearer credential in its entirety, so a signature reaching the label is the
// same disclosure as a password reaching it, with the added effect that rotating
// the signature mints a new time series and unbounds the cardinality this label
// is documented to keep.
//
// A reference that does not parse cannot be shown to be free of one, so it is
// reported as chartUnknown rather than passed through. Nothing that parses is
// rewritten: a bare chart name from a git source, and the ordinary URLs that make
// up every other pull, are all returned exactly as they arrived — byte-identical,
// so the label still joins across this counter and chartCacheHitsTotal.
func pullChart(spec ReleaseSpec) string {
	u, err := url.Parse(spec.ChartRef)
	if err != nil {
		return chartUnknown
	}
	if u.User == nil && u.RawQuery == "" {
		return spec.ChartRef
	}
	u.User = nil
	u.RawQuery = ""
	return u.String()
}

// chartCacheHitsTotal counts chart loads served from an already-downloaded
// artifact instead of from the registry.
//
// It is the denominator chartPullsTotal needs to be read honestly. A flat pull
// count could mean the operator has settled or that the cache is hiding traffic
// that would otherwise be visible; only the two together say which. Reported
// against the same labels, including the registry a hit means the operator did
// not have to reach, so the ratio can be taken per chart or per host.
var chartCacheHitsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "aif_helm_chart_cache_hits_total",
		Help: "Number of Helm chart loads served from a previously downloaded artifact, by the registry host the load avoided, chart reference and requested version.",
	},
	[]string{"registry", "chart", "version"},
)

// releaseUnconverged flags a release whose stored version or values disagree
// with the requested spec even though rendering the chart proves nothing needs
// to change.
//
// This is the state the convergence latch silences, and silencing it without
// reporting it would trade a visible symptom (a climbing pull count) for an
// invisible one. It is a misconfiguration — either a chart whose Chart.yaml
// version does not match the tag the CR pins, or values the CR and storage
// disagree on — and it wants fixing at the source, not papering over. The
// accompanying log line names which.
//
// 1 while the disagreement stands, 0 once a reconcile finds spec and storage in
// agreement. Lowering it is the job of the actionSkip fast path in
// EnsureRelease, because a converged release never reaches the render that
// raised it.
var releaseUnconverged = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "aif_helm_release_unconverged",
		Help: "1 when a Helm release's stored version or values disagree with the requested spec despite the chart rendering no change.",
	},
	[]string{"release"},
)

func init() {
	ctrlmetrics.Registry.MustRegister(chartPullsTotal, chartCacheHitsTotal, releaseUnconverged)
}
