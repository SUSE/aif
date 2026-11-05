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

package kustomizeoverlay

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

const root = "patches: []\n"

func TestValidateDir(t *testing.T) {
	for _, dir := range []string{"overlay", "a/b", ".hidden", "..."} {
		if err := ValidateDir(dir); err != nil {
			t.Errorf("ValidateDir(%q) = %v, want nil", dir, err)
		}
	}
	for _, dir := range []string{"", ".", "./overlay", "overlay/", "a//b", "a/./b", "a/../b", "..", "../x", "/abs"} {
		if err := ValidateDir(dir); err == nil {
			t.Errorf("ValidateDir(%q) = nil, want an error", dir)
		}
	}
}

// TestValidateDir_MatchesCRDPattern keeps the CRD's dir pattern, which is what
// kubectl users hit, in agreement with ValidateDir.
func TestValidateDir_MatchesCRDPattern(t *testing.T) {
	for _, crd := range []string{"ai-factory.suse.com_blueprints.yaml", "ai-factory.suse.com_aiworkloads.yaml"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "config", "crd", "bases", crd))
		if err != nil {
			t.Fatal(err)
		}
		m := regexp.MustCompile(`(?s)Dir is the directory holding kustomization\.yaml.*?pattern: (\S+)`).FindSubmatch(raw)
		if m == nil {
			t.Fatalf("%s: no pattern on kustomize dir", crd)
		}
		pattern := regexp.MustCompile(string(m[1]))
		for _, dir := range []string{"overlay", "a/b", ".hidden", "...", "..x", ".", "./overlay", "overlay/", "a//b", "a/./b", "a/../b", "..", "../x", "/abs"} {
			if crdOK, goOK := pattern.MatchString(dir), ValidateDir(dir) == nil; crdOK != goOK {
				t.Errorf("%s: dir %q: CRD pattern accepts=%v, ValidateDir accepts=%v", crd, dir, crdOK, goOK)
			}
		}
	}
}

// TestFileNamesLiteral pins FileNames. The UI hard-codes the same list
// (ui/pkg/aif-ui/pages/components/wizard/kustomize-authoring.ts,
// KUSTOMIZATION_FILE_NAMES) and blueprint-config-kustomize.test.ts carries the
// TS half of this pin. Change BOTH together.
func TestFileNamesLiteral(t *testing.T) {
	want := []string{"kustomization.yaml", "kustomization.yml", "Kustomization"}
	if !slices.Equal(FileNames, want) {
		t.Errorf("FileNames drifted: %q, want %q", FileNames, want)
	}
}

func TestValidateFiles_Accepts(t *testing.T) {
	for _, name := range FileNames {
		files := map[string]string{"overlay/" + name: root, "overlay/patches/p.yaml": "a: 1\n"}
		if err := ValidateFiles("overlay", files); err != nil {
			t.Errorf("root %s: %v", name, err)
		}
	}
}

func TestValidateFiles_RejectsFileOutsideDir(t *testing.T) {
	// A key outside dir would land next to the chart in the Bundle and could
	// shadow one of its files.
	files := map[string]string{"overlay/kustomization.yaml": root, "mychart/templates/x.yaml": "a: 1\n"}
	if err := ValidateFiles("overlay", files); err == nil || !strings.Contains(err.Error(), "outside overlay dir") {
		t.Fatalf("err = %v, want a file-outside-dir error", err)
	}
}

func TestValidateFiles_RejectsTraversal(t *testing.T) {
	files := map[string]string{"overlay/kustomization.yaml": root, "overlay/../../etc/x": "a: 1\n"}
	if err := ValidateFiles("overlay", files); err == nil {
		t.Fatalf("want an error for a path escaping the bundle root")
	}
}

func TestValidateFiles_RejectsMissingRoot(t *testing.T) {
	files := map[string]string{"overlay/patch.yaml": "a: 1\n", "overlay/sub/kustomization.yaml": root}
	if err := ValidateFiles("overlay", files); err == nil || !strings.Contains(err.Error(), "no kustomization.yaml") {
		t.Fatalf("err = %v, want a missing-root error", err)
	}
}

func TestValidateFiles_RejectsBadDir(t *testing.T) {
	if err := ValidateFiles(".", map[string]string{"kustomization.yaml": root}); err == nil {
		t.Fatalf("want an error for dir %q", ".")
	}
}

func TestValidate(t *testing.T) {
	inline := &aiplatformv1alpha1.ComponentKustomize{Dir: "overlay", Files: map[string]string{"overlay/kustomization.yaml": root}}
	gitRef := &aiplatformv1alpha1.ComponentKustomize{Dir: "overlay", GitRef: &aiplatformv1alpha1.ComponentKustomizeGitRef{ChartRepo: "repo", Path: "overlays/prod"}}
	for name, k := range map[string]*aiplatformv1alpha1.ComponentKustomize{"nil": nil, "inline": inline, "gitRef": gitRef} {
		if err := Validate(k); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}

	bad := map[string]*aiplatformv1alpha1.ComponentKustomize{
		"no source":       {Dir: "overlay"},
		"both sources":    {Dir: "overlay", Files: inline.Files, GitRef: gitRef.GitRef},
		"bad dir":         {Dir: "overlay/", Files: inline.Files},
		"file outside":    {Dir: "overlay", Files: map[string]string{"overlay/kustomization.yaml": root, "x.yaml": ""}},
		"gitRef bad dir":  {Dir: "../overlay", GitRef: gitRef.GitRef},
		"gitRef no repo":  {Dir: "overlay", GitRef: &aiplatformv1alpha1.ComponentKustomizeGitRef{Path: "p"}},
		"gitRef no path":  {Dir: "overlay", GitRef: &aiplatformv1alpha1.ComponentKustomizeGitRef{ChartRepo: "repo"}},
		"gitRef escapes":  {Dir: "overlay", GitRef: &aiplatformv1alpha1.ComponentKustomizeGitRef{ChartRepo: "repo", Path: "../other"}},
		"gitRef absolute": {Dir: "overlay", GitRef: &aiplatformv1alpha1.ComponentKustomizeGitRef{ChartRepo: "repo", Path: "/etc"}},
	}
	for name, k := range bad {
		if err := Validate(k); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
