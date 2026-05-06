package blueprint

import (
	"context"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// k8sRepository implements Repository against a controller-runtime client.
type k8sRepository struct {
	c client.Client
}

// NewK8sRepository returns a Repository backed by the given client.
func NewK8sRepository(c client.Client) Repository {
	return &k8sRepository{c: c}
}

func (r *k8sRepository) Get(ctx context.Context, name string) (*aifv1.Blueprint, error) {
	var bp aifv1.Blueprint
	if err := r.c.Get(ctx, client.ObjectKey{Name: name}, &bp); err != nil {
		return nil, err
	}
	return &bp, nil
}

func (r *k8sRepository) List(ctx context.Context, selector labels.Selector) ([]aifv1.Blueprint, error) {
	var list aifv1.BlueprintList
	var opts []client.ListOption
	if selector != nil {
		opts = append(opts, client.MatchingLabelsSelector{Selector: selector})
	}
	if err := r.c.List(ctx, &list, opts...); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func (r *k8sRepository) Update(ctx context.Context, bp *aifv1.Blueprint) error {
	return r.c.Update(ctx, bp)
}

func (r *k8sRepository) UpdateStatus(ctx context.Context, bp *aifv1.Blueprint) error {
	return r.c.Status().Update(ctx, bp)
}
