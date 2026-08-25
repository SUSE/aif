package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func TestRetryEndpointSetsAnnotationWhenIdle(t *testing.T) {
	h, c := handlerWithWorkload(t, blueprintWL("rt"))
	req := httptest.NewRequest("POST", "/api/v1/namespaces/default/aiworkloads/rt/retry", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d: %s", rec.Code, rec.Body.String())
	}
	got := &aiplatformv1alpha1.AIWorkload{}
	_ = c.Get(context.TODO(), client.ObjectKey{Namespace: "default", Name: "rt"}, got)
	if got.Annotations[retryRequestAnnotation] == "" {
		t.Fatal("retry-request annotation should be set")
	}
}

func TestRetryEndpoint409WhenInProgress(t *testing.T) {
	wl := blueprintWL("rt-busy")
	wl.Status.ActiveOperation = &aiplatformv1alpha1.AIWorkloadOperation{State: aiplatformv1alpha1.OperationStateInProgress}
	h, c := handlerWithWorkload(t, wl)
	req := httptest.NewRequest("POST", "/api/v1/namespaces/default/aiworkloads/rt-busy/retry", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409 while an op is in flight, got %d", rec.Code)
	}
	got := &aiplatformv1alpha1.AIWorkload{}
	_ = c.Get(context.TODO(), client.ObjectKey{Namespace: "default", Name: "rt-busy"}, got)
	if got.Annotations[retryRequestAnnotation] != "" {
		t.Fatal("retry-request must NOT be set on 409")
	}
}
