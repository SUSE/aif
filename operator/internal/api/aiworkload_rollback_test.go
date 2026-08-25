package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func TestRollbackEndpointSetsAnnotation(t *testing.T) {
	h, c := handlerWithWorkload(t, blueprintWL("rb"))
	req := httptest.NewRequest("POST", "/api/v1/namespaces/default/aiworkloads/rb/rollback", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d: %s", rec.Code, rec.Body.String())
	}
	got := &aiplatformv1alpha1.AIWorkload{}
	_ = c.Get(context.TODO(), client.ObjectKey{Namespace: "default", Name: "rb"}, got)
	if got.Annotations[rollbackRequestAnnotation] == "" {
		t.Fatal("rollback-request annotation should be set")
	}
}

func TestRollbackEndpointAllowedDuringInProgress(t *testing.T) {
	wl := blueprintWL("rb-busy")
	wl.Status.ActiveOperation = &aiplatformv1alpha1.AIWorkloadOperation{State: aiplatformv1alpha1.OperationStateInProgress}
	h, _ := handlerWithWorkload(t, wl)
	req := httptest.NewRequest("POST", "/api/v1/namespaces/default/aiworkloads/rb-busy/rollback", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("rollback must be allowed during an in-flight op (supersedes), got %d", rec.Code)
	}
}
