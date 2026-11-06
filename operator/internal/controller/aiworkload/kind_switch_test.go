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
	"fmt"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	infrahelm "github.com/SUSE/aif-operator/internal/infra/helm"
)

const (
	switchUID    = "uid-1"
	switchBundle = "wl-comp"
)

func switchWorkload() *aiplatformv1alpha1.AIWorkload {
	w := newBlueprintWorkload(time.Now())
	w.UID = switchUID
	return w
}

func seedHelmOp(name, uid string, keep bool) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(helmOpGVK)
	u.SetNamespace("fleet-local")
	u.SetName(name)
	u.SetLabels(map[string]string{workloadUIDLabel: uid})
	if keep {
		_ = unstructured.SetNestedField(u.Object, true, "spec", "keepResources")
	}
	return u
}

func seedBundle(name string, labels map[string]string, keep bool) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(bundleGVK)
	u.SetNamespace("fleet-local")
	u.SetName(name)
	u.SetLabels(labels)
	if keep {
		_ = unstructured.SetNestedField(u.Object, true, "spec", "keepResources")
	}
	return u
}

func seedBD(bundleName, deploymentID, appliedID string, keep bool) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(bundleDeploymentGVK)
	u.SetNamespace("cluster-fleet-local-local-abc")
	u.SetName(bundleName)
	u.SetLabels(map[string]string{"fleet.cattle.io/bundle-name": bundleName, "fleet.cattle.io/bundle-namespace": "fleet-local"})
	_ = unstructured.SetNestedField(u.Object, deploymentID, "spec", "deploymentID")
	_ = unstructured.SetNestedField(u.Object, keep, "spec", "options", "keepResources")
	_ = unstructured.SetNestedField(u.Object, appliedID, "status", "appliedDeploymentID")
	return u
}

// seedOperatorBundle is an embedded-chart Bundle the operator wrote: owner label
// plus the chart fingerprint annotation only operator-built Bundles carry.
func seedOperatorBundle(keep bool) *unstructured.Unstructured {
	b := seedBundle(switchBundle, map[string]string{workloadUIDLabel: switchUID}, keep)
	b.SetAnnotations(map[string]string{chartFingerprintAnnotation: "fp"})
	return b
}

// fleetHelmOpLabel is the label Fleet v0.16.2 puts on a HelmOp's Bundle, written
// out literally (as observed on a cluster) rather than via the production
// constant, so a wrong constant fails these tests.
const fleetHelmOpLabel = "fleet.cattle.io/fleet-helm-name"

func helmOpBundleLabels() map[string]string {
	return map[string]string{fleetHelmOpLabel: switchBundle, workloadUIDLabel: switchUID}
}

// switchStep runs one observed step of moving switchBundle in fleet-local to
// want, the way ensureComponentKind does for the FleetBundle strategy.
func switchStep(r *AIWorkloadReconciler, w *aiplatformv1alpha1.AIWorkload, want renderKind) (done bool, msg string, err error) {
	obs, err := r.observeKindSwitch(context.Background(), w, "fleet-local", switchBundle, want)
	if err != nil {
		return false, "", err
	}
	return r.actuateSwitch(context.Background(), "fleet-local", switchBundle, obs)
}

func switchReconciler(t *testing.T, objs ...client.Object) (*AIWorkloadReconciler, client.Client) {
	t.Helper()
	scheme := clusterRepoErrorScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	return &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: "aif-operator"}, c
}

func objExists(t *testing.T, c client.Client, isHelmOp bool, name string) (*unstructured.Unstructured, bool) {
	t.Helper()
	u := &unstructured.Unstructured{}
	if isHelmOp {
		u.SetGroupVersionKind(helmOpGVK)
	} else {
		u.SetGroupVersionKind(bundleGVK)
	}
	err := c.Get(context.Background(), types.NamespacedName{Namespace: "fleet-local", Name: name}, u)
	if err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("get %s: %v", name, err)
	}
	return u, err == nil
}

func TestSwitch_NoConflictIsDone(t *testing.T) {
	r, _ := switchReconciler(t)
	done, _, err := switchStep(r, switchWorkload(), kindEmbeddedBundle)
	if err != nil || !done {
		t.Fatalf("done=%v err=%v, want done", done, err)
	}
}

func TestSwitch_ExistingEmbeddedBundleIsDone(t *testing.T) {
	r, _ := switchReconciler(t, seedBundle(switchBundle, map[string]string{workloadUIDLabel: switchUID}, false))
	done, _, err := switchStep(r, switchWorkload(), kindEmbeddedBundle)
	if err != nil || !done {
		t.Fatalf("an existing embedded Bundle is the desired kind: done=%v err=%v", done, err)
	}
}

func TestSwitch_SetsKeepResourcesFirst(t *testing.T) {
	r, c := switchReconciler(t, seedHelmOp("wl-comp", switchUID, false))
	done, msg, err := switchStep(r, switchWorkload(), kindEmbeddedBundle)
	if err != nil || done || msg == "" {
		t.Fatalf("done=%v msg=%q err=%v, want waiting", done, msg, err)
	}
	h, ok := objExists(t, c, true, "wl-comp")
	if !ok {
		t.Fatalf("the HelmOp must not be deleted before keepResources is applied")
	}
	if keep, _, _ := unstructured.NestedBool(h.Object, "spec", "keepResources"); !keep {
		t.Fatalf("spec.keepResources not set on the HelmOp")
	}
}

func TestSwitch_WaitsForAppliedKeepResources(t *testing.T) {
	for name, bd := range map[string]*unstructured.Unstructured{
		"not applied yet":        seedBD(switchBundle, "new", "old", true),
		"BD options not updated": seedBD(switchBundle, "same", "same", false),
	} {
		t.Run(name, func(t *testing.T) {
			r, c := switchReconciler(t, seedHelmOp("wl-comp", switchUID, true), seedBundle(switchBundle, helmOpBundleLabels(), true), bd)
			done, _, err := switchStep(r, switchWorkload(), kindEmbeddedBundle)
			if err != nil || done {
				t.Fatalf("done=%v err=%v, want waiting", done, err)
			}
			if _, ok := objExists(t, c, true, "wl-comp"); !ok {
				t.Fatalf("deleted the HelmOp before every BundleDeployment applied keepResources")
			}
		})
	}
}

func TestSwitch_DeletesOnceApplied(t *testing.T) {
	r, c := switchReconciler(t, seedHelmOp("wl-comp", switchUID, true), seedBundle(switchBundle, helmOpBundleLabels(), true), seedBD(switchBundle, "d1", "d1", true))
	done, _, err := switchStep(r, switchWorkload(), kindEmbeddedBundle)
	if err != nil || done {
		t.Fatalf("done=%v err=%v, want waiting after delete", done, err)
	}
	if _, ok := objExists(t, c, true, "wl-comp"); ok {
		t.Fatalf("the HelmOp should be deleted once keepResources is applied")
	}
}

func TestSwitch_LeftoverHelmOpBundleWaitsUntilApplied(t *testing.T) {
	r, c := switchReconciler(t, seedBundle(switchBundle, helmOpBundleLabels(), true), seedBD(switchBundle, "new", "old", true))
	done, _, err := switchStep(r, switchWorkload(), kindEmbeddedBundle)
	if err != nil || done {
		t.Fatalf("done=%v err=%v, want waiting", done, err)
	}
	if _, ok := objExists(t, c, false, switchBundle); !ok {
		t.Fatalf("the HelmOp's leftover Bundle must not be deleted before keepResources is applied")
	}
}

func TestSwitch_DeletesLeftoverHelmOpBundleOnceApplied(t *testing.T) {
	// Fleet links the Bundle to its HelmOp only by label and its own cleanup is
	// racy (see deleteBundleIn), so a leftover Bundle is deleted directly.
	r, c := switchReconciler(t, seedBundle(switchBundle, helmOpBundleLabels(), true), seedBD(switchBundle, "d1", "d1", true))
	done, _, err := switchStep(r, switchWorkload(), kindEmbeddedBundle)
	if err != nil || done {
		t.Fatalf("done=%v err=%v, want waiting after delete", done, err)
	}
	if _, ok := objExists(t, c, false, switchBundle); ok {
		t.Fatalf("the HelmOp's leftover Bundle should be deleted once keepResources is applied")
	}
}

func TestSwitch_WaitsForOldBundleDeploymentsToGo(t *testing.T) {
	r, _ := switchReconciler(t, seedBD(switchBundle, "d1", "d1", true))
	if done, _, err := switchStep(r, switchWorkload(), kindEmbeddedBundle); err != nil || done {
		t.Fatalf("done=%v err=%v, want waiting while the old BundleDeployment remains", done, err)
	}
}

func TestSwitch_HelmOpPresentMeansNoConflictForHelmOp(t *testing.T) {
	r, c := switchReconciler(t, seedHelmOp(switchBundle, switchUID, false), seedOperatorBundle(false))
	if done, _, err := switchStep(r, switchWorkload(), kindHelmOp); err != nil || !done {
		t.Fatalf("done=%v err=%v, want done when the HelmOp already exists", done, err)
	}
	b, _ := objExists(t, c, false, switchBundle)
	if keep, _, _ := unstructured.NestedBool(b.Object, "spec", "keepResources"); keep {
		t.Fatalf("must not touch a Bundle while its HelmOp exists")
	}
}

func TestSwitch_BundleWithoutFingerprintIsNotOperatorBundle(t *testing.T) {
	// A Bundle without Fleet's HelmOp label is still not ours unless it carries
	// the operator's chart fingerprint annotation.
	r, c := switchReconciler(t, seedBundle(switchBundle, map[string]string{workloadUIDLabel: switchUID}, false))
	if done, _, err := switchStep(r, switchWorkload(), kindHelmOp); err != nil || !done {
		t.Fatalf("done=%v err=%v, want no conflict", done, err)
	}
	b, _ := objExists(t, c, false, switchBundle)
	if keep, _, _ := unstructured.NestedBool(b.Object, "spec", "keepResources"); keep {
		t.Fatalf("a Bundle that isn't the operator's must not be modified")
	}
}

func TestSwitch_ClearsLeftoverKeepResources(t *testing.T) {
	// A switch abandoned halfway (overlay added then removed) leaves keepResources
	// on the object that stays; it must not survive, or deleting the workload
	// would orphan the resources.
	r, c := switchReconciler(t, seedOperatorBundle(true))
	if done, _, err := switchStep(r, switchWorkload(), kindEmbeddedBundle); err != nil || !done {
		t.Fatalf("done=%v err=%v, want done", done, err)
	}
	b, _ := objExists(t, c, false, switchBundle)
	if keep, _, _ := unstructured.NestedBool(b.Object, "spec", "keepResources"); keep {
		t.Fatalf("leftover keepResources not cleared on the embedded Bundle")
	}

	r, c = switchReconciler(t, seedHelmOp(switchBundle, switchUID, true))
	if done, _, err := switchStep(r, switchWorkload(), kindHelmOp); err != nil || !done {
		t.Fatalf("HelmOp: done=%v err=%v, want done", done, err)
	}
	h, _ := objExists(t, c, true, switchBundle)
	if keep, _, _ := unstructured.NestedBool(h.Object, "spec", "keepResources"); keep {
		t.Fatalf("leftover keepResources not cleared on the HelmOp")
	}
}

func TestSwitch_WaitingMessageNamesPendingDeployments(t *testing.T) {
	bd := seedBD(switchBundle, "new", "old", true)
	_ = unstructured.SetNestedField(bd.Object, "ErrApplied", "status", "display", "state")
	r, _ := switchReconciler(t, seedHelmOp(switchBundle, switchUID, true), seedBundle(switchBundle, helmOpBundleLabels(), true), bd)
	_, msg, err := switchStep(r, switchWorkload(), kindEmbeddedBundle)
	if err != nil || !strings.Contains(msg, "ErrApplied") || !strings.Contains(msg, bd.GetNamespace()) {
		t.Fatalf("msg = %q (err %v), want it to name the pending BundleDeployment and its state", msg, err)
	}
}

func TestSwitch_BundleToHelmOp(t *testing.T) {
	r, c := switchReconciler(t, seedOperatorBundle(false))
	w := switchWorkload()
	step := func(n int, wantDone bool) {
		t.Helper()
		if done, _, err := switchStep(r, w, kindHelmOp); err != nil || done != wantDone {
			t.Fatalf("step %d: done=%v err=%v, want done=%v", n, done, err, wantDone)
		}
	}
	step(1, false)
	b, _ := objExists(t, c, false, switchBundle)
	if keep, _, _ := unstructured.NestedBool(b.Object, "spec", "keepResources"); !keep {
		t.Fatalf("keepResources not set on the embedded Bundle")
	}
	bd := seedBD(switchBundle, "d1", "d1", true)
	if err := c.Create(context.Background(), bd); err != nil {
		t.Fatal(err)
	}
	step(2, false)
	if _, ok := objExists(t, c, false, switchBundle); ok {
		t.Fatalf("the embedded Bundle should be deleted once keepResources is applied")
	}
	// The agent removes the old BundleDeployment; until then the switch waits.
	step(3, false)
	if err := c.Delete(context.Background(), bd); err != nil {
		t.Fatal(err)
	}
	step(4, true)
}

func TestSwitch_RefusesForeignObject(t *testing.T) {
	r, c := switchReconciler(t, seedHelmOp("wl-comp", "someone-else", false))
	if _, _, err := switchStep(r, switchWorkload(), kindEmbeddedBundle); err == nil {
		t.Fatalf("want an error for a HelmOp owned by another workload")
	}
	h, _ := objExists(t, c, true, "wl-comp")
	if keep, _, _ := unstructured.NestedBool(h.Object, "spec", "keepResources"); keep {
		t.Fatalf("a foreign HelmOp must not be modified")
	}
}

func TestReconcile_SwitchRequeuesAndReportsMessage(t *testing.T) {
	scheme := clusterRepoErrorScheme(t)
	w := switchWorkload()
	w.Spec.Source.Blueprint = &aiplatformv1alpha1.BlueprintSource{Name: "ov", Version: "1.0.0"}
	w.Spec.ComponentValues = []aiplatformv1alpha1.ComponentValueOverride{{ComponentName: "comp", Kustomize: inlineOverlay("inst", emptyPatches)}}
	bp := &aiplatformv1alpha1.Blueprint{ObjectMeta: metav1.ObjectMeta{Name: bpCRName("ov", "1.0.0")}}
	bp.Spec.Components = []aiplatformv1alpha1.BlueprintComponent{{ChartRepo: "application-collection", ChartName: "comp", ChartVersion: "1.0.0", Vendor: "suse"}}
	repo := repoObj("application-collection", map[string]any{"url": "https://charts.example.com"})
	name := blueprintBundleName(w.Name, "comp")
	tgz := compChartTgz(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(w, bp, repo, seedHelmOp(name, switchUID, false)).Build()
	r := &AIWorkloadReconciler{
		Client: c, Scheme: scheme, OperatorNamespace: "aif-operator",
		ChartPuller: func(context.Context, infrahelm.ChartArchiveRequest) ([]byte, error) { return tgz, nil },
	}

	res, err := r.reconcileBlueprintStatus(context.Background(), w)
	if err != nil {
		t.Fatalf("reconcileBlueprintStatus: %v", err)
	}
	if res.RequeueAfter != 5*time.Second {
		t.Fatalf("RequeueAfter = %v, want 5s", res.RequeueAfter)
	}
	ready := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady)
	if ready == nil || !strings.Contains(ready.Message, "switching") {
		t.Fatalf("Ready = %+v, want a switching message", ready)
	}
	if b, ok := objExists(t, c, false, name); ok && b.GetLabels()[fleetHelmOpLabel] == "" {
		t.Fatalf("the embedded Bundle must not be written before the HelmOp is gone")
	}
}

// switchReconcileEnv reconciles a two-component Blueprint (comp, other) on an
// HTTP repo where comp currently runs as a HelmOp and gains an install overlay.
func switchReconcileEnv(t *testing.T, puller chartArchivePuller) (*aiplatformv1alpha1.AIWorkload, *AIWorkloadReconciler, client.Client) {
	t.Helper()
	scheme := clusterRepoErrorScheme(t)
	w := switchWorkload()
	w.Spec.Source.Blueprint = &aiplatformv1alpha1.BlueprintSource{Name: "ov", Version: "1.0.0"}
	w.Spec.ComponentValues = []aiplatformv1alpha1.ComponentValueOverride{{ComponentName: "comp", Kustomize: inlineOverlay("inst", emptyPatches)}}
	bp := &aiplatformv1alpha1.Blueprint{ObjectMeta: metav1.ObjectMeta{Name: bpCRName("ov", "1.0.0")}}
	bp.Spec.Components = []aiplatformv1alpha1.BlueprintComponent{
		{ChartRepo: "application-collection", ChartName: "comp", ChartVersion: "1.0.0", Vendor: "suse"},
		{ChartRepo: "application-collection", ChartName: "other", ChartVersion: "1.0.0", Vendor: "suse"},
	}
	repo := repoObj("application-collection", map[string]any{"url": "https://charts.example.com"})
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(w, bp, repo, seedHelmOp(blueprintBundleName(w.Name, "comp"), switchUID, false)).Build()
	return w, &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: "aif-operator", ChartPuller: puller}, c
}

func TestReconcile_SwitchNotStartedWhenNewBundleCannotBeBuilt(t *testing.T) {
	w, r, c := switchReconcileEnv(t, func(context.Context, infrahelm.ChartArchiveRequest) ([]byte, error) {
		return nil, fmt.Errorf("registry unreachable")
	})
	_, _ = r.reconcileBlueprintStatus(context.Background(), w)

	h, ok := objExists(t, c, true, blueprintBundleName(w.Name, "comp"))
	if !ok {
		t.Fatalf("the running HelmOp must stay when its replacement cannot be built")
	}
	if keep, _, _ := unstructured.NestedBool(h.Object, "spec", "keepResources"); keep {
		t.Fatalf("the switch must not start (keepResources set) when the new Bundle cannot be built")
	}
	ready := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady)
	if ready == nil || ready.Reason == "Switching" || !strings.Contains(ready.Message, "registry unreachable") {
		t.Fatalf("Ready = %+v, want the build failure reported, not a switch", ready)
	}
}

func TestReconcile_SwitchDoesNotBlockOtherComponents(t *testing.T) {
	tgz := compChartTgz(t)
	w, r, c := switchReconcileEnv(t, func(context.Context, infrahelm.ChartArchiveRequest) ([]byte, error) { return tgz, nil })
	res, err := r.reconcileBlueprintStatus(context.Background(), w)
	if err != nil {
		t.Fatalf("reconcileBlueprintStatus: %v", err)
	}
	if _, ok := objExists(t, c, true, blueprintBundleName(w.Name, "other")); !ok {
		t.Fatalf("a component switching kind must not stop the other components from rendering")
	}
	if res.RequeueAfter != switchRequeue {
		t.Fatalf("RequeueAfter = %v, want %v", res.RequeueAfter, switchRequeue)
	}
	if ready := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady); ready == nil || ready.Reason != "Switching" {
		t.Fatalf("Ready = %+v, want Switching", ready)
	}
}
