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
	"context"
	"encoding/base64"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

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

// maxFleetBundleChartBytes caps the embedded chart size. Fleet stores bundle
// resources in the Bundle/BundleDeployment objects, which are subject to an
// etcd-backed size limit; oversized content is rejected downstream, so we fail
// early with an actionable message instead.
const maxFleetBundleChartBytes = 1 << 20 // 1 MiB

// buildGitChartBundle assembles a self-contained Fleet Bundle that carries the
// chart tgz as a base64 resource, with a helm spec mirroring the one produced
// for HelmOps (releaseName/takeOwnership/disablePreProcess/values) so a
// git-backed component installs identically to an http/oci one.
func buildGitChartBundle(bundleName, namespace string, tgz []byte,
	c aiplatformv1alpha1.BlueprintComponent, vals map[string]any, targets []any) (*unstructured.Unstructured, error) {
	if len(tgz) > maxFleetBundleChartBytes {
		return nil, fmt.Errorf(
			"chart %q (%d bytes) exceeds the Fleet bundle limit of %d bytes; host it via an OCI or HTTP ClusterRepo instead",
			c.ChartName, len(tgz), maxFleetBundleChartBytes)
	}

	helm := map[string]any{
		// chart points at the embedded tgz resource below (relative path).
		"chart":   "chart.tgz",
		"version": c.ChartVersion,
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
	resources := []any{map[string]any{
		"name":     "chart.tgz",
		"content":  base64.StdEncoding.EncodeToString(tgz),
		"encoding": "base64",
	}}
	_ = unstructured.SetNestedSlice(b.Object, resources, "spec", "resources")
	return b, nil
}
