package blueprint

import (
	"context"
	"sync"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
