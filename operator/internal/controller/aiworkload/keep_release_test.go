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
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
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
	wait, _, err := r.releaseKeepResources(context.Background(), w)
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
		wait, _, err := r.releaseKeepResources(context.Background(), keepReleaseWorkload())
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
	if wait, _, err := r.releaseKeepResources(context.Background(), w); err != nil || wait {
		t.Fatalf("wait=%v err=%v, want no wait past the timeout", wait, err)
	}
}

func TestReleaseKeepResources_IgnoresObjectsOfOtherWorkloads(t *testing.T) {
	w := keepReleaseWorkload()
	other := seedHelmOp(switchBundle, "someone-else", true)
	r, c := switchReconciler(t, w, other)
	if wait, _, err := r.releaseKeepResources(context.Background(), w); err != nil || wait {
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

// deletingEnv is a workload being deleted at deletedAt, with objs and the
// interceptors given, plus a fake event recorder.
type deletingEnv struct {
	r   *AIWorkloadReconciler
	c   client.Client
	w   *aiplatformv1alpha1.AIWorkload
	rec *record.FakeRecorder
}

func newDeletingEnv(t *testing.T, deletedAt time.Time, funcs interceptor.Funcs, objs ...client.Object) *deletingEnv {
	t.Helper()
	w := keepReleaseWorkload()
	w.Finalizers = []string{aiWorkloadFinalizer}
	ts := metav1.NewTime(deletedAt)
	w.DeletionTimestamp = &ts
	scheme := clusterRepoErrorScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(append([]client.Object{w}, objs...)...).WithInterceptorFuncs(funcs).Build()
	rec := record.NewFakeRecorder(16)
	return &deletingEnv{r: &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: "aif-operator", Recorder: rec}, c: c, w: w, rec: rec}
}

// pass runs handleDeletion on the stored workload, as the next reconcile would.
func (e *deletingEnv) pass(t *testing.T) (ctrl.Result, error) {
	t.Helper()
	var cur aiplatformv1alpha1.AIWorkload
	if err := e.c.Get(context.Background(), client.ObjectKeyFromObject(e.w), &cur); err != nil {
		t.Fatalf("workload already gone: %v", err)
	}
	return e.r.handleDeletion(context.Background(), &cur)
}

func (e *deletingEnv) finalized(t *testing.T) bool {
	t.Helper()
	var cur aiplatformv1alpha1.AIWorkload
	return e.c.Get(context.Background(), client.ObjectKeyFromObject(e.w), &cur) != nil
}

func (e *deletingEnv) events() []string {
	var out []string
	for {
		select {
		case ev := <-e.rec.Events:
			out = append(out, ev)
		default:
			return out
		}
	}
}

// failFirst counts a call and reports whether it is the first.
func failFirst(n *int) bool {
	*n++
	return *n == 1
}

// A transient error clearing keepResources keeps the finalizer and is retried;
// the retry clears it and deletion then waits for Fleet as usual.
func TestHandleDeletion_RetriesTransientKeepReleaseError(t *testing.T) {
	for _, tc := range []struct {
		name  string
		funcs func(n *int) interceptor.Funcs
	}{
		{"patch", func(n *int) interceptor.Funcs {
			return interceptor.Funcs{Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, p client.Patch, opts ...client.PatchOption) error {
				if failFirst(n) {
					return errors.New("etcdserver: request timed out")
				}
				return cl.Patch(ctx, obj, p, opts...)
			}}
		}},
		{"get", func(n *int) interceptor.Funcs {
			return interceptor.Funcs{Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if u, ok := obj.(*unstructured.Unstructured); ok && u.GetKind() == "HelmOp" && failFirst(n) {
					return errors.New("the server is currently unable to handle the request")
				}
				return cl.Get(ctx, key, obj, opts...)
			}}
		}},
	} {
		n := 0
		e := newDeletingEnv(t, time.Now(), tc.funcs(&n), seedHelmOp(switchBundle, switchUID, true), seedBD(switchBundle, "d-keep", "d-keep", true))
		if _, err := e.pass(t); err == nil || e.finalized(t) {
			t.Fatalf("%s: err=%v finalized=%v, want the error returned and the finalizer kept", tc.name, err, e.finalized(t))
		}
		res, err := e.pass(t)
		if err != nil || res.RequeueAfter == 0 || e.finalized(t) {
			t.Fatalf("%s retry: res=%+v err=%v, want keepResources cleared and a wait for Fleet", tc.name, res, err)
		}
		ho, _ := objExists(t, e.c, true, switchBundle)
		if keep, _, _ := unstructured.NestedBool(ho.Object, "spec", "keepResources"); keep {
			t.Fatalf("%s: keepResources still set after the retry", tc.name)
		}
		if ev := e.events(); len(ev) != 1 || !strings.Contains(ev[0], "Warning "+reasonKeepResourcesReleaseRetrying) {
			t.Fatalf("%s: events = %v, want one retrying event explaining Terminating", tc.name, ev)
		}
	}
}

// With Fleet's CRDs gone there is nothing to release or delete: deletion
// completes instead of retrying until the deadline.
func TestHandleDeletion_FleetCRDsGone(t *testing.T) {
	noMatch := func(obj client.Object) error {
		if u, ok := obj.(*unstructured.Unstructured); ok && (u.GetKind() == "HelmOp" || u.GetKind() == "Bundle") {
			return &meta.NoKindMatchError{GroupKind: u.GroupVersionKind().GroupKind()}
		}
		return nil
	}
	e := newDeletingEnv(t, time.Now(), interceptor.Funcs{
		Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if err := noMatch(obj); err != nil {
				return err
			}
			return cl.Get(ctx, key, obj, opts...)
		},
		Delete: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			if err := noMatch(obj); err != nil {
				return err
			}
			return cl.Delete(ctx, obj, opts...)
		},
	})
	if res, err := e.pass(t); err != nil || res.RequeueAfter != 0 || !e.finalized(t) {
		t.Fatalf("res=%+v err=%v, want deletion to complete", res, err)
	}
}

func TestForClusters_EmptyTargets(t *testing.T) {
	w := keepReleaseWorkload()
	if got := forClusters(w); got != " for clusters local" {
		t.Fatalf("forClusters = %q", got)
	}
	w.Spec.TargetClusters = nil
	if got := forClusters(w); got != "" {
		t.Fatalf("forClusters with no targets = %q, want empty", got)
	}
}

// Past keepReleaseDeleteDeadline a persistent error no longer blocks deletion,
// but it is reported.
func TestHandleDeletion_ProceedsWithWarningAfterDeadline(t *testing.T) {
	deleted := time.Now().Add(-time.Hour)
	forbidden := interceptor.Funcs{Patch: func(context.Context, client.WithWatch, client.Object, client.Patch, ...client.PatchOption) error {
		return errors.New("forbidden")
	}}
	e := newDeletingEnv(t, deleted, forbidden, seedHelmOp(switchBundle, switchUID, true))
	e.r.now = func() time.Time { return deleted.Add(keepReleaseDeleteDeadline - time.Second) }
	if _, err := e.pass(t); err == nil || e.finalized(t) {
		t.Fatalf("before the deadline: err=%v, want a retry with the finalizer kept", err)
	}
	e.events()
	e.r.now = func() time.Time { return deleted.Add(keepReleaseDeleteDeadline + time.Second) }
	if res, err := e.pass(t); err != nil || res.RequeueAfter != 0 || !e.finalized(t) {
		t.Fatalf("after the deadline: res=%+v err=%v, want deletion to proceed", res, err)
	}
	if _, ok := objExists(t, e.c, true, switchBundle); ok {
		t.Fatalf("HelmOp not deleted after the deadline")
	}
	ev := e.events()
	if len(ev) != 1 || !strings.Contains(ev[0], "Warning "+reasonKeepResourcesNotReleased) ||
		!strings.Contains(ev[0], switchBundle) || !strings.Contains(ev[0], "may be left running") {
		t.Fatalf("events = %v, want one Warning naming %s", ev, switchBundle)
	}
}

// A release Fleet never applies is given up on after keepReleaseTimeout, with
// a Warning naming the cluster.
func TestHandleDeletion_WarnsWhenReleaseNeverSettles(t *testing.T) {
	ho := seedHelmOp(switchBundle, switchUID, false)
	ho.SetAnnotations(map[string]string{
		keepReleasedAnnotation:   time.Now().Add(-keepReleaseTimeout - time.Minute).UTC().Format(time.RFC3339),
		keepReleaseGenAnnotation: "1",
	})
	e := newDeletingEnv(t, time.Now(), interceptor.Funcs{}, ho, seedBD(switchBundle, "d-keep", "d-keep", true))
	if res, err := e.pass(t); err != nil || res.RequeueAfter != 0 || !e.finalized(t) {
		t.Fatalf("res=%+v err=%v, want deletion to proceed after the settle timeout", res, err)
	}
	ev := e.events()
	if len(ev) != 1 || !strings.Contains(ev[0], "Warning "+reasonKeepResourcesNotReleased) || !strings.Contains(ev[0], switchBundle+" on cluster-fleet-local-local-abc") {
		t.Fatalf("events = %v, want one Warning naming the bundle and cluster", ev)
	}
}

func TestHandleDeletion_HealthyPathHasNoWarning(t *testing.T) {
	e := newDeletingEnv(t, time.Now(), interceptor.Funcs{}, seedHelmOp(switchBundle, switchUID, false), seedBD(switchBundle, "d1", "d1", false))
	if res, err := e.pass(t); err != nil || res.RequeueAfter != 0 || !e.finalized(t) {
		t.Fatalf("res=%+v err=%v, want an immediate delete", res, err)
	}
	if ev := e.events(); len(ev) != 0 {
		t.Fatalf("unexpected events %v", ev)
	}
}
