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

package aiworkload

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
	"unicode/utf8"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// RancherCatalogClient fetches a chart .tgz from a Rancher ClusterRepo via the
// Steve catalog API. Only git-backed ClusterRepos need it: they have no
// url/ociRepo a Fleet HelmOp could pull from, so the operator resolves the chart
// from Rancher (which has already cloned and indexed the git repo) and embeds it
// in a self-contained Fleet Bundle.
type RancherCatalogClient interface {
	FetchChart(ctx context.Context, repoName, chartName, version string) ([]byte, error)
}

// maxFleetBundleChartBytes caps the fetched chart archive size. Fleet stores the
// unpacked chart files in the Bundle/BundleDeployment objects, which are subject
// to an etcd-backed size limit; we guard on the (compressed) archive size — a
// close proxy for the gzipped bundle Fleet ultimately stores — and fail early
// with an actionable message instead of letting the API server reject it.
const maxFleetBundleChartBytes = 1 << 20 // 1 MiB

// buildGitChartBundle assembles a self-contained Fleet Bundle from a fetched
// chart archive. Fleet does NOT unpack a .tgz supplied as a single bundle
// resource — doing so yields a silent empty release — so the archive is expanded
// into one bundle resource per chart file (path-preserving) with spec.helm.chart
// pointing at the chart's root directory. The helm spec mirrors the one produced
// for HelmOps (releaseName/takeOwnership/disablePreProcess/values) so a
// git-backed component installs identically to an http/oci one. The chart version
// is pinned by the fetched archive itself, so spec.helm.version is omitted.
func buildGitChartBundle(bundleName, namespace string, tgz []byte,
	c aiplatformv1alpha1.BlueprintComponent, vals map[string]any, targets []any) (*unstructured.Unstructured, error) {
	if len(tgz) > maxFleetBundleChartBytes {
		return nil, fmt.Errorf(
			"chart %q (%d bytes) exceeds the Fleet bundle limit of %d bytes; host it via an OCI or HTTP ClusterRepo instead",
			c.ChartName, len(tgz), maxFleetBundleChartBytes)
	}

	resources, chartDir, err := chartTgzToBundleResources(tgz)
	if err != nil {
		return nil, fmt.Errorf("unpack chart %q: %w", c.ChartName, err)
	}

	helm := map[string]any{
		// chart points at the unpacked chart directory carried in resources below.
		"chart": chartDir,
		// releaseName uses the chart name (not bundleName) so chart sub-resources
		// templated as `{{ .Release.Name }}-foo` fit under the 63-char DNS-label
		// limit — see ensureBlueprintHelmOp for the full rationale.
		"releaseName": capReleaseName(c.ChartName),
		// disablePreProcess: we resolve all values ourselves and upstream charts
		// legitimately use ${ } which Fleet would otherwise mis-parse.
		"disablePreProcess": true,
		// takeOwnership lets the install adopt operator-delivered pull secrets.
		"takeOwnership": true,
	}
	if len(vals) > 0 {
		helm["values"] = vals
	}

	b := &unstructured.Unstructured{}
	b.SetGroupVersionKind(bundleGVK)
	b.SetName(bundleName)
	_ = unstructured.SetNestedField(b.Object, namespace, "spec", "defaultNamespace")
	_ = unstructured.SetNestedField(b.Object, helm, "spec", "helm")
	if targets == nil {
		targets = []any{}
	}
	_ = unstructured.SetNestedSlice(b.Object, targets, "spec", "targets")
	_ = unstructured.SetNestedSlice(b.Object, resources, "spec", "resources")
	return b, nil
}

// chartTgzToBundleResources expands a Helm chart .tgz into Fleet bundle
// resources — one entry per regular file, preserving the archive paths — and
// returns the chart's top-level directory name for spec.helm.chart. UTF-8 files
// are stored inline; binary files (e.g. icons) are base64-encoded.
func chartTgzToBundleResources(tgz []byte) (resources []any, chartDir string, err error) {
	gz, err := gzip.NewReader(bytes.NewReader(tgz))
	if err != nil {
		return nil, "", fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", fmt.Errorf("tar: %w", err)
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		name := path.Clean(h.Name)
		if i := strings.IndexByte(name, '/'); i > 0 && chartDir == "" {
			chartDir = name[:i]
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, "", fmt.Errorf("read %s: %w", name, err)
		}
		res := map[string]any{"name": name}
		if utf8.Valid(data) {
			res["content"] = string(data)
		} else {
			res["content"] = base64.StdEncoding.EncodeToString(data)
			res["encoding"] = "base64"
		}
		resources = append(resources, res)
	}
	if chartDir == "" || len(resources) == 0 {
		return nil, "", fmt.Errorf("no chart directory found in archive")
	}
	return resources, chartDir, nil
}

// splitWorkloadTargets returns Fleet target selectors split by workspace:
// local-cluster targets (deployed via fleet-local) and downstream targets
// (deployed via fleet-default). Shared by the HelmOp, GitOps, and git-backed
// Bundle paths so all three agree on target shape.
func splitWorkloadTargets(w *aiplatformv1alpha1.AIWorkload) (local, downstream []any) {
	local = make([]any, 0)
	downstream = make([]any, 0)
	for _, id := range w.Spec.TargetClusters {
		if id == "local" {
			local = append(local, map[string]any{"clusterName": "local"})
		} else {
			downstream = append(downstream, map[string]any{
				"clusterSelector": map[string]any{
					"matchLabels": map[string]any{"management.cattle.io/cluster-name": id},
				},
			})
		}
	}
	return local, downstream
}

// gitOpsFleetNamespace mirrors the GitOps path's fleet-local vs fleet-default
// choice: fleet-local only when every target is the local cluster and at least
// one target is set; otherwise fleet-default.
func gitOpsFleetNamespace(w *aiplatformv1alpha1.AIWorkload) string {
	for _, id := range w.Spec.TargetClusters {
		if id != "local" {
			return "fleet-default"
		}
	}
	if len(w.Spec.TargetClusters) == 0 {
		return "fleet-default"
	}
	return "fleet-local"
}

// ensureBlueprintGitChartBundle fetches a git-backed ClusterRepo's chart from
// Rancher and applies (gitOps=false) or git-publishes (gitOps=true) a
// self-contained Fleet Bundle carrying the chart. It reuses the same value and
// pull-secret injection and per-workspace target split as the HelmOp path.
func (r *AIWorkloadReconciler) ensureBlueprintGitChartBundle(
	ctx context.Context,
	w *aiplatformv1alpha1.AIWorkload,
	c aiplatformv1alpha1.BlueprintComponent,
	bundleName string,
	repoInfo clusterRepoInfo,
	gitOps bool,
) error {
	if r.CatalogClient == nil {
		return fmt.Errorf("git-backed ClusterRepo %q requires the Rancher catalog client, which is not configured", c.ChartRepo)
	}
	tgz, err := r.CatalogClient.FetchChart(ctx, c.ChartRepo, c.ChartName, c.ChartVersion)
	if err != nil {
		return fmt.Errorf("fetch chart %s@%s from git repo %q: %w", c.ChartName, c.ChartVersion, c.ChartRepo, err)
	}

	vals := map[string]any{}
	if c.Values != nil {
		_ = json.Unmarshal(c.Values.Raw, &vals)
	}
	ns := componentNamespace(w, c)
	created, err := r.injectorFor(c.Vendor).Apply(ctx, r.localCC(), ns, repoInfo, vals, targetsLocalCluster(w))
	if err != nil {
		return fmt.Errorf("inject secrets for %s: %w", c.ChartName, err)
	}
	w.Status.PullSecretDeliveries = mergePullSecretDelivery(w.Status.PullSecretDeliveries, ns, created)

	localTargets, downstreamTargets := splitWorkloadTargets(w)

	if gitOps {
		allTargets := append(append([]any{}, localTargets...), downstreamTargets...)
		b, err := buildGitChartBundle(bundleName, ns, tgz, c, vals, allTargets)
		if err != nil {
			return err
		}
		b.SetNamespace(gitOpsFleetNamespace(w))
		yamlBytes, err := json.MarshalIndent(b.Object, "", "  ")
		if err != nil {
			return err
		}
		return r.publishBlueprintGitFile(ctx, w, bundleName, string(yamlBytes))
	}

	for _, pair := range []struct {
		ns      string
		targets []any
	}{
		{"fleet-local", localTargets},
		{"fleet-default", downstreamTargets},
	} {
		if len(pair.targets) == 0 {
			continue
		}
		b, err := buildGitChartBundle(bundleName, ns, tgz, c, vals, pair.targets)
		if err != nil {
			return err
		}
		b.SetNamespace(pair.ns)
		if err := r.Patch(ctx, b, client.Apply, client.ForceOwnership, client.FieldOwner("aif-operator")); err != nil {
			return fmt.Errorf("patch Bundle %s/%s: %w", pair.ns, bundleName, err)
		}
	}
	return nil
}
