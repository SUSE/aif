package workload

import (
	"context"
	"fmt"
	"testing"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFakeRepository_Patch_HappyPath(t *testing.T) {
	f := NewFakeRepository()
	w := &aifv1.Workload{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "wl", ResourceVersion: "1"},
		Spec: aifv1.WorkloadSpec{
			Source: aifv1.WorkloadSource{
				Kind:      aifv1.WorkloadSourceKindBlueprint,
				Blueprint: &aifv1.BlueprintRef{Name: "rag", Version: "1.0.0"},
			},
		},
	}
	f.Seed(w)

	orig, err := f.Get(context.Background(), "ns", "wl")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	mutated := orig.DeepCopy()
	mutated.Spec.Source.Blueprint.Version = "1.1.0"

	if err := f.Patch(context.Background(), mutated, orig); err != nil {
		t.Fatalf("Patch: %v", err)
	}

	got, _ := f.Get(context.Background(), "ns", "wl")
	if got.Spec.Source.Blueprint.Version != "1.1.0" {
		t.Errorf("expected version 1.1.0, got %s", got.Spec.Source.Blueprint.Version)
	}
}

func TestFakeRepository_Patch_Conflict(t *testing.T) {
	f := NewFakeRepository()
	w := &aifv1.Workload{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "wl", ResourceVersion: "1"},
		Spec: aifv1.WorkloadSpec{
			Source: aifv1.WorkloadSource{
				Kind:      aifv1.WorkloadSourceKindBlueprint,
				Blueprint: &aifv1.BlueprintRef{Name: "rag", Version: "1.0.0"},
			},
		},
	}
	f.Seed(w)

	// Snapshot at RV=1
	orig, _ := f.Get(context.Background(), "ns", "wl")

	// Simulate a concurrent writer bumping the stored RV.
	concurrent := orig.DeepCopy()
	concurrent.ResourceVersion = "2"
	f.Seed(concurrent)

	mutated := orig.DeepCopy()
	mutated.Spec.Source.Blueprint.Version = "1.1.0"

	err := f.Patch(context.Background(), mutated, orig)
	if err == nil || !apierrors.IsConflict(err) {
		t.Fatalf("expected apierrors.IsConflict, got %v", err)
	}
}

func TestFakeRepository_Create(t *testing.T) {
	f := NewFakeRepository()
	w := &aifv1.Workload{}
	w.Namespace = "ns"
	w.Name = "wl"
	w.Spec.Source.Kind = aifv1.WorkloadSourceKindApp

	if err := f.Create(context.Background(), w); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := f.Get(context.Background(), "ns", "wl")
	if err != nil {
		t.Fatalf("Get after Create: %v", err)
	}
	if got.Spec.Source.Kind != aifv1.WorkloadSourceKindApp {
		t.Errorf("source kind = %v, want App", got.Spec.Source.Kind)
	}
}

func TestFakeRepository_Create_ErrorInjection(t *testing.T) {
	f := NewFakeRepository()
	f.CreateErr = fmt.Errorf("injected")
	w := &aifv1.Workload{}
	w.Namespace = "ns"
	w.Name = "wl"

	if err := f.Create(context.Background(), w); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFakeRepository_Delete(t *testing.T) {
	f := NewFakeRepository()
	w := &aifv1.Workload{}
	w.Namespace = "ns"
	w.Name = "wl"
	f.Seed(w)

	if err := f.Delete(context.Background(), "ns", "wl"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := f.Get(context.Background(), "ns", "wl"); err == nil {
		t.Fatal("expected NotFound after Delete, got nil")
	}
}

func TestFakeRepository_Delete_NotFound(t *testing.T) {
	f := NewFakeRepository()
	err := f.Delete(context.Background(), "ns", "missing")
	if err == nil {
		t.Fatal("expected error for missing workload, got nil")
	}
	if !apierrors.IsNotFound(err) {
		t.Errorf("expected NotFound error, got %v", err)
	}
}

func TestFakeRepository_Delete_ErrorInjection(t *testing.T) {
	f := NewFakeRepository()
	w := &aifv1.Workload{}
	w.Namespace = "ns"
	w.Name = "wl"
	f.Seed(w)
	f.DeleteErr = fmt.Errorf("injected delete error")

	if err := f.Delete(context.Background(), "ns", "wl"); err == nil {
		t.Fatal("expected error, got nil")
	}
}
