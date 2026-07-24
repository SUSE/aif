package aiworkload

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// makeChartTgz builds a minimal Helm chart .tgz from the given files (paths
// relative to the archive root, e.g. "mychart/Chart.yaml").
func makeChartTgz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatalf("write header %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

func resourceByName(res []any, name string) (map[string]any, bool) {
	for _, r := range res {
		m := r.(map[string]any)
		if m["name"] == name {
			return m, true
		}
	}
	return nil, false
}

func TestBuildGitChartBundle_UnpacksChart(t *testing.T) {
	tgz := makeChartTgz(t, map[string]string{
		"rancher-ai-agent/Chart.yaml":            "apiVersion: v2\nname: rancher-ai-agent\nversion: 109.0.1\n",
		"rancher-ai-agent/templates/cm.yaml":     "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n",
		"rancher-ai-agent/values.yaml":           "replicaCount: 1\n",
	})
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
	// helm.chart must point at the chart's top-level directory (not a tgz).
	chart, _, _ := unstructured.NestedString(b.Object, "spec", "helm", "chart")
	if chart != "rancher-ai-agent" {
		t.Fatalf("helm.chart = %q, want chart dir", chart)
	}
	// version is pinned by the fetched archive, so helm.version must be absent.
	if _, ok, _ := unstructured.NestedString(b.Object, "spec", "helm", "version"); ok {
		t.Fatal("helm.version should be omitted for unpacked git charts")
	}
	if own, _, _ := unstructured.NestedBool(b.Object, "spec", "helm", "takeOwnership"); !own {
		t.Fatal("expected helm.takeOwnership=true")
	}
	// Every chart file is present as its own resource, path-preserved and inline.
	res, _, _ := unstructured.NestedSlice(b.Object, "spec", "resources")
	if len(res) != 3 {
		t.Fatalf("want 3 chart-file resources, got %d", len(res))
	}
	chartYaml, ok := resourceByName(res, "rancher-ai-agent/Chart.yaml")
	if !ok {
		t.Fatalf("Chart.yaml resource missing; got %v", res)
	}
	if _, hasEnc := chartYaml["encoding"]; hasEnc {
		t.Fatal("text chart file should be stored inline, not base64")
	}
	if _, ok, _ := unstructured.NestedMap(b.Object, "spec", "helm", "values"); !ok {
		t.Fatal("expected helm.values to be set")
	}
}

func TestBuildGitChartBundle_NoValuesOmitsKey(t *testing.T) {
	tgz := makeChartTgz(t, map[string]string{"x/Chart.yaml": "apiVersion: v2\nname: x\nversion: 1.0.0\n"})
	c := aiplatformv1alpha1.BlueprintComponent{ChartName: "x", ChartVersion: "1.0.0"}
	b, err := buildGitChartBundle("wl-x", "ns", tgz, c, map[string]any{}, nil)
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

func TestBuildGitChartBundle_RejectsNonChartArchive(t *testing.T) {
	c := aiplatformv1alpha1.BlueprintComponent{ChartName: "bad", ChartVersion: "1.0.0"}
	if _, err := buildGitChartBundle("wl-bad", "ns", []byte("not a gzip"), c, nil, nil); err == nil {
		t.Fatal("expected unpack error for non-archive bytes")
	}
}
