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
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestStatefulSetRolloutIncomplete pins the readiness contract for the
// StatefulSets a Helm/App workload owns. The masking case is the one that
// matters: mid RollingUpdate the previous revision's pods keep ReadyReplicas at
// its desired value, so a check that only counted ready pods would report a
// broken upgrade as complete.
func TestStatefulSetRolloutIncomplete(t *testing.T) {
	tests := []struct {
		name      string
		sts       *appsv1.StatefulSet
		wantReady bool
	}{
		{
			name: "spec update not yet observed is not ready",
			sts: statefulSet(2, 1, appsv1.StatefulSetSpec{Replicas: ptr(1)}, appsv1.StatefulSetStatus{
				ReadyReplicas: 1,
			}),
			wantReady: false,
		},
		{
			name: "fewer ready than desired is not ready",
			sts: statefulSet(1, 1, appsv1.StatefulSetSpec{Replicas: ptr(1)}, appsv1.StatefulSetStatus{
				ReadyReplicas: 0,
			}),
			wantReady: false,
		},
		{
			// The old pod is Ready and keeps the count at desired, but the update
			// revision has not rolled out — ReadyReplicas alone would call this done.
			name: "rolling update in progress with old pod ready is not ready",
			sts: statefulSet(1, 1,
				appsv1.StatefulSetSpec{
					Replicas:       ptr(1),
					UpdateStrategy: appsv1.StatefulSetUpdateStrategy{Type: appsv1.RollingUpdateStatefulSetStrategyType},
				},
				appsv1.StatefulSetStatus{
					ReadyReplicas:   1,
					UpdatedReplicas: 0,
					CurrentRevision: "rev-old",
					UpdateRevision:  "rev-new",
				}),
			wantReady: false,
		},
		{
			name: "rolling update complete is ready",
			sts: statefulSet(1, 1,
				appsv1.StatefulSetSpec{
					Replicas:       ptr(1),
					UpdateStrategy: appsv1.StatefulSetUpdateStrategy{Type: appsv1.RollingUpdateStatefulSetStrategyType},
				},
				appsv1.StatefulSetStatus{
					ReadyReplicas:   1,
					UpdatedReplicas: 1,
					CurrentRevision: "rev-current",
					UpdateRevision:  "rev-current",
				}),
			wantReady: true,
		},
		{
			name: "fresh install with a ready pod is ready",
			sts: statefulSet(0, 0, appsv1.StatefulSetSpec{Replicas: ptr(1)}, appsv1.StatefulSetStatus{
				ReadyReplicas: 1,
			}),
			wantReady: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := StatefulSetRolloutIncomplete(tt.sts)
			if gotReady := reason == ""; gotReady != tt.wantReady {
				t.Errorf("StatefulSetRolloutIncomplete() ready = %v, want %v (reason: %q)",
					gotReady, tt.wantReady, reason)
			}
		})
	}
}

// TestDaemonSetRolloutIncomplete pins the DaemonSet contract. As with the others,
// an upgrade whose new pods never become available must not be masked by the old
// pods still counting as ready.
func TestDaemonSetRolloutIncomplete(t *testing.T) {
	tests := []struct {
		name      string
		ds        *appsv1.DaemonSet
		wantReady bool
	}{
		{
			name: "spec update not yet observed is not ready",
			ds: daemonSet(2, appsv1.DaemonSetStatus{
				ObservedGeneration:     1,
				DesiredNumberScheduled: 3, UpdatedNumberScheduled: 3, NumberAvailable: 3,
			}),
			wantReady: false,
		},
		{
			// The old pods are available and keep the count up, but the new revision
			// has not been scheduled onto every node yet.
			name: "not all pods updated is not ready",
			ds: daemonSet(1, appsv1.DaemonSetStatus{
				ObservedGeneration:     1,
				DesiredNumberScheduled: 3, UpdatedNumberScheduled: 1, NumberAvailable: 3,
			}),
			wantReady: false,
		},
		{
			name: "updated but not all available is not ready",
			ds: daemonSet(1, appsv1.DaemonSetStatus{
				ObservedGeneration:     1,
				DesiredNumberScheduled: 3, UpdatedNumberScheduled: 3, NumberAvailable: 2,
			}),
			wantReady: false,
		},
		{
			name: "fully rolled out is ready",
			ds: daemonSet(1, appsv1.DaemonSetStatus{
				ObservedGeneration:     1,
				DesiredNumberScheduled: 3, UpdatedNumberScheduled: 3, NumberAvailable: 3,
			}),
			wantReady: true,
		},
		{
			name: "scheduled onto zero nodes is ready",
			ds: daemonSet(1, appsv1.DaemonSetStatus{
				ObservedGeneration:     1,
				DesiredNumberScheduled: 0, UpdatedNumberScheduled: 0, NumberAvailable: 0,
			}),
			wantReady: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := DaemonSetRolloutIncomplete(tt.ds)
			if gotReady := reason == ""; gotReady != tt.wantReady {
				t.Errorf("DaemonSetRolloutIncomplete() ready = %v, want %v (reason: %q)",
					gotReady, tt.wantReady, reason)
			}
		})
	}
}

func statefulSet(gen, observedGen int64, spec appsv1.StatefulSetSpec, status appsv1.StatefulSetStatus) *appsv1.StatefulSet {
	status.ObservedGeneration = observedGen
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Generation: gen},
		Spec:       spec,
		Status:     status,
	}
}

func daemonSet(gen int64, status appsv1.DaemonSetStatus) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Generation: gen},
		Status:     status,
	}
}

func ptr(i int32) *int32 { return &i }
