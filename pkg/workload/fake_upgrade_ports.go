package workload

import (
	"context"
	"fmt"
	"sync"
)

// FakeWorkloadStore is an in-memory implementation of the upgrader's
// workloadStore port. Domain-typed end to end — no aifv1, no apierrors —
// so unit tests for Upgrader stay framework-agnostic.
//
// The fake stores BlueprintRef-anchored views keyed by namespace/name.
// PatchBlueprintVersion mutates the stored Blueprint.Version in place and
// bumps ResourceVersion to detect concurrent-mutation tests. Error
// injection (GetErr, PatchErr) lets tests force the apierror-translated
// sentinels that the production adapter would emit.
type FakeWorkloadStore struct {
	mu    sync.Mutex
	items map[string]*UpgradeWorkloadView

	GetErr   error
	PatchErr error
}

// NewFakeWorkloadStore returns an empty fake.
func NewFakeWorkloadStore() *FakeWorkloadStore {
	return &FakeWorkloadStore{items: map[string]*UpgradeWorkloadView{}}
}

// Seed pre-populates the fake. The view is deep-copied so callers can
// mutate their input without surprising other tests.
func (f *FakeWorkloadStore) Seed(views ...*UpgradeWorkloadView) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, v := range views {
		f.items[viewKey(v.Namespace, v.Name)] = copyView(v)
	}
}

// GetUpgradeView returns a deep copy of the stored view. Returns
// ErrWorkloadNotFound (the same sentinel the production adapter emits) when
// the key is unknown, mirroring apierrors.IsNotFound at the K8s boundary.
func (f *FakeWorkloadStore) GetUpgradeView(_ context.Context, namespace, name string) (*UpgradeWorkloadView, error) {
	if f.GetErr != nil {
		return nil, f.GetErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.items[viewKey(namespace, name)]
	if !ok {
		return nil, fmt.Errorf("%w: %s/%s", ErrWorkloadNotFound, namespace, name)
	}
	return copyView(v), nil
}

// PatchBlueprintVersion bumps the stored view's Blueprint.Version when
// view.ResourceVersion matches the stored RV, otherwise returns
// ErrUpgradeConflict (mirroring apierrors.IsConflict at the K8s boundary).
// PatchErr takes precedence — tests use it to force non-conflict failures.
func (f *FakeWorkloadStore) PatchBlueprintVersion(_ context.Context, view *UpgradeWorkloadView, newVersion string) error {
	if f.PatchErr != nil {
		return f.PatchErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	stored, ok := f.items[viewKey(view.Namespace, view.Name)]
	if !ok {
		return fmt.Errorf("%w: %s/%s", ErrWorkloadNotFound, view.Namespace, view.Name)
	}
	if stored.ResourceVersion != view.ResourceVersion {
		return fmt.Errorf("%w: %s/%s", ErrUpgradeConflict, view.Namespace, view.Name)
	}
	if stored.Blueprint == nil {
		stored.Blueprint = &BlueprintRef{}
	}
	stored.Blueprint.Version = newVersion
	return nil
}

// FakeBlueprintReader is an in-memory implementation of the blueprintReader
// port. Same domain-typed pattern as FakeWorkloadStore.
type FakeBlueprintReader struct {
	mu    sync.Mutex
	items map[string]*UpgradeBlueprintView

	GetErr error
}

// NewFakeBlueprintReader returns an empty fake.
func NewFakeBlueprintReader() *FakeBlueprintReader {
	return &FakeBlueprintReader{items: map[string]*UpgradeBlueprintView{}}
}

// Seed pre-populates the fake.
func (f *FakeBlueprintReader) Seed(views ...*UpgradeBlueprintView) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, v := range views {
		copy := *v
		f.items[v.Name] = &copy
	}
}

// GetForUpgrade returns the stored view by name (the CR's metadata.name,
// e.g. "rag.1.1.0"). Returns ErrBlueprintVersionNotFound when missing.
func (f *FakeBlueprintReader) GetForUpgrade(_ context.Context, name string) (*UpgradeBlueprintView, error) {
	if f.GetErr != nil {
		return nil, f.GetErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.items[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrBlueprintVersionNotFound, name)
	}
	copy := *v
	return &copy, nil
}

func viewKey(ns, name string) string { return ns + "/" + name }

func copyView(v *UpgradeWorkloadView) *UpgradeWorkloadView {
	out := *v
	if v.Blueprint != nil {
		bp := *v.Blueprint
		out.Blueprint = &bp
	}
	return &out
}
