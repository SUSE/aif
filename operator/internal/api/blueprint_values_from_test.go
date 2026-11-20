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
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func blueprintBody(rollback string, valuesFrom any) string {
	comp := map[string]any{"chartRepo": "suse-ai", "chartName": "runai-cluster", "chartVersion": "2.0.0", "targetNamespace": "runai"}
	if valuesFrom != nil {
		comp["valuesFrom"] = valuesFrom
	}
	backend := map[string]any{"chartRepo": "suse-ai", "chartName": "control-plane", "chartVersion": "2.0.0", "targetNamespace": "runai-backend"}
	spec := map[string]any{"displayName": "Acme", "version": "1.0.0", "components": []any{comp, backend}}
	if rollback != "" {
		spec["rollback"] = rollback
	}
	b, _ := json.Marshal(map[string]any{"spec": spec})
	return string(b)
}

// serveBlueprintWrite sends body as a create (POST) or as an update (PUT) of
// acme-1-0-0, against a fake client; an update gets an existing acme-1-0-0.
func serveBlueprintWrite(t *testing.T, method, body string, funcs interceptor.Funcs) (*httptest.ResponseRecorder, client.Client) {
	t.Helper()
	b := fake.NewClientBuilder().WithScheme(newBlueprintScheme(t)).WithInterceptorFuncs(funcs)
	path := "/api/v1/blueprints"
	if method == http.MethodPut {
		b = b.WithObjects(&aiplatformv1alpha1.Blueprint{
			ObjectMeta: metav1.ObjectMeta{Name: "acme-1-0-0"},
			Spec:       aiplatformv1alpha1.BlueprintSpec{DisplayName: "Acme", Version: "1.0.0"},
		})
		path += "/acme-1-0-0"
	}
	c := b.Build()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	newBlueprintHandlerWithClient(t, c).ServeHTTP(w, req)
	return w, c
}

var blueprintWriteMethods = []string{http.MethodPost, http.MethodPut}

func TestBlueprint_ValidatesValuesFrom(t *testing.T) {
	ref := func(kv ...string) map[string]string {
		m := map[string]string{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return m
	}
	cases := []struct {
		name       string
		valuesFrom []map[string]any
		wantErr    string
	}{
		{"valid", []map[string]any{
			{"secretKeyRef": ref("name", "runai-cluster-values", "namespace", "runai-backend", "key", "values.yaml")},
			{"configMapKeyRef": ref("name", "runai.config")},
		}, ""},
		{"missing name", []map[string]any{{"secretKeyRef": ref("namespace", "ns")}}, "components[runai-cluster].valuesFrom[0]: name"},
		{"bad name", []map[string]any{{"secretKeyRef": ref("name", "Bad_Name")}}, "components[runai-cluster].valuesFrom[0]: name"},
		{"bad namespace", []map[string]any{{"configMapKeyRef": ref("name", "cfg")}, {"configMapKeyRef": ref("name", "cfg", "namespace", "with.dot")}},
			"components[runai-cluster].valuesFrom[1]: namespace"},
		{"own namespace", []map[string]any{{"secretKeyRef": ref("name", "s", "namespace", "runai")}}, ""},
		{"foreign namespace", []map[string]any{{"secretKeyRef": ref("name", "s", "namespace", "kube-system")}},
			`components[runai-cluster].valuesFrom[0]: namespace \"kube-system\" is not a namespace this Blueprint deploys to; use one of runai, runai-backend`},
		{"bad key", []map[string]any{{"secretKeyRef": ref("name", "s", "key", "a/b")}}, "components[runai-cluster].valuesFrom[0]: key"},
		{"no kind", []map[string]any{{}}, "components[runai-cluster].valuesFrom[0]: set exactly one of configMapKeyRef or secretKeyRef"},
		{"both kinds", []map[string]any{{"secretKeyRef": ref("name", "s"), "configMapKeyRef": ref("name", "c")}},
			"components[runai-cluster].valuesFrom[0]: set exactly one of configMapKeyRef or secretKeyRef"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, method := range blueprintWriteMethods {
				w, _ := serveBlueprintWrite(t, method, blueprintBody("", tc.valuesFrom), interceptor.Funcs{})
				if tc.wantErr == "" {
					if w.Code != http.StatusCreated && w.Code != http.StatusOK {
						t.Fatalf("%s: want success, got %d: %s", method, w.Code, w.Body.String())
					}
					continue
				}
				if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), tc.wantErr) {
					t.Fatalf("%s: want 422 containing %q, got %d: %s", method, tc.wantErr, w.Code, w.Body.String())
				}
			}
		})
	}
}

// outdatedBlueprintCRD simulates a Blueprint CRD that predates rollback and
// valuesFrom: dry-run writes come back with both pruned. dryRuns counts them.
func outdatedBlueprintCRD(dryRuns *int) interceptor.Funcs {
	prune := func(obj client.Object) {
		if bp, ok := obj.(*aiplatformv1alpha1.Blueprint); ok {
			bp.Spec.Rollback = ""
			for i := range bp.Spec.Components {
				bp.Spec.Components[i].ValuesFrom = nil
				bp.Spec.Components[i].DependsOn = nil
				bp.Spec.Components[i].Kustomize = nil
			}
		}
	}
	return interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			o := &client.CreateOptions{}
			o.ApplyOptions(opts)
			if slices.Contains(o.DryRun, metav1.DryRunAll) {
				*dryRuns++
				prune(obj)
			}
			return c.Create(ctx, obj, opts...)
		},
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			o := &client.UpdateOptions{}
			o.ApplyOptions(opts)
			if slices.Contains(o.DryRun, metav1.DryRunAll) {
				*dryRuns++
				prune(obj)
			}
			return c.Update(ctx, obj, opts...)
		},
	}
}

var runaiValuesFrom = []map[string]any{{"secretKeyRef": map[string]string{"name": "runai-cluster-values"}}}

func TestCreateBlueprint_OutdatedCRDIsRejectedWithoutPersisting(t *testing.T) {
	dryRuns := 0
	w, c := serveBlueprintWrite(t, http.MethodPost, blueprintBody("Disabled", runaiValuesFrom), outdatedBlueprintCRD(&dryRuns))
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "Blueprint CRD is outdated") ||
		!strings.Contains(w.Body.String(), "charts/aif-operator/crds/ai-factory.suse.com_blueprints.yaml") {
		t.Fatalf("want 422 outdated-CRD error, got %d: %s", w.Code, w.Body.String())
	}
	for _, field := range []string{"rollback", "components[runai-cluster].valuesFrom"} {
		if !strings.Contains(w.Body.String(), field) {
			t.Errorf("error should name %s: %s", field, w.Body.String())
		}
	}
	var bp aiplatformv1alpha1.Blueprint
	if err := c.Get(context.Background(), client.ObjectKey{Name: "acme-1-0-0"}, &bp); !apierrors.IsNotFound(err) {
		t.Fatalf("blueprint must not be persisted, Get err = %v", err)
	}
	if dryRuns != 1 {
		t.Fatalf("dry-run writes = %d, want 1", dryRuns)
	}
}

func TestUpdateBlueprint_OutdatedCRDIsRejectedWithoutPersisting(t *testing.T) {
	dryRuns := 0
	w, c := serveBlueprintWrite(t, http.MethodPut, blueprintBody("Disabled", runaiValuesFrom), outdatedBlueprintCRD(&dryRuns))
	want := "Blueprint CRD is outdated (rollback, components[runai-cluster].valuesFrom were dropped)"
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), want) {
		t.Fatalf("want 422 containing %q, got %d: %s", want, w.Code, w.Body.String())
	}
	if dryRuns != 1 {
		t.Fatalf("dry-run writes = %d, want 1", dryRuns)
	}
	var bp aiplatformv1alpha1.Blueprint
	if err := c.Get(context.Background(), client.ObjectKey{Name: "acme-1-0-0"}, &bp); err != nil {
		t.Fatal(err)
	}
	if want := (aiplatformv1alpha1.BlueprintSpec{DisplayName: "Acme", Version: "1.0.0"}); !reflect.DeepEqual(bp.Spec, want) {
		t.Fatalf("stored spec changed: %+v", bp.Spec)
	}
}

func TestBlueprintWrites_WithoutNewFieldsSkipDryRun(t *testing.T) {
	dryRuns := 0
	for _, method := range blueprintWriteMethods {
		w, _ := serveBlueprintWrite(t, method, blueprintBody("", nil), outdatedBlueprintCRD(&dryRuns))
		if w.Code != http.StatusCreated && w.Code != http.StatusOK {
			t.Fatalf("%s: want success, got %d: %s", method, w.Code, w.Body.String())
		}
	}
	if dryRuns != 0 {
		t.Fatalf("dry-run writes = %d, want 0 for requests without rollback/valuesFrom", dryRuns)
	}
}

func TestBlueprintWrites_CurrentCRDPersistsNewFields(t *testing.T) {
	for _, method := range blueprintWriteMethods {
		w, c := serveBlueprintWrite(t, method, blueprintBody("Disabled", runaiValuesFrom), interceptor.Funcs{})
		if w.Code != http.StatusCreated && w.Code != http.StatusOK {
			t.Fatalf("%s: want success, got %d: %s", method, w.Code, w.Body.String())
		}
		var bp aiplatformv1alpha1.Blueprint
		if err := c.Get(context.Background(), client.ObjectKey{Name: "acme-1-0-0"}, &bp); err != nil {
			t.Fatal(err)
		}
		if bp.Spec.Rollback != aiplatformv1alpha1.RollbackDisabled || len(bp.Spec.Components[0].ValuesFrom) != 1 {
			t.Fatalf("%s: stored spec = %+v, want rollback and valuesFrom intact", method, bp.Spec)
		}
	}
}

func TestBlueprintCRDGuard_CoversOrderingAndOverlays(t *testing.T) {
	k := &aiplatformv1alpha1.ComponentKustomize{Dir: "overlay"}
	sent := aiplatformv1alpha1.BlueprintSpec{Components: []aiplatformv1alpha1.BlueprintComponent{
		{ChartName: "a", DependsOn: []string{"b"}},
		{ChartName: "b", Kustomize: k},
		{ChartName: "c"},
	}}
	if !usesNewBlueprintFields(sent) {
		t.Fatal("dependsOn/kustomize must trigger the CRD guard")
	}
	if usesNewBlueprintFields(aiplatformv1alpha1.BlueprintSpec{Components: []aiplatformv1alpha1.BlueprintComponent{{ChartName: "c"}}}) {
		t.Fatal("plain components must not trigger the CRD guard")
	}
	stored := aiplatformv1alpha1.BlueprintSpec{Components: []aiplatformv1alpha1.BlueprintComponent{{ChartName: "a"}, {ChartName: "b"}, {ChartName: "c"}}}
	want := []string{"components[a].dependsOn", "components[b].kustomize"}
	if got := droppedBlueprintFields(sent, stored); !reflect.DeepEqual(got, want) {
		t.Fatalf("droppedBlueprintFields = %v, want %v", got, want)
	}
	if got := droppedBlueprintFields(sent, sent); len(got) != 0 {
		t.Fatalf("nothing dropped, got %v", got)
	}
}
