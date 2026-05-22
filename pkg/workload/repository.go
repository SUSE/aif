package workload

import (
	"context"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"k8s.io/apimachinery/pkg/labels"
)

// Reader is the read-only K8s port for Workload CRs. It is a K8s adapter
// port — aifv1 imports are allowed here per the layering rule.
type Reader interface {
	Get(ctx context.Context, namespace, name string) (*aifv1.Workload, error)
	List(ctx context.Context, namespace string, selector labels.Selector) ([]aifv1.Workload, error)
}

// Writer is the mutation port for Workload CRs.
//
// Patch was added in P5-3 for the upgrade action: MergeFrom-based optimistic
// concurrency surfaces field-level conflicts as apierrors.IsConflict, which
// callers map to HTTP 409. Update remains for full-spec replacements.
type Writer interface {
	Update(ctx context.Context, w *aifv1.Workload) error
	UpdateStatus(ctx context.Context, w *aifv1.Workload) error
	Patch(ctx context.Context, w, orig *aifv1.Workload) error
}

// Repository is the union of Reader + Writer. Existing consumers (deployer,
// reconciler) depend on the union because their CRUD usage is wide. New
// consumers SHOULD depend on Reader or Writer directly, or define an even
// narrower consumer-defined port (see Upgrader's local workloadStore in
// upgrader.go for the pattern).
type Repository interface {
	Reader
	Writer
}

// DeploymentCounter is the read-only port BlueprintReconciler uses to count
// Workloads sourced from a given Blueprint version. Splitting this off
// Repository (a) keeps it under the ISP method budget and (b) lets the
// implementation push the count to a label-indexed query instead of the
// cluster-wide List the reconciler does today.
//
// The K8s adapter is expected to label-index Workloads as they're created
// (blueprint-name=, blueprint-version=) so this query becomes O(matching),
// not O(all-workloads). That label work is part of plan task E1.
type DeploymentCounter interface {
	CountByBlueprint(ctx context.Context, name, version string) (int32, error)
}
