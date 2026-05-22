package workload

import (
	"context"
	"fmt"
	"sync"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// FakeRepository is an in-memory implementation of Repository and
// DeploymentCounter for unit tests. The internal sync.RWMutex makes
// Get/List/Update/UpdateStatus/CountByBlueprint safe to call from multiple
// goroutines; the error-injection fields (GetErr, ListErr, …) are NOT
// mutex-guarded — set them from the test goroutine before kicking off any
// concurrent work.
type FakeRepository struct {
	mu    sync.RWMutex
	items map[string]*aifv1.Workload

	GetErr              error
	ListErr             error
	UpdateErr           error
	UpdateStatusErr     error
	PatchErr            error
	CountByBlueprintErr error
}

// NewFakeRepository returns an empty FakeRepository.
func NewFakeRepository() *FakeRepository {
	return &FakeRepository{items: map[string]*aifv1.Workload{}}
}

// Seed pre-populates the fake.
func (f *FakeRepository) Seed(workloads ...*aifv1.Workload) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, w := range workloads {
		f.items[key(w.Namespace, w.Name)] = w.DeepCopy()
	}
}

func (f *FakeRepository) Get(_ context.Context, namespace, name string) (*aifv1.Workload, error) {
	if f.GetErr != nil {
		return nil, f.GetErr
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	w, ok := f.items[key(namespace, name)]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: "ai.suse.com", Resource: "workloads"}, name)
	}
	return w.DeepCopy(), nil
}

func (f *FakeRepository) List(_ context.Context, namespace string, selector labels.Selector) ([]aifv1.Workload, error) {
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]aifv1.Workload, 0, len(f.items))
	for _, w := range f.items {
		if namespace != "" && w.Namespace != namespace {
			continue
		}
		if selector != nil && !selector.Matches(labels.Set(w.Labels)) {
			continue
		}
		out = append(out, *w.DeepCopy())
	}
	return out, nil
}

func (f *FakeRepository) Update(_ context.Context, w *aifv1.Workload) error {
	if f.UpdateErr != nil {
		return f.UpdateErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items[key(w.Namespace, w.Name)] = w.DeepCopy()
	return nil
}

func (f *FakeRepository) UpdateStatus(_ context.Context, w *aifv1.Workload) error {
	if f.UpdateStatusErr != nil {
		return f.UpdateStatusErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.items[key(w.Namespace, w.Name)]
	if !ok {
		return apierrors.NewNotFound(schema.GroupResource{Group: "ai.suse.com", Resource: "workloads"}, w.Name)
	}
	existing.Status = *w.Status.DeepCopy()
	return nil
}

// Patch simulates optimistic concurrency: if orig.ResourceVersion differs
// from the stored item's ResourceVersion, return apierrors.NewConflict.
// Otherwise replace the stored item with w. The patch payload itself is
// NOT computed — for upgrade-test purposes, callers verify the spec change
// by reading back via Get. PatchErr takes precedence (test-injected error).
func (f *FakeRepository) Patch(_ context.Context, w, orig *aifv1.Workload) error {
	if f.PatchErr != nil {
		return f.PatchErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.items[key(w.Namespace, w.Name)]
	if !ok {
		return apierrors.NewNotFound(schema.GroupResource{Group: "ai.suse.com", Resource: "workloads"}, w.Name)
	}
	if existing.ResourceVersion != orig.ResourceVersion {
		return apierrors.NewConflict(
			schema.GroupResource{Group: "ai.suse.com", Resource: "workloads"},
			w.Name,
			fmt.Errorf("resourceVersion %q does not match stored %q", orig.ResourceVersion, existing.ResourceVersion),
		)
	}
	f.items[key(w.Namespace, w.Name)] = w.DeepCopy()
	return nil
}

func (f *FakeRepository) CountByBlueprint(_ context.Context, name, version string) (int32, error) {
	if f.CountByBlueprintErr != nil {
		return 0, f.CountByBlueprintErr
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	var count int32
	for _, w := range f.items {
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

func key(ns, name string) string { return fmt.Sprintf("%s/%s", ns, name) }
