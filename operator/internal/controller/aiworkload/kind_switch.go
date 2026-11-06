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
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// renderKind is how a component's bundle is rendered.
type renderKind int

const (
	kindHelmOp renderKind = iota
	kindEmbeddedBundle
)

func (k renderKind) String() string {
	if k == kindEmbeddedBundle {
		return "an embedded bundle"
	}
	return "a HelmOp"
}

// switchRequeue is how often a component that is changing render kind is
// re-checked. Every step waits on Fleet (a Bundle update reaching its
// BundleDeployments, an object being removed), never on the operator.
const switchRequeue = 5 * time.Second

// helmOpBundleLabel is the label Fleet's HelmOp controller puts on the Bundle it
// generates (fleet.HelmOpLabel in Fleet's helmop_types.go); it is the only link
// between the two (there is no ownerReference), and Fleet's finalizer purges the
// HelmOp's Bundles by it.
const helmOpBundleLabel = "fleet.cattle.io/fleet-helm-name"

// maxGitOpsFileBytes bounds reading a component's published file back: two
// Fleet objects (fleet-local and fleet-default), each an embedded Bundle of up
// to maxBundleResourcesBytes of resources.
const maxGitOpsFileBytes = 4 << 20

// switchPhase is the next step a component's kind switch needs, as observed.
// The order is the order a switch goes through them.
type switchPhase int

const (
	switchDone        switchPhase = iota // nothing of the other kind left
	switchClearKeep                      // done, but the remaining object still carries keepResources from an abandoned switch
	switchSetKeep                        // set keepResources on the old object
	switchWaitApplied                    // wait until every BundleDeployment applied keepResources
	switchDelete                         // delete the old object
	switchWaitGone                       // wait until the old object and its BundleDeployments are gone
)

func (p switchPhase) String() string {
	switch p {
	case switchSetKeep:
		return "keeping the deployed resources"
	case switchWaitApplied:
		return "waiting for Fleet to apply keepResources"
	case switchDelete:
		return "removing the previous Fleet object"
	case switchWaitGone:
		return "waiting for the previous Fleet object to be removed"
	}
	return "done"
}

// switchObservation is what observeKindSwitch found: the next phase, the object
// that phase acts on, and for a wait, which BundleDeployments it waits on.
type switchObservation struct {
	phase  switchPhase
	target *unstructured.Unstructured
	detail string
}

func (o switchObservation) pending() bool {
	return o.phase != switchDone && o.phase != switchClearKeep
}

// componentSwitchingError reports a component whose render kind is still
// changing. The reconcile loop records it, carries on with the other
// components, and reports Ready=False "Switching" with a short requeue.
type componentSwitchingError struct{ msg string }

func (e *componentSwitchingError) Error() string { return e.msg }

// observeKindSwitch decides the next step for moving bundleName in ns to want,
// from cluster state alone, so it is idempotent across reconciles. Changing kind
// without uninstalling relies on Fleet's keepResources: deleting a Bundle makes
// the agent helm-uninstall the release unless the deployed release carries
// keepResources, which the agent stamps at install/upgrade time. So keepResources
// must reach every BundleDeployment, and be applied there, before the old object
// is deleted; and the new object is only written once the old one and its
// BundleDeployments are gone, so the agent's cleanup of the old release (keyed by
// the same bundle name) cannot touch the new one.
func (r *AIWorkloadReconciler) observeKindSwitch(ctx context.Context, w *aiplatformv1alpha1.AIWorkload, ns, bundleName string, want renderKind) (switchObservation, error) {
	ho, err := r.getFleetObject(ctx, helmOpGVK, ns, bundleName)
	if err != nil {
		return switchObservation{}, err
	}
	bundle, err := r.getFleetObject(ctx, bundleGVK, ns, bundleName)
	if err != nil {
		return switchObservation{}, err
	}
	operatorBundle := bundle != nil && isOperatorBundle(bundle)

	if want == kindHelmOp {
		if ho != nil {
			return clearLeftoverKeep(w, ho), nil
		}
		if operatorBundle {
			return r.retire(ctx, w, ns, bundleName, bundle)
		}
		// A HelmOp-generated Bundle without its HelmOp is adopted by name once the
		// HelmOp is written; only a bare set of old BundleDeployments must drain.
		return r.waitForBundleDeployments(ctx, ns, bundleName, bundle == nil)
	}

	if ho != nil {
		return r.retire(ctx, w, ns, bundleName, ho)
	}
	if bundle != nil && bundle.GetLabels()[helmOpBundleLabel] != "" {
		// The HelmOp is gone but Fleet has not removed the Bundle it generated:
		// that cleanup is racy (see deleteBundleIn), so delete it directly once
		// keepResources is applied there too.
		return r.retire(ctx, w, ns, bundleName, bundle)
	}
	if operatorBundle {
		return clearLeftoverKeep(w, bundle), nil
	}
	return r.waitForBundleDeployments(ctx, ns, bundleName, bundle == nil)
}

// isOperatorBundle tells an embedded-chart Bundle the operator wrote from one
// Fleet generated for a HelmOp: no HelmOp label, and the chart fingerprint
// annotation only buildEmbeddedChartBundle sets.
func isOperatorBundle(b *unstructured.Unstructured) bool {
	return b.GetLabels()[helmOpBundleLabel] == "" && b.GetAnnotations()[chartFingerprintAnnotation] != ""
}

// retire is the hand-over of an old object: keepResources set, then applied on
// every BundleDeployment, then delete, then wait until it is gone.
func (r *AIWorkloadReconciler) retire(ctx context.Context, w *aiplatformv1alpha1.AIWorkload, ns, bundleName string, old *unstructured.Unstructured) (switchObservation, error) {
	if old.GetLabels()[workloadUIDLabel] != string(w.UID) {
		return switchObservation{}, fmt.Errorf("%s %s/%s is not owned by this workload; refusing to replace it", old.GetKind(), ns, bundleName)
	}
	if old.GetDeletionTimestamp() != nil {
		return switchObservation{phase: switchWaitGone}, nil
	}
	if keep, _, _ := unstructured.NestedBool(old.Object, "spec", "keepResources"); !keep {
		return switchObservation{phase: switchSetKeep, target: old}, nil
	}
	pending, err := r.keepResourcesPending(ctx, ns, bundleName)
	if err != nil {
		return switchObservation{}, err
	}
	if len(pending) > 0 {
		return switchObservation{phase: switchWaitApplied, detail: strings.Join(pending, ", ")}, nil
	}
	return switchObservation{phase: switchDelete, target: old}, nil
}

// clearLeftoverKeep reports done, or done-but-clear when the object that stays
// still carries keepResources from a switch that was abandoned (the overlay was
// added and removed again before it finished). Left in place, deleting the
// workload later would orphan the component's resources instead of removing them.
func clearLeftoverKeep(w *aiplatformv1alpha1.AIWorkload, obj *unstructured.Unstructured) switchObservation {
	keep, _, _ := unstructured.NestedBool(obj.Object, "spec", "keepResources")
	if keep && obj.GetLabels()[workloadUIDLabel] == string(w.UID) {
		return switchObservation{phase: switchClearKeep, target: obj}
	}
	return switchObservation{phase: switchDone}
}

// waitForBundleDeployments keeps a switch waiting while the old Bundle's
// BundleDeployments are still being removed by the agent (only checked when no
// Bundle of that name exists, i.e. the old one is gone and the new one not yet written).
func (r *AIWorkloadReconciler) waitForBundleDeployments(ctx context.Context, ns, bundleName string, noBundle bool) (switchObservation, error) {
	if !noBundle {
		return switchObservation{phase: switchDone}, nil
	}
	bds, err := r.listBundleDeployments(ctx, ns, bundleName)
	if err != nil || len(bds) == 0 {
		return switchObservation{phase: switchDone}, err
	}
	return switchObservation{phase: switchWaitGone}, nil
}

func (r *AIWorkloadReconciler) getFleetObject(ctx context.Context, gvk schema.GroupVersionKind, ns, name string) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)
	if err := r.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, u); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return u, nil
}

func (r *AIWorkloadReconciler) listBundleDeployments(ctx context.Context, ns, bundleName string) ([]unstructured.Unstructured, error) {
	bdList := &unstructured.UnstructuredList{}
	bdList.SetGroupVersionKind(schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "BundleDeploymentList"})
	if err := r.List(ctx, bdList, client.MatchingLabels{
		"fleet.cattle.io/bundle-name":      bundleName,
		"fleet.cattle.io/bundle-namespace": ns,
	}); err != nil {
		return nil, err
	}
	return bdList.Items, nil
}

// keepResourcesPending lists the BundleDeployments of ns/bundleName that have
// not yet applied keepResources, as "<namespace> (<state>)" so the condition
// says what is holding the switch (an offline cluster, an ErrApplied
// deployment). Checking the options too matters: right after keepResources is
// set, Fleet has not yet recomputed the BundleDeployment, whose old deploymentID
// still equals the applied one. None pending means safe to delete.
func (r *AIWorkloadReconciler) keepResourcesPending(ctx context.Context, ns, bundleName string) ([]string, error) {
	bds, err := r.listBundleDeployments(ctx, ns, bundleName)
	if err != nil {
		return nil, err
	}
	pending := make([]string, 0, len(bds))
	for i := range bds {
		bd := bds[i].Object
		keep, _, _ := unstructured.NestedBool(bd, "spec", "options", "keepResources")
		want, _, _ := unstructured.NestedString(bd, "spec", "deploymentID")
		applied, _, _ := unstructured.NestedString(bd, "status", "appliedDeploymentID")
		if keep && want != "" && applied == want {
			continue
		}
		state, _, _ := unstructured.NestedString(bd, "status", "display", "state")
		if state == "" {
			state = "not applied"
		}
		pending = append(pending, fmt.Sprintf("%s (%s)", bds[i].GetNamespace(), state))
	}
	return pending, nil
}

// actuateSwitch performs an observed step directly on the cluster.
func (r *AIWorkloadReconciler) actuateSwitch(ctx context.Context, ns, bundleName string, obs switchObservation) (done bool, msg string, err error) {
	switch obs.phase {
	case switchDone:
		return true, "", nil
	case switchClearKeep:
		return true, "", r.patchKeepResources(ctx, obs.target, ns, bundleName, false)
	case switchSetKeep:
		if err := r.patchKeepResources(ctx, obs.target, ns, bundleName, true); err != nil {
			return false, "", err
		}
	case switchDelete:
		if err := r.Delete(ctx, obs.target, client.PropagationPolicy(metav1.DeletePropagationForeground)); client.IgnoreNotFound(err) != nil {
			return false, "", fmt.Errorf("delete %s %s/%s: %w", obs.target.GetKind(), ns, bundleName, err)
		}
	}
	return false, switchMessage(obs), nil
}

func switchMessage(obs switchObservation) string {
	if obs.detail != "" {
		return obs.phase.String() + " on " + obs.detail
	}
	return obs.phase.String()
}

// patchKeepResources sets spec.keepResources, or removes it, with a merge patch.
func (r *AIWorkloadReconciler) patchKeepResources(ctx context.Context, obj *unstructured.Unstructured, ns, bundleName string, keep bool) error {
	body := `{"spec":{"keepResources":null}}`
	if keep {
		body = `{"spec":{"keepResources":true}}`
	}
	if err := r.Patch(ctx, obj, client.RawPatch(types.MergePatchType, []byte(body))); err != nil {
		return fmt.Errorf("update keepResources on %s %s/%s: %w", obj.GetKind(), ns, bundleName, err)
	}
	return nil
}

// fleetWorkspace is a Fleet namespace the workload targets, with its targets.
type fleetWorkspace struct {
	ns      string
	targets []any
}

// targetedWorkspaces returns the Fleet workspaces a workload deploys to:
// fleet-local for the local cluster, fleet-default for downstream clusters.
func targetedWorkspaces(w *aiplatformv1alpha1.AIWorkload) []fleetWorkspace {
	local, downstream := splitWorkloadTargets(w)
	var out []fleetWorkspace
	if len(local) > 0 {
		out = append(out, fleetWorkspace{"fleet-local", local})
	}
	if len(downstream) > 0 {
		out = append(out, fleetWorkspace{"fleet-default", downstream})
	}
	return out
}

// ensureComponentKind moves a component to want in every targeted workspace,
// one step per reconcile, and returns a componentSwitchingError while any is in
// progress. validate (optional) proves the new object can be built; it runs
// right before the first destructive step, so a component whose new Bundle
// cannot be built (download failure, chart too large) keeps its old object and
// reports that error instead of being left unmanaged.
func (r *AIWorkloadReconciler) ensureComponentKind(ctx context.Context, w *aiplatformv1alpha1.AIWorkload, c aiplatformv1alpha1.BlueprintComponent, bundleName string, want renderKind, gitOps bool, validate func() error) error {
	type observed struct {
		ns  string
		obs switchObservation
	}
	workspaces := targetedWorkspaces(w)
	all := make([]observed, 0, len(workspaces))
	var next *switchObservation
	for _, ws := range workspaces {
		obs, err := r.observeKindSwitch(ctx, w, ws.ns, bundleName, want)
		if err != nil {
			return err
		}
		all = append(all, observed{ws.ns, obs})
		if obs.pending() && (next == nil || obs.phase < next.phase) {
			o := obs
			next = &o
		}
	}
	if next == nil {
		if !gitOps {
			// Clear any keepResources left by an abandoned switch. (GitOps needs
			// nothing: republishing the desired object drops it from the file.)
			for _, o := range all {
				if _, _, err := r.actuateSwitch(ctx, o.ns, bundleName, o.obs); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if next.phase == switchSetKeep && validate != nil {
		if err := validate(); err != nil {
			return err
		}
	}
	if gitOps {
		if err := r.actuateSwitchGitOps(ctx, w, bundleName, *next); err != nil {
			return err
		}
	} else {
		for _, o := range all {
			if _, _, err := r.actuateSwitch(ctx, o.ns, bundleName, o.obs); err != nil {
				return err
			}
		}
	}
	return &componentSwitchingError{msg: fmt.Sprintf("Component %q is switching to %s: %s.", c.ChartName, want, switchMessage(*next))}
}

// actuateSwitchGitOps performs a step for the GitOps strategy, where the old
// object is deployed by the GitRepo from the component's file: keepResources is
// republished in that file and the file is removed to delete it. One file holds
// every workspace's objects, so the least advanced workspace's step is taken
// once. A leftover Bundle Fleet generated for a HelmOp is not in the file and
// is deleted directly.
func (r *AIWorkloadReconciler) actuateSwitchGitOps(ctx context.Context, w *aiplatformv1alpha1.AIWorkload, bundleName string, next switchObservation) error {
	switch next.phase {
	case switchSetKeep:
		return r.republishKeepResources(ctx, w, bundleName, true, 0, "")
	case switchDelete:
		if next.target.GetLabels()[helmOpBundleLabel] != "" && next.target.GetKind() == "Bundle" {
			if err := r.Delete(ctx, next.target, client.PropagationPolicy(metav1.DeletePropagationForeground)); client.IgnoreNotFound(err) != nil {
				return fmt.Errorf("delete Bundle %s/%s: %w", next.target.GetNamespace(), bundleName, err)
			}
			return nil
		}
		if err := r.deleteGitFileByName(ctx, w, bundleName); err != nil {
			return fmt.Errorf("remove %s: %w", gitOpsFilePath(bundleName), err)
		}
		// Forget the published hash so the new kind is always written, even if
		// its content happened to match an earlier publication.
		if _, ok := w.Annotations[gitFileHashAnnotation(bundleName)]; ok {
			delete(w.Annotations, gitFileHashAnnotation(bundleName))
			return r.Update(ctx, w)
		}
	}
	return nil
}

// republishKeepResources rewrites the component's published file with
// spec.keepResources set (or removed) on every object in it. When gen > 0 it also
// sets spec.forceSyncGeneration and the release annotations (see releaseKeepResources). It edits what was published
// rather than re-rendering, because the old kind may no longer be renderable
// (removing an overlay leaves nothing to render the old Bundle from).
func (r *AIWorkloadReconciler) republishKeepResources(ctx context.Context, w *aiplatformv1alpha1.AIWorkload, bundleName string, keep bool, gen int64, releasedAt string) error {
	s, err := r.gitOpsSettings(ctx)
	if err != nil {
		return err
	}
	gc, err := r.gitOpsClient(ctx, s)
	if err != nil {
		return err
	}
	content, ok, err := gc.ReadFile(ctx, gitOpsFilePath(bundleName), maxGitOpsFileBytes)
	if err != nil {
		return err
	}
	if !ok {
		// The deployed object did not come from this file (or the file was
		// removed), so keepResources cannot be published for it.
		return fmt.Errorf("%s not found in the GitOps repository, cannot update keepResources", gitOpsFilePath(bundleName))
	}
	docs := strings.Split(content, "\n---\n")
	objects := make([]map[string]any, 0, len(docs))
	for _, doc := range docs {
		obj := map[string]any{}
		if err := json.Unmarshal([]byte(doc), &obj); err != nil {
			return fmt.Errorf("decode %s: %w", gitOpsFilePath(bundleName), err)
		}
		if !keep {
			unstructured.RemoveNestedField(obj, "spec", "keepResources")
		} else if err := unstructured.SetNestedField(obj, true, "spec", "keepResources"); err != nil {
			return fmt.Errorf("set keepResources in %s: %w", gitOpsFilePath(bundleName), err)
		}
		if gen > 0 {
			if err := unstructured.SetNestedField(obj, gen, "spec", "forceSyncGeneration"); err != nil {
				return fmt.Errorf("set forceSyncGeneration in %s: %w", gitOpsFilePath(bundleName), err)
			}
			u := unstructured.Unstructured{Object: obj}
			ann := u.GetAnnotations()
			if ann == nil {
				ann = map[string]string{}
			}
			ann[keepReleasedAnnotation] = releasedAt
			ann[keepReleaseGenAnnotation] = strconv.FormatInt(gen, 10)
			u.SetAnnotations(ann)
		}
		objects = append(objects, obj)
	}
	updated, err := marshalGitResources(objects)
	if err != nil {
		return err
	}
	return r.publishBlueprintGitFile(ctx, w, bundleName, updated)
}

// keepReleasedAnnotation and keepReleaseGenAnnotation mark a HelmOp or Bundle whose
// keepResources deletion cleared: when, and the forceSyncGeneration set alongside,
// which its BundleDeployments must reach before the release is known to be applied
// without keep. They live on the Fleet objects, which are read uncached, so a
// reconcile can never miss them the way it could a stale cached AIWorkload.
const (
	keepReleasedAnnotation   = "ai-factory.suse.com/keep-resources-released-at"
	keepReleaseGenAnnotation = "ai-factory.suse.com/keep-resources-release-generation"
	keepReleaseTimeout       = 5 * time.Minute
)

// releaseKeepResources clears keepResources on the workload's Fleet objects and
// reports whether deletion must wait for Fleet to apply that. Without it, deleting
// mid-switch would leave the deployed release behind instead of uninstalling it.
// Clearing keepResources alone restores the pre-switch deploymentID, which may equal
// the last applied one while the agent is still applying keep, so forceSyncGeneration
// is bumped too: only a deploymentID carrying the new generation proves the release.
func (r *AIWorkloadReconciler) releaseKeepResources(ctx context.Context, w *aiplatformv1alpha1.AIWorkload) (bool, error) {
	wait := false
	for _, name := range w.Spec.FleetBundleNames {
		objs, err := r.ownedFleetObjects(ctx, w, name)
		if err != nil {
			return false, err
		}
		var gen, target int64
		var since time.Time
		kept := false
		for _, obj := range objs {
			if g, _, _ := unstructured.NestedInt64(obj.Object, "spec", "forceSyncGeneration"); g > gen {
				gen = g
			}
			if k, _, _ := unstructured.NestedBool(obj.Object, "spec", "keepResources"); k {
				kept = true
			}
			if g, err := strconv.ParseInt(obj.GetAnnotations()[keepReleaseGenAnnotation], 10, 64); err == nil && g > target {
				target = g
			}
			if t, err := time.Parse(time.RFC3339, obj.GetAnnotations()[keepReleasedAnnotation]); err == nil && (since.IsZero() || t.Before(since)) {
				since = t
			}
		}
		if !since.IsZero() && time.Since(since) > keepReleaseTimeout {
			continue
		}
		if kept {
			if since.IsZero() {
				since = time.Now().UTC()
			}
			gen = max(gen+1, target)
			at := since.Format(time.RFC3339)
			if w.Spec.DeployStrategy == aiplatformv1alpha1.AIWorkloadDeployGitOps {
				if err := r.republishKeepResources(ctx, w, name, false, gen, at); err != nil {
					return false, err
				}
			} else {
				for _, obj := range objs {
					if err := r.releaseKeep(ctx, obj, name, gen, at); err != nil {
						return false, err
					}
				}
			}
			wait = true
			continue
		}
		if target == 0 {
			continue
		}
		pending, err := r.releasePending(ctx, name, target)
		if err != nil {
			return false, err
		}
		wait = wait || pending
	}
	return wait, nil
}

// releasePending reports whether any BundleDeployment of name has yet to apply a
// deployment without keepResources at forceSyncGeneration target or later.
func (r *AIWorkloadReconciler) releasePending(ctx context.Context, name string, target int64) (bool, error) {
	for _, ns := range fleetNamespaces {
		bds, err := r.listBundleDeployments(ctx, ns, name)
		if err != nil {
			return false, err
		}
		for i := range bds {
			keep, _, _ := unstructured.NestedBool(bds[i].Object, "spec", "options", "keepResources")
			fsg, _, _ := unstructured.NestedInt64(bds[i].Object, "spec", "options", "forceSyncGeneration")
			want, _, _ := unstructured.NestedString(bds[i].Object, "spec", "deploymentID")
			applied, _, _ := unstructured.NestedString(bds[i].Object, "status", "appliedDeploymentID")
			if keep || fsg < target || applied != want {
				return true, nil
			}
		}
	}
	return false, nil
}

// releaseKeep removes keepResources from obj, sets its forceSyncGeneration to gen
// and records the release on it.
func (r *AIWorkloadReconciler) releaseKeep(ctx context.Context, obj *unstructured.Unstructured, bundleName string, gen int64, at string) error {
	body := fmt.Sprintf(`{"metadata":{"annotations":{%q:%q,%q:%q}},"spec":{"keepResources":null,"forceSyncGeneration":%d}}`,
		keepReleasedAnnotation, at, keepReleaseGenAnnotation, strconv.FormatInt(gen, 10), gen)
	if err := r.Patch(ctx, obj, client.RawPatch(types.MergePatchType, []byte(body))); err != nil {
		return fmt.Errorf("release keepResources on %s %s/%s: %w", obj.GetKind(), obj.GetNamespace(), bundleName, err)
	}
	return nil
}

// ownedFleetObjects returns this workload's HelmOps and Bundles named name.
func (r *AIWorkloadReconciler) ownedFleetObjects(ctx context.Context, w *aiplatformv1alpha1.AIWorkload, name string) ([]*unstructured.Unstructured, error) {
	var out []*unstructured.Unstructured
	for _, ns := range fleetNamespaces {
		for _, gvk := range []schema.GroupVersionKind{helmOpGVK, bundleGVK} {
			obj, err := r.getFleetObject(ctx, gvk, ns, name)
			if err != nil {
				return nil, err
			}
			if obj != nil && obj.GetLabels()[workloadUIDLabel] == string(w.UID) {
				out = append(out, obj)
			}
		}
	}
	return out, nil
}
