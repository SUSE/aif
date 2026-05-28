package blueprint

import (
	"context"
	"testing"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFakeRepository_ImplementsWrappedBlueprintStore(t *testing.T) {
	var _ WrappedBlueprintStore = NewFakeRepository()
}

func TestFakeRepository_CreateAndGet(t *testing.T) {
	f := NewFakeRepository()
	bp := &aifv1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{Name: "rag.1.0.0"},
		Spec:       aifv1.BlueprintSpec{BlueprintName: "rag", Version: "1.0.0"},
	}
	if err := f.Create(context.Background(), bp); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := f.Get(context.Background(), "rag.1.0.0")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Spec.BlueprintName != "rag" {
		t.Errorf("got lineage %q, want rag", got.Spec.BlueprintName)
	}
}

func TestFakeRepository_CreateAlreadyExists(t *testing.T) {
	f := NewFakeRepository()
	bp := &aifv1.Blueprint{ObjectMeta: metav1.ObjectMeta{Name: "rag.1.0.0"}}
	if err := f.Create(context.Background(), bp); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	err := f.Create(context.Background(), bp)
	if !apierrors.IsAlreadyExists(err) {
		t.Errorf("second Create error = %v, want IsAlreadyExists", err)
	}
}

func TestFakeRepository_Delete(t *testing.T) {
	f := NewFakeRepository()
	f.Seed(&aifv1.Blueprint{ObjectMeta: metav1.ObjectMeta{Name: "rag.1.0.0"}})
	if err := f.Delete(context.Background(), "rag.1.0.0"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := f.Get(context.Background(), "rag.1.0.0")
	if !apierrors.IsNotFound(err) {
		t.Errorf("Get after Delete error = %v, want IsNotFound", err)
	}
}

func TestFakeRepository_DeleteNotFound(t *testing.T) {
	f := NewFakeRepository()
	err := f.Delete(context.Background(), "missing.1.0.0")
	if !apierrors.IsNotFound(err) {
		t.Errorf("Delete missing error = %v, want IsNotFound", err)
	}
}

func TestFakeRepository_FindByLineageVersion(t *testing.T) {
	f := NewFakeRepository()
	f.Seed(&aifv1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{Name: "rag.1.0.0"},
		Spec:       aifv1.BlueprintSpec{BlueprintName: "rag", Version: "1.0.0"},
	})
	got, err := f.FindByLineageVersion(context.Background(), "rag", "1.0.0")
	if err != nil {
		t.Fatalf("FindByLineageVersion: %v", err)
	}
	if got.Name != "rag.1.0.0" {
		t.Errorf("got name %q, want rag.1.0.0", got.Name)
	}
}
