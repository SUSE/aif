package aiworkload

import (
	"encoding/base64"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func TestBuildGitChartBundle_EmbedsChart(t *testing.T) {
	tgz := []byte("fake-tgz-bytes")
	c := aiplatformv1alpha1.BlueprintComponent{ChartName: "rancher-ai-agent", ChartVersion: "109.0.1"}
	vals := map[string]any{"replicaCount": int64(2)}
	targets := []any{map[string]any{"clusterName": "local"}}

	b, err := buildGitChartBundle("wl-agent", "cattle-ai-agent-system", tgz, c, vals, targets)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.GroupVersionKind() != bundleGVK {
		t.Fatalf("wrong gvk: %v", b.GroupVersionKind())
	}
	if b.GetName() != "wl-agent" {
		t.Fatalf("name = %q", b.GetName())
	}
	ns, _, _ := unstructured.NestedString(b.Object, "spec", "defaultNamespace")
	if ns != "cattle-ai-agent-system" {
		t.Fatalf("defaultNamespace = %q", ns)
	}
	chart, _, _ := unstructured.NestedString(b.Object, "spec", "helm", "chart")
	if chart != "chart.tgz" {
		t.Fatalf("helm.chart = %q", chart)
	}
	ver, _, _ := unstructured.NestedString(b.Object, "spec", "helm", "version")
	if ver != "109.0.1" {
		t.Fatalf("helm.version = %q", ver)
	}
	if own, _, _ := unstructured.NestedBool(b.Object, "spec", "helm", "takeOwnership"); !own {
		t.Fatal("expected helm.takeOwnership=true")
	}
	res, _, _ := unstructured.NestedSlice(b.Object, "spec", "resources")
	if len(res) != 1 {
		t.Fatalf("want 1 resource, got %d", len(res))
	}
	got := res[0].(map[string]any)
	if got["encoding"] != "base64" || got["content"] != base64.StdEncoding.EncodeToString(tgz) {
		t.Fatalf("resource content mismatch: %+v", got)
	}
	if _, ok, _ := unstructured.NestedMap(b.Object, "spec", "helm", "values"); !ok {
		t.Fatal("expected helm.values to be set")
	}
}

func TestBuildGitChartBundle_NoValuesOmitsKey(t *testing.T) {
	c := aiplatformv1alpha1.BlueprintComponent{ChartName: "x", ChartVersion: "1.0.0"}
	b, err := buildGitChartBundle("wl-x", "ns", []byte("t"), c, map[string]any{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok, _ := unstructured.NestedMap(b.Object, "spec", "helm", "values"); ok {
		t.Fatal("expected helm.values to be omitted when empty")
	}
}

func TestBuildGitChartBundle_RejectsOversizedChart(t *testing.T) {
	tgz := make([]byte, maxFleetBundleChartBytes+1)
	c := aiplatformv1alpha1.BlueprintComponent{ChartName: "huge", ChartVersion: "1.0.0"}
	if _, err := buildGitChartBundle("wl-huge", "ns", tgz, c, nil, nil); err == nil {
		t.Fatal("expected size-limit error")
	}
}
