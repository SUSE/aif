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

package v1alpha1

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apixv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	structuralschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/yaml"
)

func TestBlueprintTypesCompile(t *testing.T) {
	_ = Blueprint{}
	_ = BlueprintList{}
	_ = BlueprintComponent{ChartRepo: "r", ChartName: "n", ChartVersion: "1.0.0"}
	_ = BlueprintSpec{
		DisplayName: "d",
		Version:     "1.0.0",
		Source:      BlueprintOriginCustom,
		Components:  []BlueprintComponent{{ChartRepo: "r", ChartName: "n", ChartVersion: "1.0.0"}},
	}
	_ = BlueprintNameLabel
	_ = BlueprintVersionLabel
	_ = BlueprintOriginSUSE
	_ = BlueprintOriginNvidia
	_ = BlueprintOriginPartner
	_ = BlueprintOriginCommunity
	_ = BlueprintOriginCustom
}

func TestBlueprintComponentTargetNamespaceJSON(t *testing.T) {
	// Set value round-trips through JSON under the "targetNamespace" key.
	in := BlueprintComponent{ChartRepo: "r", ChartName: "n", ChartVersion: "1.0.0", TargetNamespace: "ai-system"}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !json.Valid(b) {
		t.Fatalf("invalid json: %s", b)
	}
	var out BlueprintComponent
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.TargetNamespace != "ai-system" {
		t.Errorf("expected targetNamespace round-trip, got %q from %s", out.TargetNamespace, b)
	}

	// Empty value is omitted from JSON (omitempty).
	empty, _ := json.Marshal(BlueprintComponent{ChartRepo: "r", ChartName: "n", ChartVersion: "1.0.0"})
	if string(empty) == "" || jsonHasKey(empty, "targetNamespace") {
		t.Errorf("expected targetNamespace omitted when empty, got %s", empty)
	}
}

func jsonHasKey(b []byte, key string) bool {
	var m map[string]json.RawMessage
	_ = json.Unmarshal(b, &m)
	_, ok := m[key]
	return ok
}

func TestComponentKustomizeRoundTrips(t *testing.T) {
	c := BlueprintComponent{
		ChartRepo: "r", ChartName: "n", ChartVersion: "1.0.0",
		DependsOn: []string{"other"},
		Kustomize: &ComponentKustomize{Dir: "overlay", Files: map[string]string{"overlay/kustomization.yaml": "resources: []"}},
	}
	got := c.DeepCopy()
	if got.Kustomize == nil || got.Kustomize.Dir != "overlay" {
		t.Fatalf("deepcopy dropped kustomize: %+v", got.Kustomize)
	}
	if got.Kustomize.Files["overlay/kustomization.yaml"] != "resources: []" {
		t.Fatalf("deepcopy dropped inline files")
	}
	got.Kustomize.Files["overlay/kustomization.yaml"] = "mutated"
	if c.Kustomize.Files["overlay/kustomization.yaml"] == "mutated" {
		t.Fatalf("deepcopy is shallow — mutation leaked to original")
	}
	if len(got.DependsOn) != 1 || got.DependsOn[0] != "other" {
		t.Fatalf("deepcopy dropped dependsOn")
	}
}

// valuesFromItemValidator compiles the CEL rules of the generated Blueprint CRD's
// spec.components[].valuesFrom[] item schema.
func valuesFromItemValidator(t *testing.T) (*cel.Validator, *structuralschema.Structural) {
	t.Helper()
	raw, err := os.ReadFile("../../config/crd/bases/ai-factory.suse.com_blueprints.yaml")
	if err != nil {
		t.Fatalf("read CRD: %v", err)
	}
	var crd apixv1.CustomResourceDefinition
	if err := yaml.Unmarshal(raw, &crd); err != nil {
		t.Fatalf("decode CRD: %v", err)
	}
	vf, ok := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.
		Properties["spec"].Properties["components"].Items.Schema.
		Properties["valuesFrom"]
	if !ok || vf.Items == nil || vf.Items.Schema == nil {
		t.Fatalf("CRD has no spec.components[].valuesFrom[] schema")
	}
	props := vf.Items.Schema
	var internal apiextensions.JSONSchemaProps
	if err := apixv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(props, &internal, nil); err != nil {
		t.Fatalf("convert schema: %v", err)
	}
	s, err := structuralschema.NewStructural(&internal)
	if err != nil {
		t.Fatalf("structural schema: %v", err)
	}
	v := cel.NewValidator(s, false, 1000000)
	if v == nil {
		t.Fatalf("no CEL rules on valuesFrom item schema")
	}
	return v, s
}

func TestComponentValuesFrom_CEL(t *testing.T) {
	v, s := valuesFromItemValidator(t)
	ref := map[string]any{"name": "runai-cluster-values", "namespace": "runai-backend", "key": "values.yaml"}
	const msg = "set exactly one of configMapKeyRef or secretKeyRef"
	cases := map[string]struct {
		obj     map[string]any
		wantErr bool
	}{
		"both":      {map[string]any{"configMapKeyRef": ref, "secretKeyRef": ref}, true},
		"neither":   {map[string]any{}, true},
		"secret":    {map[string]any{"secretKeyRef": ref}, false},
		"configMap": {map[string]any{"configMapKeyRef": ref}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			errs, _ := v.Validate(context.Background(), field.NewPath("valuesFrom").Index(0), s, tc.obj, nil, 10000000)
			if !tc.wantErr {
				if len(errs) != 0 {
					t.Fatalf("want accepted, got %v", errs)
				}
				return
			}
			if len(errs) == 0 || !strings.Contains(errs.ToAggregate().Error(), msg) {
				t.Fatalf("want rejection %q, got %v", msg, errs)
			}
		})
	}
}
