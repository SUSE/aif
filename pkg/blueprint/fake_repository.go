package blueprint

import (
	"context"
	"sync"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// FakeRepository is an in-memory implementation of Repository for unit tests.
// Blueprint is cluster-scoped, so the keying is by name only. The internal
// sync.RWMutex makes Get/List/Update/UpdateStatus safe to call from multiple
// goroutines; the error-injection fields (GetErr, ListErr, …) are NOT
// mutex-guarded — set them from the test goroutine before kicking off any
// concurrent work.
type FakeRepository struct {
	mu    sync.RWMutex
	items map[string]*aifv1.Blueprint

	GetErr          error
	ListErr         error
	UpdateErr       error
	UpdateStatusErr error
	CreateErr       error
	WithdrawErr     error
	ListWrappedErr  error
	// CreateCRErr injects errors into the typed-CR Create path used by the
	// HTTP handler (distinct from the wrapper-store Create above which takes
	// a domain Blueprint).
	CreateCRErr error
	DeleteErr   error
}

// NewFakeRepository returns an empty FakeRepository.
func NewFakeRepository() *FakeRepository {
	return &FakeRepository{items: map[string]*aifv1.Blueprint{}}
}

// Seed pre-populates the fake.
func (f *FakeRepository) Seed(bps ...*aifv1.Blueprint) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, bp := range bps {
		f.items[bp.Name] = bp.DeepCopy()
	}
}

func (f *FakeRepository) Get(_ context.Context, name string) (*aifv1.Blueprint, error) {
	if f.GetErr != nil {
		return nil, f.GetErr
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	bp, ok := f.items[name]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: "ai.suse.com", Resource: "blueprints"}, name)
	}
	return bp.DeepCopy(), nil
}

func (f *FakeRepository) List(_ context.Context, selector labels.Selector) ([]aifv1.Blueprint, error) {
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]aifv1.Blueprint, 0, len(f.items))
	for _, bp := range f.items {
		if selector != nil && !selector.Matches(labels.Set(bp.Labels)) {
			continue
		}
		out = append(out, *bp.DeepCopy())
	}
	return out, nil
}

func (f *FakeRepository) Update(_ context.Context, bp *aifv1.Blueprint) error {
	if f.UpdateErr != nil {
		return f.UpdateErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items[bp.Name] = bp.DeepCopy()
	return nil
}

func (f *FakeRepository) UpdateStatus(_ context.Context, bp *aifv1.Blueprint) error {
	if f.UpdateStatusErr != nil {
		return f.UpdateStatusErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.items[bp.Name]
	if !ok {
		return apierrors.NewNotFound(schema.GroupResource{Group: "ai.suse.com", Resource: "blueprints"}, bp.Name)
	}
	existing.Status = *bp.Status.DeepCopy()
	return nil
}

func (f *FakeRepository) ListWrapped(_ context.Context) ([]Blueprint, error) {
	if f.ListWrappedErr != nil {
		return nil, f.ListWrappedErr
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	var out []Blueprint
	for _, bp := range f.items {
		if bp.Labels != nil && bp.Labels["ai.suse.com/blueprint-source"] == LabelValueWrapsVendorChart {
			out = append(out, FromCR(bp))
		}
	}
	return out, nil
}

func (f *FakeRepository) CreateWrapped(_ context.Context, b Blueprint) (bool, error) {
	if f.CreateErr != nil {
		return false, f.CreateErr
	}
	cr := ToWrappedCR(b)
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.items[cr.Name]; exists {
		return false, nil
	}
	f.items[cr.Name] = cr
	return true, nil
}

// Create persists a Blueprint CR by name. Returns apierrors.IsAlreadyExists
// on collision so HTTP handler callers can map to 409. Mirror of
// k8sRepository.Create.
func (f *FakeRepository) Create(_ context.Context, bp *aifv1.Blueprint) error {
	if f.CreateCRErr != nil {
		return f.CreateCRErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.items[bp.Name]; exists {
		return apierrors.NewAlreadyExists(schema.GroupResource{Group: "ai.suse.com", Resource: "blueprints"}, bp.Name)
	}
	f.items[bp.Name] = bp.DeepCopy()
	return nil
}

// Delete removes a Blueprint CR by name. Returns apierrors.IsNotFound when
// the name is absent. Mirror of k8sRepository.Delete.
func (f *FakeRepository) Delete(_ context.Context, name string) error {
	if f.DeleteErr != nil {
		return f.DeleteErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.items[name]; !ok {
		return apierrors.NewNotFound(schema.GroupResource{Group: "ai.suse.com", Resource: "blueprints"}, name)
	}
	delete(f.items, name)
	return nil
}

// FindByLineageVersion looks up a Blueprint by "{lineage}.{version}" name.
// Mirror of k8sRepository.FindByLineageVersion.
func (f *FakeRepository) FindByLineageVersion(ctx context.Context, lineage, version string) (*aifv1.Blueprint, error) {
	return f.Get(ctx, lineage+"."+version)
}

func (f *FakeRepository) Withdraw(_ context.Context, name string) error {
	if f.WithdrawErr != nil {
		return f.WithdrawErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	bp, ok := f.items[name]
	if !ok {
		return apierrors.NewNotFound(schema.GroupResource{Group: "ai.suse.com", Resource: "blueprints"}, name)
	}
	bp.Status.Phase = aifv1.BlueprintPhaseWithdrawn
	bp.Status.Deprecation = &aifv1.DeprecationStatus{
		Reason:     "Vendor chart no longer present in catalog",
		ActionedBy: "aif-system",
		ActionedAt: metav1.Now(),
	}
	return nil
}
