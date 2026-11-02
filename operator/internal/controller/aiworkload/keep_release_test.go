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
	"errors"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func keepReleaseWorkload() *aiplatformv1alpha1.AIWorkload {
	w := switchWorkload()
	w.Spec.DeployStrategy = aiplatformv1alpha1.AIWorkloadDeployFleetBundle
	w.Spec.FleetBundleNames = []string{switchBundle}
	return w
}

func TestReleaseKeepResources_NothingKeptDoesNotWait(t *testing.T) {
	w := keepReleaseWorkload()
	// A failing BundleDeployment must not hold up an ordinary delete.
	r, c := switchReconciler(t, w, seedHelmOp(switchBundle, switchUID, false), seedBD(switchBundle, "d2", "d1", false))
	wait, err := r.releaseKeepResources(context.Background(), w)
	if err != nil || wait {
		t.Fatalf("wait=%v err=%v, want no wait", wait, err)
	}
	if ho, _ := objExists(t, c, true, switchBundle); ho.GetAnnotations()[keepReleasedAnnotation] != "" {
		t.Fatalf("release recorded although nothing carried keepResources")
	}
}

// Every call gets a fresh AIWorkload without annotations, the way a reconcile reading
// a stale cache sees it: the release state must come from the Fleet objects alone.
func TestReleaseKeepResources_ClearsThenWaitsForFleet(t *testing.T) {
	ho := seedHelmOp(switchBundle, switchUID, true)
	_ = unstructured.SetNestedField(ho.Object, int64(2), "spec", "forceSyncGeneration")
	bd := seedBD(switchBundle, "d-keep", "d-orig", true)
	r, c := switchReconciler(t, keepReleaseWorkload(), ho, bd)
	release := func() bool {
		t.Helper()
		wait, err := r.releaseKeepResources(context.Background(), keepReleaseWorkload())
		if err != nil {
			t.Fatal(err)
		}
		return wait
	}

	if !release() {
		t.Fatalf("want wait after clearing")
	}
	got, _ := objExists(t, c, true, switchBundle)
	if keep, _, _ := unstructured.NestedBool(got.Object, "spec", "keepResources"); keep {
		t.Fatalf("keepResources still set on the HelmOp")
	}
	if g, _, _ := unstructured.NestedInt64(got.Object, "spec", "forceSyncGeneration"); g != 3 {
		t.Fatalf("forceSyncGeneration = %d, want it bumped to 3", g)
	}
	if a := got.GetAnnotations(); a[keepReleaseGenAnnotation] != "3" || a[keepReleasedAnnotation] == "" {
		t.Fatalf("release not recorded on the HelmOp: %v", a)
	}

	setBD := func(fsg int64, want, applied string) {
		t.Helper()
		_ = unstructured.SetNestedField(bd.Object, false, "spec", "options", "keepResources")
		_ = unstructured.SetNestedField(bd.Object, fsg, "spec", "options", "forceSyncGeneration")
		_ = unstructured.SetNestedField(bd.Object, want, "spec", "deploymentID")
		_ = unstructured.SetNestedField(bd.Object, applied, "status", "appliedDeploymentID")
		if err := c.Update(context.Background(), bd); err != nil {
			t.Fatal(err)
		}
	}
	// Clearing keepResources alone restores the pre-switch deploymentID, which still
	// equals the last applied one while the agent may be applying keep.
	setBD(2, "d-orig", "d-orig")
	if !release() {
		t.Fatalf("want wait until the bumped generation is applied")
	}
	setBD(3, "d-new", "d-orig")
	if !release() {
		t.Fatalf("want wait until applied")
	}
	setBD(3, "d-new", "d-new")
	if release() {
		t.Fatalf("want done once applied")
	}
}

func TestReleaseKeepResources_GivesUpAfterTimeout(t *testing.T) {
	w := keepReleaseWorkload()
	ho := seedHelmOp(switchBundle, switchUID, false)
	ho.SetAnnotations(map[string]string{
		keepReleasedAnnotation:   time.Now().Add(-keepReleaseTimeout - time.Minute).UTC().Format(time.RFC3339),
		keepReleaseGenAnnotation: "1",
	})
	r, _ := switchReconciler(t, w, ho, seedBD(switchBundle, "d2", "d1", false))
	if wait, err := r.releaseKeepResources(context.Background(), w); err != nil || wait {
		t.Fatalf("wait=%v err=%v, want no wait past the timeout", wait, err)
	}
}

func TestReleaseKeepResources_IgnoresObjectsOfOtherWorkloads(t *testing.T) {
	w := keepReleaseWorkload()
	other := seedHelmOp(switchBundle, "someone-else", true)
	r, c := switchReconciler(t, w, other)
	if wait, err := r.releaseKeepResources(context.Background(), w); err != nil || wait {
		t.Fatalf("wait=%v err=%v, want no wait", wait, err)
	}
	got, _ := objExists(t, c, true, switchBundle)
	if keep, _, _ := unstructured.NestedBool(got.Object, "spec", "keepResources"); !keep {
		t.Fatalf("cleared keepResources on another workload's HelmOp")
	}
}

func TestHandleDeletion_ClearsKeepResourcesBeforeDeleting(t *testing.T) {
	w := keepReleaseWorkload()
	w.Finalizers = []string{aiWorkloadFinalizer}
	r, c := switchReconciler(t, w, seedHelmOp(switchBundle, switchUID, true), seedBD(switchBundle, "d-keep", "d-keep", true))
	res, err := r.handleDeletion(context.Background(), w)
	if err != nil || res.RequeueAfter == 0 {
		t.Fatalf("res=%+v err=%v, want a requeue while Fleet applies the change", res, err)
	}
	if _, ok := objExists(t, c, true, switchBundle); !ok {
		t.Fatalf("HelmOp deleted before keepResources was released")
	}
	var got aiplatformv1alpha1.AIWorkload
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(w), &got); err != nil || len(got.Finalizers) == 0 {
		t.Fatalf("finalizer removed early (err %v)", err)
	}
}

// A persistent failure clearing keepResources must not block deleting the workload.
func TestHandleDeletion_ProceedsWhenKeepReleaseFails(t *testing.T) {
	w := keepReleaseWorkload()
	w.Finalizers = []string{aiWorkloadFinalizer}
	scheme := clusterRepoErrorScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(w, seedHelmOp(switchBundle, switchUID, true)).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(context.Context, client.WithWatch, client.Object, client.Patch, ...client.PatchOption) error {
				return errors.New("forbidden")
			},
		}).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: "aif-operator"}

	res, err := r.handleDeletion(context.Background(), w)
	if err != nil || res.RequeueAfter != 0 {
		t.Fatalf("res=%+v err=%v, want deletion to proceed", res, err)
	}
	if _, ok := objExists(t, c, true, switchBundle); ok {
		t.Fatalf("HelmOp not deleted after keepResources release failed")
	}
}
