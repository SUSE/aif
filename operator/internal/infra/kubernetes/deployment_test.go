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
	"context"
	"testing"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testNamespace   = "cattle-ui-plugin-system"
	testReleaseName = "aif-ui-server"
)

// deployment builds a Deployment labelled the way the chart labels it, so the
// instance-label lookup IsDeploymentReady performs is exercised too.
//
// One replica throughout: that is what the extension chart deploys, and it is
// the count that makes maxUnavailable round down to zero and keep the previous
// revision's pod Ready for the whole rollout — the case these tests exist for.
func deployment(name string, gen, observedGen int64, status appsv1.DeploymentStatus) *appsv1.Deployment {
	replicas := int32(1)
	status.ObservedGeneration = observedGen
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  testNamespace,
			Generation: gen,
			Labels: map[string]string{
				"app.kubernetes.io/instance": testReleaseName,
			},
		},
		Spec:   appsv1.DeploymentSpec{Replicas: &replicas},
		Status: status,
	}
}

// TestIsDeploymentReadyDuringRollout pins the readiness contract the CR's
// DeploymentReady condition rests on.
//
// These cases are the rollout states a Helm upgrade passes through. They matter
// because the operator no longer asks Helm to wait (helm.upgrade sets
// Wait=false): this function is now the only thing standing between "the
// manifest was applied" and the CR reporting Installed. Every state below
// except the last one has a previous-revision pod still serving, so answering
// ready would advertise a version that is not running yet.
func TestIsDeploymentReadyDuringRollout(t *testing.T) {
	tests := []struct {
		name      string
		deploy    *appsv1.Deployment
		wantReady bool
	}{
		{
			// The apply has landed but the deployment controller has not acted on it,
			// so every count below still describes the previous revision.
			name: "status not yet observed for the applied generation is not ready",
			deploy: deployment("aif-ui-server", 2, 1, appsv1.DeploymentStatus{
				Replicas:          1,
				UpdatedReplicas:   1,
				ReadyReplicas:     1,
				AvailableReplicas: 1,
			}),
			wantReady: false,
		},
		{
			// maxUnavailable rounds to 0 at one replica, so the old pod stays Ready
			// for the whole rollout. ReadyReplicas alone cannot tell this apart from
			// a finished upgrade.
			name: "no updated pods yet while the old pod is still ready is not ready",
			deploy: deployment("aif-ui-server", 2, 2, appsv1.DeploymentStatus{
				Replicas:          1,
				UpdatedReplicas:   0,
				ReadyReplicas:     1,
				AvailableReplicas: 1,
			}),
			wantReady: false,
		},
		{
			name: "surged new pod not ready yet is not ready",
			deploy: deployment("aif-ui-server", 2, 2, appsv1.DeploymentStatus{
				Replicas:          2,
				UpdatedReplicas:   1,
				ReadyReplicas:     1,
				AvailableReplicas: 1,
			}),
			wantReady: false,
		},
		{
			// The new pod is up, but the old one has not gone away, so requests can
			// still be served by the previous revision.
			name: "old pod still terminating is not ready",
			deploy: deployment("aif-ui-server", 2, 2, appsv1.DeploymentStatus{
				Replicas:          2,
				UpdatedReplicas:   1,
				ReadyReplicas:     2,
				AvailableReplicas: 2,
			}),
			wantReady: false,
		},
		{
			name: "new pod updated but not ready is not ready",
			deploy: deployment("aif-ui-server", 2, 2, appsv1.DeploymentStatus{
				Replicas:          1,
				UpdatedReplicas:   1,
				ReadyReplicas:     0,
				AvailableReplicas: 0,
			}),
			wantReady: false,
		},
		{
			name: "rollout complete is ready",
			deploy: deployment("aif-ui-server", 2, 2, appsv1.DeploymentStatus{
				Replicas:          1,
				UpdatedReplicas:   1,
				ReadyReplicas:     1,
				AvailableReplicas: 1,
			}),
			wantReady: true,
		},
		{
			name: "fresh install with a ready pod is ready",
			deploy: deployment("aif-ui-server", 1, 1, appsv1.DeploymentStatus{
				Replicas:          1,
				UpdatedReplicas:   1,
				ReadyReplicas:     1,
				AvailableReplicas: 1,
			}),
			wantReady: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fake.NewClientBuilder().
				WithScheme(testScheme(t)).
				WithObjects(tt.deploy).
				Build()

			got, err := IsDeploymentReady(context.Background(), c, testNamespace, testReleaseName, logr.Discard())
			if err != nil {
				t.Fatalf("IsDeploymentReady() returned an unexpected error: %v", err)
			}
			if got.Ready != tt.wantReady {
				t.Errorf("IsDeploymentReady().Ready = %v, want %v (message: %q)",
					got.Ready, tt.wantReady, got.Message)
			}
			if !got.Ready && got.Message == "" {
				t.Error("IsDeploymentReady() reported not ready without a message explaining why")
			}
		})
	}
}

func TestIsDeploymentReadyNoDeployments(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()

	got, err := IsDeploymentReady(context.Background(), c, testNamespace, testReleaseName, logr.Discard())
	if err != nil {
		t.Fatalf("IsDeploymentReady() returned an unexpected error: %v", err)
	}
	if got.Ready {
		t.Error("IsDeploymentReady() = ready with no deployments for the release, want not ready")
	}
	// Ready alone cannot express this case: "none exist" and "one exists and is
	// still rolling out" are both not-ready, and only the caller knows which is a
	// failure. The git source installs a Rancher UI-plugin chart that may contain
	// no Deployment at all and is finished the moment Helm applies it; collapse the
	// two and that chart waits out the readiness bound and then reports Failed.
	if got.Found {
		t.Error("IsDeploymentReady() = found with no deployments for the release; a chart " +
			"that legitimately ships none is indistinguishable from one still rolling out")
	}
}

// The other half of Found: an existing Deployment reports found whether or not
// it is ready. Without this, hardwiring Found to false would satisfy the case
// above while making the flag useless — every release would look like it owns no
// workload, and the deployment-required policy would never wait for anything.
func TestIsDeploymentReadyReportsFoundWhileRollingOut(t *testing.T) {
	rolling := deployment(testReleaseName, 2, 2, appsv1.DeploymentStatus{
		Replicas: 1, UpdatedReplicas: 0, ReadyReplicas: 1, AvailableReplicas: 1,
	})
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(rolling).Build()

	got, err := IsDeploymentReady(context.Background(), c, testNamespace, testReleaseName, logr.Discard())
	if err != nil {
		t.Fatalf("IsDeploymentReady() returned an unexpected error: %v", err)
	}
	if got.Ready {
		t.Error("IsDeploymentReady() = ready mid-rollout, want not ready")
	}
	if !got.Found {
		t.Error("IsDeploymentReady() = not found for a Deployment that exists; the release " +
			"would be reported installed without ever waiting for it")
	}
}

// TestIsDeploymentReadyAllMustBeReady covers a chart that ships more than one
// Deployment: one lagging revision is enough to hold the release back.
func TestIsDeploymentReadyAllMustBeReady(t *testing.T) {
	done := deployment("aif-ui-server", 2, 2, appsv1.DeploymentStatus{
		Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1, AvailableReplicas: 1,
	})
	lagging := deployment("aif-ui-server-sidecar", 2, 2, appsv1.DeploymentStatus{
		Replicas: 1, UpdatedReplicas: 0, ReadyReplicas: 1, AvailableReplicas: 1,
	})

	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(done, lagging).Build()

	got, err := IsDeploymentReady(context.Background(), c, testNamespace, testReleaseName, logr.Discard())
	if err != nil {
		t.Fatalf("IsDeploymentReady() returned an unexpected error: %v", err)
	}
	if got.Ready {
		t.Error("IsDeploymentReady() = ready while one deployment is mid-rollout, want not ready")
	}
}

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := appsv1.AddToScheme(s); err != nil {
		t.Fatalf("failed to build scheme: %v", err)
	}
	return s
}
