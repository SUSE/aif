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
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// justAppliedDeployment is what the API server holds in the instant after a
// Helm upgrade applies a new chart version: the new spec is stored, so
// Generation has advanced, but the deployment controller has not acted on it
// yet, so every count still describes the revision being replaced.
func justAppliedDeployment() *appsv1.Deployment {
	d := readyDeployment()
	d.Generation = 2
	d.Status.ObservedGeneration = 1
	return d
}

// TestDeploymentReadinessDoesNotReadTheCache covers the race that removing
// Helm's own wait exposed.
//
// The readiness check runs in the same reconcile pass that applied the
// manifest, microseconds later. Read through the manager's cache, it can be
// served an informer entry that predates the apply — and that entry is a
// perfectly consistent picture of a rollout that finished, because the
// *previous* one did. Generation matches ObservedGeneration, one replica is
// updated and available, so every condition in rolloutIncomplete passes and the
// CR reports the new version Installed while only the old pod is serving.
//
// No consistency check inside the object can catch this; nothing in the stale
// entry says which revision it describes. The only fix is to not read a cache,
// so that is what this test pins. The two clients below disagree exactly the
// way a lagging informer and the API server disagree.
//
// Helm's Wait used to hide this: it blocked on its own live client until the
// rollout finished, by which point the stale entry and the fresh one both
// looked ready. A fake client is always self-consistent, so no other test in
// this package can express the divergence.
func TestDeploymentReadinessDoesNotReadTheCache(t *testing.T) {
	ext := helmExtension()

	// The cache still holds the previous revision, fully rolled out.
	r := readinessReconciler(t, ext, interceptor.Funcs{}, readyDeployment())

	// The API server holds what the upgrade just applied.
	r.APIReader = fake.NewClientBuilder().
		WithScheme(r.Scheme).
		WithObjects(justAppliedDeployment()).
		Build()

	result, err := r.reconcileHelmSource(context.Background(), ext, wiringNamespace)
	if err != nil {
		t.Fatalf("reconcile error = %v", err)
	}

	cond := meta.FindStatusCondition(ext.Status.Conditions, conditionTypeDeploymentReady)
	if cond == nil {
		t.Fatal("no DeploymentReady condition recorded")
	}
	if cond.Status != metav1.ConditionFalse || cond.Reason != "NotReady" {
		t.Errorf("DeploymentReady = %s/%s, want False/NotReady: the readiness check was "+
			"served a pre-upgrade Deployment from the cache and reported the new version "+
			"ready while the old pod was still the only one serving (message: %q)",
			cond.Status, cond.Reason, cond.Message)
	}
	if result.RequeueAfter != readinessRequeue {
		t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, readinessRequeue)
	}
}

// TestDeploymentReadinessFallsBackToTheClient keeps the nil-APIReader default
// honest. Reconcilers built by hand — every other test in this package — leave
// the field unset, and a nil Reader would panic rather than fail.
func TestDeploymentReadinessFallsBackToTheClient(t *testing.T) {
	ext := helmExtension()
	r := readinessReconciler(t, ext, interceptor.Funcs{}, readyDeployment())
	r.APIReader = nil

	if _, err := r.reconcileHelmSource(context.Background(), ext, wiringNamespace); err != nil {
		t.Fatalf("reconcile error = %v", err)
	}

	cond := meta.FindStatusCondition(ext.Status.Conditions, conditionTypeDeploymentReady)
	if cond == nil || cond.Status != metav1.ConditionTrue {
		t.Errorf("DeploymentReady = %+v, want True; with no APIReader the check must fall "+
			"back to Client rather than panic", cond)
	}
}
