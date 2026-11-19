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
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// advancedDecision answers the advanced-customization SubjectAccessReview
// with allowed/err and allows every route SAR. It fails the test when the
// advanced check asks anything but cluster-wide update on blueprints.
func advancedDecision(t *testing.T, allowed bool, err error, asked *int) func(*authorizationv1.SubjectAccessReview) (bool, error) {
	return func(sar *authorizationv1.SubjectAccessReview) (bool, error) {
		attrs := sar.Spec.ResourceAttributes
		if attrs.Resource != resourceBlueprints || attrs.Verb == verbList {
			return true, nil
		}
		*asked++
		want := authorizationv1.ResourceAttributes{Group: "ai-factory.suse.com", Version: "v1alpha1", Verb: verbUpdate, Resource: resourceBlueprints}
		if *attrs != want || sar.Spec.User != testUser.GetName() {
			t.Errorf("advanced SAR = %+v for %q, want %+v", *attrs, sar.Spec.User, want)
		}
		return allowed, err
	}
}

// advancedMux serves the AIWorkload and capabilities handlers behind an
// Authorizer whose SARs are answered by decide.
func advancedMux(t *testing.T, decide func(*authorizationv1.SubjectAccessReview) (bool, error), objs ...client.Object) (http.Handler, client.Client) {
	t.Helper()
	_, c := newAIWorkloadHandlerWithInterceptor(t, interceptor.Funcs{}, objs...)
	a := NewAuthorizer(sarClient(nil, decide), DefaultRules(testNS))
	NewAIWorkloadHandler(c).Register(a)
	NewCapabilitiesHandler().Register(a)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.ServeHTTP(w, withUser(r, testUser))
	}), c
}

func storedBlueprintWorkload(ov ...aiplatformv1alpha1.ComponentValueOverride) *aiplatformv1alpha1.AIWorkload {
	return &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			DisplayName: "wl", TargetNamespace: "ai", TargetClusters: []string{"local"},
			Source: aiplatformv1alpha1.AIWorkloadSource{
				SourceType: aiplatformv1alpha1.AIWorkloadSourceBlueprint,
				Blueprint:  &aiplatformv1alpha1.BlueprintSource{Name: "rag", Version: "1.0.0"},
			},
			ComponentValues: ov,
		},
	}
}

func storedOverride() aiplatformv1alpha1.ComponentValueOverride {
	deps := []string{"milvus"}
	return aiplatformv1alpha1.ComponentValueOverride{
		ComponentName: "open-webui",
		DependsOn:     &deps,
		Kustomize:     &aiplatformv1alpha1.ComponentKustomize{Dir: "overlay", Files: map[string]string{"overlay/kustomization.yaml": "patches: []\n"}},
	}
}

func assertAdvancedForbidden(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), ErrCodeForbidden) ||
		!strings.Contains(rec.Body.String(), "permission to update Blueprints") {
		t.Fatalf("want 403 naming the permission, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateAIWorkload_AdvancedCustomizationPermission(t *testing.T) {
	bp := testBlueprint("rag-1-0-0", "rag", "1.0.0", "milvus", "open-webui")
	for _, allowed := range []bool{true, false} {
		asked := 0
		h, c := advancedMux(t, advancedDecision(t, allowed, nil, &asked), bp)
		rec := serve(h, createReq(blueprintSpec(overrideComponentValues())))
		if asked != 1 {
			t.Fatalf("allowed=%v: advanced SARs = %d, want 1", allowed, asked)
		}
		if allowed {
			if rec.Code != http.StatusCreated {
				t.Fatalf("allowed: want 201, got %d: %s", rec.Code, rec.Body.String())
			}
			continue
		}
		assertAdvancedForbidden(t, rec)
		var wl aiplatformv1alpha1.AIWorkload
		if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "wl"}, &wl); !apierrors.IsNotFound(err) {
			t.Fatalf("workload must not be persisted, Get err = %v", err)
		}
		var ns corev1.Namespace
		if err := c.Get(context.Background(), types.NamespacedName{Name: "default"}, &ns); !apierrors.IsNotFound(err) {
			t.Fatalf("namespace must not be applied, Get err = %v", err)
		}
	}
}

func TestCreateAIWorkload_ValuesOnlyDoesNotAskAdvancedPermission(t *testing.T) {
	bp := testBlueprint("rag-1-0-0", "rag", "1.0.0", "milvus", "open-webui")
	asked := 0
	h, _ := advancedMux(t, advancedDecision(t, false, nil, &asked), bp)
	values := []map[string]any{{"componentName": "milvus", "values": map[string]any{"replicas": 3}}}
	if rec := serve(h, createReq(blueprintSpec(values))); rec.Code != http.StatusCreated || asked != 0 {
		t.Fatalf("want 201 without an advanced SAR, got %d (asked %d): %s", rec.Code, asked, rec.Body.String())
	}
}

func TestUpdateAIWorkload_AdvancedCustomizationDenied(t *testing.T) {
	bp := testBlueprint("rag-1-0-0", "rag", "1.0.0", "milvus", "open-webui")
	modified := inlineKustomize()
	modified["files"] = map[string]string{"overlay/kustomization.yaml": "resources: [extra.yaml]\n"}
	cases := map[string]struct {
		stored []aiplatformv1alpha1.ComponentValueOverride
		sent   map[string]any
	}{
		"add kustomize":      {nil, map[string]any{"componentName": "open-webui", "kustomize": inlineKustomize()}},
		"add dependsOn":      {nil, map[string]any{"componentName": "open-webui", "dependsOn": []string{"milvus"}}},
		"modify kustomize":   {[]aiplatformv1alpha1.ComponentValueOverride{storedOverride()}, map[string]any{"componentName": "open-webui", "dependsOn": []string{"milvus"}, "kustomize": modified}},
		"change dependsOn":   {[]aiplatformv1alpha1.ComponentValueOverride{storedOverride()}, map[string]any{"componentName": "open-webui", "dependsOn": []string{}, "kustomize": inlineKustomize()}},
		"first entry only":   {[]aiplatformv1alpha1.ComponentValueOverride{{ComponentName: "open-webui"}, storedOverride()}, map[string]any{"componentName": "open-webui", "dependsOn": []string{"milvus"}, "kustomize": inlineKustomize()}},
		"move to other comp": {[]aiplatformv1alpha1.ComponentValueOverride{storedOverride()}, map[string]any{"componentName": "milvus", "kustomize": inlineKustomize()}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			asked := 0
			h, c := advancedMux(t, advancedDecision(t, false, nil, &asked), bp, storedBlueprintWorkload(tc.stored...))
			before := storedWorkload(t, c).Spec
			assertAdvancedForbidden(t, serve(h, patchReq(blueprintSpec([]map[string]any{tc.sent}))))
			if after := storedWorkload(t, c).Spec; !reflect.DeepEqual(before, after) {
				t.Fatalf("stored spec changed:\nbefore %+v\nafter  %+v", before, after)
			}
		})
	}
}

func TestUpdateAIWorkload_AdvancedCustomizationAllowed(t *testing.T) {
	bp := testBlueprint("rag-1-0-0", "rag", "1.0.0", "milvus", "open-webui")
	asked := 0
	h, c := advancedMux(t, advancedDecision(t, true, nil, &asked), bp, storedBlueprintWorkload())
	if rec := serve(h, patchReq(blueprintSpec(overrideComponentValues()))); rec.Code != http.StatusOK || asked != 1 {
		t.Fatalf("want 200 after one advanced SAR, got %d (asked %d): %s", rec.Code, asked, rec.Body.String())
	}
	if ov := storedWorkload(t, c).Spec.ComponentValues[0]; ov.Kustomize == nil || ov.DependsOn == nil {
		t.Fatalf("stored override = %+v, want dependsOn and kustomize", ov)
	}
}

func TestUpdateAIWorkload_DeniedUserKeepsOrRemovesAdvancedFields(t *testing.T) {
	bp := testBlueprint("rag-1-0-0", "rag", "1.0.0", "milvus", "open-webui")
	cases := map[string]map[string]any{
		"unchanged, values edited": {"componentName": "open-webui", "dependsOn": []string{"milvus"}, "kustomize": inlineKustomize(), "values": map[string]any{"replicas": 2}},
		"removed":                  {"componentName": "open-webui", "values": map[string]any{"replicas": 2}},
	}
	for name, sent := range cases {
		t.Run(name, func(t *testing.T) {
			asked := 0
			h, c := advancedMux(t, advancedDecision(t, false, nil, &asked), bp, storedBlueprintWorkload(storedOverride()))
			if rec := serve(h, patchReq(blueprintSpec([]map[string]any{sent}))); rec.Code != http.StatusOK || asked != 0 {
				t.Fatalf("want 200 without an advanced SAR, got %d (asked %d): %s", rec.Code, asked, rec.Body.String())
			}
			if ov := storedWorkload(t, c).Spec.ComponentValues[0]; ov.Values == nil {
				t.Fatalf("values edit not stored: %+v", ov)
			}
		})
	}
}

func TestUpdateAIWorkload_StaleDependsOnPruning(t *testing.T) {
	bp := testBlueprint("rag-1-0-0", "rag", "1.0.0", "milvus", "open-webui")
	cases := map[string]struct {
		stored, sent []string
		want         int
	}{
		"stale name pruned":         {[]string{"milvus", "gone"}, []string{"milvus"}, http.StatusOK},
		"all stale pruned to empty": {[]string{"gone", "old"}, []string{}, http.StatusOK},
		"existing name dropped":     {[]string{"milvus", "gone"}, []string{}, http.StatusForbidden},
		"self-reference pruned":     {[]string{"open-webui", "milvus", "gone"}, []string{"milvus"}, http.StatusOK},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			stored := aiplatformv1alpha1.ComponentValueOverride{ComponentName: "open-webui", DependsOn: &tc.stored}
			asked := 0
			h, c := advancedMux(t, advancedDecision(t, false, nil, &asked), bp, storedBlueprintWorkload(stored))
			rec := serve(h, patchReq(blueprintSpec([]map[string]any{{"componentName": "open-webui", "dependsOn": tc.sent}})))
			if tc.want == http.StatusForbidden {
				assertAdvancedForbidden(t, rec)
				return
			}
			if rec.Code != tc.want || asked != 0 {
				t.Fatalf("want %d without an advanced SAR, got %d (asked %d): %s", tc.want, rec.Code, asked, rec.Body.String())
			}
			if got := storedWorkload(t, c).Spec.ComponentValues[0].DependsOn; got == nil || !sameStringSet(*got, tc.sent) {
				t.Fatalf("stored dependsOn = %v, want %v", got, tc.sent)
			}
		})
	}
}

func TestAIWorkloadWrites_AdvancedCheckErrorIs500(t *testing.T) {
	bp := testBlueprint("rag-1-0-0", "rag", "1.0.0", "milvus", "open-webui")
	asked := 0
	h, c := advancedMux(t, advancedDecision(t, true, errors.New("apiserver down: secret detail"), &asked), bp)
	rec := serve(h, createReq(blueprintSpec(overrideComponentValues())))
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "secret detail") {
		t.Fatalf("want 500 without details, got %d: %s", rec.Code, rec.Body.String())
	}
	var wl aiplatformv1alpha1.AIWorkload
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "wl"}, &wl); !apierrors.IsNotFound(err) {
		t.Fatalf("workload must not be persisted, Get err = %v", err)
	}
}

func TestAIWorkloadWrites_WithoutAuthorizerFailClosed(t *testing.T) {
	bp := testBlueprint("rag-1-0-0", "rag", "1.0.0", "milvus", "open-webui")
	_, c := newAIWorkloadHandlerWithClient(t, bp, storedBlueprintWorkload())
	h := NewAIWorkloadHandler(c)

	req := createReq(blueprintSpec(overrideComponentValues()))
	req.SetPathValue("namespace", "default")
	rec := httptest.NewRecorder()
	h.createAIWorkload(rec, req)
	assertAdvancedForbidden(t, rec)

	req = patchReq(blueprintSpec(overrideComponentValues()))
	req.SetPathValue("namespace", "default")
	req.SetPathValue("name", "wl")
	rec = httptest.NewRecorder()
	h.updateAIWorkload(rec, req)
	assertAdvancedForbidden(t, rec)
}

func TestCapabilities(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		asked := 0
		h, _ := advancedMux(t, advancedDecision(t, allowed, nil, &asked))
		rec := serve(h, httptest.NewRequest(http.MethodGet, "/api/v1/capabilities", nil))
		want := `{"advancedCustomization":false}`
		if allowed {
			want = `{"advancedCustomization":true}`
		}
		if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != want || asked != 1 {
			t.Fatalf("allowed=%v: got %d %s (asked %d), want 200 %s", allowed, rec.Code, rec.Body.String(), asked, want)
		}
	}
}

func TestCapabilities_SARErrorIs500(t *testing.T) {
	asked := 0
	h, _ := advancedMux(t, advancedDecision(t, true, errors.New("boom"), &asked))
	rec := serve(h, httptest.NewRequest(http.MethodGet, "/api/v1/capabilities", nil))
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "boom") {
		t.Fatalf("want 500 without details, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAdvancedChanges(t *testing.T) {
	deps := func(d ...string) *[]string { return &d }
	k := func(dir string) *aiplatformv1alpha1.ComponentKustomize {
		return &aiplatformv1alpha1.ComponentKustomize{Dir: dir}
	}
	ov := func(name string, d *[]string, kz *aiplatformv1alpha1.ComponentKustomize) aiplatformv1alpha1.ComponentValueOverride {
		return aiplatformv1alpha1.ComponentValueOverride{ComponentName: name, DependsOn: d, Kustomize: kz}
	}
	spec := func(ovs ...aiplatformv1alpha1.ComponentValueOverride) *aiplatformv1alpha1.AIWorkloadSpec {
		return &aiplatformv1alpha1.AIWorkloadSpec{ComponentValues: ovs}
	}
	known := map[string]bool{"a": true, "b": true, "c": true}
	cases := map[string]struct {
		existing *aiplatformv1alpha1.AIWorkloadSpec
		next     *aiplatformv1alpha1.AIWorkloadSpec
		known    map[string]bool
		want     bool
	}{
		"create without advanced fields":    {nil, spec(ov("a", nil, nil)), known, false},
		"create with kustomize":             {nil, spec(ov("a", nil, k("o"))), known, true},
		"create with empty dependsOn":       {nil, spec(ov("a", deps(), nil)), known, true},
		"nil to empty dependsOn":            {spec(ov("a", nil, nil)), spec(ov("a", deps(), nil)), known, true},
		"empty dependsOn unchanged":         {spec(ov("a", deps(), nil)), spec(ov("a", deps(), nil)), known, false},
		"dependsOn changed":                 {spec(ov("a", deps("b"), nil)), spec(ov("a", deps("c"), nil)), known, true},
		"dependsOn reordered":               {spec(ov("a", deps("b", "c"), nil)), spec(ov("a", deps("c", "b"), nil)), known, false},
		"dependsOn name added":              {spec(ov("a", deps("b"), nil)), spec(ov("a", deps("b", "c"), nil)), known, true},
		"dependsOn removed":                 {spec(ov("a", deps("b"), k("o"))), spec(ov("a", nil, k("o"))), known, false},
		"stale name pruned":                 {spec(ov("a", deps("b", "gone"), nil)), spec(ov("a", deps("b"), nil)), known, false},
		"all stale pruned to empty":         {spec(ov("a", deps("gone", "old"), nil)), spec(ov("a", deps(), nil)), known, false},
		"existing name dropped":             {spec(ov("a", deps("b", "gone"), nil)), spec(ov("a", deps(), nil)), known, true},
		"existing name dropped, no stale":   {spec(ov("a", deps("b", "c"), nil)), spec(ov("a", deps("b"), nil)), known, true},
		"self-reference pruned":             {spec(ov("a", deps("a", "b", "gone"), nil)), spec(ov("a", deps("b"), nil)), known, false},
		"self-reference kept without known": {spec(ov("a", deps("a", "b"), nil)), spec(ov("a", deps("b"), nil)), nil, true},
		"pruned and name added":             {spec(ov("a", deps("gone"), nil)), spec(ov("a", deps("b"), nil)), known, true},
		"unknown Blueprint: exact only":     {spec(ov("a", deps("b"), nil)), spec(ov("a", deps("b"), nil)), nil, false},
		"unknown Blueprint: no pruning":     {spec(ov("a", deps("b", "gone"), nil)), spec(ov("a", deps("b"), nil)), nil, true},
		"kustomize changed":                 {spec(ov("a", nil, k("o"))), spec(ov("a", nil, k("p"))), known, true},
		"kustomize removed":                 {spec(ov("a", nil, k("o"))), spec(ov("a", nil, nil)), known, false},
		"component order independent":       {spec(ov("a", deps("b"), nil), ov("b", nil, k("o"))), spec(ov("b", nil, k("o")), ov("a", deps("b"), nil)), known, false},
		"moved to another component":        {spec(ov("a", nil, k("o"))), spec(ov("b", nil, k("o"))), known, true},
		"only first stored entry counts":    {spec(ov("a", nil, nil), ov("a", deps("b"), k("o"))), spec(ov("a", nil, k("o"))), known, true},
		"first stored dependsOn counts":     {spec(ov("a", nil, nil), ov("a", deps("b"), nil)), spec(ov("a", deps("b"), nil)), known, true},
		"first stored entry kept":           {spec(ov("a", deps("b"), k("o")), ov("a", nil, nil)), spec(ov("a", deps("b"), k("o"))), known, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := advancedChanges(tc.existing, *tc.next, tc.known); got != tc.want {
				t.Fatalf("advancedChanges = %v, want %v", got, tc.want)
			}
		})
	}
}
