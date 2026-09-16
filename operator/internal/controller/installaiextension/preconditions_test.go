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

package controller

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/infra/rancher"
)

// The extension namespace and the Rancher CRDs are the two things this operator
// needs but does not own. Deleting cattle-ui-plugin-system used to be
// unrecoverable: the namespaced Role went with it, so every reinstall was
// Forbidden, and the CR reported an opaque Helm error rather than the one fact
// that would have helped — Rancher does not recreate that namespace on its own.
//
// The permissions half of that fix lives in the ClusterRole. This file covers
// the other half: recognising the state, naming the action that resolves it,
// and not attempting an install that can only fail confusingly.

// gateRancherManager makes CheckCRDs answerable per-test — the shared
// stubRancherManager always succeeds — and counts the deletes that
// cleanupStaleResources would issue, so a test can prove the gate ran first.
// The two kinds are counted separately because reapOrphanedClusterRepo also
// calls DeleteClusterRepo: only a UIPlugin delete can prove cleanupStaleResources
// itself ran, since the reap never touches UIPlugin.
type gateRancherManager struct {
	stubRancherManager
	crdErr             error
	clusterRepoDeletes int
	uiPluginDeletes    int
}

func (g *gateRancherManager) CheckCRDs(context.Context, []string) error { return g.crdErr }

func (g *gateRancherManager) DeleteClusterRepo(context.Context, string) error {
	g.clusterRepoDeletes++
	return nil
}

func (g *gateRancherManager) DeleteUIPlugin(context.Context, string, string) error {
	g.uiPluginDeletes++
	return nil
}

// gateReconciler builds a reconciler whose Rancher preflight passes by default,
// so a case reaches the namespace check instead of stopping at the CRD check
// the way every other test in this package does (CheckCRDs dials the in-cluster
// config, which does not exist under `go test`).
func gateReconciler(
	t *testing.T,
	ext *v1alpha1.InstallAIExtension,
	mgr rancherManager,
	objs ...client.Object,
) (*InstallAIExtensionReconciler, *record.FakeRecorder) {
	t.Helper()

	r := readinessReconciler(t, ext, interceptor.Funcs{}, objs...)
	r.rancherMgr = mgr
	rec := record.NewFakeRecorder(10)
	r.Recorder = rec
	return r, rec
}

// extensionNamespace returns the namespace object the gate looks for.
func extensionNamespace() *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: wiringNamespace}}
}

// terminatingNamespace is what `kubectl delete namespace` leaves behind while
// Kubernetes drains it. The finalizer is not decoration: the fake client
// refuses to seed an object carrying a deletionTimestamp without one, for the
// same reason the API server does — nothing would keep it from vanishing.
func terminatingNamespace() *corev1.Namespace {
	now := metav1.Now()
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:              wiringNamespace,
			DeletionTimestamp: &now,
			Finalizers:        []string{"kubernetes"},
		},
	}
}

func readyCondition(t *testing.T, ext *v1alpha1.InstallAIExtension) *metav1.Condition {
	t.Helper()
	ready := meta.FindStatusCondition(ext.Status.Conditions, conditionTypeReady)
	if ready == nil {
		t.Fatalf("no Ready condition; the pass did not reach the precondition gate")
	}
	return ready
}

// A namespace that is simply gone is the reported case in the ticket, and the
// message is the whole deliverable: an admin who reads it must not have to
// discover on their own that Rancher owns this namespace and will not bring it
// back by itself.
func TestMissingExtensionNamespaceIsReportedNotAttempted(t *testing.T) {
	ext := helmExtension()
	r, _ := gateReconciler(t, ext, &gateRancherManager{})

	result, err := r.reconcile(context.Background(), ext)
	if err != nil {
		t.Fatalf("reconcile error = %v; a missing namespace is a state to report, not an "+
			"error to retry blindly", err)
	}

	ready := readyCondition(t, ext)
	if ready.Reason != "ExtensionNamespaceMissing" {
		t.Fatalf("Ready reason = %s, want ExtensionNamespaceMissing", ready.Reason)
	}

	// Asserting on the remediation rather than the whole string: the wording can
	// be improved, but a message that stops naming the command stops being the
	// fix. Recovering from this without knowing about the restart means deleting
	// and recreating things by hand.
	if !strings.Contains(ready.Message, "rollout restart") {
		t.Errorf("message = %q, want it to name the Rancher rollout restart that recreates "+
			"the namespace", ready.Message)
	}
	if !strings.Contains(ready.Message, wiringNamespace) {
		t.Errorf("message = %q, want it to name the namespace", ready.Message)
	}

	if ext.Status.Phase != v1alpha1.InstallAIExtensionPhasePending {
		t.Errorf("Phase = %s, want Pending; this extension never installed, so Failed would "+
			"send an admin hunting a bug in the operator", ext.Status.Phase)
	}
	if result.RequeueAfter != healthCheckInterval {
		t.Errorf("RequeueAfter = %v, want %v; a namespace coming back produces no event on "+
			"this CR, so nothing else re-checks it", result.RequeueAfter, healthCheckInterval)
	}
}

// The same cause on an extension that was serving users is an outage, not a
// startup delay, and the phase is the only part of the report that can say so —
// the reason and message are identical.
func TestMissingNamespaceOnAnInstalledExtensionReportsFailed(t *testing.T) {
	ext := helmExtension()
	ext.Status.ActiveExtensionName = ext.Spec.Extension.Name

	r, _ := gateReconciler(t, ext, &gateRancherManager{})

	if _, err := r.reconcile(context.Background(), ext); err != nil {
		t.Fatalf("reconcile error = %v", err)
	}

	if ready := readyCondition(t, ext); ready.Reason != "ExtensionNamespaceMissing" {
		t.Fatalf("Ready reason = %s, want ExtensionNamespaceMissing", ready.Reason)
	}
	if ext.Status.Phase != v1alpha1.InstallAIExtensionPhaseFailed {
		t.Errorf("Phase = %s, want Failed; this extension had installed, so the UI users were "+
			"reaching is gone — Pending would describe a live outage as a startup delay",
			ext.Status.Phase)
	}
}

// A terminating namespace answers Get successfully, so the not-found check
// alone would wave the install through into a namespace that rejects every
// write — reporting a forbidden that names the wrong problem.
func TestTerminatingExtensionNamespaceIsBlocked(t *testing.T) {
	ext := helmExtension()
	r, _ := gateReconciler(t, ext, &gateRancherManager{}, terminatingNamespace())

	if _, err := r.reconcile(context.Background(), ext); err != nil {
		t.Fatalf("reconcile error = %v", err)
	}

	if ready := readyCondition(t, ext); ready.Reason != "ExtensionNamespaceTerminating" {
		t.Fatalf("Ready reason = %s, want ExtensionNamespaceTerminating; a namespace being "+
			"drained is not the same state as one that is gone", ready.Reason)
	}
	if ext.Status.Phase != v1alpha1.InstallAIExtensionPhasePending {
		t.Errorf("Phase = %s, want Pending", ext.Status.Phase)
	}
}

// Ordering matters for the diagnosis, not just for correctness. Uninstalling
// Rancher takes the namespace with it, so both checks fail at once — and
// "restore Rancher" is the instruction that helps, while "recreate this
// namespace" sends an admin to recreate a namespace Rancher will not populate.
func TestRancherUnavailableOutranksMissingNamespace(t *testing.T) {
	ext := helmExtension()
	r, _ := gateReconciler(t, ext, &gateRancherManager{crdErr: errors.New("no matches for kind")})

	if _, err := r.reconcile(context.Background(), ext); err != nil {
		t.Fatalf("reconcile error = %v", err)
	}

	if ready := readyCondition(t, ext); ready.Reason != "RancherUnavailable" {
		t.Errorf("Ready reason = %s, want RancherUnavailable; with Rancher gone the missing "+
			"namespace is a symptom, and reporting the symptom hides the cause", ready.Reason)
	}
}

// A condition is only visible to someone already looking at this CR. Whoever
// notices this first is looking at a UI that stopped loading, and the event
// stream is where they land before they know which object to describe.
func TestBlockedPreconditionEmitsAWarningEvent(t *testing.T) {
	ext := helmExtension()
	r, rec := gateReconciler(t, ext, &gateRancherManager{})

	if _, err := r.reconcile(context.Background(), ext); err != nil {
		t.Fatalf("reconcile error = %v", err)
	}

	select {
	case got := <-rec.Events:
		if !strings.Contains(got, "Warning") || !strings.Contains(got, "ExtensionNamespaceMissing") {
			t.Errorf("event = %q, want a Warning naming ExtensionNamespaceMissing", got)
		}
		if !strings.Contains(got, "rollout restart") {
			t.Errorf("event = %q, want it to carry the same remediation as the condition", got)
		}
	default:
		t.Fatal("no event recorded; the condition is then the only report, and it is the one " +
			"an admin has to already suspect this CR to find")
	}
}

// The gate runs before cleanupStaleResources on purpose. That cleanup deletes
// Rancher objects and a Helm release belonging to the *previous* extension
// name, all of which live in the namespace that is missing — attempting it
// first turns a diagnosable state into an opaque cleanup error.
func TestPreconditionsRunBeforeStaleCleanup(t *testing.T) {
	ext := helmExtension()
	ext.Status.ActiveExtensionName = "previous-extension"

	mgr := &gateRancherManager{}
	r, _ := gateReconciler(t, ext, mgr)

	if _, err := r.reconcile(context.Background(), ext); err != nil {
		t.Fatalf("reconcile error = %v", err)
	}

	if mgr.uiPluginDeletes != 0 {
		t.Errorf("cleanup issued %d UIPlugin deletes, want 0; only cleanupStaleResources deletes "+
			"UIPlugins, and it targets objects in a namespace that does not exist — running it "+
			"first would replace the diagnosis with its own failure", mgr.uiPluginDeletes)
	}
}

// The gate has to be invisible when both preconditions hold, or it becomes a
// new way for a healthy install to stall.
func TestPreconditionsPassWhenNamespaceAndCRDsExist(t *testing.T) {
	ext := helmExtension()
	r, _ := gateReconciler(t, ext, &gateRancherManager{}, extensionNamespace())

	result, blocked, err := r.checkPreconditions(context.Background(), ext)
	if err != nil {
		t.Fatalf("checkPreconditions error = %v", err)
	}
	if blocked {
		t.Fatalf("blocked = true with the namespace present and the CRDs registered; "+
			"Ready = %+v", meta.FindStatusCondition(ext.Status.Conditions, conditionTypeReady))
	}
	if !result.IsZero() {
		t.Errorf("result = %+v, want zero; a passing gate must leave the requeue to the rest "+
			"of the pass", result)
	}
}

// reapRancherManager records the names DeleteClusterRepo is called with, so a
// test can check reapOrphanedClusterRepo targets the right extension rather
// than merely firing at all. crdErr lets a case reach RancherUnavailable
// instead of the namespace check, the same way gateRancherManager does.
type reapRancherManager struct {
	stubRancherManager
	crdErr              error
	deletedClusterRepos []string
}

func (r *reapRancherManager) CheckCRDs(context.Context, []string) error { return r.crdErr }

func (r *reapRancherManager) DeleteClusterRepo(_ context.Context, name string) error {
	r.deletedClusterRepos = append(r.deletedClusterRepos, name)
	return nil
}

// The scenario item 1 exists for: UIPlugin is namespaced and is garbage
// collected along with cattle-ui-plugin-system, but ClusterRepo is
// cluster-scoped and survives, left pointing at a Service URL in a namespace
// that no longer exists — a resource dangling exactly the way the platform
// cannot afford one to. Once the namespace is confirmed gone, that ClusterRepo
// must not be left standing for the whole outage.
func TestMissingNamespaceReapsTheOrphanedClusterRepo(t *testing.T) {
	ext := helmExtension()
	mgr := &reapRancherManager{}
	r, _ := gateReconciler(t, ext, mgr)

	if _, err := r.reconcile(context.Background(), ext); err != nil {
		t.Fatalf("reconcile error = %v", err)
	}

	want := rancher.ClusterRepoName(ext.Spec.Extension.Name)
	if len(mgr.deletedClusterRepos) != 1 || mgr.deletedClusterRepos[0] != want {
		t.Fatalf("DeleteClusterRepo calls = %v, want exactly [%q]; the extension's own "+
			"ClusterRepo must be reaped once its namespace is confirmed gone",
			mgr.deletedClusterRepos, want)
	}
}

// A rename in flight has two candidate names — cleanupStaleResources targets
// both for the same reason, and reaping only the new one would leave the old
// ClusterRepo dangling instead.
func TestMissingNamespaceReapsBothNamesDuringARename(t *testing.T) {
	ext := helmExtension()
	ext.Status.ActiveExtensionName = "previous-extension"
	mgr := &reapRancherManager{}
	r, _ := gateReconciler(t, ext, mgr)

	if _, err := r.reconcile(context.Background(), ext); err != nil {
		t.Fatalf("reconcile error = %v", err)
	}

	wantOld := rancher.ClusterRepoName("previous-extension")
	wantNew := rancher.ClusterRepoName(ext.Spec.Extension.Name)
	got := map[string]bool{}
	for _, name := range mgr.deletedClusterRepos {
		got[name] = true
	}
	if !got[wantOld] || !got[wantNew] {
		t.Fatalf("DeleteClusterRepo calls = %v, want both %q and %q",
			mgr.deletedClusterRepos, wantOld, wantNew)
	}
}

// A namespace merely terminating is not yet the "gone" state this exists for
// — its Service may still be resolving requests during the grace period — so
// reaping here is left out on purpose, per the design.
func TestTerminatingNamespaceDoesNotReapTheClusterRepo(t *testing.T) {
	ext := helmExtension()
	mgr := &reapRancherManager{}
	r, _ := gateReconciler(t, ext, mgr, terminatingNamespace())

	if _, err := r.reconcile(context.Background(), ext); err != nil {
		t.Fatalf("reconcile error = %v", err)
	}

	if len(mgr.deletedClusterRepos) != 0 {
		t.Errorf("DeleteClusterRepo calls = %v, want none while the namespace is only "+
			"terminating", mgr.deletedClusterRepos)
	}
}

// Rancher being entirely unavailable must not fall through to the reap: if
// the CRDs are gone, so is the ClusterRepo's own type, and RancherUnavailable
// is the cause that needs reporting — not a namespace-shaped symptom of it.
func TestRancherUnavailableDoesNotReapTheClusterRepo(t *testing.T) {
	ext := helmExtension()
	mgr := &reapRancherManager{crdErr: context.DeadlineExceeded}
	r, _ := gateReconciler(t, ext, mgr)

	if _, err := r.reconcile(context.Background(), ext); err != nil {
		t.Fatalf("reconcile error = %v", err)
	}

	if len(mgr.deletedClusterRepos) != 0 {
		t.Errorf("DeleteClusterRepo calls = %v, want none when Rancher itself is unavailable",
			mgr.deletedClusterRepos)
	}
}
