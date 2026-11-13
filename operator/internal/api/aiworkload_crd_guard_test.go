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
	"bytes"
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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// outdatedCRD simulates an AIWorkload CRD that predates componentValues
// kustomize/dependsOn: the API server prunes them. Only dry-run writes are
// pruned here, which is enough to prove the handler checks before persisting;
// dryRuns counts dry-run AIWorkload writes.
func outdatedCRD(dryRuns *int) interceptor.Funcs {
	prune := func(obj client.Object) {
		if wl, ok := obj.(*aiplatformv1alpha1.AIWorkload); ok {
			for i := range wl.Spec.ComponentValues {
				wl.Spec.ComponentValues[i].Kustomize = nil
				wl.Spec.ComponentValues[i].DependsOn = nil
			}
		}
	}
	return interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			o := &client.CreateOptions{}
			o.ApplyOptions(opts)
			if _, ok := obj.(*aiplatformv1alpha1.AIWorkload); ok && slices.Contains(o.DryRun, metav1.DryRunAll) {
				*dryRuns++
				prune(obj)
			}
			return c.Create(ctx, obj, opts...)
		},
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			o := &client.PatchOptions{}
			o.ApplyOptions(opts)
			if _, ok := obj.(*aiplatformv1alpha1.AIWorkload); ok && slices.Contains(o.DryRun, metav1.DryRunAll) {
				*dryRuns++
				prune(obj)
			}
			return c.Patch(ctx, obj, patch, opts...)
		},
	}
}

func overrideComponentValues() []map[string]any {
	return []map[string]any{{"componentName": "open-webui", "dependsOn": []string{"milvus"}, "kustomize": inlineKustomize()}}
}

func patchReq(spec map[string]any) *http.Request {
	body, _ := json.Marshal(map[string]any{"metadata": map[string]string{"name": "wl"}, "spec": spec})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/namespaces/default/aiworkloads/wl", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestCreateAIWorkload_OutdatedCRDIsRejectedWithoutPersisting(t *testing.T) {
	bp := testBlueprint("rag-1-0-0", "rag", "1.0.0", "milvus", "open-webui")
	dryRuns := 0
	h, c := newAIWorkloadHandlerWithInterceptor(t, outdatedCRD(&dryRuns), bp)

	rec := serve(h, createReq(blueprintSpec(overrideComponentValues())))
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "AIWorkload CRD is outdated") {
		t.Fatalf("want 422 outdated-CRD error, got %d: %s", rec.Code, rec.Body.String())
	}
	for _, field := range []string{"componentValues[open-webui].kustomize", "componentValues[open-webui].dependsOn"} {
		if !strings.Contains(rec.Body.String(), field) {
			t.Errorf("error should name %s: %s", field, rec.Body.String())
		}
	}
	var wl aiplatformv1alpha1.AIWorkload
	err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "wl"}, &wl)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("workload must not be persisted, Get err = %v", err)
	}
}

func TestUpdateAIWorkload_OutdatedCRDIsRejectedWithoutPersisting(t *testing.T) {
	bp := testBlueprint("rag-1-0-0", "rag", "1.0.0", "milvus", "open-webui")
	existing := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			DisplayName: "wl", TargetNamespace: "ai", TargetClusters: []string{"local"},
			Source: aiplatformv1alpha1.AIWorkloadSource{
				SourceType: aiplatformv1alpha1.AIWorkloadSourceBlueprint,
				Blueprint:  &aiplatformv1alpha1.BlueprintSource{Name: "rag", Version: "1.0.0"},
			},
		},
	}
	dryRuns := 0
	h, c := newAIWorkloadHandlerWithInterceptor(t, outdatedCRD(&dryRuns), bp, existing)
	before := storedWorkload(t, c).Spec

	rec := serve(h, patchReq(blueprintSpec(overrideComponentValues())))
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "AIWorkload CRD is outdated") {
		t.Fatalf("want 422 outdated-CRD error, got %d: %s", rec.Code, rec.Body.String())
	}
	if after := storedWorkload(t, c).Spec; !reflect.DeepEqual(before, after) {
		t.Fatalf("stored spec changed:\nbefore %+v\nafter  %+v", before, after)
	}
}

func TestAIWorkloadWrites_WithoutNewFieldsSkipDryRun(t *testing.T) {
	bp := testBlueprint("rag-1-0-0", "rag", "1.0.0", "milvus", "open-webui")
	dryRuns := 0
	h, _ := newAIWorkloadHandlerWithInterceptor(t, outdatedCRD(&dryRuns), bp)

	values := []map[string]any{{"componentName": "milvus", "values": map[string]any{"replicas": 3}}}
	if rec := serve(h, createReq(blueprintSpec(values))); rec.Code != http.StatusCreated {
		t.Fatalf("create: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := serve(h, patchReq(blueprintSpec(values))); rec.Code != http.StatusOK {
		t.Fatalf("update: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if dryRuns != 0 {
		t.Fatalf("dry-run writes = %d, want 0 for requests without kustomize/dependsOn", dryRuns)
	}
}

func TestUpdateAIWorkload_AcceptsValidOverrides(t *testing.T) {
	bp := testBlueprint("rag-1-0-0", "rag", "1.0.0", "milvus", "open-webui")
	h, c := newAIWorkloadHandlerWithInterceptor(t, interceptor.Funcs{}, bp)
	if rec := serve(h, createReq(blueprintSpec(nil))); rec.Code != http.StatusCreated {
		t.Fatalf("create: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := serve(h, patchReq(blueprintSpec(overrideComponentValues()))); rec.Code != http.StatusOK {
		t.Fatalf("update: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	ov := storedWorkload(t, c).Spec.ComponentValues[0]
	if ov.DependsOn == nil || ov.Kustomize == nil {
		t.Fatalf("stored override = %+v, want dependsOn and kustomize intact", ov)
	}
}

func TestDroppedOverrideFields(t *testing.T) {
	deps := []string{}
	k := &aiplatformv1alpha1.ComponentKustomize{Dir: "overlay"}
	sent := []aiplatformv1alpha1.ComponentValueOverride{
		{ComponentName: "a", DependsOn: &deps, Kustomize: k},
		{ComponentName: "b", Kustomize: k},
		{ComponentName: "c"},
	}
	stored := []aiplatformv1alpha1.ComponentValueOverride{
		{ComponentName: "a", DependsOn: &deps},
		{ComponentName: "b"},
		{ComponentName: "c"},
	}
	got := droppedOverrideFields(sent, stored)
	want := []string{"componentValues[a].kustomize", "componentValues[b].kustomize"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("droppedOverrideFields = %v, want %v", got, want)
	}
	if got := droppedOverrideFields(sent, sent); len(got) != 0 {
		t.Fatalf("nothing dropped, got %v", got)
	}
}
