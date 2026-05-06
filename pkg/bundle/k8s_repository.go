package bundle

import (
	"context"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// k8sRepository implements Repository against a controller-runtime client.
// It is the only file in the bundle package allowed to import controller-runtime;
// keep it that way.
type k8sRepository struct {
	c client.Client
}

// NewK8sRepository returns a Repository backed by the given client.
func NewK8sRepository(c client.Client) Repository {
	return &k8sRepository{c: c}
}

func (r *k8sRepository) Get(ctx context.Context, namespace, name string) (*aifv1.Bundle, error) {
	var b aifv1.Bundle
	if err := r.c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *k8sRepository) List(ctx context.Context, namespace string, selector labels.Selector) ([]aifv1.Bundle, error) {
	var list aifv1.BundleList
	opts := []client.ListOption{client.InNamespace(namespace)}
	if selector != nil {
		opts = append(opts, client.MatchingLabelsSelector{Selector: selector})
	}
	if err := r.c.List(ctx, &list, opts...); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func (r *k8sRepository) Update(ctx context.Context, b *aifv1.Bundle) error {
	return r.c.Update(ctx, b)
}

func (r *k8sRepository) UpdateStatus(ctx context.Context, b *aifv1.Bundle) error {
	return r.c.Status().Update(ctx, b)
}
