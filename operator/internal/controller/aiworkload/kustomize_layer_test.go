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
	"reflect"
	"sort"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

func parseYAMLMap(t *testing.T, content string) map[string]any {
	t.Helper()
	m := map[string]any{}
	if err := yaml.Unmarshal([]byte(content), &m); err != nil {
		t.Fatalf("unmarshal %q: %v", content, err)
	}
	return m
}

func overlayKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestAsKustomizeComponent_RewritesKindAndAPIVersion(t *testing.T) {
	for _, in := range []string{
		"patches:\n  - path: patch.yaml\n",
		"apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\npatches:\n  - path: patch.yaml\n",
	} {
		out, err := asKustomizeComponent(in)
		if err != nil {
			t.Fatalf("asKustomizeComponent(%q): %v", in, err)
		}
		m := parseYAMLMap(t, out)
		if m["apiVersion"] != kustomizeComponentAPIVersion || m["kind"] != kustomizeComponentKind {
			t.Fatalf("got apiVersion=%v kind=%v, want kustomize.config.k8s.io/v1alpha1 Component", m["apiVersion"], m["kind"])
		}
		want := []any{map[string]any{"path": "patch.yaml"}}
		if !reflect.DeepEqual(m["patches"], want) {
			t.Fatalf("patches = %v, want preserved %v", m["patches"], want)
		}
	}
}

func TestAsKustomizeComponent_RejectsNonMapping(t *testing.T) {
	if _, err := asKustomizeComponent("- a\n- b\n"); err == nil {
		t.Fatalf("want an error for a non-mapping kustomization")
	}
}

func TestComposeLayeredOverlay_Layout(t *testing.T) {
	files, err := composeLayeredOverlay(
		"overlay", map[string]string{
			"overlay/kustomization.yaml": "patches:\n  - path: patch.yaml\n",
			"overlay/patch.yaml":         "kind: ConfigMap\n",
		},
		"overlay", map[string]string{"overlay/kustomization.yaml": "patches: []\n"},
	)
	if err != nil {
		t.Fatalf("composeLayeredOverlay: %v", err)
	}
	want := []string{
		"overlay/blueprint/kustomization.yaml",
		"overlay/blueprint/patch.yaml",
		"overlay/install/kustomization.yaml",
		"overlay/kustomization.yaml",
	}
	if got := overlayKeys(files); !reflect.DeepEqual(got, want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
	top := parseYAMLMap(t, files["overlay/kustomization.yaml"])
	if top["kind"] != "Kustomization" || top["apiVersion"] != "kustomize.config.k8s.io/v1beta1" {
		t.Fatalf("top kustomization = %v", top)
	}
	if !reflect.DeepEqual(top["components"], []any{"blueprint", "install"}) {
		t.Fatalf("components = %v, want [blueprint install] in that order", top["components"])
	}
	for _, k := range []string{"overlay/blueprint/kustomization.yaml", "overlay/install/kustomization.yaml"} {
		if parseYAMLMap(t, files[k])["kind"] != kustomizeComponentKind {
			t.Fatalf("%s not rewritten to a Component: %q", k, files[k])
		}
	}
	if files["overlay/blueprint/patch.yaml"] != "kind: ConfigMap\n" {
		t.Fatalf("non-root files must be carried verbatim, got %q", files["overlay/blueprint/patch.yaml"])
	}
}

func TestComposeLayeredOverlay_StripsOwnDir(t *testing.T) {
	files, err := composeLayeredOverlay(
		"ov", map[string]string{"ov/kustomization.yaml": "patches: []\n"},
		"overlay", map[string]string{"overlay/kustomization.yaml": "patches: []\n"},
	)
	if err != nil {
		t.Fatalf("composeLayeredOverlay: %v", err)
	}
	for k := range files {
		if strings.Contains(k, "/ov/") || strings.HasPrefix(k, "overlay/install/overlay/") {
			t.Fatalf("own dir not stripped: %q", k)
		}
	}
	if _, ok := files["overlay/blueprint/kustomization.yaml"]; !ok {
		t.Fatalf("missing overlay/blueprint/kustomization.yaml in %v", overlayKeys(files))
	}
}

func TestComposeLayeredOverlay_HandlesKustomizationYml(t *testing.T) {
	files, err := composeLayeredOverlay(
		"overlay", map[string]string{"overlay/kustomization.yml": "patches: []\n"},
		"overlay", map[string]string{"overlay/kustomization.yaml": "patches: []\n"},
	)
	if err != nil {
		t.Fatalf("composeLayeredOverlay: %v", err)
	}
	got, ok := files["overlay/blueprint/kustomization.yml"]
	if !ok || parseYAMLMap(t, got)["kind"] != kustomizeComponentKind {
		t.Fatalf("kustomization.yml root not rewritten in place: %v", overlayKeys(files))
	}
}

func TestComposeLayeredOverlay_RejectsMissingRoot(t *testing.T) {
	if _, err := composeLayeredOverlay(
		"overlay", map[string]string{"overlay/patch.yaml": "a: 1\n"},
		"overlay", map[string]string{"overlay/kustomization.yaml": "patches: []\n"},
	); err == nil {
		t.Fatalf("want an error when an overlay has no root kustomization file")
	}
}

func TestComposeLayeredOverlay_RejectsKeyOutsideDir(t *testing.T) {
	if _, err := composeLayeredOverlay(
		"overlay", map[string]string{"overlay/kustomization.yaml": "patches: []\n"},
		"overlay", map[string]string{"overlay/kustomization.yaml": "patches: []\n", "other/x.yaml": "a: 1\n"},
	); err == nil {
		t.Fatalf("want an error for a file outside the overlay dir")
	}
}

func TestComposeLayeredOverlay_RejectsTraversal(t *testing.T) {
	if _, err := composeLayeredOverlay(
		"overlay", map[string]string{"overlay/kustomization.yaml": "patches: []\n"},
		"overlay", map[string]string{"overlay/kustomization.yaml": "patches: []\n", "overlay/../../etc/x": "a: 1\n"},
	); err == nil {
		t.Fatalf("want an error for a path escaping the overlay dir")
	}
}
