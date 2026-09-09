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

package kubernetes

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
)

// StatefulSetRolloutIncomplete names why a StatefulSet has not finished rolling
// out the revision currently in its spec, or returns "" once it has. It is the
// StatefulSet analogue of RolloutIncomplete (see deployment.go) and exists for
// the same reason: counting ready pods alone reports a RollingUpdate as done
// while the previous revision's pods are still serving, so a broken upgrade
// would look healthy. It follows `kubectl rollout status`' StatefulSet logic
// (kubectl/pkg/polymorphichelpers/rollout_status.go).
//
// The generation check mirrors RolloutIncomplete's (ObservedGeneration <
// Generation) rather than kubectl's extra ObservedGeneration == 0 clause, so the
// two sibling helpers reason about a stale spec identically.
func StatefulSetRolloutIncomplete(s *appsv1.StatefulSet) string {
	if s.Status.ObservedGeneration < s.Generation {
		return "waiting for the statefulset spec update to be observed"
	}

	desired := int32(1)
	if s.Spec.Replicas != nil {
		desired = *s.Spec.Replicas
	}
	if s.Status.ReadyReplicas < desired {
		return fmt.Sprintf("%d of %d pods are ready", s.Status.ReadyReplicas, desired)
	}

	// A RollingUpdate is only complete once every pod runs the update revision.
	// Until then CurrentRevision still names the revision being replaced and its
	// pods keep ReadyReplicas at the desired value, masking the incomplete roll.
	if s.Spec.UpdateStrategy.Type == appsv1.RollingUpdateStatefulSetStrategyType &&
		s.Status.UpdateRevision != "" &&
		s.Status.UpdateRevision != s.Status.CurrentRevision {
		return fmt.Sprintf("waiting for the rolling update to complete: %d of %d pods updated",
			s.Status.UpdatedReplicas, desired)
	}
	return ""
}

// DaemonSetRolloutIncomplete names why a DaemonSet has not finished rolling out
// the revision currently in its spec, or returns "" once it has. It is the
// DaemonSet analogue of RolloutIncomplete (see deployment.go) and follows
// `kubectl rollout status`' DaemonSet logic: an update is complete only once
// every scheduled node runs the new revision and its pods are available, so an
// upgrade whose new pods never become available is caught rather than masked by
// the old pods still counting as ready.
//
// A DaemonSet scheduled onto zero nodes (DesiredNumberScheduled == 0) has
// nothing to roll out and is reported ready, matching kubectl.
func DaemonSetRolloutIncomplete(d *appsv1.DaemonSet) string {
	if d.Status.ObservedGeneration < d.Generation {
		return "waiting for the daemonset spec update to be observed"
	}
	if d.Status.UpdatedNumberScheduled < d.Status.DesiredNumberScheduled {
		return fmt.Sprintf("%d of %d pods have been updated to the new revision",
			d.Status.UpdatedNumberScheduled, d.Status.DesiredNumberScheduled)
	}
	if d.Status.NumberAvailable < d.Status.DesiredNumberScheduled {
		return fmt.Sprintf("%d of %d updated pods are available",
			d.Status.NumberAvailable, d.Status.DesiredNumberScheduled)
	}
	return ""
}
