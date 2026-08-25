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

package v1alpha1

import (
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAIWorkloadStatusRecoveryTypesRoundTrip(t *testing.T) {
	st := AIWorkloadStatus{
		DeployedSource: &DeployedSourceSnapshot{
			Version:      "1.2.0",
			RenderDigest: "sha256:abc",
			CertifiedAt:  metav1.Now(),
		},
		ComponentStatuses: []AIWorkloadComponentStatus{{
			ComponentName:    "open-webui",
			ClusterID:        "local",
			Phase:            AIWorkloadClusterPhaseRunning,
			Revision:         "s-1",
			InstalledVersion: "3.1.0",
			Message:          "",
		}},
		ActiveOperation: &AIWorkloadOperation{
			Type:          OperationTypeUpgrade,
			Nonce:         "n1",
			TargetVersion: "1.3.0",
			RetryEpoch:    0,
			RequestedAt:   metav1.Now(),
			IntentDigest:  "sha256:intent",
			State:         OperationStateInProgress,
		},
		RenderBaselines: []RenderBaseline{{
			HelmOpUID:           "uid-1",
			RenderDigest:        "sha256:def",
			RetryEpoch:          2,
			HelmOpGeneration:    4,
			AcceptedFingerprint: "sha256:cond",
		}},
	}
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out AIWorkloadStatus
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.DeployedSource == nil || out.DeployedSource.Version != "1.2.0" {
		t.Errorf("deployedSource round-trip failed: %s", b)
	}
	if len(out.ComponentStatuses) != 1 || out.ComponentStatuses[0].ComponentName != "open-webui" {
		t.Errorf("componentStatuses round-trip failed: %s", b)
	}
	if out.ActiveOperation == nil || out.ActiveOperation.State != OperationStateInProgress {
		t.Errorf("activeOperation round-trip failed: %s", b)
	}
	if len(out.RenderBaselines) != 1 || out.RenderBaselines[0].HelmOpUID != "uid-1" {
		t.Errorf("renderBaselines round-trip failed: %s", b)
	}
}

func TestAIWorkloadStatusRecoveryFieldsOmitEmpty(t *testing.T) {
	b, _ := json.Marshal(AIWorkloadStatus{})
	for _, k := range []string{"deployedSource", "componentStatuses", "activeOperation", "renderBaselines"} {
		if jsonHasKey(b, k) {
			t.Errorf("expected %q omitted when empty, got %s", k, b)
		}
	}
}
