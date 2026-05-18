package workload

import (
	"testing"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestWorkloadToDeployRequest_AppSource(t *testing.T) {
	w := &aifv1.Workload{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "wid"},
		Spec: aifv1.WorkloadSpec{
			Name: "my-llm",
			Source: aifv1.WorkloadSource{
				Kind: aifv1.WorkloadSourceKindApp,
				App:  &aifv1.AppRef{Repo: "oci://r", Chart: "c", Version: "1.0"},
			},
			ValueOverrides: map[string]string{"my-llm": "replicaCount: 5"},
		},
	}

	req := WorkloadToDeployRequest(w)

	if req.Namespace != "ns" || req.ID != "wid" || req.SpecName != "my-llm" {
		t.Errorf("got %+v, want ns/wid/my-llm", req)
	}
	if req.Source.Kind != SourceKindApp {
		t.Errorf("Source.Kind=%q, want App", req.Source.Kind)
	}
	if req.Source.App == nil || req.Source.App.Chart != "c" {
		t.Errorf("Source.App=%+v", req.Source.App)
	}
	if req.Replicas != 1 {
		t.Errorf("Replicas=%d, want default 1", req.Replicas)
	}
	if got := req.Overrides["my-llm"]; got != "replicaCount: 5" {
		t.Errorf("Overrides[my-llm]=%q", got)
	}
}

func TestWorkloadToDeployRequest_ReplicasOverride(t *testing.T) {
	r := int32(7)
	w := &aifv1.Workload{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "wid"},
		Spec: aifv1.WorkloadSpec{
			Name:     "n",
			Replicas: &r,
			Source:   aifv1.WorkloadSource{Kind: aifv1.WorkloadSourceKindApp, App: &aifv1.AppRef{Repo: "r", Chart: "c", Version: "1"}},
		},
	}
	if got := WorkloadToDeployRequest(w).Replicas; got != 7 {
		t.Errorf("Replicas=%d, want 7", got)
	}
}

func TestWorkloadToDeployRequest_BlueprintSource(t *testing.T) {
	w := &aifv1.Workload{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "wid"},
		Spec: aifv1.WorkloadSpec{
			Name: "n",
			Source: aifv1.WorkloadSource{
				Kind:      aifv1.WorkloadSourceKindBlueprint,
				Blueprint: &aifv1.BlueprintRef{Name: "rag", Version: "1.2.0"},
			},
		},
	}
	req := WorkloadToDeployRequest(w)
	if req.Source.Kind != SourceKindBlueprint || req.Source.Blueprint == nil ||
		req.Source.Blueprint.Name != "rag" || req.Source.Blueprint.Version != "1.2.0" {
		t.Errorf("Source=%+v", req.Source)
	}
}

func TestWorkloadToDeployRequest_BundleTestSource(t *testing.T) {
	w := &aifv1.Workload{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "wid"},
		Spec: aifv1.WorkloadSpec{
			Name: "n",
			Source: aifv1.WorkloadSource{
				Kind:       aifv1.WorkloadSourceKindBundleTest,
				BundleTest: &aifv1.BundleTestRef{Namespace: "ns2", Name: "b1", Generation: 5},
			},
		},
	}
	req := WorkloadToDeployRequest(w)
	if req.Source.BundleTest == nil || req.Source.BundleTest.Generation != 5 {
		t.Errorf("BundleTest=%+v", req.Source.BundleTest)
	}
}

func TestWorkloadToDeployRequest_PreviousFromStatus(t *testing.T) {
	w := &aifv1.Workload{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "wid"},
		Spec:       aifv1.WorkloadSpec{Name: "n", Source: aifv1.WorkloadSource{Kind: aifv1.WorkloadSourceKindApp, App: &aifv1.AppRef{Repo: "r", Chart: "c", Version: "1"}}},
		Status: aifv1.WorkloadStatus{
			ComponentReleases: []aifv1.ComponentReleaseStatus{
				{Name: "c1", ReleaseName: "wid-c1", Status: "deployed", Revision: 3},
			},
		},
	}
	req := WorkloadToDeployRequest(w)
	if len(req.Previous) != 1 || req.Previous[0].Name != "c1" || req.Previous[0].Revision != 3 {
		t.Errorf("Previous=%+v", req.Previous)
	}
}

func TestApplyDeployResult_WritesPhaseAndComponents(t *testing.T) {
	w := &aifv1.Workload{}
	r := DeployResult{
		Phase: PhaseRunning,
		Components: []ComponentRelease{
			{Name: "c1", ReleaseName: "wid-c1", ChartRef: "oci://x/c1:1", Status: "deployed", Revision: 2},
		},
		ObservedBundleGeneration: 7,
	}

	ApplyDeployResult(w, r)

	if w.Status.Phase != aifv1.WorkloadPhaseRunning {
		t.Errorf("Phase=%q", w.Status.Phase)
	}
	if len(w.Status.ComponentReleases) != 1 || w.Status.ComponentReleases[0].Status != "deployed" {
		t.Errorf("ComponentReleases=%+v", w.Status.ComponentReleases)
	}
	if w.Status.ObservedBundleGeneration != 7 {
		t.Errorf("ObservedBundleGeneration=%d", w.Status.ObservedBundleGeneration)
	}
}

func TestApplyDeployResult_PreservesUnrelatedStatusFields(t *testing.T) {
	w := &aifv1.Workload{Status: aifv1.WorkloadStatus{
		Replicas:      3,
		ReadyReplicas: 2,
		Conditions: []metav1.Condition{
			{Type: conditions.TypeReady, Status: metav1.ConditionTrue, Reason: "X"},
		},
	}}

	ApplyDeployResult(w, DeployResult{Phase: PhaseDeploying})

	if w.Status.Replicas != 3 || w.Status.ReadyReplicas != 2 {
		t.Errorf("replicas wiped: %d/%d", w.Status.Replicas, w.Status.ReadyReplicas)
	}
	if len(w.Status.Conditions) != 1 {
		t.Errorf("Conditions wiped: %+v", w.Status.Conditions)
	}
}
