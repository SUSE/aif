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

	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	helmClient "github.com/SUSE/aif-operator/internal/infra/helm"
)

// A failure the cluster can resolve on its own has to be re-checked, or the CR
// reports Failed forever over a condition that cleared minutes later.
//
// Nothing else brings the CR back. The controller watches only
// InstallAIExtension, so a registry coming back, a Rancher CRD being installed,
// or a slow rollout finishing produces no event — and a pass that returns a
// zero Result asks to never be woken again. Until now the only ways out were
// editing the CR or waiting for the informer's ~10h resync.
//
// This is not a claim that the extension is healthy. Phase stays Failed and the
// conditions still say what went wrong; the CR is just re-examined on the same
// cadence a healthy one is, so it can recover without anyone touching it.
func TestRecoverableFailuresAreRetried(t *testing.T) {
	ext := helmExtension()
	r := readinessReconciler(t, ext, interceptor.Funcs{})
	r.helmClientFor = func(string) (helmClient.HelmClient, error) {
		return &stubHelmClient{ensureErr: errors.New("registry unreachable")}, nil
	}

	result, err := r.reconcileHelmSource(context.Background(), ext, wiringNamespace)
	if err != nil {
		t.Fatalf("reconcile error = %v", err)
	}

	if ext.Status.Phase != v1alpha1.InstallAIExtensionPhaseFailed {
		t.Errorf("Phase = %s, want Failed; the failure must still be reported", ext.Status.Phase)
	}
	if result.RequeueAfter != healthCheckInterval {
		t.Errorf("RequeueAfter = %v, want %v; a registry that comes back produces no event, "+
			"so nothing would ever re-check this CR", result.RequeueAfter, healthCheckInterval)
	}
}

// TestMissingRancherCRDsAreRetried covers the recoverable failure most likely
// to be hit in practice, and the last one that was still terminal.
//
// Install the operator before Rancher and `uiplugins.catalog.cattle.io` does not
// exist yet. It appears minutes later, and nothing tells the controller: the
// preflight is against CRDs, the watch is on InstallAIExtension. So the very
// first reconcile of a brand-new install used to park the CR at Failed until
// someone edited it — the worst possible first impression, and the exact bug
// the rest of this file exists to prevent.
func TestMissingRancherCRDsAreRetried(t *testing.T) {
	ext := helmExtension()
	r := readinessReconciler(t, ext, interceptor.Funcs{})

	result, err := r.reconcile(context.Background(), ext)
	if err != nil {
		t.Fatalf("reconcile error = %v", err)
	}

	// Guards the test as much as the code: CheckCRDs dials the in-cluster config,
	// which does not exist under `go test`, so the preflight fails the same shape
	// a cluster without Rancher does. Asserting the reason means that if it ever
	// stops failing, the pass falls through to the Helm stub and this reports a
	// different reason — rather than passing green on a branch it never entered.
	ready := meta.FindStatusCondition(ext.Status.Conditions, conditionTypeReady)
	if ready == nil || ready.Reason != "CRDsMissing" {
		t.Fatalf("Ready condition = %+v, want reason CRDsMissing; the pass did not reach "+
			"the preflight branch this test covers", ready)
	}

	if ext.Status.Phase != v1alpha1.InstallAIExtensionPhaseFailed {
		t.Errorf("Phase = %s, want Failed; the CRDs really are missing", ext.Status.Phase)
	}
	if result.RequeueAfter != healthCheckInterval {
		t.Errorf("RequeueAfter = %v, want %v; Rancher installing its CRDs produces no event, "+
			"so nothing re-checks this CR until the informer's ~10h resync",
			result.RequeueAfter, healthCheckInterval)
	}
}

// The other half of the contract: a spec the operator will never accept must
// not be retried.
//
// Retrying buys nothing here — the verdict is a pure function of the spec and
// the operator's own configuration, so it cannot come out differently until one
// of them changes, and both changes already trigger a reconcile (a CR write via
// the watch, an operator flag via the restart). A requeue would only re-derive
// the same answer every minute for the life of the CR.
func TestInvalidSpecIsNotRetried(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*v1alpha1.InstallAIExtension)
	}{
		{
			name:   "source.helm missing",
			mutate: func(e *v1alpha1.InstallAIExtension) { e.Spec.Source.Helm = nil },
		},
		{
			name: "chart URL scheme unsupported",
			mutate: func(e *v1alpha1.InstallAIExtension) {
				e.Spec.Source.Helm.ChartURL = "ftp://example.invalid/chart"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ext := helmExtension()
			tt.mutate(ext)
			r := readinessReconciler(t, ext, interceptor.Funcs{})

			result, err := r.reconcileHelmSource(context.Background(), ext, wiringNamespace)
			if err != nil {
				t.Fatalf("reconcile error = %v", err)
			}

			if ext.Status.Phase != v1alpha1.InstallAIExtensionPhaseFailed {
				t.Errorf("Phase = %s, want Failed", ext.Status.Phase)
			}
			if result.RequeueAfter != 0 {
				t.Errorf("RequeueAfter = %v, want 0; this verdict cannot change without a "+
					"write that already wakes the controller", result.RequeueAfter)
			}
		})
	}
}
