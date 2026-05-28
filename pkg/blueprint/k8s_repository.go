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

// NewK8sRepository returns the concrete k8sRepository. Callers narrow
// to the interface they need via AsRepository / AsWrappedStore.
func NewK8sRepository(c client.Client) *k8sRepository { //nolint:revive
	return &k8sRepository{c: c}
}

func (r *k8sRepository) AsRepository() Repository             { return r }
func (r *k8sRepository) AsWrappedStore() WrappedBlueprintStore { return r }

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

// Create persists a new Blueprint CR. Blueprint is cluster-scoped, so the
// caller must set bp.Name to the canonical "{lineage}.{version}" form.
// Surfaces apierrors.IsAlreadyExists when the name collides — callers map
// that to HTTP 409.
//
// Not part of the Repository interface (ISP — Repository is already at 4
// methods serving the reconciler + wrapper). The HTTP handler declares a
// consumer-defined port that this method satisfies.
func (r *k8sRepository) Create(ctx context.Context, bp *aifv1.Blueprint) error {
	return r.c.Create(ctx, bp)
}

// Delete removes a Blueprint CR by name. Surfaces apierrors.IsNotFound when
// the name doesn't exist — callers map that to HTTP 404.
//
// Not part of the Repository interface; see Create above.
func (r *k8sRepository) Delete(ctx context.Context, name string) error {
	bp := &aifv1.Blueprint{}
	bp.Name = name
	return r.c.Delete(ctx, bp)
}

// FindByLineageVersion looks up a Blueprint CR by its lineage name + version
// using the canonical "{lineage}.{version}" object name. This is cheaper than
// a List+filter and avoids the label-index dependency, but assumes callers
// (POST handler today) keep CR.Name in that form.
//
// Returns apierrors.IsNotFound when no Blueprint with that name exists.
// Not part of the Repository interface; see Create above.
func (r *k8sRepository) FindByLineageVersion(ctx context.Context, lineage, version string) (*aifv1.Blueprint, error) {
	return r.Get(ctx, lineage+"."+version)
}

func (r *k8sRepository) ListWrapped(ctx context.Context) ([]Blueprint, error) {
	sel, err := labels.Parse("ai.suse.com/blueprint-source=" + LabelValueWrapsVendorChart)
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

// CreateWrapped persists a wrapped Blueprint (origin: WrapsVendorChart).
// See WrappedBlueprintStore for the naming rationale.
func (r *k8sRepository) CreateWrapped(ctx context.Context, b Blueprint) (bool, error) {
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
