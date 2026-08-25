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
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"path"
	"strings"
	"unicode/utf8"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/infra/rancher"
)

// maxBundleResourcesBytes caps the size of the payload we put in the Bundle's
// spec.resources — the chart files UNPACKED, with binary files base64-inflated.
// That payload, not the archive, is what the API server has to store: a Bundle is
// a plain CR, so it is subject to the ~1.5 MiB etcd object limit with no
// compression. Compression ratios on Helm charts are ~10x, so the compressed
// archive is a badly misleading proxy: rancher-monitoring is 490 KiB as a .tgz
// but ~3.9 MiB as bundle resources. Measuring the archive would let such a chart
// pass this check and then fail at the API server with an opaque
// "request entity too large". Budget 1 MiB and leave the remainder of the object
// limit for targets, values, managedFields and Fleet's own status.
const maxBundleResourcesBytes = 1 << 20 // 1 MiB

// maxChartArchiveBytes is a cheap pre-unpack rejection. Nothing above this can
// possibly fit in maxBundleResourcesBytes once expanded, so we skip the work —
// and it bounds how much a malicious archive can make us decompress before the
// payload cap kicks in during extraction.
const maxChartArchiveBytes = 4 << 20 // 4 MiB

// errChartTooLarge marks a git-backed component whose chart cannot fit in a
// Fleet Bundle. Unlike a fetch or apply failure this is a property of the chart
// itself, so retrying can never succeed: every attempt re-downloads the archive
// from Rancher and fails identically. Reconcile turns it into a terminal
// Ready=False condition instead of returning it, which both stops the hot retry
// loop and replaces the stale "Component bundles reconciled" message the
// AIWorkload would otherwise keep showing while failing.
var errChartTooLarge = stderrors.New("chart too large for a Fleet bundle")

// chartFingerprintAnnotation records what the Bundle was built from, so a
// reconcile can tell an up-to-date Bundle from a stale one without downloading
// the chart again. See gitChartFingerprint.
const chartFingerprintAnnotation = "ai-factory.suse.com/git-chart-fingerprint"

// buildGitChartBundle assembles a self-contained Fleet Bundle from a fetched
// chart archive. Fleet does NOT unpack a .tgz supplied as a single bundle
// resource — doing so yields a silent empty release — so the archive is expanded
// into one bundle resource per chart file (path-preserving) with spec.helm.chart
// pointing at the chart's root directory. The helm spec mirrors the one produced
// for HelmOps (releaseName/takeOwnership/disablePreProcess/values) so a
// git-backed component installs identically to an http/oci one. The chart version
// is pinned by the fetched archive itself, so spec.helm.version is omitted.
func buildGitChartBundle(bundleName, namespace, fingerprint string, tgz []byte,
	c aiplatformv1alpha1.BlueprintComponent, vals map[string]any, targets []any) (*unstructured.Unstructured, error) {
	if len(tgz) > maxChartArchiveBytes {
		return nil, fmt.Errorf(
			"%w: chart %q archive is %d bytes, over the %d-byte limit for a git-backed repo; host it via an OCI or HTTP ClusterRepo instead",
			errChartTooLarge, c.ChartName, len(tgz), maxChartArchiveBytes)
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
		// limit — see ensureBlueprintHelmOp for the full rationale. A component
		// may override this default via its ReleaseName (componentReleaseName).
		"releaseName": capReleaseName(componentReleaseName(c)),
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
	if fingerprint != "" {
		b.SetAnnotations(map[string]string{chartFingerprintAnnotation: fingerprint})
		// renderDigestLabel mirrors the HelmOp path: certifyDeployedSource and
		// buildComponentMatrix compare this label against the per-component
		// expected digest (the fingerprint returned by ensureBlueprintGitChartBundle),
		// so a git-backed Bundle certifies the same way an http/oci one does.
		b.SetLabels(map[string]string{renderDigestLabel: renderDigestLabelValue(fingerprint)})
	}
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
	var total int64
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
		// Reject entries that escape the chart root. path.Clean leaves a leading
		// ../ intact and keeps absolute paths absolute, so a crafted archive could
		// otherwise write a bundle resource outside the chart directory.
		if name == ".." || strings.HasPrefix(name, "../") || path.IsAbs(name) {
			return nil, "", fmt.Errorf("archive entry %q escapes the chart root", h.Name)
		}
		if i := strings.IndexByte(name, '/'); i > 0 && chartDir == "" {
			chartDir = name[:i]
		}
		// Read at most the remaining payload budget +1, so an overrun is detectable
		// without allocating past the cap. This doubles as the decompression-bomb
		// guard: a bomb blows the budget on its first entry and is rejected here.
		data, err := io.ReadAll(io.LimitReader(tr, maxBundleResourcesBytes-total+1))
		if err != nil {
			return nil, "", fmt.Errorf("read %s: %w", name, err)
		}
		res := map[string]any{"name": name}
		if utf8.Valid(data) {
			res["content"] = string(data)
			total += int64(len(data))
		} else {
			// Binary files are base64-encoded, which inflates them 4/3 in the
			// stored object — charge the encoded size, not the raw size.
			enc := base64.StdEncoding.EncodeToString(data)
			res["content"] = enc
			res["encoding"] = "base64"
			total += int64(len(enc))
		}
		total += int64(len(name))
		if total > maxBundleResourcesBytes {
			return nil, "", fmt.Errorf(
				"%w: unpacked chart exceeds the %d-byte Fleet bundle payload limit; host it via an OCI or HTTP ClusterRepo instead",
				errChartTooLarge, maxBundleResourcesBytes)
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
) (string, error) {
	var fetcher rancher.ChartFetcher
	if r.CatalogClient != nil {
		fetcher = r.CatalogClient.Get()
	}
	if fetcher == nil {
		return "", fmt.Errorf("%w: git-backed ClusterRepo %q needs a Rancher API token", errCatalogClientNotConfigured, c.ChartRepo)
	}

	// Resolve values and inject pull secrets BEFORE deciding whether to re-fetch:
	// the injector mutates vals (it appends imagePullSecrets), so the fingerprint
	// has to be taken after it runs, and secret delivery must happen on every
	// reconcile regardless of whether the chart itself changed.
	vals := map[string]any{}
	if c.Values != nil {
		_ = json.Unmarshal(c.Values.Raw, &vals)
	}
	ns := componentNamespace(w, c)
	created, err := r.injectorFor(c.Vendor).Apply(ctx, r.localCC(), ns, repoInfo, vals, targetsLocalCluster(w))
	if err != nil {
		return "", fmt.Errorf("inject secrets for %s: %w", c.ChartName, err)
	}
	w.Status.PullSecretDeliveries = mergePullSecretDelivery(w.Status.PullSecretDeliveries, ns, created)

	localTargets, downstreamTargets := splitWorkloadTargets(w)
	fingerprint := gitChartFingerprint(c, ns, repoInfo.Commit, vals, localTargets, downstreamTargets)
	pairs := []struct {
		ns      string
		targets []any
	}{
		{"fleet-local", localTargets},
		{"fleet-default", downstreamTargets},
	}

	if gitOps {
		upToDate := true
		desiredNamespaces := make(map[string]bool, len(pairs))
		for _, pair := range pairs {
			if len(pair.targets) == 0 {
				continue
			}
			desiredNamespaces[pair.ns] = true
			if !r.gitChartBundleMatches(ctx, pair.ns, bundleName, fingerprint) {
				upToDate = false
			}
		}
		if len(desiredNamespaces) == 0 {
			return "", fmt.Errorf("GitOps blueprint component %q has no target clusters", c.ChartName)
		}
		for _, namespace := range fleetNamespaces {
			if desiredNamespaces[namespace] {
				continue
			}
			b := &unstructured.Unstructured{}
			b.SetGroupVersionKind(bundleGVK)
			if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: bundleName}, b); err == nil {
				upToDate = false
			} else if !apierrors.IsNotFound(err) {
				return "", err
			}
		}
		if upToDate {
			return fingerprint, nil
		}
		tgz, err := fetchGitChart(ctx, fetcher, c)
		if err != nil {
			return "", err
		}
		objects := make([]map[string]any, 0, len(pairs))
		for _, pair := range pairs {
			if len(pair.targets) == 0 {
				continue
			}
			b, err := buildGitChartBundle(bundleName, ns, fingerprint, tgz, c, vals, pair.targets)
			if err != nil {
				return "", err
			}
			b.SetNamespace(pair.ns)
			objects = append(objects, b.Object)
		}
		content, err := marshalGitResources(objects)
		if err != nil {
			return "", err
		}
		if err := r.publishBlueprintGitFile(ctx, w, bundleName, content); err != nil {
			return "", err
		}
		return fingerprint, nil
	}

	// Skip the chart download entirely when every Bundle we would write is
	// already at this fingerprint. Without this the operator re-downloads the
	// chart from Rancher on every reconcile, and a healthy workload reconciles
	// continuously as its BundleDeployment status churns.
	upToDate := true
	for _, pair := range pairs {
		if len(pair.targets) == 0 {
			continue
		}
		if !r.gitChartBundleMatches(ctx, pair.ns, bundleName, fingerprint) {
			upToDate = false
			break
		}
	}
	if upToDate {
		return fingerprint, nil
	}

	tgz, err := fetchGitChart(ctx, fetcher, c)
	if err != nil {
		return "", err
	}
	for _, pair := range pairs {
		if len(pair.targets) == 0 {
			continue
		}
		b, err := buildGitChartBundle(bundleName, ns, fingerprint, tgz, c, vals, pair.targets)
		if err != nil {
			return "", err
		}
		b.SetNamespace(pair.ns)
		if err := r.Patch(ctx, b, client.Apply, client.ForceOwnership, client.FieldOwner("aif-operator")); err != nil {
			return "", fmt.Errorf("patch Bundle %s/%s: %w", pair.ns, bundleName, err)
		}
	}
	return fingerprint, nil
}

func fetchGitChart(ctx context.Context, fetcher rancher.ChartFetcher, c aiplatformv1alpha1.BlueprintComponent) ([]byte, error) {
	tgz, err := fetcher.FetchChart(ctx, c.ChartRepo, c.ChartName, c.ChartVersion)
	if err != nil {
		return nil, fmt.Errorf("fetch chart %s@%s from git repo %q: %w", c.ChartName, c.ChartVersion, c.ChartRepo, err)
	}
	return tgz, nil
}

// gitChartBundleMatches reports whether the Bundle in ns already carries this
// fingerprint. Any read error (including NotFound) means "rebuild it".
func (r *AIWorkloadReconciler) gitChartBundleMatches(ctx context.Context, ns, bundleName, fingerprint string) bool {
	b := &unstructured.Unstructured{}
	b.SetGroupVersionKind(bundleGVK)
	if err := r.Get(ctx, types.NamespacedName{Namespace: ns, Name: bundleName}, b); err != nil {
		return false
	}
	return b.GetAnnotations()[chartFingerprintAnnotation] == fingerprint
}

// gitChartFingerprint identifies everything that feeds the generated Bundle,
// including the chart archive and the per-app release-name override. The archive
// is NOT pinned by (repo, name, version) here: this path only ever serves a
// git-backed repo, which tracks a branch, and re-pushing a chart without bumping
// Chart.yaml's version is the normal development workflow the feature exists to
// support. What does pin the archive is the repo's indexed commit (ClusterRepo
// status.commit), so that is hashed too — without it a chart that changes in
// place is never re-fetched and the Bundle serves the old chart forever.
//
// c.ReleaseName is hashed because it is baked into the Bundle's Helm options
// (see the "releaseName" key in buildGitChartBundle); without it, changing only
// the release name on an existing component would leave the fingerprint
// unchanged and the override would never reach the cluster.
//
// A change in any of these means the Bundle must be rebuilt — and the chart
// re-fetched, since the archive is the one input we cannot diff without it.
func gitChartFingerprint(c aiplatformv1alpha1.BlueprintComponent, ns, repoCommit string, vals map[string]any, targetSets ...[]any) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00", c.ChartRepo, c.ChartName, c.ChartVersion, c.ReleaseName, ns, repoCommit)
	// json.Marshal sorts map keys, so equivalent values hash equally.
	valsJSON, err := json.Marshal(vals)
	if err != nil {
		// Unhashable values: return a sentinel that never matches, so we rebuild.
		return ""
	}
	h.Write(valsJSON)
	for _, ts := range targetSets {
		tsJSON, err := json.Marshal(ts)
		if err != nil {
			return ""
		}
		h.Write(tsJSON)
	}
	return hex.EncodeToString(h.Sum(nil))
}
