package settings

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

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
