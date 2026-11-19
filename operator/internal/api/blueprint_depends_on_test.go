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
	"slices"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func blueprintDependsOnBody(clusterDeps, backendDeps []string) string {
	comp := func(name, ns string, deps []string) map[string]any {
		return map[string]any{"chartRepo": "suse-ai", "chartName": name, "chartVersion": "2.0.0", "targetNamespace": ns, "dependsOn": deps}
	}
	spec := map[string]any{"displayName": "Acme", "version": "1.0.0", "components": []any{
		comp("runai-cluster", "runai", clusterDeps), comp("control-plane", "runai-backend", backendDeps),
	}}
	b, _ := json.Marshal(map[string]any{"spec": spec})
	return string(b)
}

func TestBlueprint_ValidatesDependsOn(t *testing.T) {
	cases := []struct {
		name                     string
		clusterDeps, backendDeps []string
		wantErr                  string
	}{
		{"unknown", []string{"ghost"}, nil, `component "runai-cluster" dependsOn unknown component "ghost"`},
		{"self", []string{"runai-cluster"}, nil, `component "runai-cluster" cannot depend on itself`},
		{"duplicate", []string{"control-plane", "control-plane"}, nil, `component "runai-cluster" lists duplicate dependency "control-plane"`},
		{"cycle", []string{"control-plane"}, []string{"runai-cluster"}, "component dependency cycle detected: runai-cluster -> control-plane -> runai-cluster"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, method := range blueprintWriteMethods {
				w, c := serveBlueprintWrite(t, method, blueprintDependsOnBody(tc.clusterDeps, tc.backendDeps), interceptor.Funcs{})
				var apiErr APIError
				_ = json.Unmarshal(w.Body.Bytes(), &apiErr)
				if w.Code != http.StatusUnprocessableEntity || apiErr.Code != ErrCodeInvalidInput || !strings.Contains(apiErr.Message, tc.wantErr) {
					t.Fatalf("%s: want 422 containing %q, got %d: %s", method, tc.wantErr, w.Code, w.Body.String())
				}
				var bp aiplatformv1alpha1.Blueprint
				err := c.Get(context.Background(), client.ObjectKey{Name: "acme-1-0-0"}, &bp)
				if method == http.MethodPost && !apierrors.IsNotFound(err) {
					t.Fatalf("create must not persist, Get err = %v", err)
				}
				if method == http.MethodPut && (err != nil || len(bp.Spec.Components) != 0) {
					t.Fatalf("update must not persist: err = %v, components = %+v", err, bp.Spec.Components)
				}
			}
		})
	}
}

func TestBlueprint_AcceptsValidDependsOn(t *testing.T) {
	for _, method := range blueprintWriteMethods {
		w, c := serveBlueprintWrite(t, method, blueprintDependsOnBody([]string{"control-plane"}, nil), interceptor.Funcs{})
		if w.Code != http.StatusCreated && w.Code != http.StatusOK {
			t.Fatalf("%s: want success, got %d: %s", method, w.Code, w.Body.String())
		}
		var bp aiplatformv1alpha1.Blueprint
		if err := c.Get(context.Background(), client.ObjectKey{Name: "acme-1-0-0"}, &bp); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(bp.Spec.Components[0].DependsOn, []string{"control-plane"}) {
			t.Fatalf("%s: stored dependsOn = %v, want [control-plane]", method, bp.Spec.Components[0].DependsOn)
		}
	}
}
