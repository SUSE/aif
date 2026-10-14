package aiworkload

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func bundleDeploymentOn(name, namespace, fleetCluster, fleetClusterNamespace, bundleName string) *unstructured.Unstructured {
	o := &unstructured.Unstructured{Object: map[string]any{}}
	o.SetGroupVersionKind(bundleDeploymentGVK)
	o.SetName(name)
	o.SetNamespace(namespace)
	o.SetLabels(map[string]string{
		"fleet.cattle.io/bundle-name":       bundleName,
		"fleet.cattle.io/cluster":           fleetCluster,
		"fleet.cattle.io/cluster-namespace": fleetClusterNamespace,
	})
	_ = unstructured.SetNestedField(o.Object, "Ready", "status", "display", "state")
	return o
}

// App-sourced workloads report the same management cluster ID that
// spec.targetClusters uses, not the Fleet cluster name.
func TestMirrorFleetStatus_ReportsManagementClusterID(t *testing.T) {
	ctx := context.Background()
	cl := fakeClient(
		bundleDeploymentOn("app-bundle", "cluster-fleet-default-gpu-alpha-0123456789ab", "gpu-alpha", "fleet-default", "app-bundle"),
		newFleetCluster("gpu-alpha", "c-m-alpha"),
	)
	r := &AIWorkloadReconciler{Client: cl, Scheme: cl.Scheme()}
	w := &aiplatformv1alpha1.AIWorkload{
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			TargetClusters:   []string{"c-m-alpha"},
			FleetBundleNames: []string{"app-bundle"},
		},
	}

	if err := r.mirrorFleetStatus(ctx, w); err != nil {
		t.Fatalf("mirrorFleetStatus failed: %v", err)
	}
	if len(w.Status.ClusterStatuses) != 1 || w.Status.ClusterStatuses[0].ClusterID != "c-m-alpha" {
		t.Fatalf("cluster statuses = %+v, want one entry for c-m-alpha", w.Status.ClusterStatuses)
	}
}

func TestBundleDeploymentClusterID(t *testing.T) {
	ctx := context.Background()
	cl := fakeClient(
		newFleetCluster("gpu-alpha", "c-m-alpha"),
		newFleetCluster("unlabelled", ""),
	)
	r := &AIWorkloadReconciler{Client: cl, Scheme: cl.Scheme()}

	cases := []struct {
		desc, fleetCluster, fleetClusterNamespace, want string
	}{
		{"labelled Fleet Cluster resolves to its management ID", "gpu-alpha", "fleet-default", "c-m-alpha"},
		{"Fleet Cluster without the management label keeps the Fleet name", "unlabelled", "fleet-default", "unlabelled"},
		{"missing Fleet Cluster keeps the Fleet name", "gone", "fleet-default", "gone"},
		{"BundleDeployment without a cluster namespace keeps the Fleet name", "gpu-alpha", "", "gpu-alpha"},
		{"BundleDeployment without a cluster label has no ID", "", "fleet-default", ""},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			bd := bundleDeploymentOn("x", "ns", tc.fleetCluster, tc.fleetClusterNamespace, "b")
			got, err := r.bundleDeploymentClusterID(ctx, bd, map[string]string{})
			if err != nil {
				t.Fatalf("bundleDeploymentClusterID failed: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
