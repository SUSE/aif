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
	"encoding/json"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"sigs.k8s.io/yaml"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/kustomizeoverlay"
)

// layeredOverlayDir is the spec.kustomize.dir used when a Blueprint overlay and
// an install-time overlay are layered together.
const layeredOverlayDir = "overlay"

// Kustomize Component identity, required of every directory listed under `components:`.
const (
	kustomizeComponentAPIVersion = "kustomize.config.k8s.io/v1alpha1"
	kustomizeComponentKind       = "Component"
)

// layeredKustomization is the generated top-level kustomization. Fleet's helm
// post-render (modifyKustomize) prepends the rendered chart as
// fleet-manifests.yaml to this file's resources; the components then patch it
// in order, so the install overlay applies after the Blueprint overlay.
// Verified on Fleet v0.16.2.
const layeredKustomization = `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
components:
  - blueprint
  - install
`

// asKustomizeComponent rewrites a kustomization file as a Kustomize Component,
// which is what a directory listed under `components:` must declare. All other
// fields are preserved; comments are not (the result only lives in the bundle).
func asKustomizeComponent(content string) (string, error) {
	m := map[string]any{}
	if err := yaml.Unmarshal([]byte(content), &m); err != nil {
		return "", fmt.Errorf("parse kustomization: %w", err)
	}
	m["apiVersion"] = kustomizeComponentAPIVersion
	m["kind"] = kustomizeComponentKind
	out, err := yaml.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("marshal component: %w", err)
	}
	return string(out), nil
}

// composeLayeredOverlay merges a Blueprint overlay and an install-time overlay
// into one tree under layeredOverlayDir: each set moves under blueprint/ or
// install/ with its root kustomization rewritten as a Component, plus a
// generated top-level kustomization listing them in that order. Keys must sit
// under their own dir and may not escape it.
func composeLayeredOverlay(bpDir string, bpFiles map[string]string, instDir string, instFiles map[string]string) (map[string]string, error) {
	out := map[string]string{layeredOverlayDir + "/kustomization.yaml": layeredKustomization}
	for _, layer := range []struct {
		name  string
		dir   string
		files map[string]string
	}{{"blueprint", bpDir, bpFiles}, {"install", instDir, instFiles}} {
		if err := addOverlayLayer(out, layer.name, layer.dir, layer.files); err != nil {
			return nil, fmt.Errorf("%s overlay: %w", layer.name, err)
		}
	}
	return out, nil
}

// addOverlayLayer re-keys one overlay's files from <dir>/ to
// <layeredOverlayDir>/<name>/ in out, rewriting its root kustomization file.
func addOverlayLayer(out map[string]string, name, dir string, files map[string]string) error {
	if err := kustomizeoverlay.ValidateFiles(dir, files); err != nil {
		return err
	}
	if _, err := overlayRoot(dir, files); err != nil {
		return err
	}
	prefix := dir + "/"
	for k, content := range files {
		rel := strings.TrimPrefix(path.Clean(k), prefix)
		if slices.Contains(kustomizeoverlay.FileNames, rel) {
			c, err := asKustomizeComponent(content)
			if err != nil {
				return fmt.Errorf("%s: %w", k, err)
			}
			content = c
		}
		out[layeredOverlayDir+"/"+name+"/"+rel] = content
	}
	return nil
}

// fleetKustomizationFile is the only root file Fleet's post-render reads
// (internal/helmdeployer/kustomize, v0.14 to v0.16); under any other name the
// overlay is silently skipped.
const fleetKustomizationFile = "kustomization.yaml"

// overlayRoot returns the key of the root kustomization file under dir ("" if
// none), rejecting more than one, which kustomize refuses to build.
func overlayRoot(dir string, files map[string]string) (string, error) {
	var roots []string
	for _, name := range kustomizeoverlay.FileNames {
		if _, ok := files[dir+"/"+name]; ok {
			roots = append(roots, dir+"/"+name)
		}
	}
	if len(roots) > 1 {
		return "", fmt.Errorf("overlay dir %q has more than one kustomization file (%s); keep only %s",
			dir, strings.Join(roots, ", "), fleetKustomizationFile)
	}
	if len(roots) == 0 {
		return "", nil
	}
	return roots[0], nil
}

// normalizeOverlayRoot renames a single overlay's root file to
// kustomization.yaml, the only name Fleet reads. Files already using it are
// returned unchanged, so their render digest does not change.
func normalizeOverlayRoot(dir string, files map[string]string) (map[string]string, error) {
	root, err := overlayRoot(dir, files)
	if err != nil {
		return nil, err
	}
	want := dir + "/" + fleetKustomizationFile
	if root == "" || root == want {
		return files, nil
	}
	out := maps.Clone(files)
	delete(out, root)
	out[want] = files[root]
	return out, nil
}

// resolvedOverlay is one overlay's fetched content: its dir, its files keyed
// under that dir, and the resolved git commit ("" for inline files).
type resolvedOverlay struct {
	Dir    string
	Files  map[string]string
	Commit string
}

// overlayRender is what a component's embedded-chart Bundle carries for its
// effective overlay: spec.kustomize.dir, spec.resources, and the fingerprint input.
type overlayRender struct {
	Dir       string
	Resources []any
	Digest    map[string]any
}

// hasEffectiveOverlay reports whether a component renders any Kustomize
// overlay: its Blueprint overlay, an install-time overlay, or both.
func hasEffectiveOverlay(w *aiplatformv1alpha1.AIWorkload, c aiplatformv1alpha1.BlueprintComponent) bool {
	return c.Kustomize != nil || installOverlay(w, c.ChartName) != nil
}

// usesEmbeddedChart reports whether a component is rendered as a Fleet Bundle
// with its chart embedded instead of a HelmOp: always for git-backed repos (a
// HelmOp cannot pull from git), and for any component with a Kustomize overlay,
// because Fleet's HelmOp controller drops spec.resources when it builds the
// Bundle, so a HelmOp can never carry overlay files to the agent.
func usesEmbeddedChart(w *aiplatformv1alpha1.AIWorkload, c aiplatformv1alpha1.BlueprintComponent, repoInfo clusterRepoInfo) bool {
	return repoInfo.Kind == repoKindGit || hasEffectiveOverlay(w, c)
}

// overlayDigest is the fingerprint input for a resolved overlay: the sha256 of
// its canonical digest map (dir, files, commits). "" when there is no overlay.
func overlayDigest(o *overlayRender) (string, error) {
	if o == nil {
		return "", nil
	}
	b, err := json.Marshal(o.Digest)
	if err != nil {
		return "", fmt.Errorf("hash kustomize overlay: %w", err)
	}
	return sha256Hex(b), nil
}

// overlayCommits are the gitRef overlay commits a fingerprint was taken at
// ("" for a layer that is inline or absent); the deferred read uses them.
type overlayCommits struct {
	blueprint, install string
}

// overlayFingerprint returns the fingerprint input for a component's overlays
// and, when no layer comes from git, the rendered overlay. A gitRef layer is
// identified by its repo commit so its content is only fetched on a rebuild,
// at the returned commits.
func (r *AIWorkloadReconciler) overlayFingerprint(ctx context.Context, w *aiplatformv1alpha1.AIWorkload, c aiplatformv1alpha1.BlueprintComponent) (string, *overlayRender, overlayCommits, error) {
	bpK, instK := c.Kustomize, installOverlay(w, c.ChartName)
	if !isGitOverlay(bpK) && !isGitOverlay(instK) {
		o, err := r.resolveEffectiveOverlay(ctx, w, c, overlayCommits{})
		if err != nil {
			return "", nil, overlayCommits{}, err
		}
		d, err := overlayDigest(o)
		return d, o, overlayCommits{}, err
	}
	bpCommit, err := r.overlayCommit(ctx, bpK)
	if err != nil {
		return "", nil, overlayCommits{}, err
	}
	instCommit, err := r.overlayCommit(ctx, instK)
	if err != nil {
		return "", nil, overlayCommits{}, fmt.Errorf("install overlay: %w", err)
	}
	b, err := json.Marshal(map[string]any{"blueprint": bpK, "blueprintCommit": bpCommit, "install": instK, "installCommit": instCommit})
	if err != nil {
		return "", nil, overlayCommits{}, fmt.Errorf("hash kustomize overlay: %w", err)
	}
	return sha256Hex(b), nil, overlayCommits{blueprint: bpCommit, install: instCommit}, nil
}

func isGitOverlay(k *aiplatformv1alpha1.ComponentKustomize) bool {
	return k != nil && len(k.Files) == 0 && k.GitRef != nil
}

func (r *AIWorkloadReconciler) overlayCommit(ctx context.Context, k *aiplatformv1alpha1.ComponentKustomize) (string, error) {
	if !isGitOverlay(k) {
		return "", nil
	}
	info, err := r.overlayRepo(ctx, k.GitRef.ChartRepo)
	if err != nil {
		return "", err
	}
	return indexedCommit(k.GitRef.ChartRepo, info)
}

// indexedCommit is the overlay repo commit Rancher last indexed, which both
// the fingerprint and the overlay read use.
func indexedCommit(repoName string, info clusterRepoInfo) (string, error) {
	if info.Commit == "" {
		return "", fmt.Errorf("kustomize gitRef repo %q has not been indexed yet", repoName)
	}
	return info.Commit, nil
}

// resolveEffectiveOverlay fetches a component's Blueprint overlay and its
// install-time overlay (if any) and builds the overlay its embedded Bundle carries.
// Returns nil when the component has neither. gitRef layers are read at pins
// when set (the fingerprinted commits), else at the repo's indexed commit.
func (r *AIWorkloadReconciler) resolveEffectiveOverlay(ctx context.Context, w *aiplatformv1alpha1.AIWorkload, c aiplatformv1alpha1.BlueprintComponent, pins overlayCommits) (*overlayRender, error) {
	bp, err := r.resolveOverlay(ctx, c, pins.blueprint)
	if err != nil {
		return nil, err
	}
	ic := c
	ic.Kustomize = installOverlay(w, c.ChartName)
	inst, err := r.resolveOverlay(ctx, ic, pins.install)
	if err != nil {
		return nil, fmt.Errorf("install overlay: %w", err)
	}
	return buildOverlayRender(bp, inst)
}

// resolveOverlay fetches c.Kustomize through resolveKustomizeOverlay, so inline
// and gitRef overlays get the same validation wherever they come from.
func (r *AIWorkloadReconciler) resolveOverlay(ctx context.Context, c aiplatformv1alpha1.BlueprintComponent, pin string) (*resolvedOverlay, error) {
	if c.Kustomize == nil {
		return nil, nil
	}
	files, commit, err := r.resolveKustomizeOverlay(ctx, c, pin)
	if err != nil {
		return nil, err
	}
	return &resolvedOverlay{Dir: c.Kustomize.Dir, Files: files, Commit: commit}, nil
}

// buildOverlayRender turns resolved overlays into the rendered overlay. A
// single overlay renders exactly as before install-time overlays existed (same
// dir and digest shape, so existing workloads keep their render digest); two
// overlays are layered with composeLayeredOverlay and the digest tracks both
// commits. Both inputs come from resolveOverlay, which has already checked that
// every file sits under its dir; every file also passes
// overlayFilesToResources' path and size checks.
func buildOverlayRender(bp, inst *resolvedOverlay) (*overlayRender, error) {
	var dir string
	var files map[string]string
	var digest map[string]any
	switch {
	case bp == nil && inst == nil:
		return nil, nil
	case inst == nil || bp == nil:
		only := bp
		if only == nil {
			only = inst
		}
		normalized, err := normalizeOverlayRoot(only.Dir, only.Files)
		if err != nil {
			return nil, err
		}
		dir, files = only.Dir, normalized
		digest = map[string]any{"dir": only.Dir, "files": normalized, "commit": only.Commit}
	default:
		composed, err := composeLayeredOverlay(bp.Dir, bp.Files, inst.Dir, inst.Files)
		if err != nil {
			return nil, err
		}
		dir, files = layeredOverlayDir, composed
		digest = map[string]any{"dir": dir, "files": files, "commit": bp.Commit, "installCommit": inst.Commit}
	}
	res, err := overlayFilesToResources(files)
	if err != nil {
		return nil, err
	}
	return &overlayRender{Dir: dir, Resources: res, Digest: digest}, nil
}
