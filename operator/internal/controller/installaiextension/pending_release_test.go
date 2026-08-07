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
	"fmt"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	helmClient "github.com/SUSE/aif-operator/internal/infra/helm"
)

// pendingFixture returns a reconciler backed by a fake client holding ext, so
// handlePendingRelease's annotation writes go somewhere.
func pendingFixture(t *testing.T, ext *v1alpha1.InstallAIExtension) *InstallAIExtensionReconciler {
	t.Helper()

	scheme := kruntime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}
	if ext.Name == "" {
		ext.Name = "aif-ui"
	}
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha1.InstallAIExtension{}).
		WithObjects(ext.DeepCopy()).
		Build()

	// Update is optimistic-concurrency checked, so ext has to carry the stored
	// resourceVersion the way the reconcile loop's own Get leaves it.
	var stored v1alpha1.InstallAIExtension
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(ext), &stored); err != nil {
		t.Fatalf("get seeded object: %v", err)
	}
	ext.ResourceVersion = stored.ResourceVersion

	return &InstallAIExtensionReconciler{Client: c, Scheme: scheme}
}

func pendingErr(status string) error {
	return fmt.Errorf("%w: release %q is %s at revision 2",
		helmClient.ErrReleasePending, "aif-ui-server", status)
}

func TestHandlePendingRelease_RequeuesInsteadOfFailing(t *testing.T) {
	ext := &v1alpha1.InstallAIExtension{}
	ext.Generation = 3
	r := pendingFixture(t, ext)

	// First observation only starts the clock; conditions land on the next pass,
	// once the marker is durable.
	result, handled, err := r.handlePendingRelease(context.Background(), ext,
		conditionTypeHelmInstalled, pendingErr("pending-upgrade"))
	if err != nil {
		t.Fatalf("handlePendingRelease() error = %v", err)
	}
	if !handled {
		t.Fatal("handled = false, want true for a pending release")
	}
	if result.RequeueAfter != pendingReleaseRequeue {
		t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, pendingReleaseRequeue)
	}
	if r.getWaitingSince(ext, annotationReleasePendingSince).IsZero() {
		t.Fatal("pending marker not set, so the wait would never time out")
	}

	result, handled, err = r.handlePendingRelease(context.Background(), ext,
		conditionTypeHelmInstalled, pendingErr("pending-upgrade"))
	if err != nil || !handled || result.RequeueAfter != pendingReleaseRequeue {
		t.Fatalf("second pass: handled = %v, RequeueAfter = %v, err = %v; want true, %v, nil",
			handled, result.RequeueAfter, err, pendingReleaseRequeue)
	}
	// Pending is a timing state; marking the phase Failed would strand the CR
	// with no requeue to recover it.
	if ext.Status.Phase == v1alpha1.InstallAIExtensionPhaseFailed {
		t.Error("phase = Failed, want the CR left recoverable")
	}

	cond := meta.FindStatusCondition(ext.Status.Conditions, conditionTypeHelmInstalled)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "ReleasePending" {
		t.Fatalf("HelmInstalled condition = %+v, want False/ReleasePending", cond)
	}
	if cond.ObservedGeneration != 3 {
		t.Errorf("observedGeneration = %d, want 3", cond.ObservedGeneration)
	}
}

// A CR that already reached Installed carries Ready=True. Leaving it that way
// while an upgrade is stuck reports the extension as healthy when it is not.
func TestHandlePendingRelease_ClearsStaleReady(t *testing.T) {
	ext := &v1alpha1.InstallAIExtension{}
	ext.Generation = 4
	markPendingSince(ext, 1*time.Minute)
	setCondition(&ext.Status.Conditions, conditionTypeReady, metav1.ConditionTrue,
		"Installed", "extension installed", 3)
	r := pendingFixture(t, ext)

	if _, handled, err := r.handlePendingRelease(context.Background(), ext,
		conditionTypeHelmInstalled, pendingErr("pending-upgrade")); err != nil || !handled {
		t.Fatalf("handled = %v, err = %v; want true, nil", handled, err)
	}

	rd := meta.FindStatusCondition(ext.Status.Conditions, conditionTypeReady)
	if rd == nil || rd.Status != metav1.ConditionFalse || rd.Reason != "ReleasePending" {
		t.Fatalf("Ready condition = %+v, want False/ReleasePending", rd)
	}
	if rd.ObservedGeneration != 4 {
		t.Errorf("Ready observedGeneration = %d, want 4", rd.ObservedGeneration)
	}
}

// Both source kinds reach EnsureRelease, so both must get the same treatment for
// the same cluster state. The git path previously failed terminally here.
func TestHandlePendingRelease_AppliesToBothSourceKinds(t *testing.T) {
	for _, condType := range []string{conditionTypeHelmInstalled, conditionTypeUIPlugin} {
		t.Run(condType, func(t *testing.T) {
			ext := &v1alpha1.InstallAIExtension{}
			markPendingSince(ext, 1*time.Minute)
			r := pendingFixture(t, ext)

			result, handled, err := r.handlePendingRelease(context.Background(), ext,
				condType, pendingErr("pending-install"))
			if err != nil {
				t.Fatalf("handlePendingRelease() error = %v", err)
			}
			if !handled || result.RequeueAfter != pendingReleaseRequeue {
				t.Fatalf("handled = %v, RequeueAfter = %v; want true, %v",
					handled, result.RequeueAfter, pendingReleaseRequeue)
			}
			cond := meta.FindStatusCondition(ext.Status.Conditions, condType)
			if cond == nil || cond.Reason != "ReleasePending" {
				t.Fatalf("%s condition = %+v, want False/ReleasePending", condType, cond)
			}
		})
	}
}

// A wrapped sentinel still has to be recognised: ensureUIPluginGit returns the
// EnsureRelease error through its own call chain.
func TestHandlePendingRelease_MatchesWrappedSentinel(t *testing.T) {
	ext := &v1alpha1.InstallAIExtension{}
	r := pendingFixture(t, ext)
	wrapped := fmt.Errorf("UIPlugin install failed: %w", pendingErr("pending-upgrade"))

	if _, handled, err := r.handlePendingRelease(context.Background(), ext,
		conditionTypeUIPlugin, wrapped); err != nil || !handled {
		t.Errorf("handled = %v, err = %v for a wrapped ErrReleasePending; want true, nil", handled, err)
	}
}

func TestHandlePendingRelease_IgnoresOtherErrors(t *testing.T) {
	ext := &v1alpha1.InstallAIExtension{}
	r := pendingFixture(t, ext)

	result, handled, err := r.handlePendingRelease(context.Background(), ext,
		conditionTypeHelmInstalled, errors.New("chart pull failed: 404"))
	if err != nil {
		t.Fatalf("handlePendingRelease() error = %v", err)
	}
	if handled {
		t.Error("handled = true, want false so the caller fails terminally")
	}
	if !result.IsZero() {
		t.Errorf("result = %+v, want zero", result)
	}
	if len(ext.Status.Conditions) != 0 {
		t.Errorf("conditions = %+v, want none set", ext.Status.Conditions)
	}
}

// Requeuing forever on a release that Helm will never settle — the marker a
// process killed mid-upgrade leaves behind — hides the CR in a permanent
// "waiting" state. Past the timeout it has to say so, and say what to run.
func TestHandlePendingRelease_TimesOutAndNamesTheManualFix(t *testing.T) {
	ext := &v1alpha1.InstallAIExtension{}
	markPendingSince(ext, pendingReleaseTimeout+time.Minute)
	r := pendingFixture(t, ext)

	result, handled, err := r.handlePendingRelease(context.Background(), ext,
		conditionTypeHelmInstalled, pendingErr("pending-upgrade"))
	if err != nil {
		t.Fatalf("handlePendingRelease() error = %v", err)
	}
	if !handled {
		t.Fatal("handled = false, want the timeout owned here rather than reported as an install failure")
	}
	if result.RequeueAfter != 0 {
		t.Errorf("RequeueAfter = %v, want 0 once the wait is given up on", result.RequeueAfter)
	}
	if ext.Status.Phase != v1alpha1.InstallAIExtensionPhaseFailed {
		t.Errorf("phase = %q, want Failed", ext.Status.Phase)
	}

	cond := meta.FindStatusCondition(ext.Status.Conditions, conditionTypeHelmInstalled)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "ReleasePendingTimedOut" {
		t.Fatalf("HelmInstalled condition = %+v, want False/ReleasePendingTimedOut", cond)
	}
	// The operator cannot clear a wedged release itself, so the message is the
	// only place an operator learns what to do.
	if !containsAll(cond.Message, "helm rollback", "helm uninstall") {
		t.Errorf("message = %q, want it to name the manual recovery commands", cond.Message)
	}
}

// Just under the timeout the CR must still be waiting, or a slow-but-healthy
// upgrade gets failed out from under itself.
func TestHandlePendingRelease_WaitsUntilTheTimeoutElapses(t *testing.T) {
	ext := &v1alpha1.InstallAIExtension{}
	markPendingSince(ext, pendingReleaseTimeout-time.Minute)
	r := pendingFixture(t, ext)

	result, _, err := r.handlePendingRelease(context.Background(), ext,
		conditionTypeHelmInstalled, pendingErr("pending-upgrade"))
	if err != nil {
		t.Fatalf("handlePendingRelease() error = %v", err)
	}
	if result.RequeueAfter != pendingReleaseRequeue {
		t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, pendingReleaseRequeue)
	}
	if ext.Status.Phase == v1alpha1.InstallAIExtensionPhaseFailed {
		t.Error("phase = Failed before the timeout elapsed")
	}
}

// The marker has to be dropped once the release settles. Kept, it would make the
// next pending operation inherit this window and time out immediately.
func TestHandlePendingRelease_ClearsMarkerWhenNoLongerPending(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"release settled", nil},
		{"failed for another reason", errors.New("chart pull failed: 404")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ext := &v1alpha1.InstallAIExtension{}
			markPendingSince(ext, 5*time.Minute)
			r := pendingFixture(t, ext)

			if _, handled, err := r.handlePendingRelease(context.Background(), ext,
				conditionTypeHelmInstalled, tt.err); err != nil || handled {
				t.Fatalf("handled = %v, err = %v; want false, nil", handled, err)
			}
			if !r.getWaitingSince(ext, annotationReleasePendingSince).IsZero() {
				t.Error("pending marker still set; the next pending release would inherit it")
			}
		})
	}
}

// The two waits can be live in the same pass, so neither may disturb the other's
// clock — sharing one annotation would make the readiness timeout restart, or
// fire early, whenever a release went pending.
func TestHandlePendingRelease_LeavesReadinessMarkerAlone(t *testing.T) {
	ext := &v1alpha1.InstallAIExtension{}
	r := pendingFixture(t, ext)
	r.setWaitingSince(ext, annotationWaitingSince)
	readinessStart := r.getWaitingSince(ext, annotationWaitingSince)

	if _, _, err := r.handlePendingRelease(context.Background(), ext,
		conditionTypeHelmInstalled, pendingErr("pending-upgrade")); err != nil {
		t.Fatalf("handlePendingRelease() error = %v", err)
	}
	if got := r.getWaitingSince(ext, annotationWaitingSince); !got.Equal(readinessStart) {
		t.Errorf("readiness marker = %v, want it untouched at %v", got, readinessStart)
	}

	// And clearing the pending wait must not take the readiness marker with it.
	if _, _, err := r.handlePendingRelease(context.Background(), ext,
		conditionTypeHelmInstalled, nil); err != nil {
		t.Fatalf("handlePendingRelease() error = %v", err)
	}
	if got := r.getWaitingSince(ext, annotationWaitingSince); !got.Equal(readinessStart) {
		t.Errorf("readiness marker = %v after clearing the pending wait, want %v", got, readinessStart)
	}
}

// markPendingSince backdates the pending marker by age.
func markPendingSince(ext *v1alpha1.InstallAIExtension, age time.Duration) {
	if ext.Annotations == nil {
		ext.Annotations = make(map[string]string)
	}
	ext.Annotations[annotationReleasePendingSince] = time.Now().Add(-age).Format(time.RFC3339)
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
