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
	"testing"
)

func injectedObjects(names ...string) []any {
	out := make([]any, 0, len(names))
	for _, n := range names {
		out = append(out, map[string]any{"name": n})
	}
	return out
}

func TestAdaptPullSecretShape_NetworkOperatorWritesFlatStrings(t *testing.T) {
	ps := injectedObjects(combinedPullSecretName)
	vals := map[string]any{
		"imagePullSecrets": ps,
		"global":           map[string]any{"imagePullSecrets": ps},
	}
	adaptPullSecretShapeForChart("network-operator", vals)

	op, _ := vals["operator"].(map[string]any)
	if got, want := op["imagePullSecrets"], []any{combinedPullSecretName}; !reflect.DeepEqual(got, want) {
		t.Fatalf("operator.imagePullSecrets = %#v, want %#v", got, want)
	}
	// Subcharts read the object shape: top-level and global stay untouched.
	if !reflect.DeepEqual(vals["imagePullSecrets"], ps) {
		t.Fatalf("top-level imagePullSecrets changed: %#v", vals["imagePullSecrets"])
	}
	if g := vals["global"].(map[string]any); !reflect.DeepEqual(g["imagePullSecrets"], ps) {
		t.Fatalf("global.imagePullSecrets changed: %#v", g["imagePullSecrets"])
	}
}

func TestAdaptPullSecretShape_MatchesChartBaseName(t *testing.T) {
	vals := map[string]any{"imagePullSecrets": injectedObjects("s1")}
	adaptPullSecretShapeForChart("nvidia/network-operator", vals)
	if _, ok := vals["operator"]; !ok {
		t.Fatal("expected operator.imagePullSecrets for a repo-prefixed chart name")
	}
}

func TestAdaptPullSecretShape_KeepsAuthorEntriesAndSiblings(t *testing.T) {
	vals := map[string]any{
		"imagePullSecrets": injectedObjects(combinedPullSecretName),
		"operator": map[string]any{
			"repository":       "nvcr.io/nvidia",
			"imagePullSecrets": []any{"my-secret", combinedPullSecretName},
		},
	}
	adaptPullSecretShapeForChart("network-operator", vals)
	op := vals["operator"].(map[string]any)
	if got, want := op["imagePullSecrets"], []any{"my-secret", combinedPullSecretName}; !reflect.DeepEqual(got, want) {
		t.Fatalf("operator.imagePullSecrets = %#v, want %#v", got, want)
	}
	if op["repository"] != "nvcr.io/nvidia" {
		t.Fatal("sibling operator key dropped")
	}

	vals = map[string]any{
		"imagePullSecrets": injectedObjects(combinedPullSecretName),
		"operator":         map[string]any{"imagePullSecrets": []any{"my-secret"}},
	}
	adaptPullSecretShapeForChart("network-operator", vals)
	op = vals["operator"].(map[string]any)
	if got, want := op["imagePullSecrets"], []any{combinedPullSecretName, "my-secret"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("operator.imagePullSecrets = %#v, want %#v", got, want)
	}
}

func TestAdaptPullSecretShape_NoOpCases(t *testing.T) {
	// Other charts are untouched.
	vals := map[string]any{"imagePullSecrets": injectedObjects("s1")}
	adaptPullSecretShapeForChart("gpu-operator", vals)
	if _, ok := vals["operator"]; ok {
		t.Fatal("gpu-operator must not get operator.imagePullSecrets")
	}

	// Nothing injected (no credentials configured).
	vals = map[string]any{}
	adaptPullSecretShapeForChart("network-operator", vals)
	if len(vals) != 0 {
		t.Fatalf("expected no values, got %#v", vals)
	}

	// Unexpected parent shape is left alone.
	vals = map[string]any{"imagePullSecrets": injectedObjects("s1"), "operator": "weird"}
	adaptPullSecretShapeForChart("network-operator", vals)
	if vals["operator"] != "weird" {
		t.Fatalf("operator overwritten: %#v", vals["operator"])
	}

	// Unexpected leaf shape is left alone.
	vals = map[string]any{
		"imagePullSecrets": injectedObjects("s1"),
		"operator":         map[string]any{"imagePullSecrets": "s2"},
	}
	adaptPullSecretShapeForChart("network-operator", vals)
	if vals["operator"].(map[string]any)["imagePullSecrets"] != "s2" {
		t.Fatal("scalar operator.imagePullSecrets overwritten")
	}
}

// network-operator is catalogued as an NVIDIA component, so the nvidiaInjector
// path is the one hit in practice: operator.image is a string in this chart (the
// k8s-nim-operator operator.image.pullSecrets write is skipped) and the chart's
// default imagePullSecrets: [] gets the object-shaped ngc-secret prepended.
func TestAdaptPullSecretShape_AfterNvidiaInjector(t *testing.T) {
	vals := map[string]any{
		"imagePullSecrets": []any{},
		"operator":         map[string]any{"image": "network-operator", "repository": "nvcr.io/nvidia/cloud-native"},
	}
	injectNvidiaPullSecretRefs(vals)
	adaptPullSecretShapeForChart("network-operator", vals)

	op := vals["operator"].(map[string]any)
	if op["image"] != "network-operator" {
		t.Fatalf("operator.image changed: %#v", op["image"])
	}
	if got, want := op["imagePullSecrets"], []any{nvidiaImagePullSecretName}; !reflect.DeepEqual(got, want) {
		t.Fatalf("operator.imagePullSecrets = %#v, want %#v", got, want)
	}
}
