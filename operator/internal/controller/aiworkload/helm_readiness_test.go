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
	"time"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func TestHelmPhaseFromReadiness(t *testing.T) {
	recent := time.Now()
	stale := time.Now().Add(-10 * time.Minute)

	ready := func(kind, name string) workloadReadiness {
		return workloadReadiness{kind: kind, name: name, ready: true}
	}
	notReady := func(kind, name string) workloadReadiness {
		return workloadReadiness{kind: kind, name: name, ready: false}
	}

	tests := []struct {
		name        string
		controllers []workloadReadiness
		createdAt   time.Time
		wantPhase   aiplatformv1alpha1.AIWorkloadPhase
		wantInMsg   string // substring the message must contain ("" = message must be empty)
	}{
		{
			name:        "all ready → Running",
			controllers: []workloadReadiness{ready("Deployment", "frontend"), ready("StatefulSet", "nim-llm")},
			createdAt:   stale,
			wantPhase:   aiplatformv1alpha1.AIWorkloadPhaseRunning,
			wantInMsg:   "",
		},
		{
			name:        "no controllers found → Running",
			controllers: nil,
			createdAt:   stale,
			wantPhase:   aiplatformv1alpha1.AIWorkloadPhaseRunning,
			wantInMsg:   "",
		},
		{
			name:        "unready within grace → Pending",
			controllers: []workloadReadiness{ready("Deployment", "frontend"), notReady("StatefulSet", "nim-llm")},
			createdAt:   recent,
			wantPhase:   aiplatformv1alpha1.AIWorkloadPhasePending,
			wantInMsg:   "nim-llm",
		},
		{
			name:        "unready past grace → Degraded",
			controllers: []workloadReadiness{ready("Deployment", "frontend"), notReady("StatefulSet", "nim-llm")},
			createdAt:   stale,
			wantPhase:   aiplatformv1alpha1.AIWorkloadPhaseDegraded,
			wantInMsg:   "nim-llm",
		},
		{
			name:        "all unready past grace → Degraded",
			controllers: []workloadReadiness{notReady("Deployment", "frontend"), notReady("StatefulSet", "nim-llm")},
			createdAt:   stale,
			wantPhase:   aiplatformv1alpha1.AIWorkloadPhaseDegraded,
			wantInMsg:   "frontend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPhase, gotMsg := helmPhaseFromReadiness(tt.controllers, tt.createdAt)
			if gotPhase != tt.wantPhase {
				t.Errorf("helmPhaseFromReadiness() phase = %q, want %q", gotPhase, tt.wantPhase)
			}
			if tt.wantInMsg == "" {
				if gotMsg != "" {
					t.Errorf("helmPhaseFromReadiness() message = %q, want empty", gotMsg)
				}
			} else if !strings.Contains(gotMsg, tt.wantInMsg) {
				t.Errorf("helmPhaseFromReadiness() message = %q, want to contain %q", gotMsg, tt.wantInMsg)
			}
		})
	}
}
