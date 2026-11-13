package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// handlerWithObjects mirrors newAIWorkloadHandler (aiworkload_test.go) but
// accepts extra seed objects (e.g. a Blueprint CR) that helper has no way to
// take. corev1 must be registered: createAIWorkload's namespace-ensure step
// does a server-side-apply Patch of a corev1.Namespace before creating the
// AIWorkload, which fails against a scheme that doesn't know that type.
func handlerWithObjects(t *testing.T, objs ...client.Object) http.Handler {
	t.Helper()
	s := kruntime.NewScheme()
	_ = aiplatformv1alpha1.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	c := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&aiplatformv1alpha1.AIWorkload{}).WithObjects(objs...).Build()
	mux := http.NewServeMux()
	NewAIWorkloadHandler(c).Register(mux)
	return mux
}

func testBlueprint(crName, family, version string, componentNames ...string) *aiplatformv1alpha1.Blueprint {
	comps := make([]aiplatformv1alpha1.BlueprintComponent, 0, len(componentNames))
	for _, name := range componentNames {
		comps = append(comps, aiplatformv1alpha1.BlueprintComponent{ChartRepo: "suse-ai", ChartName: name, ChartVersion: "1.0.0"})
	}
	return &aiplatformv1alpha1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{Name: crName},
		Spec:       aiplatformv1alpha1.BlueprintSpec{DisplayName: family, Version: version, Components: comps},
	}
}

func createReq(spec map[string]any) *http.Request {
	body, _ := json.Marshal(map[string]any{"metadata": map[string]string{"name": "wl"}, "spec": spec})
	req := httptest.NewRequest("POST", "/api/v1/namespaces/default/aiworkloads", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestCreateAIWorkload_RejectsUnknownComponentValueName(t *testing.T) {
	bp := testBlueprint("rag-1-0-0", "rag", "1.0.0", "milvus", "open-webui")
	h := handlerWithObjects(t, bp)

	req := createReq(map[string]any{
		"displayName": "wl", "targetNamespace": "ai", "targetClusters": []string{"local"},
		"source":          map[string]any{"sourceType": "Blueprint", "blueprint": map[string]string{"name": "rag", "version": "1.0.0"}},
		"componentValues": []map[string]any{{"componentName": "not-a-real-component"}},
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateAIWorkload_AcceptsKnownComponentValueName(t *testing.T) {
	bp := testBlueprint("rag-1-0-0", "rag", "1.0.0", "milvus", "open-webui")
	h := handlerWithObjects(t, bp)

	req := createReq(map[string]any{
		"displayName": "wl", "targetNamespace": "ai", "targetClusters": []string{"local"},
		"source":          map[string]any{"sourceType": "Blueprint", "blueprint": map[string]string{"name": "rag", "version": "1.0.0"}},
		"componentValues": []map[string]any{{"componentName": "milvus", "values": map[string]any{"replicas": 3}}},
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateAIWorkload_RejectsAllComponentsDisabled(t *testing.T) {
	bp := testBlueprint("rag-1-0-0", "rag", "1.0.0", "milvus", "open-webui")
	h := handlerWithObjects(t, bp)

	req := createReq(map[string]any{
		"displayName": "wl", "targetNamespace": "ai", "targetClusters": []string{"local"},
		"source": map[string]any{"sourceType": "Blueprint", "blueprint": map[string]string{"name": "rag", "version": "1.0.0"}},
		"componentValues": []map[string]any{
			{"componentName": "milvus", "enabled": false},
			{"componentName": "open-webui", "enabled": false},
		},
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateAIWorkload_AcceptsSomeComponentsDisabled(t *testing.T) {
	bp := testBlueprint("rag-1-0-0", "rag", "1.0.0", "milvus", "open-webui")
	h := handlerWithObjects(t, bp)

	req := createReq(map[string]any{
		"displayName": "wl", "targetNamespace": "ai", "targetClusters": []string{"local"},
		"source": map[string]any{"sourceType": "Blueprint", "blueprint": map[string]string{"name": "rag", "version": "1.0.0"}},
		"componentValues": []map[string]any{
			{"componentName": "milvus", "enabled": false},
		},
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestEffectiveComponentEnabled(t *testing.T) {
	cases := []struct {
		name      string
		overrides []aiplatformv1alpha1.ComponentValueOverride
		want      bool
	}{
		{"no overrides defaults to enabled", nil, true},
		{"override for a different component is ignored", []aiplatformv1alpha1.ComponentValueOverride{
			{ComponentName: "open-webui", Enabled: boolPtr(false)},
		}, true},
		{"nil Enabled on a matching override defaults to enabled", []aiplatformv1alpha1.ComponentValueOverride{
			{ComponentName: "milvus"},
		}, true},
		{"Enabled=false on a matching override disables it", []aiplatformv1alpha1.ComponentValueOverride{
			{ComponentName: "milvus", Enabled: boolPtr(false)},
		}, false},
		{"first matching override wins on a duplicate componentName", []aiplatformv1alpha1.ComponentValueOverride{
			{ComponentName: "milvus", Enabled: boolPtr(true)},
			{ComponentName: "milvus", Enabled: boolPtr(false)},
		}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := effectiveComponentEnabled(c.overrides, "milvus"); got != c.want {
				t.Errorf("got %v want %v", got, c.want)
			}
		})
	}
}

func boolPtr(b bool) *bool { return &b }

// TestCreateAIWorkload_ConflictingOverrideAgreesWithController guards that the validator's
// "at least one enabled" check resolves a duplicate/conflicting componentName the same way
// isComponentEnabled (blueprint.go, package aiworkload) resolves it for rendering: first
// matching override wins. Before effectiveComponentEnabled, the validator instead flagged a
// component as disabled if ANY of its overrides had Enabled=false, so this exact request —
// which the controller would render with milvus enabled — was wrongly rejected as leaving
// zero components enabled.
func TestCreateAIWorkload_ConflictingOverrideAgreesWithController(t *testing.T) {
	bp := testBlueprint("rag-1-0-0", "rag", "1.0.0", "milvus")
	h := handlerWithObjects(t, bp)

	req := createReq(map[string]any{
		"displayName": "wl", "targetNamespace": "ai", "targetClusters": []string{"local"},
		"source": map[string]any{"sourceType": "Blueprint", "blueprint": map[string]string{"name": "rag", "version": "1.0.0"}},
		"componentValues": []map[string]any{
			{"componentName": "milvus", "enabled": true},
			{"componentName": "milvus", "enabled": false},
		},
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201 (first override wins, milvus is enabled), got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestCreateAIWorkload_AppSourceSkipsComponentNameValidation guards that App-sourced
// workloads never trigger a Blueprint lookup — no Blueprint is registered in the fake
// client here, so a wrongly-triggered lookup would fail this test with a 500/404.
func TestCreateAIWorkload_AppSourceSkipsComponentNameValidation(t *testing.T) {
	h := handlerWithObjects(t)

	req := createReq(map[string]any{
		"displayName": "wl", "targetNamespace": "ai",
		"source":          map[string]any{"sourceType": "App", "app": map[string]string{"chartRepo": "r", "chartName": "llama", "chartVersion": "1", "release": "llama-1"}},
		"componentValues": []map[string]any{{"componentName": "llama", "values": map[string]any{"replicas": 3}}},
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func blueprintSpec(componentValues []map[string]any) map[string]any {
	return map[string]any{
		"displayName": "wl", "targetNamespace": "ai", "targetClusters": []string{"local"},
		"source":          map[string]any{"sourceType": "Blueprint", "blueprint": map[string]string{"name": "rag", "version": "1.0.0"}},
		"componentValues": componentValues,
	}
}

func inlineKustomize() map[string]any {
	return map[string]any{"dir": "overlay", "files": map[string]string{"overlay/kustomization.yaml": "patches: []\n"}}
}

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCreateAIWorkload_AppSourceRejectsKustomizeAndDependsOn(t *testing.T) {
	h := handlerWithObjects(t)
	for _, ov := range []map[string]any{
		{"componentName": "llama", "kustomize": inlineKustomize()},
		{"componentName": "llama", "dependsOn": []string{}},
	} {
		rec := serve(h, createReq(map[string]any{
			"displayName": "wl", "targetNamespace": "ai",
			"source":          map[string]any{"sourceType": "App", "app": map[string]string{"chartRepo": "r", "chartName": "llama", "chartVersion": "1", "release": "llama-1"}},
			"componentValues": []map[string]any{ov},
		}))
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "kustomize and dependsOn are only supported for Blueprint workloads") {
			t.Fatalf("override %v: want 422 Blueprint-only error, got %d: %s", ov, rec.Code, rec.Body.String())
		}
	}
}

func TestCreateAIWorkload_RejectsInvalidDependsOnOverride(t *testing.T) {
	bp := testBlueprint("rag-1-0-0", "rag", "1.0.0", "milvus", "open-webui")
	bp.Spec.Components[1].DependsOn = []string{"milvus"}
	cases := map[string]struct {
		ov   map[string]any
		want string
	}{
		"unknown": {map[string]any{"componentName": "milvus", "dependsOn": []string{"gone"}}, "unknown component"},
		"self":    {map[string]any{"componentName": "milvus", "dependsOn": []string{"milvus"}}, "cannot depend on itself"},
		"cycle":   {map[string]any{"componentName": "milvus", "dependsOn": []string{"open-webui"}}, "cycle"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rec := serve(handlerWithObjects(t, bp), createReq(blueprintSpec([]map[string]any{tc.ov})))
			if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("want 422 containing %q, got %d: %s", tc.want, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestCreateAIWorkload_RejectsInvalidKustomizeOverride(t *testing.T) {
	bp := testBlueprint("rag-1-0-0", "rag", "1.0.0", "milvus", "open-webui")
	root := map[string]string{"overlay/kustomization.yaml": "patches: []\n"}
	cases := map[string]struct {
		kustomize map[string]any
		want      string
	}{
		"no root":      {map[string]any{"dir": "overlay", "files": map[string]string{"overlay/patch.yaml": "a: 1\n"}}, "no kustomization.yaml"},
		"file outside": {map[string]any{"dir": "overlay", "files": map[string]string{"overlay/kustomization.yaml": "patches: []\n", "milvus/values.yaml": "a: 1\n"}}, "outside overlay dir"},
		"bad dir":      {map[string]any{"dir": "./overlay", "files": root}, "clean relative path"},
		"gitRef path":  {map[string]any{"dir": "overlay", "gitRef": map[string]string{"chartRepo": "overlays", "path": "../x"}}, "escapes"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rec := serve(handlerWithObjects(t, bp), createReq(blueprintSpec([]map[string]any{{"componentName": "milvus", "kustomize": tc.kustomize}})))
			if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "componentValues[milvus].kustomize") ||
				!strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("want 422 containing %q, got %d: %s", tc.want, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestCreateAIWorkload_AcceptsKustomizeOnGitBackedChart(t *testing.T) {
	// Git-backed charts deploy as embedded-chart Bundles, which carry overlays.
	bp := testBlueprint("rag-1-0-0", "rag", "1.0.0", "milvus", "open-webui")
	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(schema.GroupVersionKind{Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo"})
	repo.SetName("suse-ai")
	_ = unstructured.SetNestedField(repo.Object, "https://git.example.com/charts", "spec", "gitRepo")

	rec := serve(handlerWithObjects(t, bp, repo), createReq(blueprintSpec([]map[string]any{{"componentName": "milvus", "kustomize": inlineKustomize()}})))
	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateAIWorkload_AcceptsValidOverrides(t *testing.T) {
	bp := testBlueprint("rag-1-0-0", "rag", "1.0.0", "milvus", "open-webui")
	h, c := newAIWorkloadHandlerWithClient(t, bp)
	rec := serve(h, createReq(blueprintSpec([]map[string]any{{"componentName": "open-webui", "dependsOn": []string{"milvus"}, "kustomize": inlineKustomize()}})))
	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	ov := storedWorkload(t, c).Spec.ComponentValues[0]
	if ov.DependsOn == nil || len(*ov.DependsOn) != 1 || ov.Kustomize == nil || ov.Kustomize.Files["overlay/kustomization.yaml"] == "" {
		t.Fatalf("stored override = %+v, want dependsOn and kustomize intact", ov)
	}
}
