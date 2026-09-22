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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// TestHelmPhaseFromReadiness pins the pod-state-driven classification: a
// not-ready workload with no hard-failure reason is a slow start (Pending, no
// timer), and one with a reason is stuck (Degraded immediately).
func TestHelmPhaseFromReadiness(t *testing.T) {
	tests := []struct {
		name           string
		notReadyCount  int
		failureReasons []string
		wantPhase      aiplatformv1alpha1.AIWorkloadPhase
		wantInMsg      string
	}{
		{
			name:          "all ready → Running",
			notReadyCount: 0,
			wantPhase:     aiplatformv1alpha1.AIWorkloadPhaseRunning,
		},
		{
			name:          "not ready, no failure reason → Pending",
			notReadyCount: 1,
			wantPhase:     aiplatformv1alpha1.AIWorkloadPhasePending,
			wantInMsg:     "Waiting for 1 workload",
		},
		{
			name:           "not ready with a hard failure → Degraded",
			notReadyCount:  1,
			failureReasons: []string{reasonCrashLoopBackOff},
			wantPhase:      aiplatformv1alpha1.AIWorkloadPhaseDegraded,
			wantInMsg:      reasonCrashLoopBackOff,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPhase, _, gotMsg := helmPhaseFromReadiness(tt.notReadyCount, tt.failureReasons)
			if gotPhase != tt.wantPhase {
				t.Errorf("phase = %q, want %q", gotPhase, tt.wantPhase)
			}
			if tt.wantInMsg != "" && !strings.Contains(gotMsg, tt.wantInMsg) {
				t.Errorf("message = %q, want to contain %q", gotMsg, tt.wantInMsg)
			}
		})
	}
}

// TestPodHardFailureReason pins which pod states count as "stuck" (Degraded) vs
// "still starting" (Pending). The starting states must return "" so a slow image
// pull or model load is never mislabeled as a failure.
func TestPodHardFailureReason(t *testing.T) {
	waiting := func(reason string) *corev1.Pod {
		return &corev1.Pod{Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{{
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason}},
			}},
		}}
	}
	initWaiting := func(reason string) *corev1.Pod {
		return &corev1.Pod{Status: corev1.PodStatus{
			InitContainerStatuses: []corev1.ContainerStatus{{
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason}},
			}},
		}}
	}
	unschedulable := &corev1.Pod{Status: corev1.PodStatus{
		Conditions: []corev1.PodCondition{{
			Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: podScheduleFailure,
		}},
	}}

	tests := []struct {
		name string
		pod  *corev1.Pod
		want string
	}{
		{"crash loop", waiting(reasonCrashLoopBackOff), reasonCrashLoopBackOff},
		{"image pull backoff", waiting(reasonImagePullBackOff), reasonImagePullBackOff},
		{"bad config", waiting("CreateContainerConfigError"), "CreateContainerConfigError"},
		{"init container image pull", initWaiting(reasonImagePullBackOff), reasonImagePullBackOff},
		{"unschedulable", unschedulable, "Unschedulable"},
		{"still pulling is not a failure", waiting("ContainerCreating"), ""},
		{"still initializing is not a failure", waiting("PodInitializing"), ""},
		{"running is not a failure", &corev1.Pod{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := podHardFailureReason(tt.pod); got != tt.want {
				t.Errorf("podHardFailureReason() = %q, want %q", got, tt.want)
			}
		})
	}
}
