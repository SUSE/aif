package workload

import (
	"context"
	"errors"
	"testing"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/blueprint"
	"github.com/SUSE/aif/pkg/workload"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestBlueprintReader_GetForUpgrade_ActivePhase verifies that an Active-phase
// Blueprint is projected with Withdrawn=false (the adapter derives the
// boolean from status.phase so the upgrader stays free of the aifv1 phase
// enum).
func TestBlueprintReader_GetForUpgrade_ActivePhase(t *testing.T) {
	repo := blueprint.NewFakeRepository()
	repo.Seed(&aifv1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{Name: "rag.1.1.0"},
		Spec:       aifv1.BlueprintSpec{BlueprintName: "rag", Version: "1.1.0"},
		Status:     aifv1.BlueprintStatus{Phase: aifv1.BlueprintPhaseActive},
	})
	r := NewBlueprintReader(repo)

	view, err := r.GetForUpgrade(context.Background(), "rag.1.1.0")
	if err != nil {
		t.Fatalf("GetForUpgrade: %v", err)
	}
	if view.Name != "rag.1.1.0" || view.Lineage != "rag" || view.Withdrawn {
		t.Errorf("unexpected view: %+v", view)
	}
}

func TestBlueprintReader_GetForUpgrade_WithdrawnPhase(t *testing.T) {
	repo := blueprint.NewFakeRepository()
	repo.Seed(&aifv1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{Name: "rag.1.1.0"},
		Spec:       aifv1.BlueprintSpec{BlueprintName: "rag", Version: "1.1.0"},
		Status:     aifv1.BlueprintStatus{Phase: aifv1.BlueprintPhaseWithdrawn},
	})
	r := NewBlueprintReader(repo)

	view, err := r.GetForUpgrade(context.Background(), "rag.1.1.0")
	if err != nil {
		t.Fatalf("GetForUpgrade: %v", err)
	}
	if !view.Withdrawn {
		t.Errorf("expected Withdrawn=true for Withdrawn phase, got %+v", view)
	}
}

func TestBlueprintReader_GetForUpgrade_NotFoundTranslated(t *testing.T) {
	r := NewBlueprintReader(blueprint.NewFakeRepository())
	_, err := r.GetForUpgrade(context.Background(), "missing.1.0.0")
	if !errors.Is(err, workload.ErrBlueprintVersionNotFound) {
		t.Errorf("expected ErrBlueprintVersionNotFound, got %v", err)
	}
}
