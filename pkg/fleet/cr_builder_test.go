package fleet

import (
	"strings"
	"testing"

	fleetv1 "github.com/rancher/fleet/pkg/apis/fleet.cattle.io/v1alpha1"
)

func TestFleetBundleName_BasicShape(t *testing.T) {
	got := fleetBundleName("team-a", "demo-workload")
	want := "team-a-demo-workload"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFleetBundleName_LowercasesAndSanitizes(t *testing.T) {
	got := fleetBundleName("Team_A", "Demo.Workload")
	if got != "team-a-demo-workload" {
		t.Fatalf("expected sanitized lowercase, got %q", got)
	}
}

func TestFleetBundleName_TruncatesWithStableSuffix(t *testing.T) {
	longID := strings.Repeat("x", 80)
	got := fleetBundleName("ns", longID)
	if len(got) > 63 {
		t.Fatalf("length %d > 63: %q", len(got), got)
	}
	// Same input twice → identical output (suffix is stable hash).
	if fleetBundleName("ns", longID) != got {
		t.Fatal("fleetBundleName is not deterministic")
	}
}

func TestFleetBundleName_CollisionResistantAfterTruncation(t *testing.T) {
	// Two long names that share the first 55 chars but differ further out
	// MUST yield different bundle names.
	a := fleetBundleName("ns", strings.Repeat("a", 55)+"foo")
	b := fleetBundleName("ns", strings.Repeat("a", 55)+"bar")
	if a == b {
		t.Fatalf("collision: %q == %q", a, b)
	}
}

func TestBuildBundleCR_SingleComponent(t *testing.T) {
	spec := BundleDeploymentSpec{
		WorkloadID:     "demo",
		WorkloadNS:     "team-a",
		TargetClusters: []string{"prod-east", "prod-west"},
		Components: []ComponentBundle{{
			Name:     "llama",
			ChartRef: "oci://registry.example.test/ai/charts/nim-llm:1.2.3",
			Values:   map[string]any{"replicas": 2, "image": map[string]any{"repository": "nim-llm"}},
		}},
		Owner: OwnerRef{
			APIVersion: "ai.suse.com/v1alpha1",
			Kind:       "Workload",
			Name:       "demo",
			UID:        "u-1",
			Controller: true,
		},
	}
	b, err := buildBundleCR(spec)
	if err != nil {
		t.Fatalf("buildBundleCR: %v", err)
	}
	if b.Name != "team-a-demo" {
		t.Fatalf("Name = %q, want team-a-demo", b.Name)
	}
	if b.Namespace != "team-a" {
		t.Fatalf("Namespace = %q, want team-a", b.Namespace)
	}
	if got := len(b.Spec.Targets); got != 2 {
		t.Fatalf("Targets len = %d, want 2", got)
	}
	if b.Spec.Helm == nil {
		t.Fatal("Spec.Helm is nil")
	}
	if b.Spec.Helm.Chart != "oci://registry.example.test/ai/charts/nim-llm:1.2.3" {
		t.Fatalf("Spec.Helm.Chart = %q", b.Spec.Helm.Chart)
	}
	if len(b.Spec.Helm.ValuesFiles) != 0 {
		t.Fatalf("expected no extra valuesFiles for single component, got %d", len(b.Spec.Helm.ValuesFiles))
	}
	if len(b.Spec.Resources) != 0 {
		t.Fatalf("expected no resources without pull-secret, got %d", len(b.Spec.Resources))
	}
	if len(b.OwnerReferences) != 1 || b.OwnerReferences[0].UID != "u-1" {
		t.Fatalf("OwnerReferences not propagated: %+v", b.OwnerReferences)
	}
	// Avoid unused import lint
	_ = fleetv1.Bundle{}
}

func TestBuildBundleCR_MultiComponent(t *testing.T) {
	spec := BundleDeploymentSpec{
		WorkloadID:     "demo",
		WorkloadNS:     "team-a",
		TargetClusters: []string{"c1"},
		Components: []ComponentBundle{
			{Name: "llama", ChartRef: "oci://r/llama:1", Values: map[string]any{"r": 1}},
			{Name: "vllm", ChartRef: "oci://r/vllm:2", Values: map[string]any{"r": 2}},
			{Name: "ui", ChartRef: "oci://r/ui:3", Values: map[string]any{"r": 3}},
		},
		Owner: OwnerRef{APIVersion: "ai.suse.com/v1alpha1", Kind: "Workload", Name: "demo", UID: "u-1"},
	}
	b, err := buildBundleCR(spec)
	if err != nil {
		t.Fatal(err)
	}
	if b.Spec.Helm.Chart != "oci://r/llama:1" {
		t.Fatalf("first chart wins; got %q", b.Spec.Helm.Chart)
	}
	if len(b.Spec.Helm.ValuesFiles) != 2 {
		t.Fatalf("ValuesFiles len = %d, want 2", len(b.Spec.Helm.ValuesFiles))
	}
	if b.Spec.Helm.ValuesFiles[0] != "values/vllm.yaml" || b.Spec.Helm.ValuesFiles[1] != "values/ui.yaml" {
		t.Fatalf("ValuesFiles order/content wrong: %v", b.Spec.Helm.ValuesFiles)
	}
	if len(b.Spec.Resources) != 2 {
		t.Fatalf("Resources len = %d, want 2 (no pull-secret)", len(b.Spec.Resources))
	}
}

func TestBuildBundleCR_PullSecretEmbedded(t *testing.T) {
	spec := BundleDeploymentSpec{
		WorkloadID:     "demo",
		WorkloadNS:     "team-a",
		TargetClusters: []string{"c1"},
		Components:     []ComponentBundle{{Name: "x", ChartRef: "oci://r/x:1", Values: map[string]any{}}},
		PullSecretData: []byte(`{"auths":{"registry.suse.com":{"auth":"dXNlcjpwYXNz"}}}`),
		Owner:          OwnerRef{APIVersion: "ai.suse.com/v1alpha1", Kind: "Workload", Name: "demo", UID: "u-1"},
	}
	b, err := buildBundleCR(spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource (pull-secret), got %d", len(b.Spec.Resources))
	}
	r := b.Spec.Resources[0]
	if r.Name != "manifests/suse-registry-creds.yaml" {
		t.Fatalf("unexpected resource name: %q", r.Name)
	}
	if !strings.Contains(r.Content, "kind: Secret") {
		t.Fatalf("Secret manifest missing kind: %s", r.Content)
	}
	if !strings.Contains(r.Content, "suse-registry-creds") {
		t.Fatalf("Secret manifest missing name: %s", r.Content)
	}
}

func TestBuildBundleCR_Validation(t *testing.T) {
	bad := []BundleDeploymentSpec{
		{},
		{WorkloadID: "x"},
		{WorkloadID: "x", WorkloadNS: "n"},
		{WorkloadID: "x", WorkloadNS: "n", Components: []ComponentBundle{{}}},
		{WorkloadID: "x", WorkloadNS: "n", Components: []ComponentBundle{{Name: "c"}}, TargetClusters: []string{"c"}},
	}
	for i, s := range bad {
		if _, err := buildBundleCR(s); err == nil {
			t.Errorf("case %d: expected error, got nil", i)
		}
	}
}
