package aiworkload

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// bundleDeploymentClusterID returns the Rancher management cluster ID a
// BundleDeployment was delivered to — the same ID spec.targetClusters holds.
//
// Fleet labels a BundleDeployment with the Fleet Cluster's metadata.name
// (fleet.cattle.io/cluster), which is not necessarily the management ID: a
// cluster registered under a display name gets a Fleet Cluster of that name,
// while its management ID is a generated "c-m-..." value. The management ID
// lives on the Fleet Cluster's management.cattle.io/cluster-name label, the
// same label workload targets select on. When the Fleet Cluster or its label
// is absent, the Fleet name is the best available ID and is returned as-is.
//
// Forbidden is treated like a missing Cluster: without read access the Fleet
// name is still a usable ID, and failing would stop status updates entirely
// for operators deployed with RBAC that predates this permission. Other
// errors are returned so a transient failure retries instead of reporting a
// different ID for one pass.
//
// resolved memoizes lookups by Fleet Cluster namespace/name across the
// BundleDeployments of one status pass.
func (r *AIWorkloadReconciler) bundleDeploymentClusterID(
	ctx context.Context,
	bd *unstructured.Unstructured,
	resolved map[string]string,
) (string, error) {
	labels := bd.GetLabels()
	name := labels["fleet.cattle.io/cluster"]
	namespace := labels["fleet.cattle.io/cluster-namespace"]
	if name == "" || namespace == "" {
		return name, nil
	}
	key := namespace + "/" + name
	if id, ok := resolved[key]; ok {
		return id, nil
	}

	id := name
	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(fleetClusterGVK)
	err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, cluster)
	switch {
	case apierrors.IsNotFound(err):
	case apierrors.IsForbidden(err):
		r.fleetClusterForbiddenOnce.Do(func() {
			log.FromContext(ctx).Info("cannot read Fleet Clusters; reporting Fleet cluster names instead of management cluster IDs",
				"error", err.Error())
		})
	case err != nil:
		return "", err
	default:
		if mgmtID := cluster.GetLabels()["management.cattle.io/cluster-name"]; mgmtID != "" {
			id = mgmtID
		}
	}
	resolved[key] = id
	return id, nil
}
