package workload

import (
	"context"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"k8s.io/apimachinery/pkg/labels"
)

// Repository is the K8s-backed CRUD port for Workload CRs.
// Methods are kept ≤4 (ISP) — counting queries live on DeploymentCounter.
type Repository interface {
	Get(ctx context.Context, namespace, name string) (*aifv1.Workload, error)
	List(ctx context.Context, namespace string, selector labels.Selector) ([]aifv1.Workload, error)
	Update(ctx context.Context, w *aifv1.Workload) error
	UpdateStatus(ctx context.Context, w *aifv1.Workload) error
}

// DeploymentCounter is the read-only port BlueprintReconciler uses to count
// Workloads sourced from a given Blueprint version. Splitting this off
// Repository (a) keeps Repository at ≤4 methods and (b) lets the
// implementation push the count to a label-indexed query instead of the
// cluster-wide List the reconciler does today.
//
// The K8s adapter is expected to label-index Workloads as they're created
// (blueprint-name=, blueprint-version=) so this query becomes O(matching),
// not O(all-workloads). That label work is part of plan task E1.
type DeploymentCounter interface {
	CountByBlueprint(ctx context.Context, name, version string) (int32, error)
}
