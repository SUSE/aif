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
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/infra/rancher"
)

// maxBundleResourcesBytes caps the size of the payload we put in the Bundle's
// spec.resources: the chart files as stored, each gzip-compressed and
// base64-encoded (encoding "base64+gz"), plus their names. That payload, not
// the archive, is what the API server keeps: a Bundle is a plain CR, subject
// to etcd's ~1.5 MiB request limit. The archive is not a reliable proxy, since
// compressing each file separately and base64-encoding it makes the payload
// larger than the .tgz (a 265 KB archive stores as ~0.43 MB). Budget 1 MiB and
// leave the remainder of the object limit for targets, values, managedFields
// and Fleet's own status.
const maxBundleResourcesBytes = 1 << 20 // 1 MiB

// maxChartUnpackedBytes caps how far a chart may expand when unpacked, independent of how well
// it compresses. Resources are stored gzip-compressed, so maxBundleResourcesBytes no longer
// bounds the decompression work and a 4 MiB archive could expand to about 4 GB; this does
// (decompression-bomb guard).
//
// Margin: across the Rancher, partner, RKE2 and Prime charts, the largest chart that fits
// maxBundleResourcesBytes is 6.2 MiB unpacked (8.2 MiB overall), so a chart would have to grow
// about 5x before being rejected here, which leaves room for the larger AI charts to come. A
// chart over the cap, or a bomb, fails with errChartTooLarge after reading at most this many
// bytes, instead of running the operator out of memory.
//
// Cost: one file, its compressed copy and its base64 string are held at a time, so the worst
// case is about 3x this value (about 100 MiB) per reconcile.
const maxChartUnpackedBytes = 32 << 20 // 32 MiB

// maxChartArchiveBytes is a cheap pre-unpack rejection: an archive this large
// would be far over maxBundleResourcesBytes once each file is stored as
// base64+gz, so we skip the work. Decompression itself is bounded by
// maxChartUnpackedBytes.
const maxChartArchiveBytes = 4 << 20 // 4 MiB

// errChartTooLarge marks a component whose chart cannot be embedded in a Fleet
// Bundle (a git-backed chart, or one embedded because of its Kustomize overlay).
// Unlike a fetch or apply failure this is a property of the chart itself, so
// retrying can never succeed: every attempt re-downloads the archive and fails
// identically. Reconcile turns it into a terminal
// Ready=False condition instead of returning it, which both stops the hot retry
// loop and replaces the stale "Component bundles reconciled" message the
// AIWorkload would otherwise keep showing while failing.
var errChartTooLarge = stderrors.New("chart too large for a Fleet bundle")

// chartFingerprintAnnotation records what the Bundle was built from, so a
// reconcile can tell an up-to-date Bundle from a stale one without downloading
// the chart again. See embeddedChartFingerprint.
const chartFingerprintAnnotation = "ai-factory.suse.com/git-chart-fingerprint"

// buildEmbeddedChartBundle assembles a self-contained Fleet Bundle from a fetched
// chart archive. Fleet does NOT unpack a .tgz supplied as a single bundle
// resource — doing so yields a silent empty release — so the archive is expanded
// into one bundle resource per chart file (path-preserving) with spec.helm.chart
// pointing at the chart's root directory. The helm spec mirrors the one produced
// for HelmOps (releaseName/takeOwnership/disablePreProcess/values) so a
// git-backed component installs identically to an http/oci one. The chart version
// is pinned by the fetched archive itself, so spec.helm.version is omitted.
func buildEmbeddedChartBundle(bundleName, namespace, fingerprint string, tgz []byte,
	c aiplatformv1alpha1.BlueprintComponent, vals map[string]any, targets []any, dependsOn []any,
	overlay *overlayRender, epoch int64, overlayOnly bool) (*unstructured.Unstructured, error) {
	if len(tgz) > maxChartArchiveBytes {
		return nil, chartTooLarge(c, overlayOnly, fmt.Sprintf("its archive is %d MiB, over the %d MiB limit",
			len(tgz)>>20, maxChartArchiveBytes>>20), "for a git-backed repo; host it via an OCI or HTTP ClusterRepo instead")
	}

	resources, chartDir, err := chartTgzToBundleResources(tgz)
	if err != nil {
		if overlayOnly && stderrors.Is(err, errChartTooLarge) {
			return nil, chartTooLarge(c, true, "it is too large to embed in a Fleet bundle", "")
		}
		return nil, fmt.Errorf("unpack chart %q: %w", c.ChartName, err)
	}

	if overlay != nil {
		if dir := path.Clean(overlay.Dir); dir == chartDir || strings.HasPrefix(dir, chartDir+"/") {
			return nil, fmt.Errorf("kustomize overlay dir %q for %q is inside the chart directory %q", overlay.Dir, c.ChartName, chartDir)
		}
		// One stored-size budget for the whole Bundle: the encoded chart files and
		// the plain overlay files are what the API server and etcd keep together.
		if storedResourceBytes(resources)+storedResourceBytes(overlay.Resources) > maxBundleResourcesBytes {
			return nil, chartTooLarge(c, overlayOnly, fmt.Sprintf("the chart plus its Kustomize overlay exceed the %d MiB Fleet bundle payload limit",
				maxBundleResourcesBytes>>20), "")
		}
		resources = append(resources, overlay.Resources...)
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
		// expected digest (the fingerprint returned by ensureEmbeddedChartBundle),
		// so an embedded Bundle certifies the same way a HelmOp does.
		b.SetLabels(map[string]string{renderDigestLabel: renderDigestLabelValue(fingerprint)})
	}
	_ = unstructured.SetNestedField(b.Object, namespace, "spec", "defaultNamespace")
	_ = unstructured.SetNestedField(b.Object, helm, "spec", "helm")
	if targets == nil {
		targets = []any{}
	}
	_ = unstructured.SetNestedSlice(b.Object, targets, "spec", "targets")
	if len(dependsOn) > 0 {
		_ = unstructured.SetNestedSlice(b.Object, dependsOn, "spec", "dependsOn")
	}
	_ = unstructured.SetNestedSlice(b.Object, resources, "spec", "resources")
	if overlay != nil {
		// Fleet post-renders the chart with the kustomization under this dir.
		_ = unstructured.SetNestedMap(b.Object, map[string]any{"dir": overlay.Dir}, "spec", "kustomize")
	}
	if epoch > 0 {
		// Same as the HelmOp path: a Retry bumps the epoch, which forces Fleet to
		// re-sync and lets retrySyncSettled observe syncGeneration >= epoch.
		_ = unstructured.SetNestedField(b.Object, epoch, "spec", "forceSyncGeneration")
	}
	return b, nil
}

// labelOwner marks an embedded Bundle with the workload that owns it, the same
// label HelmOps carry, so a later kind switch can tell it apart from a Bundle
// it must never touch.
func labelOwner(b *unstructured.Unstructured, w *aiplatformv1alpha1.AIWorkload) {
	labels := b.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[workloadUIDLabel] = string(w.UID)
	b.SetLabels(labels)
}

// chartTooLarge builds the terminal errChartTooLarge for a chart that cannot be
// embedded. A component embedded only because it has a Kustomize overlay (an
// http/oci chart that would otherwise deploy as a HelmOp) is told so, since
// removing the overlay is its way out; otherwise the given advice applies.
func chartTooLarge(c aiplatformv1alpha1.BlueprintComponent, overlayOnly bool, detail, advice string) error {
	if overlayOnly {
		return fmt.Errorf("%w: chart %q cannot be embedded: %s; it is embedded only because this component has a Kustomize overlay, remove the overlay to deploy the chart normally",
			errChartTooLarge, c.ChartName, detail)
	}
	if advice != "" {
		return fmt.Errorf("%w: chart %q: %s %s", errChartTooLarge, c.ChartName, detail, advice)
	}
	return fmt.Errorf("%w: chart %q: %s", errChartTooLarge, c.ChartName, detail)
}

// storedResourceBytes is what a resource list costs the API server: the stored
// content (encoded or plain) plus the name, as charged by chartTgzToBundleResources.
func storedResourceBytes(resources []any) int64 {
	var n int64
	for _, r := range resources {
		m, _ := r.(map[string]any)
		content, _ := m["content"].(string)
		name, _ := m["name"].(string)
		n += int64(len(content) + len(name))
	}
	return n
}

// rejectUnsafePath rejects archive/overlay entries that escape their root.
func rejectUnsafePath(name string) error {
	if name == ".." || strings.HasPrefix(name, "../") || path.IsAbs(name) {
		return fmt.Errorf("path %q escapes the bundle root", name)
	}
	return nil
}

// overlayFilesToResources converts an inline Kustomize overlay (path->content)
// into sorted Fleet bundle resources, applying the same path-escape guard and
// 1 MiB payload cap as embedded charts.
func overlayFilesToResources(files map[string]string) ([]any, error) {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var total int64
	resources := make([]any, 0, len(names))
	for _, name := range names {
		clean := path.Clean(name)
		if err := rejectUnsafePath(clean); err != nil {
			return nil, err
		}
		content := files[name]
		total += int64(len(clean) + len(content))
		if total > maxBundleResourcesBytes {
			return nil, fmt.Errorf("%w: kustomize overlay exceeds the %d-byte Fleet bundle payload limit", errChartTooLarge, maxBundleResourcesBytes)
		}
		resources = append(resources, map[string]any{"name": clean, "content": content})
	}
	return resources, nil
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
	var raw, stored int64
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
		if err := rejectUnsafePath(name); err != nil {
			return nil, "", err
		}
		if i := strings.IndexByte(name, '/'); i > 0 && chartDir == "" {
			chartDir = name[:i]
		}
		// Read at most the remaining unpacked budget +1, so an overrun is detectable
		// without allocating past the cap (decompression-bomb guard).
		data, err := io.ReadAll(io.LimitReader(tr, maxChartUnpackedBytes-raw+1))
		if err != nil {
			return nil, "", fmt.Errorf("read %s: %w", name, err)
		}
		raw += int64(len(data))
		if raw > maxChartUnpackedBytes {
			return nil, "", fmt.Errorf(
				"%w: chart unpacks past %d MiB",
				errChartTooLarge, maxChartUnpackedBytes>>20)
		}
		// Every file is stored gzip-compressed and base64-encoded (Fleet's "base64+gz"
		// resource encoding), text and binary alike. What the API server and etcd keep is
		// this encoded string, so this is what is charged against the payload cap, and
		// compressing is what lets a chart of several MiB fit in a Bundle (a chart of
		// 2.0 MB unpacked is stored in 0.43 MB). Fleet decompresses it when it deploys the
		// chart.
		//
		// Always compressing, instead of only when it helps, keeps one code path and one
		// decode for every file; skipping tiny files would save a gzip header (about 20
		// bytes) and still need the base64 step for binaries. It is not faster: it costs
		// the operator about 0.14 s for a 165-file chart of 2.0 MB (0.28 s for the 622
		// files of rancher-monitoring) each time a Bundle is rebuilt, which only happens
		// when the chart fingerprint changes.
		content, err := gzipBase64(data)
		if err != nil {
			return nil, "", fmt.Errorf("compress %s: %w", name, err)
		}
		stored += int64(len(content)) + int64(len(name))
		if stored > maxBundleResourcesBytes {
			return nil, "", fmt.Errorf(
				"%w: chart exceeds the %d MiB Fleet bundle payload limit even compressed; host it via an OCI or HTTP ClusterRepo instead",
				errChartTooLarge, maxBundleResourcesBytes>>20)
		}
		resources = append(resources, map[string]any{"name": name, "encoding": "base64+gz", "content": content})
	}
	if chartDir == "" || len(resources) == 0 {
		return nil, "", fmt.Errorf("no chart directory found in archive")
	}
	return resources, chartDir, nil
}

// gzipBase64 compresses data with gzip and base64-encodes it, the form Fleet expects for a
// Bundle resource with encoding "base64+gz".
func gzipBase64(data []byte) (string, error) {
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return "", err
	}
	if _, err := zw.Write(data); err != nil {
		return "", err
	}
	if err := zw.Close(); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
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

// ensureEmbeddedChartBundle renders a component as a self-contained Fleet Bundle
// carrying its chart (and its Kustomize overlay, if any), and applies it
// (gitOps=false) or git-publishes it (gitOps=true). Used for git-backed charts,
// which a HelmOp cannot pull, and for any component with an overlay, because a
// HelmOp cannot carry overlay files. It reuses the same value and pull-secret
// injection and per-workspace target split as the HelmOp path.
func (r *AIWorkloadReconciler) ensureEmbeddedChartBundle(
	ctx context.Context,
	w *aiplatformv1alpha1.AIWorkload,
	c aiplatformv1alpha1.BlueprintComponent,
	bundleName string,
	repoInfo clusterRepoInfo,
	gitOps bool,
) (string, error) {
	plan, err := r.planEmbedded(ctx, w, c, repoInfo)
	if err != nil {
		return "", err
	}
	if len(plan.workspaces) == 0 {
		return "", fmt.Errorf("blueprint component %q has no target clusters", c.ChartName)
	}

	// Skip the chart download entirely when every Bundle in the cluster is
	// already at this fingerprint (for GitOps, the Bundles the GitRepo deployed
	// from the published file). Without this the operator re-downloads the
	// chart on every reconcile, and a healthy workload reconciles continuously
	// as its BundleDeployment status churns.
	upToDate := true
	for _, ws := range plan.workspaces {
		if !r.embeddedBundleMatches(ctx, ws.ns, bundleName, plan.fingerprint) {
			upToDate = false
			break
		}
	}
	if upToDate {
		return plan.fingerprint, nil
	}

	if err := r.loadOverlay(ctx, w, c, plan); err != nil {
		return "", err
	}
	tgz, err := r.fetchChartArchive(ctx, c, repoInfo)
	if err != nil {
		return "", err
	}
	bundles, err := plan.bundles(w, c, bundleName, tgz)
	if err != nil {
		return "", err
	}
	if gitOps {
		objects := make([]map[string]any, 0, len(bundles))
		for _, b := range bundles {
			objects = append(objects, b.Object)
		}
		content, err := marshalGitResources(objects)
		if err != nil {
			return "", err
		}
		if err := r.publishBlueprintGitFile(ctx, w, bundleName, content); err != nil {
			return "", err
		}
		return plan.fingerprint, nil
	}
	for _, b := range bundles {
		if err := r.Patch(ctx, b, client.Apply, client.ForceOwnership, client.FieldOwner("aif-operator")); err != nil {
			return "", fmt.Errorf("patch Bundle %s/%s: %w", b.GetNamespace(), bundleName, err)
		}
	}
	return plan.fingerprint, nil
}

// embeddedPlan is everything needed to build one component's embedded-chart
// Bundles except the chart archive itself.
type embeddedPlan struct {
	ns              string
	vals            map[string]any
	depObjs         []any
	overlay         *overlayRender
	overlayDeferred bool
	epoch           int64
	overlayOnly     bool
	fingerprint     string
	workspaces      []fleetWorkspace
}

// planEmbedded resolves a component's values, pull secrets, overlay and
// fingerprint for an embedded-chart Bundle.
func (r *AIWorkloadReconciler) planEmbedded(ctx context.Context, w *aiplatformv1alpha1.AIWorkload, c aiplatformv1alpha1.BlueprintComponent, repoInfo clusterRepoInfo) (*embeddedPlan, error) {
	// A git-backed repo is unusable without the Rancher catalog client; report
	// that up front rather than only when a re-fetch is needed.
	if repoInfo.Kind == repoKindGit && (r.CatalogClient == nil || r.CatalogClient.Get() == nil) {
		return nil, fmt.Errorf("%w: git-backed ClusterRepo %q needs a Rancher API token", errCatalogClientNotConfigured, c.ChartRepo)
	}

	// Resolve values and inject pull secrets BEFORE deciding whether to re-fetch:
	// the injector mutates vals (it appends imagePullSecrets), so the fingerprint
	// has to be taken after it runs, and secret delivery must happen on every
	// reconcile regardless of whether the chart itself changed.
	vals, err := resolveComponentValues(w, c)
	if err != nil {
		return nil, fmt.Errorf("resolve component values for %s: %w", c.ChartName, err)
	}
	ns := componentNamespace(w, c)
	created, err := r.injectorForRepo(c.Vendor, repoInfo).Apply(ctx, r.localCC(), ns, repoInfo, vals, targetsLocalCluster(w))
	if err != nil {
		return nil, fmt.Errorf("inject secrets for %s: %w", c.ChartName, err)
	}
	w.Status.PullSecretDeliveries = mergePullSecretDelivery(w.Status.PullSecretDeliveries, ns, created)

	ovDigest, overlay, err := r.overlayFingerprint(ctx, w, c)
	if err != nil {
		return nil, fmt.Errorf("resolve kustomize overlay for %s: %w", c.ChartName, err)
	}
	epoch := r.retryEpochValue(w)
	// A git-backed repo tracks a branch, so its indexed commit pins the archive;
	// an http/oci chart version is immutable, so the repo URL identifies the source.
	sourceRevision := repoInfo.Commit
	if repoInfo.Kind != repoKindGit {
		sourceRevision = repoInfo.URL
	}
	localTargets, downstreamTargets := splitWorkloadTargets(w)
	return &embeddedPlan{
		ns:              ns,
		vals:            vals,
		depObjs:         resolveDependsOn(w, c.DependsOn),
		overlay:         overlay,
		overlayDeferred: overlay == nil && ovDigest != "",
		epoch:           epoch,
		// overlayOnly: the chart is embedded only because of its overlay
		// (http/oci), which changes the advice in a size error.
		overlayOnly: repoInfo.Kind != repoKindGit,
		fingerprint: embeddedChartFingerprint(c, ns, sourceRevision, vals, resolveDependsOnNames(w, c.DependsOn), ovDigest, epoch, localTargets, downstreamTargets),
		workspaces:  targetedWorkspaces(w),
	}, nil
}

// bundles builds the plan's Bundles, one per targeted workspace, from tgz.
func (p *embeddedPlan) bundles(w *aiplatformv1alpha1.AIWorkload, c aiplatformv1alpha1.BlueprintComponent, bundleName string, tgz []byte) ([]*unstructured.Unstructured, error) {
	out := make([]*unstructured.Unstructured, 0, len(p.workspaces))
	for _, ws := range p.workspaces {
		b, err := buildEmbeddedChartBundle(bundleName, p.ns, p.fingerprint, tgz, c, p.vals, ws.targets, p.depObjs, p.overlay, p.epoch, p.overlayOnly)
		if err != nil {
			return nil, err
		}
		b.SetNamespace(ws.ns)
		labelOwner(b, w)
		out = append(out, b)
	}
	return out, nil
}

// loadOverlay fetches the gitRef overlay planEmbedded deferred.
func (r *AIWorkloadReconciler) loadOverlay(ctx context.Context, w *aiplatformv1alpha1.AIWorkload, c aiplatformv1alpha1.BlueprintComponent, p *embeddedPlan) error {
	if !p.overlayDeferred {
		return nil
	}
	o, err := r.resolveEffectiveOverlay(ctx, w, c)
	if err != nil {
		return fmt.Errorf("resolve kustomize overlay for %s: %w", c.ChartName, err)
	}
	p.overlay, p.overlayDeferred = o, false
	return nil
}

// validateEmbedded builds a component's embedded-chart Bundles without writing
// them, proving the chart can be fetched and embedded with its overlay.
func (r *AIWorkloadReconciler) validateEmbedded(ctx context.Context, w *aiplatformv1alpha1.AIWorkload, c aiplatformv1alpha1.BlueprintComponent, bundleName string, repoInfo clusterRepoInfo) error {
	plan, err := r.planEmbedded(ctx, w, c, repoInfo)
	if err != nil {
		return err
	}
	if err := r.loadOverlay(ctx, w, c, plan); err != nil {
		return err
	}
	tgz, err := r.fetchChartArchive(ctx, c, repoInfo)
	if err != nil {
		return err
	}
	_, err = plan.bundles(w, c, bundleName, tgz)
	return err
}

// embeddedValidator returns the check a kind switch runs before its first
// destructive step: for a switch to an embedded Bundle, that the new Bundle can
// be built. A switch to a HelmOp needs none (rendering a HelmOp cannot fail
// that way).
func (r *AIWorkloadReconciler) embeddedValidator(ctx context.Context, w *aiplatformv1alpha1.AIWorkload, c aiplatformv1alpha1.BlueprintComponent, bundleName string, repoInfo clusterRepoInfo, want renderKind) func() error {
	if want != kindEmbeddedBundle {
		return nil
	}
	return func() error { return r.validateEmbedded(ctx, w, c, bundleName, repoInfo) }
}

func fetchGitChart(ctx context.Context, fetcher rancher.ChartFetcher, c aiplatformv1alpha1.BlueprintComponent) ([]byte, error) {
	tgz, err := fetcher.FetchChart(ctx, c.ChartRepo, c.ChartName, c.ChartVersion)
	if err != nil {
		return nil, fmt.Errorf("fetch chart %s@%s from git repo %q: %w", c.ChartName, c.ChartVersion, c.ChartRepo, err)
	}
	return tgz, nil
}

// embeddedBundleMatches reports whether the Bundle in ns already carries this
// fingerprint. Any read error (including NotFound) means "rebuild it".
func (r *AIWorkloadReconciler) embeddedBundleMatches(ctx context.Context, ns, bundleName, fingerprint string) bool {
	b := &unstructured.Unstructured{}
	b.SetGroupVersionKind(bundleGVK)
	if err := r.Get(ctx, types.NamespacedName{Namespace: ns, Name: bundleName}, b); err != nil {
		return false
	}
	return b.GetAnnotations()[chartFingerprintAnnotation] == fingerprint
}

// embeddedChartFingerprint identifies everything that feeds the generated Bundle,
// including the chart archive, the overlay and the per-app release-name override.
// sourceRevision pins the archive: for a git-backed repo, which tracks a branch
// (re-pushing a chart without bumping Chart.yaml's version is the normal
// development workflow there), it is the repo's indexed commit (ClusterRepo
// status.commit) — without it a chart that changes in place is never re-fetched.
// For an http/oci repo, whose published versions are immutable, it is the repo
// URL. The overlay digest and retry epoch are hashed only when set, so existing
// git-backed components keep their fingerprint.
//
// c.ReleaseName is hashed because it is baked into the Bundle's Helm options
// (see the "releaseName" key in buildEmbeddedChartBundle); without it, changing only
// the release name on an existing component would leave the fingerprint
// unchanged and the override would never reach the cluster.
//
// A change in any of these means the Bundle must be rebuilt — and the chart
// re-fetched, since the archive is the one input we cannot diff without it.
func embeddedChartFingerprint(c aiplatformv1alpha1.BlueprintComponent, ns, sourceRevision string, vals map[string]any, dependsOn []string,
	overlayDigest string, epoch int64, targetSets ...[]any) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00", c.ChartRepo, c.ChartName, c.ChartVersion, c.ReleaseName, ns, sourceRevision)
	// json.Marshal sorts map keys, so equivalent values hash equally.
	valsJSON, err := json.Marshal(vals)
	if err != nil {
		// Unhashable values: return a sentinel that never matches, so we rebuild.
		return ""
	}
	h.Write(valsJSON)
	// Only hash dependsOn when non-empty, so a component with no deps fingerprints
	// identically to before this field existed (frictionless upgrade).
	if len(dependsOn) > 0 {
		depJSON, err := json.Marshal(dependsOn)
		if err != nil {
			return ""
		}
		h.Write(depJSON)
	}
	// Overlay and retry epoch are only hashed when set, so an existing git-backed
	// component (no overlay, epoch 0) keeps its fingerprint across the upgrade.
	if overlayDigest != "" {
		_, _ = fmt.Fprintf(h, "overlay\x00%s\x00", overlayDigest)
	}
	if epoch > 0 {
		_, _ = fmt.Fprintf(h, "epoch\x00%d\x00", epoch)
	}
	for _, ts := range targetSets {
		tsJSON, err := json.Marshal(ts)
		if err != nil {
			return ""
		}
		h.Write(tsJSON)
	}
	return hex.EncodeToString(h.Sum(nil))
}
