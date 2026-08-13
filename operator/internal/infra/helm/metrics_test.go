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
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// The registry label is the whole reason the metric can answer "how much of this
// is GHCR" without GHCR being baked into its name, so it has to survive every
// shape a spec arrives in.
func TestPullRegistryReportsTheHostAPullReaches(t *testing.T) {
	tests := []struct {
		name string
		spec ReleaseSpec
		want string
	}{{
		name: "oci chart url",
		spec: ReleaseSpec{ChartRef: "oci://ghcr.io/suse/chart/aif-ui"},
		want: "ghcr.io",
	}, {
		// The point of the label: the same operator build reports a different
		// host when the CR is repointed, which a name containing "ghcr" could
		// not do.
		name: "private mirror",
		spec: ReleaseSpec{ChartRef: "oci://harbor.corp.internal/suse/aif-ui"},
		want: "harbor.corp.internal",
	}, {
		name: "mirror on a non-default port",
		spec: ReleaseSpec{ChartRef: "oci://registry.internal:5000/suse/aif-ui"},
		want: "registry.internal:5000",
	}, {
		name: "https archive",
		spec: ReleaseSpec{ChartRef: "https://charts.example.com/aif-ui-2.1.0.tgz"},
		want: "charts.example.com",
	}, {
		// A Git source names the chart, not a URL, and keeps the repository in
		// RepoURL. Without the fallback these pulls carry no destination at all.
		name: "git source falls back to the repository",
		spec: ReleaseSpec{
			ChartRef: "aif-ui",
			RepoURL:  "https://raw.githubusercontent.com/SUSE/aif/refs/heads/main",
		},
		want: "raw.githubusercontent.com",
	}, {
		name: "nothing to go on",
		spec: ReleaseSpec{ChartRef: "aif-ui"},
		want: registryUnknown,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pullRegistry(tt.spec); got != tt.want {
				t.Errorf("pullRegistry() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A credential embedded in a chart URL must not reach a label. Metrics are
// readable by anything that can scrape the endpoint and are retained long after
// the credential is rotated, so this is a leak that outlives the secret.
func TestPullRegistryDropsEmbeddedCredentials(t *testing.T) {
	got := pullRegistry(ReleaseSpec{
		ChartRef: "https://robot:hunter2@charts.example.com/aif-ui-2.1.0.tgz",
	})

	if got != "charts.example.com" {
		t.Errorf("pullRegistry() = %q, want the bare host", got)
	}
}

// The chart label carries the whole reference, so unlike the registry label it
// gets no redaction for free from url.Host. Everything that parses has to come
// back byte-identical, or the label stops joining across the two counters.
func TestPullChartKeepsTheReferenceAndDropsCredentials(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		want string
	}{{
		name: "oci chart url",
		ref:  "oci://ghcr.io/suse/chart/aif-ui",
		want: "oci://ghcr.io/suse/chart/aif-ui",
	}, {
		name: "https archive",
		ref:  "https://charts.example.com/aif-ui-2.1.0.tgz",
		want: "https://charts.example.com/aif-ui-2.1.0.tgz",
	}, {
		// A git source names the chart, not a URL. It must survive untouched.
		name: "bare chart name",
		ref:  "aif-ui",
		want: "aif-ui",
	}, {
		name: "username and password",
		ref:  "https://robot:hunter2@charts.example.com/aif-ui-2.1.0.tgz",
		want: "https://charts.example.com/aif-ui-2.1.0.tgz",
	}, {
		// A token in the username position with no password is the shape most
		// registries take, and it is the one a naive password-only redaction
		// misses.
		name: "token in the username position",
		ref:  "oci://ghp_deadbeef@ghcr.io/suse/chart/aif-ui",
		want: "oci://ghcr.io/suse/chart/aif-ui",
	}, {
		// Unparseable, so it cannot be shown to carry no credential. Attribution
		// is the thing worth losing here.
		name: "not a reference this can vouch for",
		ref:  "oci://ghcr.io/suse/aif-ui\x7f",
		want: chartUnknown,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pullChart(ReleaseSpec{ChartRef: tt.ref}); got != tt.want {
				t.Errorf("pullChart(%q) = %q, want %q", tt.ref, got, tt.want)
			}
		})
	}
}

// The unit tests above prove the mapping; this proves the counter carries the
// redacted label rather than the raw reference it was handed.
func TestChartPullsTotalNeverCarriesACredential(t *testing.T) {
	chartPullsTotal.Reset()
	t.Cleanup(chartPullsTotal.Reset)

	c, _ := newCountingClient(t)
	spec := ReleaseSpec{
		Name:      testRelName,
		Namespace: testNamespace,
		ChartRef:  "https://robot:hunter2@charts.example.com/aif-ui-2.1.0.tgz",
		Version:   "2.1.0",
	}

	if err := c.EnsureRelease(context.Background(), spec); err != nil {
		t.Fatalf("EnsureRelease() error = %v", err)
	}

	// Compared against the full exposition rather than read by label, because the
	// leak this guards against is a label value nobody would think to look up.
	// Any deviation, including the raw reference, fails here.
	if err := testutil.CollectAndCompare(chartPullsTotal, strings.NewReader(`
# HELP aif_helm_chart_pulls_total Number of Helm chart pulls that left the process for a registry, by destination registry host, chart reference and requested version.
# TYPE aif_helm_chart_pulls_total counter
aif_helm_chart_pulls_total{chart="https://charts.example.com/aif-ui-2.1.0.tgz",registry="charts.example.com",version="2.1.0"} 1
`)); err != nil {
		t.Errorf("the counter is not carrying the redacted reference: %v", err)
	}
}

// The unit test above proves the mapping; this proves the label is the one the
// counter actually carries on the path that needed the fallback.
func TestChartPullsTotalLabelsAGitSourceWithItsRepositoryHost(t *testing.T) {
	chartPullsTotal.Reset()
	t.Cleanup(chartPullsTotal.Reset)

	c, _ := newCountingClient(t)
	spec := ReleaseSpec{
		Name:      testRelName,
		Namespace: testNamespace,
		ChartRef:  testRelName,
		RepoURL:   "https://raw.githubusercontent.com/SUSE/aif/refs/heads/main",
		Version:   "2.1.0",
	}

	if err := c.EnsureRelease(context.Background(), spec); err != nil {
		t.Fatalf("EnsureRelease() error = %v", err)
	}

	got := testutil.ToFloat64(chartPullsTotal.WithLabelValues(
		"raw.githubusercontent.com", testRelName, "2.1.0"))
	if got != 1 {
		t.Errorf("aif_helm_chart_pulls_total{registry=\"raw.githubusercontent.com\"} = %v, want 1; "+
			"a git-source pull is being attributed to the wrong host or to none", got)
	}
}
