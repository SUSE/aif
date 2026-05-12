package blueprint

import (
	"context"
	"fmt"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// NewK8sWrappedStore returns a WrappedBlueprintStore backed by the given client.
func NewK8sWrappedStore(c client.Client) WrappedBlueprintStore {
	return &k8sRepository{c: c}
}

func (r *k8sRepository) ListWrapped(ctx context.Context) ([]Blueprint, error) {
	sel, err := labels.Parse("ai.suse.com/blueprint-source=wraps-vendor-chart")
	if err != nil {
		return nil, fmt.Errorf("parsing label selector: %w", err)
	}
	crs, err := r.List(ctx, sel)
	if err != nil {
		return nil, err
	}
	out := make([]Blueprint, len(crs))
	for i := range crs {
		out[i] = FromCR(&crs[i])
	}
	return out, nil
}

func (r *k8sRepository) Create(ctx context.Context, b Blueprint) (bool, error) {
	cr := ToWrappedCR(b)
	if err := r.c.Create(ctx, cr); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *k8sRepository) Withdraw(ctx context.Context, name string) error {
	bp, err := r.Get(ctx, name)
	if err != nil {
		return err
	}
	bp.Status.Phase = aifv1.BlueprintPhaseWithdrawn
	bp.Status.Deprecation = &aifv1.DeprecationStatus{
		Reason:     "Vendor chart no longer present in catalog",
		ActionedBy: "aif-system",
		ActionedAt: metav1.Now(),
	}
	return r.c.Status().Update(ctx, bp)
}
