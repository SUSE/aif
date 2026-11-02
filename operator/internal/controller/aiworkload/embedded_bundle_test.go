/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package aiworkload

import (
	"errors"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// TestEmbeddedChartFingerprint_GitNoOverlayUnchanged pins the fingerprint of an
// existing git-backed component (no overlay, retry epoch 0) to the value the
// pre-overlay gitChartFingerprint produced, captured before the rename. Changing
// it would rebuild every existing git-chart Bundle after an operator upgrade.
func TestEmbeddedChartFingerprint_GitNoOverlayUnchanged(t *testing.T) {
	c := aiplatformv1alpha1.BlueprintComponent{ChartRepo: "git-repo", ChartName: "demo", ChartVersion: "1.2.3", ReleaseName: "rel"}
	local := []any{map[string]any{"clusterName": "local"}}
	if got := embeddedChartFingerprint(c, "ns", "commit-abc", map[string]any{"a": 1}, []string{"wl-dep"}, "", 0, local, []any{}); got != "52bb8d972e0a921059a2f85b30ec05ab8bf2f109620aaff60c72a6ca19c314f9" {
		t.Fatalf("fingerprint changed: %s", got)
	}
	if got := embeddedChartFingerprint(c, "ns", "commit-abc", nil, nil, "", 0, local, nil); got != "041d71a593b2fd69128e0beb736e1f780ad47210c4e002bd4f0b5d90931814f6" {
		t.Fatalf("fingerprint changed: %s", got)
	}
}

func TestEmbeddedChartFingerprint_ChangesWithOverlayAndEpoch(t *testing.T) {
	c := aiplatformv1alpha1.BlueprintComponent{ChartRepo: "r", ChartName: "demo", ChartVersion: "1.0.0"}
	base := embeddedChartFingerprint(c, "ns", "https://c.example.com", nil, nil, "", 0)
	withOverlay := embeddedChartFingerprint(c, "ns", "https://c.example.com", nil, nil, "digest-1", 0)
	otherOverlay := embeddedChartFingerprint(c, "ns", "https://c.example.com", nil, nil, "digest-2", 0)
	withEpoch := embeddedChartFingerprint(c, "ns", "https://c.example.com", nil, nil, "digest-1", 3)
	if base == withOverlay || withOverlay == otherOverlay || withOverlay == withEpoch {
		t.Fatalf("fingerprint must change with overlay digest and epoch: %s %s %s %s", base, withOverlay, otherOverlay, withEpoch)
	}
}

const emptyPatches = "patches: []\n"

func embeddedTestChart(t *testing.T) []byte {
	return makeChartTgz(t, map[string]string{
		"demo/Chart.yaml":          "apiVersion: v2\nname: demo\nversion: 1.0.0\n",
		"demo/templates/svc.yaml":  "kind: Service\n",
		"demo/templates/conf.yaml": "kind: ConfigMap\n",
	})
}

func testOverlay(t *testing.T, dir string, files map[string]string) *overlayRender {
	t.Helper()
	o, err := buildOverlayRender(&resolvedOverlay{Dir: dir, Files: files}, nil)
	if err != nil {
		t.Fatalf("buildOverlayRender: %v", err)
	}
	return o
}

func TestBuildEmbeddedChartBundle_CarriesOverlayAndDir(t *testing.T) {
	c := aiplatformv1alpha1.BlueprintComponent{ChartName: "demo"}
	overlay := testOverlay(t, "overlay", map[string]string{"overlay/kustomization.yaml": emptyPatches})
	b, err := buildEmbeddedChartBundle("wl-demo", "ns", "fp", embeddedTestChart(t), c, nil, nil, nil, overlay, 0, true)
	if err != nil {
		t.Fatalf("buildEmbeddedChartBundle: %v", err)
	}
	res, _, _ := unstructured.NestedSlice(b.Object, "spec", "resources")
	chartFile, ok := resourceByName(res, "demo/Chart.yaml")
	if !ok || !strings.Contains(string(decodeBase64Gz(t, chartFile["content"].(string))), "name: demo") {
		t.Fatalf("chart file missing or not base64+gz: %v", chartFile)
	}
	ov, ok := resourceByName(res, "overlay/kustomization.yaml")
	if !ok || ov["content"] != emptyPatches || ov["encoding"] != nil {
		t.Fatalf("overlay file = %v, want plain content without encoding", ov)
	}
	if dir, _, _ := unstructured.NestedString(b.Object, "spec", "kustomize", "dir"); dir != "overlay" {
		t.Fatalf("spec.kustomize.dir = %q, want overlay", dir)
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(b.Object, "spec", "forceSyncGeneration"); found {
		t.Fatalf("forceSyncGeneration must be omitted at epoch 0")
	}
}

func TestBuildEmbeddedChartBundle_LayeredOverlay(t *testing.T) {
	files := map[string]string{"overlay/kustomization.yaml": emptyPatches}
	overlay, err := buildOverlayRender(&resolvedOverlay{Dir: "overlay", Files: files}, &resolvedOverlay{Dir: "overlay", Files: files})
	if err != nil {
		t.Fatalf("buildOverlayRender: %v", err)
	}
	b, err := buildEmbeddedChartBundle("wl-demo", "ns", "fp", embeddedTestChart(t), aiplatformv1alpha1.BlueprintComponent{ChartName: "demo"}, nil, nil, nil, overlay, 0, true)
	if err != nil {
		t.Fatalf("buildEmbeddedChartBundle: %v", err)
	}
	res, _, _ := unstructured.NestedSlice(b.Object, "spec", "resources")
	top, ok := resourceByName(res, "overlay/kustomization.yaml")
	if !ok || !strings.Contains(top["content"].(string), "components:") {
		t.Fatalf("missing generated top-level kustomization: %v", top)
	}
}

func TestBuildEmbeddedChartBundle_ForceSyncGeneration(t *testing.T) {
	b, err := buildEmbeddedChartBundle("wl-demo", "ns", "fp", embeddedTestChart(t), aiplatformv1alpha1.BlueprintComponent{ChartName: "demo"}, nil, nil, nil, nil, 4, false)
	if err != nil {
		t.Fatalf("buildEmbeddedChartBundle: %v", err)
	}
	if got, _, _ := unstructured.NestedInt64(b.Object, "spec", "forceSyncGeneration"); got != 4 {
		t.Fatalf("forceSyncGeneration = %d, want 4", got)
	}
}

func TestBuildEmbeddedChartBundle_OneBudget(t *testing.T) {
	tgz := embeddedTestChart(t)
	chartRes, _, err := chartTgzToBundleResources(tgz)
	if err != nil {
		t.Fatalf("chartTgzToBundleResources: %v", err)
	}
	// An overlay that fits on its own but pushes the chart over the stored cap.
	name := "overlay/big.yaml"
	big := strings.Repeat("x", maxBundleResourcesBytes-storedPayload(chartRes)-len(name)+10)
	overlay := testOverlay(t, "overlay", map[string]string{"overlay/kustomization.yaml": "resources: []\n", name: big})

	_, err = buildEmbeddedChartBundle("wl-demo", "ns", "fp", tgz, aiplatformv1alpha1.BlueprintComponent{ChartName: "demo"}, nil, nil, nil, overlay, 0, true)
	if !errors.Is(err, errChartTooLarge) {
		t.Fatalf("want errChartTooLarge, got %v", err)
	}
	for _, want := range []string{"overlay", "remove the overlay"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q should mention %q", err, want)
		}
	}
	// A git-backed component (not embedded because of its overlay) gets no removal hint.
	_, err = buildEmbeddedChartBundle("wl-demo", "ns", "fp", tgz, aiplatformv1alpha1.BlueprintComponent{ChartName: "demo"}, nil, nil, nil, overlay, 0, false)
	if !errors.Is(err, errChartTooLarge) || strings.Contains(err.Error(), "remove the overlay") {
		t.Fatalf("git-backed overflow = %v, want errChartTooLarge without the removal hint", err)
	}
}

func TestBuildEmbeddedChartBundle_OverlayOnlyArchiveTooLargeGivesOverlayHint(t *testing.T) {
	tgz := make([]byte, maxChartArchiveBytes+1)
	_, err := buildEmbeddedChartBundle("wl-demo", "ns", "fp", tgz, aiplatformv1alpha1.BlueprintComponent{ChartName: "demo"}, nil, nil, nil, nil, 0, true)
	if !errors.Is(err, errChartTooLarge) || !strings.Contains(err.Error(), "remove the overlay") || strings.Contains(err.Error(), "host it via") {
		t.Fatalf("overlay-only oversize = %v, want the removal hint and no hosting advice", err)
	}
}

func TestBuildEmbeddedChartBundle_RejectsOverlayInsideChartDir(t *testing.T) {
	overlay := testOverlay(t, "demo/overlay", map[string]string{"demo/overlay/kustomization.yaml": emptyPatches})
	_, err := buildEmbeddedChartBundle("wl-demo", "ns", "fp", embeddedTestChart(t), aiplatformv1alpha1.BlueprintComponent{ChartName: "demo"}, nil, nil, nil, overlay, 0, true)
	if err == nil || !strings.Contains(err.Error(), "chart directory") {
		t.Fatalf("want an overlay-inside-chart error, got %v", err)
	}
}
