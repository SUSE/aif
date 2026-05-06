package workload

import (
	"context"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// k8sRepository implements both Repository and DeploymentCounter against a
// controller-runtime client. Both ports are served by the same backing struct
// because they share a backing store; consumers depend on whichever interface
// they need.
type k8sRepository struct {
	c client.Client
}

// NewK8sRepository returns a value implementing both Repository and
// DeploymentCounter. The two interfaces are exposed via wrapper constructors
// below so callers can ask for the narrowest dependency they need.
func NewK8sRepository(c client.Client) *k8sRepository { //nolint:revive // intentional unexported return for narrow constructors
	return &k8sRepository{c: c}
}

// AsRepository returns r typed as Repository (the narrower interface).
func (r *k8sRepository) AsRepository() Repository { return r }

// AsDeploymentCounter returns r typed as DeploymentCounter.
func (r *k8sRepository) AsDeploymentCounter() DeploymentCounter { return r }

func (r *k8sRepository) Get(ctx context.Context, namespace, name string) (*aifv1.Workload, error) {
	var w aifv1.Workload
	if err := r.c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &w); err != nil {
		return nil, err
	}
	return &w, nil
}

func (r *k8sRepository) List(ctx context.Context, namespace string, selector labels.Selector) ([]aifv1.Workload, error) {
	var list aifv1.WorkloadList
	opts := []client.ListOption{client.InNamespace(namespace)}
	if selector != nil {
		opts = append(opts, client.MatchingLabelsSelector{Selector: selector})
	}
	if err := r.c.List(ctx, &list, opts...); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func (r *k8sRepository) Update(ctx context.Context, w *aifv1.Workload) error {
	return r.c.Update(ctx, w)
}

func (r *k8sRepository) UpdateStatus(ctx context.Context, w *aifv1.Workload) error {
	return r.c.Status().Update(ctx, w)
}

// CountByBlueprint counts Workloads whose source.kind is Blueprint and whose
// source.blueprint matches (name, version). Today this is implemented as a
// cluster-wide List + filter; plan task E1 replaces it with a label-selector
// query once Workloads are labelled at create time.
func (r *k8sRepository) CountByBlueprint(ctx context.Context, name, version string) (int32, error) {
	var list aifv1.WorkloadList
	if err := r.c.List(ctx, &list); err != nil {
		return 0, err
	}
	var count int32
	for i := range list.Items {
		w := &list.Items[i]
		if w.Spec.Source.Kind != aifv1.WorkloadSourceKindBlueprint {
			continue
		}
		if w.Spec.Source.Blueprint == nil {
			continue
		}
		if w.Spec.Source.Blueprint.Name == name && w.Spec.Source.Blueprint.Version == version {
			count++
		}
	}
	return count, nil
}
