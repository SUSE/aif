package aiworkload

import (
	"strings"
	"testing"
)

func TestOverlayFilesToResourcesSortedAndSafe(t *testing.T) {
	res, err := overlayFilesToResources(map[string]string{
		"overlay/patch.yaml":         "b: 2",
		"overlay/kustomization.yaml": "a: 1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("want 2 resources, got %d", len(res))
	}
	first := res[0].(map[string]any)["name"].(string)
	if first != "overlay/kustomization.yaml" {
		t.Fatalf("resources must be sorted by name, got %q first", first)
	}
}

func TestOverlayFilesRejectsEscape(t *testing.T) {
	if _, err := overlayFilesToResources(map[string]string{"../evil.yaml": "x"}); err == nil {
		t.Fatalf("expected path-escape rejection")
	}
}

func TestOverlayFilesRejectsOversize(t *testing.T) {
	big := strings.Repeat("x", (1<<20)+1)
	if _, err := overlayFilesToResources(map[string]string{"overlay/big.yaml": big}); err == nil {
		t.Fatalf("expected oversize rejection")
	}
}

func TestKustomizeDigestChangesOutput(t *testing.T) {
	base := ComponentRenderInputs{ChartName: "n"}
	with := base
	with.Kustomize = map[string]any{"dir": "overlay", "files": map[string]string{"overlay/kustomization.yaml": "a: 1"}}
	if perHelmOpRenderDigest(base) == perHelmOpRenderDigest(with) {
		t.Fatalf("kustomize overlay must change the render digest")
	}
}
