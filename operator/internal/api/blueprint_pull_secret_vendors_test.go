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

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func blueprintVendorsBody(vendors ...string) string {
	spec := map[string]any{"displayName": "Acme", "version": "1.0.0", "components": []any{
		map[string]any{"chartRepo": "r", "chartName": "runai-cluster", "chartVersion": "1", "vendor": "nvidia", "additionalPullSecretVendors": vendors},
	}}
	b, _ := json.Marshal(map[string]any{"spec": spec})
	return string(b)
}

func TestBlueprint_RejectsUnknownAdditionalPullSecretVendor(t *testing.T) {
	for _, method := range blueprintWriteMethods {
		w, _ := serveBlueprintWrite(t, method, blueprintVendorsBody("suse", "acme"), interceptor.Funcs{})
		if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), `components[runai-cluster].additionalPullSecretVendors: unknown vendor \"acme\"`) {
			t.Fatalf("%s: want 422 naming the vendor, got %d: %s", method, w.Code, w.Body.String())
		}
	}
}

func TestBlueprint_PersistsAdditionalPullSecretVendors(t *testing.T) {
	for _, method := range blueprintWriteMethods {
		w, c := serveBlueprintWrite(t, method, blueprintVendorsBody("suse"), interceptor.Funcs{})
		if w.Code != http.StatusCreated && w.Code != http.StatusOK {
			t.Fatalf("%s: want success, got %d: %s", method, w.Code, w.Body.String())
		}
		var bp aiplatformv1alpha1.Blueprint
		if err := c.Get(context.Background(), client.ObjectKey{Name: "acme-1-0-0"}, &bp); err != nil {
			t.Fatal(err)
		}
		if got := bp.Spec.Components[0].AdditionalPullSecretVendors; !reflect.DeepEqual(got, []aiplatformv1alpha1.ComponentVendor{"suse"}) {
			t.Fatalf("%s: stored additionalPullSecretVendors = %v", method, got)
		}
	}
}

func TestBlueprintCRDGuard_CoversAdditionalPullSecretVendors(t *testing.T) {
	sent := aiplatformv1alpha1.BlueprintSpec{Components: []aiplatformv1alpha1.BlueprintComponent{
		{ChartName: "a", AdditionalPullSecretVendors: []aiplatformv1alpha1.ComponentVendor{"suse"}},
		{ChartName: "b"},
	}}
	if !usesNewBlueprintFields(sent) {
		t.Fatal("additionalPullSecretVendors must trigger the CRD guard")
	}
	stored := aiplatformv1alpha1.BlueprintSpec{Components: []aiplatformv1alpha1.BlueprintComponent{{ChartName: "a"}, {ChartName: "b"}}}
	if got, want := droppedBlueprintFields(sent, stored), []string{"components[a].additionalPullSecretVendors"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("droppedBlueprintFields = %v, want %v", got, want)
	}
}
