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
