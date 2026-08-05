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
	"errors"
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	helmClient "github.com/SUSE/aif-operator/internal/infra/helm"
)

func TestHandlePendingRelease_RequeuesInsteadOfFailing(t *testing.T) {
	ext := &v1alpha1.InstallAIExtension{}
	ext.Generation = 3

	err := fmt.Errorf("%w: release %q is pending-upgrade at revision 2",
		helmClient.ErrReleasePending, "aif-ui-server")

	result, handled := handlePendingRelease(ext, conditionTypeHelmInstalled, err)

	if !handled {
		t.Fatal("handled = false, want true for a pending release")
	}
	if result.RequeueAfter != pendingReleaseRequeue {
		t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, pendingReleaseRequeue)
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

// Both source kinds reach EnsureRelease, so both must get the same treatment for
// the same cluster state. The git path previously failed terminally here.
func TestHandlePendingRelease_AppliesToBothSourceKinds(t *testing.T) {
	for _, condType := range []string{conditionTypeHelmInstalled, conditionTypeUIPlugin} {
		t.Run(condType, func(t *testing.T) {
			ext := &v1alpha1.InstallAIExtension{}
			err := fmt.Errorf("%w: release %q is pending-install at revision 1",
				helmClient.ErrReleasePending, "aif-ui")

			result, handled := handlePendingRelease(ext, condType, err)

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
	wrapped := fmt.Errorf("UIPlugin install failed: %w",
		fmt.Errorf("%w: release is pending-upgrade", helmClient.ErrReleasePending))

	if _, handled := handlePendingRelease(ext, conditionTypeUIPlugin, wrapped); !handled {
		t.Error("handled = false for a wrapped ErrReleasePending, want true")
	}
}

func TestHandlePendingRelease_IgnoresOtherErrors(t *testing.T) {
	ext := &v1alpha1.InstallAIExtension{}

	result, handled := handlePendingRelease(ext, conditionTypeHelmInstalled,
		errors.New("chart pull failed: 404"))

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
