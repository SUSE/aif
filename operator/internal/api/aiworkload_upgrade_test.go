package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func handlerWithWorkload(t *testing.T, w *aiplatformv1alpha1.AIWorkload) (http.Handler, client.Client) {
	t.Helper()
	s := kruntime.NewScheme()
	_ = aiplatformv1alpha1.AddToScheme(s)
	c := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&aiplatformv1alpha1.AIWorkload{}).WithObjects(w).Build()
	mux := http.NewServeMux()
	NewAIWorkloadHandler(c).Register(mux)
	return mux, c
}

func blueprintWL(name string) *aiplatformv1alpha1.AIWorkload {
	return &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			DisplayName: name, TargetNamespace: "ai",
			DeployStrategy: aiplatformv1alpha1.AIWorkloadDeployFleetBundle, TargetClusters: []string{"local"},
			Source: aiplatformv1alpha1.AIWorkloadSource{
				SourceType: aiplatformv1alpha1.AIWorkloadSourceBlueprint,
				Blueprint:  &aiplatformv1alpha1.BlueprintSource{Name: "rag", Version: "1.0.0"},
			},
		},
	}
}

func TestUpgradeEndpointSetsRequestAnnotation(t *testing.T) {
	h, c := handlerWithWorkload(t, blueprintWL("up"))
	body, _ := json.Marshal(map[string]string{"targetVersion": "2.0.0"})
	req := httptest.NewRequest("POST", "/api/v1/namespaces/default/aiworkloads/up/upgrade", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d: %s", rec.Code, rec.Body.String())
	}
	got := &aiplatformv1alpha1.AIWorkload{}
	_ = c.Get(context.TODO(), client.ObjectKey{Namespace: "default", Name: "up"}, got)
	v := got.Annotations[upgradeRequestAnnotation]
	if !strings.HasSuffix(v, "|2.0.0") || v == "|2.0.0" {
		t.Fatalf("annotation should be <nonce>|2.0.0, got %q", v)
	}
	// Spec version must be UNTOUCHED by the endpoint (controller owns the change).
	if got.Spec.Source.Blueprint.Version != "1.0.0" {
		t.Fatalf("endpoint must not mutate spec version, got %s", got.Spec.Source.Blueprint.Version)
	}
}

func TestUpgradeEndpointRejectsMissingTarget(t *testing.T) {
	h, _ := handlerWithWorkload(t, blueprintWL("up2"))
	req := httptest.NewRequest("POST", "/api/v1/namespaces/default/aiworkloads/up2/upgrade", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func appWL(name string) *aiplatformv1alpha1.AIWorkload {
	return &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			DisplayName: name, TargetNamespace: "ai",
			DeployStrategy: aiplatformv1alpha1.AIWorkloadDeployFleetBundle, TargetClusters: []string{"local"},
			Source: aiplatformv1alpha1.AIWorkloadSource{
				SourceType: aiplatformv1alpha1.AIWorkloadSourceApp,
				App:        &aiplatformv1alpha1.AppSource{ChartName: "some-chart"},
			},
		},
	}
}

// TestRecoveryEndpointsRejectAppSource guards the fix that rejects recovery operations on
// App-sourced workloads at the edge (they would otherwise sit InProgress until timeout).
func TestRecoveryEndpointsRejectAppSource(t *testing.T) {
	cases := []struct {
		path string
		body string
	}{
		{"/api/v1/namespaces/default/aiworkloads/appwl/upgrade", `{"targetVersion":"2.0.0"}`},
		{"/api/v1/namespaces/default/aiworkloads/appwl/rollback", ``},
		{"/api/v1/namespaces/default/aiworkloads/appwl/retry", ``},
	}
	for _, tc := range cases {
		h, _ := handlerWithWorkload(t, appWL("appwl"))
		req := httptest.NewRequest("POST", tc.path, bytes.NewReader([]byte(tc.body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: want 400 for App source, got %d: %s", tc.path, rec.Code, rec.Body.String())
		}
	}
}

func TestUpgradeEndpoint404(t *testing.T) {
	h, _ := handlerWithWorkload(t, blueprintWL("exists"))
	req := httptest.NewRequest("POST", "/api/v1/namespaces/default/aiworkloads/missing/upgrade", bytes.NewReader([]byte(`{"targetVersion":"2.0.0"}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rec.Code)
	}
}
