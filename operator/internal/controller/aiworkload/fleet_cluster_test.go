package aiworkload

import (
	"context"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// testMgmtID is the management cluster ID of the Fleet Cluster "gpu-alpha".
const testMgmtID = "c-m-alpha"

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
		newFleetCluster("gpu-alpha", testMgmtID),
	)
	r := &AIWorkloadReconciler{Client: cl, Scheme: cl.Scheme()}
	w := &aiplatformv1alpha1.AIWorkload{
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			TargetClusters:   []string{testMgmtID},
			FleetBundleNames: []string{"app-bundle"},
		},
	}

	if err := r.mirrorFleetStatus(ctx, w); err != nil {
		t.Fatalf("mirrorFleetStatus failed: %v", err)
	}
	if len(w.Status.ClusterStatuses) != 1 || w.Status.ClusterStatuses[0].ClusterID != testMgmtID {
		t.Fatalf("cluster statuses = %+v, want one entry for c-m-alpha", w.Status.ClusterStatuses)
	}
}

func TestBundleDeploymentClusterID(t *testing.T) {
	ctx := context.Background()
	cl := fakeClient(
		newFleetCluster("gpu-alpha", testMgmtID),
		newFleetCluster("unlabelled", ""),
	)
	r := &AIWorkloadReconciler{Client: cl, Scheme: cl.Scheme()}

	cases := []struct {
		desc, fleetCluster, fleetClusterNamespace, want string
	}{
		{"labelled Fleet Cluster resolves to its management ID", "gpu-alpha", "fleet-default", testMgmtID},
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

// countingClient serves objs from a fake store and counts Fleet Cluster Gets in
// *gets. A non-nil err is returned for every Fleet Cluster Get instead.
func countingClient(err error, gets *int, objs ...client.Object) client.Client {
	base := fakeClient(objs...)
	return interceptor.NewClient(base.(client.WithWatch), interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if obj.GetObjectKind().GroupVersionKind() == fleetClusterGVK {
				*gets++
				if err != nil {
					return err
				}
			}
			return c.Get(ctx, key, obj, opts...)
		},
	})
}

// A missing permission on Fleet Clusters must not stop status updates: the
// Fleet name is a usable ID, so fall back to it and remember the result for
// the rest of the pass.
func TestBundleDeploymentClusterID_ForbiddenFallsBackToFleetName(t *testing.T) {
	ctx := context.Background()
	gets := 0
	forbidden := apierrors.NewForbidden(schema.GroupResource{Group: "fleet.cattle.io", Resource: "clusters"}, "gpu-alpha", nil)
	r := &AIWorkloadReconciler{Client: countingClient(forbidden, &gets)}
	resolved := map[string]string{}

	for i := 0; i < 2; i++ {
		bd := bundleDeploymentOn("x", "ns", "gpu-alpha", "fleet-default", "b")
		got, err := r.bundleDeploymentClusterID(ctx, bd, resolved)
		if err != nil {
			t.Fatalf("call %d: Forbidden must fall back, got error: %v", i, err)
		}
		if got != "gpu-alpha" {
			t.Fatalf("call %d: got %q, want Fleet name %q", i, got, "gpu-alpha")
		}
	}
	if gets != 1 {
		t.Errorf("Fleet Cluster Gets = %d, want 1 (fallback must be memoized for the pass)", gets)
	}
}

// Transient errors are returned so the pass retries, rather than reporting the
// Fleet name for one pass and flipping the cell's cluster ID.
func TestBundleDeploymentClusterID_TransientErrorIsReturned(t *testing.T) {
	ctx := context.Background()
	gets := 0
	r := &AIWorkloadReconciler{Client: countingClient(apierrors.NewServiceUnavailable("etcd unavailable"), &gets)}

	bd := bundleDeploymentOn("x", "ns", "gpu-alpha", "fleet-default", "b")
	if _, err := r.bundleDeploymentClusterID(ctx, bd, map[string]string{}); err == nil {
		t.Fatal("transient error must be returned, got nil")
	}
}
