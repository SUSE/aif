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

package blueprintpolicy

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func vfComponent(name, targetNS, refNS string) aiplatformv1alpha1.BlueprintComponent {
	c := aiplatformv1alpha1.BlueprintComponent{ChartName: name, TargetNamespace: targetNS}
	if refNS != "-" {
		c.ValuesFrom = []aiplatformv1alpha1.ComponentValuesFrom{{SecretKeyRef: &aiplatformv1alpha1.ValuesFromKeyRef{Name: "v", Namespace: refNS}}}
	}
	return c
}

func TestCheckValuesFromNamespaces(t *testing.T) {
	for _, tc := range []struct {
		name       string
		components []aiplatformv1alpha1.BlueprintComponent
		defaultNS  string
		wantErr    string
	}{
		{"empty namespace is the release namespace", []aiplatformv1alpha1.BlueprintComponent{vfComponent("a", "", "")}, "", ""},
		{"sibling targetNamespace", []aiplatformv1alpha1.BlueprintComponent{vfComponent("cluster", "runai", "runai-backend"), vfComponent("cp", "runai-backend", "-")}, "", ""},
		{"foreign namespace", []aiplatformv1alpha1.BlueprintComponent{vfComponent("a", "runai", "kube-system")}, "", `namespace "kube-system" is not a namespace this Blueprint deploys to; use one of runai`},
		{"workload namespace unknown on save", []aiplatformv1alpha1.BlueprintComponent{vfComponent("a", "", "apps")}, "", "leave it empty"},
		{"workload namespace at install", []aiplatformv1alpha1.BlueprintComponent{vfComponent("a", "", "apps")}, "apps", ""},
		{"workload namespace only covers components without targetNamespace", []aiplatformv1alpha1.BlueprintComponent{vfComponent("a", "runai", "apps")}, "apps", "not a namespace"},
	} {
		err := CheckValuesFromNamespaces(tc.components, tc.defaultNS)
		if tc.wantErr == "" {
			if err != nil {
				t.Errorf("%s: unexpected error %v", tc.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: error %v, want it to contain %q", tc.name, err, tc.wantErr)
		}
	}
}

// The bundled Blueprints (notably Run:ai, whose cluster component reads its
// values from the control plane's namespace) must pass the check on save.
func TestCheckValuesFromNamespaces_BundledBlueprints(t *testing.T) {
	_, filename, _, _ := runtime.Caller(0)
	files, err := filepath.Glob(filepath.Join(filepath.Dir(filename), "../../../charts/aif-operator/files/blueprints/*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("glob bundled Blueprints: %v (%d files)", err, len(files))
	}
	checked := 0
	for _, path := range files {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var bp aiplatformv1alpha1.Blueprint
		if err := yaml.Unmarshal(contents, &bp); err != nil {
			t.Fatalf("parse %s: %v", filepath.Base(path), err)
		}
		if bp.Kind != "Blueprint" {
			continue
		}
		for _, c := range bp.Spec.Components {
			if len(c.ValuesFrom) > 0 {
				checked++
			}
		}
		if err := CheckValuesFromNamespaces(bp.Spec.Components, ""); err != nil {
			t.Errorf("%s: %v", filepath.Base(path), err)
		}
	}
	if checked == 0 {
		t.Error("no bundled Blueprint uses valuesFrom; the Run:ai case is not covered")
	}
}
