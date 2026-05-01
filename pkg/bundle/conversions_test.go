package bundle

import (
	"testing"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBundleFromCR(t *testing.T) {
	cr := &aifv1.Bundle{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-ns",
			Name:      "test-bundle",
		},
		Spec: aifv1.BundleSpec{
			Title:           "Test Bundle",
			TargetBlueprint: "test-blueprint",
			UseCase:         "rag",
			Components: []aifv1.ComponentRef{
				{Name: "llm", Kind: aifv1.ComponentKindApp},
			},
			ValueOverrides: map[string]string{"llm": "replicas: 2"},
			Description:    "Test description",
			Authors:        []string{"Author 1"},
		},
		Status: aifv1.BundleStatus{
			Phase: aifv1.BundlePhaseDraft,
			Submission: &aifv1.SubmissionStatus{
				ProposedVersion: "1.0.0",
				SubmittedBy:     "user1",
			},
			PublishedVersions: []aifv1.PublishedVersionRef{
				{BlueprintName: "test", Version: "1.0.0"},
			},
		},
	}

	domain := BundleFromCR(cr)

	if domain.Namespace != "test-ns" {
		t.Errorf("expected namespace test-ns, got %s", domain.Namespace)
	}
	if domain.Name != "test-bundle" {
		t.Errorf("expected name test-bundle, got %s", domain.Name)
	}
	if domain.Phase != aifv1.BundlePhaseDraft {
		t.Errorf("expected phase Draft, got %s", domain.Phase)
	}
	if domain.TargetBlueprint != "test-blueprint" {
		t.Errorf("expected targetBlueprint test-blueprint, got %s", domain.TargetBlueprint)
	}
	if domain.UseCase != "rag" {
		t.Errorf("expected useCase rag, got %s", domain.UseCase)
	}
	if len(domain.Components) != 1 {
		t.Errorf("expected 1 component, got %d", len(domain.Components))
	}
	if domain.Submission == nil || domain.Submission.ProposedVersion != "1.0.0" {
		t.Error("expected submission to be copied")
	}
	if len(domain.PublishedVersions) != 1 {
		t.Errorf("expected 1 published version, got %d", len(domain.PublishedVersions))
	}
}

func TestBundleToCR(t *testing.T) {
	domain := Bundle{
		Namespace:       "test-ns",
		Name:            "test-bundle",
		Phase:           aifv1.BundlePhaseSubmitted,
		TargetBlueprint: "updated-blueprint",
		UseCase:         "vision",
		Components: []aifv1.ComponentRef{
			{Name: "vlm", Kind: aifv1.ComponentKindApp},
		},
		ValueOverrides: map[string]string{"vlm": "replicas: 3"},
		Description:    "Updated description",
		Authors:        []string{"Author 2"},
		Submission: &aifv1.SubmissionStatus{
			ProposedVersion: "2.0.0",
			SubmittedBy:     "user2",
		},
	}

	cr := &aifv1.Bundle{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       "test-ns",
			Name:            "test-bundle",
			ResourceVersion: "12345", // Should be preserved
		},
	}

	BundleToCR(domain, cr)

	// Verify metadata preserved
	if cr.ResourceVersion != "12345" {
		t.Errorf("expected ResourceVersion preserved, got %s", cr.ResourceVersion)
	}

	// Verify spec updated
	if cr.Spec.TargetBlueprint != "updated-blueprint" {
		t.Errorf("expected targetBlueprint updated, got %s", cr.Spec.TargetBlueprint)
	}
	if cr.Spec.UseCase != "vision" {
		t.Errorf("expected useCase vision, got %s", cr.Spec.UseCase)
	}
	if len(cr.Spec.Components) != 1 {
		t.Errorf("expected 1 component, got %d", len(cr.Spec.Components))
	}

	// Verify status updated
	if cr.Status.Phase != aifv1.BundlePhaseSubmitted {
		t.Errorf("expected phase Submitted, got %s", cr.Status.Phase)
	}
	if cr.Status.Submission == nil || cr.Status.Submission.ProposedVersion != "2.0.0" {
		t.Error("expected submission to be updated")
	}
}
