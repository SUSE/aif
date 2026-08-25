package api

import (
	"errors"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func spec(strategy aiplatformv1alpha1.AIWorkloadDeployStrategy, clusters ...string) aiplatformv1alpha1.AIWorkloadSpec {
	return aiplatformv1alpha1.AIWorkloadSpec{
		DisplayName:     "w",
		TargetNamespace: "ai",
		DeployStrategy:  strategy,
		TargetClusters:  clusters,
		Source: aiplatformv1alpha1.AIWorkloadSource{
			SourceType: aiplatformv1alpha1.AIWorkloadSourceBlueprint,
			Blueprint:  &aiplatformv1alpha1.BlueprintSource{Name: "rag", Version: "1.0.0"},
		},
	}
}

func TestValidateAIWorkloadSpec(t *testing.T) {
	cases := []struct {
		name     string
		in       aiplatformv1alpha1.AIWorkloadSpec
		existing *aiplatformv1alpha1.AIWorkload
		wantErr  bool
	}{
		{"blueprint no targets", spec(aiplatformv1alpha1.AIWorkloadDeployFleetBundle), nil, true},
		{"blueprint one target", spec(aiplatformv1alpha1.AIWorkloadDeployFleetBundle, "local"), nil, false},
		{"mixed gitops", spec(aiplatformv1alpha1.AIWorkloadDeployGitOps, "local", "c-x"), nil, true},
		{"local-only gitops", spec(aiplatformv1alpha1.AIWorkloadDeployGitOps, "local"), nil, false},
		{"downstream-only gitops", spec(aiplatformv1alpha1.AIWorkloadDeployGitOps, "c-a", "c-b"), nil, false},
		{"mixed fleetbundle ok", spec(aiplatformv1alpha1.AIWorkloadDeployFleetBundle, "local", "c-x"), nil, false},
		{
			"strategy change rejected",
			spec(aiplatformv1alpha1.AIWorkloadDeployGitOps, "c-a"),
			&aiplatformv1alpha1.AIWorkload{Spec: spec(aiplatformv1alpha1.AIWorkloadDeployFleetBundle, "c-a")},
			true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateAIWorkloadSpec(c.in, c.existing)
			if c.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if c.wantErr && err != nil && !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("error should wrap ErrInvalidInput, got %v", err)
			}
		})
	}
}
