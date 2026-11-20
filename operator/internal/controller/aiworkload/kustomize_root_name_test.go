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
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/kustomizeoverlay"
)

// Fleet only reads <dir>/kustomization.yaml, so every spelling kustomize
// accepts must reach the Bundle under that name.
func TestRender_OverlayRootFileIsKustomizationYAML(t *testing.T) {
	for _, name := range kustomizeoverlay.FileNames {
		install := &aiplatformv1alpha1.ComponentKustomize{Dir: "inst", Files: map[string]string{"inst/" + name: emptyPatches, "inst/patch.yaml": "kind: X\n"}}
		w, c := reconcileOverlay(t, nil, install, "")
		b, ok := fleetObject(t, c, "Bundle", blueprintBundleName(w.Name, "comp"))
		if !ok {
			t.Fatalf("%s: embedded Bundle not rendered", name)
		}
		res := helmOpResources(t, b.Object["spec"].(map[string]any))
		if res["inst/kustomization.yaml"] != emptyPatches || res["inst/patch.yaml"] != "kind: X\n" {
			t.Fatalf("%s: resources = %v, want the root file as inst/kustomization.yaml", name, res)
		}
		if name != "kustomization.yaml" {
			if _, ok := res["inst/"+name]; ok {
				t.Fatalf("%s: the original root file name must not be shipped as well", name)
			}
		}
	}
}

func TestBuildOverlayRender_RejectsTwoRootFiles(t *testing.T) {
	files := map[string]string{"ov/kustomization.yaml": emptyPatches, "ov/Kustomization": emptyPatches}
	if _, err := buildOverlayRender(&resolvedOverlay{Dir: "ov", Files: files}, nil); err == nil || !strings.Contains(err.Error(), "more than one kustomization file") {
		t.Fatalf("single overlay: want a more-than-one-root error, got %v", err)
	}
	other := &resolvedOverlay{Dir: "b", Files: map[string]string{"b/kustomization.yaml": emptyPatches}}
	if _, err := buildOverlayRender(other, &resolvedOverlay{Dir: "ov", Files: files}); err == nil || !strings.Contains(err.Error(), "more than one kustomization file") {
		t.Fatalf("layered overlay: want a more-than-one-root error, got %v", err)
	}
}

// An overlay already named kustomization.yaml keeps its pre-change digest, and
// an overlay-free component contributes no overlay input at all.
func TestOverlayDigest_UnchangedForKustomizationYAMLAndNoOverlay(t *testing.T) {
	files := map[string]string{"ov/kustomization.yaml": emptyPatches, "ov/p.yaml": "kind: X\n"}
	got, err := buildOverlayRender(&resolvedOverlay{Dir: "ov", Files: files, Commit: "c1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	d, err := overlayDigest(got)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"dir": "ov", "files": files, "commit": "c1"})
	if d != sha256Hex(raw) {
		t.Fatalf("digest of a kustomization.yaml overlay changed")
	}

	r := &AIWorkloadReconciler{}
	w := newBlueprintWorkload(time.Now())
	d, o, _, err := r.overlayFingerprint(context.Background(), w, aiplatformv1alpha1.BlueprintComponent{ChartName: "comp"})
	if err != nil || d != "" || o != nil {
		t.Fatalf("overlay-free component: digest=%q render=%v err=%v, want none", d, o, err)
	}
}

// A renamed root changes the digest, so a Bundle built with the unread name is rebuilt.
func TestOverlayDigest_RenamedRootRebuilds(t *testing.T) {
	yml := &resolvedOverlay{Dir: "ov", Files: map[string]string{"ov/kustomization.yml": emptyPatches}}
	got, err := buildOverlayRender(yml, nil)
	if err != nil {
		t.Fatal(err)
	}
	d, _ := overlayDigest(got)
	raw, _ := json.Marshal(map[string]any{"dir": "ov", "files": yml.Files, "commit": ""})
	if d == sha256Hex(raw) {
		t.Fatalf("the digest must reflect the renamed root file")
	}
	if yml.Files["ov/kustomization.yml"] == "" || len(yml.Files) != 1 {
		t.Fatalf("normalizing must not mutate the resolved overlay's files: %v", yml.Files)
	}
}
