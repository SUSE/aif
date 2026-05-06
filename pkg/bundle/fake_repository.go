package bundle

import (
	"context"
	"fmt"
	"sync"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// FakeRepository is an in-memory implementation of Repository suitable for
// unit tests. The internal sync.RWMutex makes Get/List/Update/UpdateStatus
// safe to call from multiple goroutines. The error-injection fields
// (GetErr, ListErr, …) are NOT mutex-guarded — set them from the test
// goroutine before kicking off any concurrent work.
type FakeRepository struct {
	mu    sync.RWMutex
	items map[string]*aifv1.Bundle

	// Optional error injection for tests. When set, the corresponding method
	// returns this error instead of executing.
	GetErr          error
	ListErr         error
	UpdateErr       error
	UpdateStatusErr error
}

// NewFakeRepository returns an empty FakeRepository.
func NewFakeRepository() *FakeRepository {
	return &FakeRepository{items: map[string]*aifv1.Bundle{}}
}

// Seed pre-populates the fake. Useful in test setup.
func (f *FakeRepository) Seed(bundles ...*aifv1.Bundle) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, b := range bundles {
		f.items[key(b.Namespace, b.Name)] = b.DeepCopy()
	}
}

func (f *FakeRepository) Get(_ context.Context, namespace, name string) (*aifv1.Bundle, error) {
	if f.GetErr != nil {
		return nil, f.GetErr
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	b, ok := f.items[key(namespace, name)]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: "ai.suse.com", Resource: "bundles"}, name)
	}
	return b.DeepCopy(), nil
}

func (f *FakeRepository) List(_ context.Context, namespace string, selector labels.Selector) ([]aifv1.Bundle, error) {
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]aifv1.Bundle, 0, len(f.items))
	for _, b := range f.items {
		if namespace != "" && b.Namespace != namespace {
			continue
		}
		if selector != nil && !selector.Matches(labels.Set(b.Labels)) {
			continue
		}
		out = append(out, *b.DeepCopy())
	}
	return out, nil
}

func (f *FakeRepository) Update(_ context.Context, b *aifv1.Bundle) error {
	if f.UpdateErr != nil {
		return f.UpdateErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items[key(b.Namespace, b.Name)] = b.DeepCopy()
	return nil
}

func (f *FakeRepository) UpdateStatus(_ context.Context, b *aifv1.Bundle) error {
	if f.UpdateStatusErr != nil {
		return f.UpdateStatusErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.items[key(b.Namespace, b.Name)]
	if !ok {
		return apierrors.NewNotFound(schema.GroupResource{Group: "ai.suse.com", Resource: "bundles"}, b.Name)
	}
	existing.Status = *b.Status.DeepCopy()
	return nil
}

func key(ns, name string) string { return fmt.Sprintf("%s/%s", ns, name) }
