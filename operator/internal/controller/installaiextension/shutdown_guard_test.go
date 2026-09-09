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

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// TestReconcileRecordsNoVerdictDuringShutdown covers the CR-side half of the
// bug that made the Helm release read `failed: context canceled`.
//
// Rolling the operator cancels the manager's context, and that context is the
// one every reconcile runs under. Whatever the pass was in the middle of fails
// with context.Canceled — and eighteen call sites record a failure without
// asking why it failed. The pass then writes
// Phase=Failed on its way out, so a routine restart leaves the CR reporting a
// broken extension.
//
// Cancellation is not a verdict about the extension. It says the process is
// going away and this pass learned nothing, so the pass must write nothing and
// let the next leader start clean.
//
// The assertion is deliberately "nothing was persisted" rather than "this
// particular verdict was suppressed". Which of the eighteen sites the pass
// reaches first is not the point and varies with how far it gets; the guard's
// contract is that a cancelled pass writes no status at all.
func TestReconcileRecordsNoVerdictDuringShutdown(t *testing.T) {
	ext := helmExtension()
	// Already finalized: otherwise the pass returns at ensureFinalizer and never
	// reaches the code under test.
	controllerutil.AddFinalizer(ext, finalizerName)

	r := readinessReconciler(t, ext, interceptor.Funcs{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(ext)}); err != nil {
		t.Errorf("Reconcile() error = %v, want nil: see "+
			"TestShutdownIsNotReportedAsAReconcileError", err)
	}

	var stored v1alpha1.InstallAIExtension
	if getErr := r.Get(context.Background(), client.ObjectKeyFromObject(ext), &stored); getErr != nil {
		t.Fatalf("get stored object: %v", getErr)
	}
	if stored.Status.Phase == v1alpha1.InstallAIExtensionPhaseFailed {
		t.Errorf("Phase = %s persisted during shutdown; the operator was restarting, "+
			"not the extension failing", stored.Status.Phase)
	}
	if len(stored.Status.Conditions) != 0 {
		t.Errorf("conditions %+v persisted during shutdown; the pass reached no verdict",
			stored.Status.Conditions)
	}
}

// TestShutdownIsNotReportedAsAReconcileError covers the operator-side half of
// the same mislabelling: not what the CR says, but what the operator's own logs
// and metrics say about a restart.
//
// controller-runtime has no case for cancellation. A non-nil error means the
// reconcile went wrong, full stop — `ERROR ... Reconciler error`,
// controller_runtime_reconcile_errors_total, reconcile_total{result="error"}.
// Report shutdown through that channel and every roll of the operator emits
// errors proportional to how many CRs were mid-pass, which is indistinguishable
// from the operator actually failing to reconcile them.
//
// Not a bare success either: the pass really did not settle the CR, so it
// requeues rather than telling the queue it is done.
func TestShutdownIsNotReportedAsAReconcileError(t *testing.T) {
	ext := helmExtension()
	controllerutil.AddFinalizer(ext, finalizerName)

	r := readinessReconciler(t, ext, interceptor.Funcs{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(ext)})
	if err != nil {
		t.Errorf("Reconcile() error = %v, want nil; a non-nil error here logs "+
			"`Reconciler error` and increments reconcile_errors_total on every "+
			"routine restart", err)
	}
	if result.RequeueAfter <= 0 {
		t.Errorf("Reconcile() result = %+v, want a requeue; the pass was abandoned "+
			"partway and must not be recorded as having settled the CR", result)
	}
}

// TestReconciliationTimeoutStillReportsAnError keeps the shutdown translation
// from widening into "any dead context is fine".
//
// Controller.ReconciliationTimeout cancels the very same context this
// controller inspects, and a pass that blows it is a genuine failure worth an
// error and a metric. It is off by default and unset here, so nothing today
// takes this path — which is exactly why a future edit enabling it would
// otherwise find its timeouts silently filed as restarts. The two are
// distinguishable only by cause, so that is what the boundary keys on.
func TestReconciliationTimeoutStillReportsAnError(t *testing.T) {
	ext := helmExtension()
	controllerutil.AddFinalizer(ext, finalizerName)

	r := readinessReconciler(t, ext, interceptor.Funcs{})

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(ext)})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Reconcile() error = %v, want context.DeadlineExceeded: an expired "+
			"reconcile budget is a failure, not a shutdown", err)
	}
}

// TestReadinessTimeoutOutlastsTheRolloutItWaitsOn pins a bound that is only
// correct relative to two numbers living elsewhere, neither of which is in this
// repo.
//
// This clock replaced Helm's own Wait, which allowed 10 minutes. Below that, an
// upgrade that used to succeed on a slow image pull now fails. It also has to
// outlast the Deployment's progressDeadlineSeconds, which defaults to 600s and
// the chart does not set: shorter, and the CR declares the rollout dead while
// Kubernetes is still working on it — and since a readiness timeout is terminal
// and does not requeue, the CR stays Failed through a rollout that then
// succeeds.
func TestReadinessTimeoutOutlastsTheRolloutItWaitsOn(t *testing.T) {
	const deploymentProgressDeadline = 600 * time.Second

	if DefaultReadinessTimeout < deploymentProgressDeadline {
		t.Errorf("DefaultReadinessTimeout = %s, want >= %s (the Deployment progress deadline); "+
			"the CR must not give up on a rollout before Kubernetes does",
			DefaultReadinessTimeout, deploymentProgressDeadline)
	}
}
