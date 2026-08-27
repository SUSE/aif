package settings

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/SUSE/aif-operator/internal/credentials"
)

// TestProvenanceLabelLiterals pins the exact label strings the operator stamps.
// These same strings are hard-coded on the UI side (ui/pkg/aif-ui/services/
// app-collection.ts as MANAGED_REPO_LABEL / NVIDIA_TEAM_REPO_LABEL) and matched
// there with a strict equality gate. The two sides are tied only by convention,
// so a rename here would silently break discovery on the reader side. This test
// is the Go half of the drift pin; app-collection.test.ts carries the TS half.
// If you change a literal, change BOTH and update both pins together.
func TestProvenanceLabelLiterals(t *testing.T) {
	if credentials.ManagedRepoLabel != "ai-factory.suse.com/managed-repo" {
		t.Errorf("ManagedRepoLabel drifted: %q", credentials.ManagedRepoLabel)
	}
	if credentials.TeamRepoLabel != "ai-factory.suse.com/nvidia-team-repo" {
		t.Errorf("TeamRepoLabel drifted: %q", credentials.TeamRepoLabel)
	}
	if credentials.LabelValueTrue != "true" {
		t.Errorf("LabelValueTrue drifted: %q", credentials.LabelValueTrue)
	}
	// The settings-package aliases must track the credentials source of truth.
	if managedRepoMarkerLabel != credentials.ManagedRepoLabel {
		t.Errorf("managedRepoMarkerLabel alias drifted from credentials.ManagedRepoLabel")
	}
	if teamRepoMarkerLabel != credentials.TeamRepoLabel {
		t.Errorf("teamRepoMarkerLabel alias drifted from credentials.TeamRepoLabel")
	}
}

// TestApplyTeamClusterRepo_StampsBothLabels verifies that applyTeamClusterRepo
// stamps BOTH the team-repo marker and the managed-repo marker on a team repo.
func TestApplyTeamClusterRepo_StampsBothLabels(t *testing.T) {
	s := runtime.NewScheme()
	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo",
	}, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepoList",
	}, &unstructured.UnstructuredList{})

	c := fake.NewClientBuilder().WithScheme(s).Build()
	r := &SettingsReconciler{Client: c, Scheme: s}

	// Apply an anonymous team repo (no clientSecret).
	err := r.applyTeamClusterRepo(context.Background(), "nvidia-omniverse", "https://helm.ngc.nvidia.com/nvidia/omniverse", "")
	if err != nil {
		t.Fatalf("applyTeamClusterRepo: %v", err)
	}

	// Get the ClusterRepo and verify both labels are present.
	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo",
	})
	if err := c.Get(context.Background(), types.NamespacedName{Name: "nvidia-omniverse"}, repo); err != nil {
		t.Fatalf("get ClusterRepo: %v", err)
	}

	labels := repo.GetLabels()
	if labels[managedRepoMarkerLabel] != managedRepoMarkerValue {
		t.Errorf("team repo missing managed-repo label, got labels=%v", labels)
	}
	if labels[teamRepoMarkerLabel] != teamRepoMarkerValue {
		t.Errorf("team repo missing team-repo label, got labels=%v", labels)
	}
}
