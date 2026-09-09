package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
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
