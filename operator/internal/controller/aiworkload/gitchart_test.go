package aiworkload

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/infra/rancher"
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
		"rancher-ai-agent/Chart.yaml":        "apiVersion: v2\nname: rancher-ai-agent\nversion: 109.0.1\n",
		"rancher-ai-agent/templates/cm.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n",
		"rancher-ai-agent/values.yaml":       "replicaCount: 1\n",
	})
	c := aiplatformv1alpha1.BlueprintComponent{ChartName: "rancher-ai-agent", ChartVersion: "109.0.1"}
	vals := map[string]any{"replicaCount": int64(2)}
	targets := []any{map[string]any{"clusterName": "local"}}
	fingerprint := strings.Repeat("a", 64)

	b, err := buildGitChartBundle("wl-agent", "cattle-ai-agent-system", fingerprint, tgz, c, vals, targets)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.GroupVersionKind() != bundleGVK {
		t.Fatalf("wrong gvk: %v", b.GroupVersionKind())
	}
	if got := b.GetAnnotations()[chartFingerprintAnnotation]; got != fingerprint {
		t.Fatalf("fingerprint annotation = %q, want %q", got, fingerprint)
	}
	if got := b.GetLabels()[renderDigestLabel]; got != renderDigestLabelValue(fingerprint) {
		t.Fatalf("render digest label = %q, want %q", got, renderDigestLabelValue(fingerprint))
	}
	if got := b.GetLabels()[renderDigestLabel]; len(got) > 63 {
		t.Fatalf("render digest label is %d characters, exceeds Kubernetes' 63-character limit", len(got))
	}
	// helm.chart must point at the chart's top-level directory (not a tgz).
	chart, _, _ := unstructured.NestedString(b.Object, "spec", "helm", "chart")
	if chart != "rancher-ai-agent" {
		t.Fatalf("helm.chart = %q, want chart dir", chart)
	}
	// releaseName defaults to the chart name when the component sets no override.
	if rn, _, _ := unstructured.NestedString(b.Object, "spec", "helm", "releaseName"); rn != "rancher-ai-agent" {
		t.Fatalf("helm.releaseName = %q, want chart name", rn)
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

func TestBuildGitChartBundle_ReleaseNameOverride(t *testing.T) {
	tgz := makeChartTgz(t, map[string]string{
		"milvus/Chart.yaml": "apiVersion: v2\nname: milvus\nversion: 1.0.0\n",
	})
	c := aiplatformv1alpha1.BlueprintComponent{ChartName: "milvus", ChartVersion: "1.0.0", ReleaseName: "my-milvus"}
	b, err := buildGitChartBundle("wl-milvus", "ns", "", tgz, c, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rn, _, _ := unstructured.NestedString(b.Object, "spec", "helm", "releaseName"); rn != "my-milvus" {
		t.Fatalf("helm.releaseName = %q, want override my-milvus", rn)
	}
}

func TestBuildGitChartBundle_NoValuesOmitsKey(t *testing.T) {
	tgz := makeChartTgz(t, map[string]string{"x/Chart.yaml": "apiVersion: v2\nname: x\nversion: 1.0.0\n"})
	c := aiplatformv1alpha1.BlueprintComponent{ChartName: "x", ChartVersion: "1.0.0"}
	b, err := buildGitChartBundle("wl-x", "ns", "", tgz, c, map[string]any{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok, _ := unstructured.NestedMap(b.Object, "spec", "helm", "values"); ok {
		t.Fatal("expected helm.values to be omitted when empty")
	}
}

func TestBuildGitChartBundle_RejectsOversizedArchive(t *testing.T) {
	tgz := make([]byte, maxChartArchiveBytes+1)
	c := aiplatformv1alpha1.BlueprintComponent{ChartName: "huge", ChartVersion: "1.0.0"}
	if _, err := buildGitChartBundle("wl-huge", "ns", "", tgz, c, nil, nil); err == nil {
		t.Fatal("expected size-limit error")
	}
}

func TestBuildGitChartBundle_RejectsNonChartArchive(t *testing.T) {
	c := aiplatformv1alpha1.BlueprintComponent{ChartName: "bad", ChartVersion: "1.0.0"}
	if _, err := buildGitChartBundle("wl-bad", "ns", "", []byte("not a gzip"), c, nil, nil); err == nil {
		t.Fatal("expected unpack error for non-archive bytes")
	}
}

func TestChartTgzToBundleResources_RejectsPathTraversal(t *testing.T) {
	cases := map[string]string{
		"parent-relative": "../evil/x.yaml",
		"absolute":        "/etc/passwd",
	}
	for name, entry := range cases {
		t.Run(name, func(t *testing.T) {
			tgz := makeChartTgz(t, map[string]string{
				"chart/Chart.yaml": "apiVersion: v2\nname: chart\nversion: 1.0.0\n",
				entry:              "pwned",
			})
			if _, _, err := chartTgzToBundleResources(tgz); err == nil {
				t.Fatalf("expected traversal error for entry %q", entry)
			}
		})
	}
}

// The cap that matters is on the UNPACKED payload, because that is what the API
// server stores: a Bundle is a plain CR with no compression. A chart can sit far
// below the archive cap and still blow past it once expanded — that is the shape
// of both a decompression bomb and of real charts like rancher-monitoring
// (490 KiB compressed, ~3.9 MiB as bundle resources).
func TestChartTgzToBundleResources_RejectsChartThatExpandsPastPayloadCap(t *testing.T) {
	big := make([]byte, maxBundleResourcesBytes+1) // all-zero bytes: compresses to ~nothing
	tgz := makeChartTgz(t, map[string]string{
		"chart/Chart.yaml": "apiVersion: v2\nname: chart\nversion: 1.0.0\n",
		"chart/big.txt":    string(big),
	})
	if len(tgz) > maxChartArchiveBytes {
		t.Fatalf("test archive is not sufficiently compressible: %d bytes", len(tgz))
	}
	if _, _, err := chartTgzToBundleResources(tgz); err == nil {
		t.Fatal("expected payload-size-limit error")
	}
}

// Binary files are base64-encoded into the Bundle, inflating them by 4/3. The
// cap has to be charged against the encoded form, so a binary blob at ~80% of
// the cap must be rejected even though its raw size fits.
func TestChartTgzToBundleResources_ChargesBase64InflatedSize(t *testing.T) {
	raw := make([]byte, maxBundleResourcesBytes*8/10)
	for i := range raw {
		raw[i] = 0xff // invalid UTF-8 → stored as base64
	}
	tgz := makeChartTgz(t, map[string]string{
		"chart/Chart.yaml": "apiVersion: v2\nname: chart\nversion: 1.0.0\n",
		"chart/blob.bin":   string(raw),
	})
	if _, _, err := chartTgzToBundleResources(tgz); err == nil {
		t.Fatal("expected payload-size-limit error for base64-inflated binary file")
	}
}

func TestFetchGitChart_PreservesErrUnauthorized(t *testing.T) {
	c := aiplatformv1alpha1.BlueprintComponent{
		ChartRepo: "rancher-charts", ChartName: "rancher-backup-crd", ChartVersion: "1.0.0",
	}
	inner := fmt.Errorf("%w (401 Unauthorized)", rancher.ErrUnauthorized)

	_, err := fetchGitChart(context.Background(), fakeCatalog{err: inner}, c)
	if err == nil {
		t.Fatal("fetchGitChart returned nil error")
	}
	if !errors.Is(err, rancher.ErrUnauthorized) {
		t.Fatalf("errors.Is(err, rancher.ErrUnauthorized) = false; err = %v", err)
	}
	// The wrap must still name the component, so the condition message is useful.
	if !strings.Contains(err.Error(), "rancher-backup-crd") {
		t.Fatalf("error does not name the chart: %v", err)
	}
}

func TestGitChartFingerprint_ChangesWithInputs(t *testing.T) {
	c := aiplatformv1alpha1.BlueprintComponent{ChartRepo: "rancher-charts", ChartName: "x", ChartVersion: "1.0.0"}
	targets := []any{map[string]any{"clusterName": "local"}}
	base := gitChartFingerprint(c, "ns", "commit-aaa", map[string]any{"a": 1}, targets)
	if base == "" {
		t.Fatal("fingerprint should not be empty for hashable inputs")
	}
	if got := gitChartFingerprint(c, "ns", "commit-aaa", map[string]any{"a": 1}, targets); got != base {
		t.Fatal("fingerprint is not stable across identical inputs")
	}

	bumped := c
	bumped.ChartVersion = "1.0.1"
	renamed := c
	renamed.ReleaseName = "custom-release"
	for name, got := range map[string]string{
		"version":   gitChartFingerprint(bumped, "ns", "commit-aaa", map[string]any{"a": 1}, targets),
		"namespace": gitChartFingerprint(c, "other-ns", "commit-aaa", map[string]any{"a": 1}, targets),
		"values":    gitChartFingerprint(c, "ns", "commit-aaa", map[string]any{"a": 2}, targets),
		"targets":   gitChartFingerprint(c, "ns", "commit-aaa", map[string]any{"a": 1}, []any{map[string]any{"clusterName": "downstream"}}),
		// The release name is baked into the Bundle's Helm options, so changing
		// only it must invalidate the fingerprint or the override never ships.
		"release name": gitChartFingerprint(renamed, "ns", "commit-aaa", map[string]any{"a": 1}, targets),
		// The reason this input exists: a git-backed repo tracks a branch, so a
		// developer can re-push a corrected chart at the SAME version. Only the
		// repo's indexed commit distinguishes the two, and without it the Bundle
		// is judged up to date and the chart is never re-fetched.
		"repo commit": gitChartFingerprint(c, "ns", "commit-bbb", map[string]any{"a": 1}, targets),
	} {
		if got == base {
			t.Errorf("fingerprint unchanged when %s changed", name)
		}
	}
}

// The size guards must be recognisable as terminal by the reconciler; a bare
// error would be returned and retried forever, re-downloading the chart on every
// backoff tick. Reconcile keys that decision off errChartTooLarge.
func TestSizeGuards_WrapErrChartTooLarge(t *testing.T) {
	t.Run("archive pre-check", func(t *testing.T) {
		oversized := make([]byte, maxChartArchiveBytes+1)
		_, err := buildGitChartBundle("b", "ns", "fp", oversized,
			aiplatformv1alpha1.BlueprintComponent{ChartName: "big"}, nil, nil)
		if !errors.Is(err, errChartTooLarge) {
			t.Fatalf("archive guard error = %v, want it to wrap errChartTooLarge", err)
		}
	})

	t.Run("unpacked payload cap", func(t *testing.T) {
		// Highly compressible, so it slips past the archive pre-check and is only
		// caught once expanded — the case the compressed-size proxy used to miss.
		tgz := makeChartTgz(t, map[string]string{
			"c/Chart.yaml": "apiVersion: v2\nname: c\nversion: 1.0.0\n",
			"c/big.txt":    strings.Repeat("a", maxBundleResourcesBytes+1),
		})
		if len(tgz) > maxChartArchiveBytes {
			t.Fatalf("fixture archive is %d bytes; it must stay under the pre-check to exercise the payload cap", len(tgz))
		}
		_, err := buildGitChartBundle("b", "ns", "fp", tgz,
			aiplatformv1alpha1.BlueprintComponent{ChartName: "c"}, nil, nil)
		if !errors.Is(err, errChartTooLarge) {
			t.Fatalf("payload guard error = %v, want it to wrap errChartTooLarge", err)
		}
	})
}
