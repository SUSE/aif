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
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	helmClient "github.com/SUSE/aif-operator/internal/infra/helm"
)

// A failed marker cleanup must not become the reconcile's answer. The caller
// treats a nil error from handlePendingRelease as "the release is not pending,
// deal with the EnsureRelease error yourself" — so returning the cleanup's error
// instead skips that entirely and the CR is left with no explanation of why the
// install failed, only an unrelated write conflict on the reconcile.
func TestHandlePendingRelease_KeepsTheInstallErrorWhenClearingTheMarkerFails(t *testing.T) {
	ext := helmExtension()
	markPendingSince(ext, time.Minute)

	installErr := errors.New("chart pull refused")
	stub := &stubHelmClient{
		deployed:  &helmClient.ReleaseInfo{Version: requestedVersion, Status: helmClient.StatusDeployed, Revision: 1},
		last:      &helmClient.ReleaseInfo{Version: requestedVersion, Status: helmClient.StatusFailed, Revision: 2},
		ensureErr: installErr,
	}
	writeErr := errors.New("the object has been modified")
	r := wiringReconcilerWith(t, ext, stub, interceptor.Funcs{
		Update: func(context.Context, client.WithWatch, client.Object, ...client.UpdateOption) error {
			return writeErr
		},
	})

	_, err := r.reconcileHelmSource(context.Background(), ext, wiringNamespace)

	if err != nil {
		t.Fatalf("reconcile error = %v, want nil; a marker cleanup failure is retried next pass, "+
			"it is not the outcome of the reconcile", err)
	}
	cond := meta.FindStatusCondition(ext.Status.Conditions, conditionTypeHelmInstalled)
	if cond == nil {
		t.Fatal("no HelmInstalled condition; the real install failure was never recorded")
	}
	if cond.Reason != "InstallFailed" {
		t.Errorf("HelmInstalled reason = %q, want InstallFailed", cond.Reason)
	}
	if !containsAll(cond.Message, installErr.Error()) {
		t.Errorf("HelmInstalled message = %q, want it to name the install failure %q",
			cond.Message, installErr)
	}
}

// The marker is only ever cleared inside handlePendingRelease, and a reconcile
// has several ways to return before it gets there. This drives the whole loop so
// the coverage is "a pass that ended somewhere else leaves no marker behind",
// not "one particular early return remembers to clean up".
func TestReconcile_ClearsTheMarkerWhenThePassNeverReachedTheRelease(t *testing.T) {
	ext := helmExtension()
	ext.Finalizers = []string{finalizerName}
	markPendingSince(ext, time.Minute)

	r := wiringReconciler(t, ext, pendingStub())
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(ext)}

	// CheckCRDs dials the in-cluster API config, which does not exist under `go
	// test`, so the pass fails the Rancher preflight and returns above the Helm
	// call — the same shape as a real cluster missing the CRDs.
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("reconcile error = %v, want nil", err)
	}

	var stored v1alpha1.InstallAIExtension
	if err := r.Get(context.Background(), req.NamespacedName, &stored); err != nil {
		t.Fatalf("get stored object: %v", err)
	}
	ready := meta.FindStatusCondition(stored.Status.Conditions, conditionTypeReady)
	if ready == nil || inReleasePendingWait(&stored) {
		t.Fatalf("Ready = %+v, want a verdict other than a pending wait; the fixture did not "+
			"return early and so proves nothing", ready)
	}
	if _, ok := stored.Annotations[annotationReleasePendingSince]; ok {
		t.Errorf("marker survived a pass that reached %q; once it outlives pendingReleaseTimeout "+
			"the next genuine wait inherits this window and fails on its first observation", ready.Reason)
	}
}

// The marker lives in an annotation, so anyone with edit access on the CR can
// set it — and a value in the future turns the bound off entirely rather than
// tightening it. The wait has to re-anchor itself instead of requeuing forever.
func TestReconcileHelmSource_ReAnchorsAFutureDatedMarker(t *testing.T) {
	ext := helmExtension()
	markPendingSince(ext, -time.Hour) // dated an hour ahead

	r := wiringReconciler(t, ext, pendingStub())

	if _, err := r.reconcileHelmSource(context.Background(), ext, wiringNamespace); err != nil {
		t.Fatalf("reconcile error = %v, want nil", err)
	}

	stamp := ext.Annotations[annotationReleasePendingSince]
	if stamp == "" {
		t.Fatal("no pending marker recorded, so the wait is unbounded")
	}
	since, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		t.Fatalf("parse marker %q: %v", stamp, err)
	}
	if since.After(time.Now()) {
		t.Errorf("marker = %v, still ahead of now; time.Since stays negative, so the wait "+
			"never exceeds pendingReleaseTimeout and the operator requeues on it forever", since)
	}
}

// The marker's lifetime is decided from the Ready reason, so what counts as
// "still waiting" is worth stating outright: a wait that has exhausted keeps its
// marker, because clearing it would restart the 15-minute clock and leave the CR
// flapping between Failed and waiting forever.
func TestInReleasePendingWait(t *testing.T) {
	tests := []struct {
		reason string
		want   bool
	}{
		{reason: reasonReleasePending, want: true},
		{reason: reasonReleasePendingTimedOut, want: true},
		{reason: "CRDsMissing", want: false},
		{reason: "InvalidSpec", want: false},
		{reason: "Installed", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			ext := helmExtension()
			setCondition(&ext.Status.Conditions, conditionTypeReady, metav1.ConditionFalse,
				tt.reason, "", ext.Generation)

			if got := inReleasePendingWait(ext); got != tt.want {
				t.Errorf("inReleasePendingWait(%s) = %v, want %v", tt.reason, got, tt.want)
			}
		})
	}

	if inReleasePendingWait(helmExtension()) {
		t.Error("a pass that set no Ready condition is not a pending wait")
	}
}

// With nothing else to report, the cleanup failure is the only signal there is,
// so it has to surface. Otherwise a marker that cannot be written stays put
// silently and shortens the next genuine wait.
func TestHandlePendingRelease_SurfacesTheClearFailureOnAnOtherwiseCleanPass(t *testing.T) {
	ext := helmExtension()
	markPendingSince(ext, time.Minute)

	stub := &stubHelmClient{
		deployed: &helmClient.ReleaseInfo{Version: requestedVersion, Status: helmClient.StatusDeployed, Revision: 2},
		last:     &helmClient.ReleaseInfo{Version: requestedVersion, Status: helmClient.StatusDeployed, Revision: 2},
	}
	writeErr := errors.New("the object has been modified")
	r := wiringReconcilerWith(t, ext, stub, interceptor.Funcs{
		Update: func(context.Context, client.WithWatch, client.Object, ...client.UpdateOption) error {
			return writeErr
		},
	})

	_, _, err := r.handlePendingRelease(context.Background(), ext, conditionTypeHelmInstalled, nil)

	if !errors.Is(err, writeErr) {
		t.Fatalf("error = %v, want the write failure; nothing else would report it", err)
	}
}
