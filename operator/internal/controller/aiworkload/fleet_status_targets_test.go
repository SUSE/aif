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
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func statusBundleDeployment(name, namespace, bundleName, clusterID string) *unstructured.Unstructured {
	deployment := &unstructured.Unstructured{}
	deployment.SetGroupVersionKind(bundleDeploymentGVK)
	deployment.SetName(name)
	deployment.SetNamespace(namespace)
	deployment.SetLabels(map[string]string{
		"fleet.cattle.io/bundle-name": bundleName,
		"fleet.cattle.io/cluster":     clusterID,
	})
	_ = unstructured.SetNestedMap(deployment.Object, map[string]any{
		"state": "Ready",
	}, "status", "display")
	return deployment
}

func requireClusterStatus(t *testing.T, workload *aiplatformv1alpha1.AIWorkload, index int, clusterID string, phase aiplatformv1alpha1.AIWorkloadClusterPhase) {
	t.Helper()
	if len(workload.Status.ClusterStatuses) <= index {
		t.Fatalf("cluster statuses = %+v", workload.Status.ClusterStatuses)
	}
	status := workload.Status.ClusterStatuses[index]
	if status.ClusterID != clusterID || status.Phase != phase {
		t.Fatalf("cluster status %d = %+v, want %s/%s", index, status, clusterID, phase)
	}
}

func TestMirrorFleetStatusRequiresEveryRequestedCluster(t *testing.T) {
	const bundleName = "workload-app"
	downstream := statusBundleDeployment("downstream", "cluster-c-downstream", bundleName, "c-downstream")
	stale := statusBundleDeployment("stale", "cluster-stale", bundleName, "c-stale")
	scheme := gitRepoTestScheme()
	reconciler := &AIWorkloadReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(downstream, stale).Build(),
	}
	workload := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "workload", Namespace: "aif-operator"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			TargetClusters:   []string{"local", "c-downstream"},
			FleetBundleNames: []string{bundleName},
		},
	}

	if err := reconciler.mirrorFleetStatus(context.Background(), workload); err != nil {
		t.Fatalf("mirror Fleet status: %v", err)
	}
	requireClusterStatus(t, workload, 0, "local", aiplatformv1alpha1.AIWorkloadClusterPhasePending)
	requireClusterStatus(t, workload, 1, "c-downstream", aiplatformv1alpha1.AIWorkloadClusterPhaseRunning)
	if workload.Status.Phase != aiplatformv1alpha1.AIWorkloadPhaseDegraded {
		t.Fatalf("workload phase = %s, want Degraded", workload.Status.Phase)
	}
}
