package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/naming"
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

func handlerWithWorkloadAndBlueprints(t *testing.T, w *aiplatformv1alpha1.AIWorkload, bps ...*aiplatformv1alpha1.Blueprint) (http.Handler, client.Client) {
	t.Helper()
	s := kruntime.NewScheme()
	_ = aiplatformv1alpha1.AddToScheme(s)
	objs := []client.Object{w}
	for _, bp := range bps {
		objs = append(objs, bp)
	}
	c := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&aiplatformv1alpha1.AIWorkload{}).WithObjects(objs...).Build()
	return newTestMux(NewAIWorkloadHandler(c)), c
}

func rollbackBP(version string, policy aiplatformv1alpha1.RollbackPolicy) *aiplatformv1alpha1.Blueprint {
	return &aiplatformv1alpha1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{Name: naming.BlueprintCRName("rag", version)},
		Spec:       aiplatformv1alpha1.BlueprintSpec{DisplayName: "RAG", Version: version, Rollback: policy},
	}
}

func rollbackableWL(name string) *aiplatformv1alpha1.AIWorkload {
	wl := blueprintWL(name)
	wl.Spec.Source.Blueprint.Version = "2.0.0"
	wl.Status.DeployedSource = &aiplatformv1alpha1.DeployedSourceSnapshot{Version: "1.0.0"}
	return wl
}

func postRollback(h http.Handler, name string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/v1/namespaces/default/aiworkloads/"+name+"/rollback", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func assertNoRollbackAnnotation(t *testing.T, c client.Client, name string) {
	t.Helper()
	got := &aiplatformv1alpha1.AIWorkload{}
	if err := c.Get(context.TODO(), client.ObjectKey{Namespace: "default", Name: name}, got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Annotations[rollbackRequestAnnotation]; ok {
		t.Fatal("rollback-request annotation must not be written")
	}
}

func TestRollback_DisabledByBlueprint(t *testing.T) {
	h, c := handlerWithWorkloadAndBlueprints(t, rollbackableWL("rb-off"),
		rollbackBP("2.0.0", aiplatformv1alpha1.RollbackDisabled), rollbackBP("1.0.0", ""))
	rec := postRollback(h, "rb-off")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "rollback is disabled for this Blueprint") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
	assertNoRollbackAnnotation(t, c, "rb-off")
}

func TestRollback_DeployedVersionDisables(t *testing.T) {
	h, c := handlerWithWorkloadAndBlueprints(t, rollbackableWL("rb-dep"),
		rollbackBP("2.0.0", aiplatformv1alpha1.RollbackAllowed), rollbackBP("1.0.0", aiplatformv1alpha1.RollbackDisabled))
	rec := postRollback(h, "rb-dep")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", rec.Code, rec.Body.String())
	}
	assertNoRollbackAnnotation(t, c, "rb-dep")
}

func TestRollback_AllowedByDefault(t *testing.T) {
	h, c := handlerWithWorkloadAndBlueprints(t, rollbackableWL("rb-def"), rollbackBP("2.0.0", ""), rollbackBP("1.0.0", ""))
	rec := postRollback(h, "rb-def")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d: %s", rec.Code, rec.Body.String())
	}
	got := &aiplatformv1alpha1.AIWorkload{}
	_ = c.Get(context.TODO(), client.ObjectKey{Namespace: "default", Name: "rb-def"}, got)
	if got.Annotations[rollbackRequestAnnotation] == "" {
		t.Fatal("rollback-request annotation should be set")
	}
}

func postUpgrade(h http.Handler, name, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/v1/namespaces/default/aiworkloads/"+name+"/upgrade", strings.NewReader(`{"targetVersion":"`+target+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestUpgrade_OlderTargetHonoursRollbackPolicy(t *testing.T) {
	deployedAt2 := func(name string) *aiplatformv1alpha1.AIWorkload {
		wl := blueprintWL(name)
		wl.Spec.Source.Blueprint.Version = "2.0.0"
		wl.Status.DeployedSource = &aiplatformv1alpha1.DeployedSourceSnapshot{Version: "2.0.0"}
		return wl
	}
	bps := []*aiplatformv1alpha1.Blueprint{
		rollbackBP("2.0.0", aiplatformv1alpha1.RollbackDisabled), rollbackBP("1.0.0", ""), rollbackBP("3.0.0", ""),
	}

	h, c := handlerWithWorkloadAndBlueprints(t, deployedAt2("up-old"), bps...)
	rec := postUpgrade(h, "up-old", "1.0.0")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "rollback is disabled for this Blueprint") {
		t.Fatalf("older target: want 400 rollback disabled, got %d: %s", rec.Code, rec.Body.String())
	}
	got := &aiplatformv1alpha1.AIWorkload{}
	_ = c.Get(context.TODO(), client.ObjectKey{Namespace: "default", Name: "up-old"}, got)
	if _, ok := got.Annotations[upgradeRequestAnnotation]; ok {
		t.Fatal("upgrade-request annotation must not be written")
	}

	h, _ = handlerWithWorkloadAndBlueprints(t, deployedAt2("up-new"), bps...)
	if rec := postUpgrade(h, "up-new", "3.0.0"); rec.Code != http.StatusAccepted {
		t.Fatalf("newer target: want 202, got %d: %s", rec.Code, rec.Body.String())
	}
}
