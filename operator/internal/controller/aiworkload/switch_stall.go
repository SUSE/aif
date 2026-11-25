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
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

const (
	reasonSwitching     = "Switching"
	reasonSwitchStalled = "SwitchStalled"

	// switchStallAfter is how long a kind switch runs before Ready says it
	// stalled. The switch is never aborted: abandoning a half-done handoff is
	// worse than waiting for Fleet.
	switchStallAfter = 10 * time.Minute
)

// switchRequeueAfter backs the switch requeue off as it ages; the Bundle,
// BundleDeployment and HelmOp watches still reconcile promptly on progress.
func switchRequeueAfter(elapsed time.Duration) time.Duration {
	switch {
	case elapsed < time.Minute:
		return switchRequeue
	case elapsed < 5*time.Minute:
		return 15 * time.Second
	case elapsed < switchStallAfter:
		return 30 * time.Second
	default:
		return time.Minute
	}
}

// switchingSince is when the current switch began: the LastTransitionTime of a
// Ready condition already Switching or SwitchStalled (kept by markSwitching),
// else now.
func switchingSince(w *aiplatformv1alpha1.AIWorkload, now time.Time) metav1.Time {
	if c := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady); c != nil && c.Status == metav1.ConditionFalse &&
		(c.Reason == reasonSwitching || c.Reason == reasonSwitchStalled) {
		return c.LastTransitionTime
	}
	return metav1.NewTime(now)
}

// markSwitching sets Ready=False/Switching with msg, or SwitchStalled once the
// switch has run for switchStallAfter (one Warning event on entering it), and
// returns the requeue for the switch's age. LastTransitionTime records when the
// switch began, as in markAutoValuePending, so the age survives restarts.
func (r *AIWorkloadReconciler) markSwitching(w *aiplatformv1alpha1.AIWorkload, msg string) time.Duration {
	now := r.clock()
	since := switchingSince(w, now)
	elapsed := now.Sub(since.Time)
	reason := reasonSwitching
	if elapsed >= switchStallAfter {
		reason = reasonSwitchStalled
		msg = fmt.Sprintf("The component switch has not finished after %d minutes; it resumes on its own once the step below completes. %s",
			int(elapsed/time.Minute), msg)
		if readyReason(w) != reasonSwitchStalled {
			r.event(w, corev1.EventTypeWarning, reasonSwitchStalled, "%s", truncateForCondition(msg))
		}
	}
	setCondition(&w.Status.Conditions, conditionTypeReady, metav1.ConditionFalse, reason, truncateForCondition(msg), w.Generation)
	meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady).LastTransitionTime = since
	return switchRequeueAfter(elapsed)
}
