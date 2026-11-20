package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apixv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func putWorkload(t *testing.T, existing *aiplatformv1alpha1.AIWorkload, spec aiplatformv1alpha1.AIWorkloadSpec, bps ...*aiplatformv1alpha1.Blueprint) *httptest.ResponseRecorder {
	t.Helper()
	b := fake.NewClientBuilder().WithScheme(aiworkloadTestScheme(t)).WithObjects(existing).WithStatusSubresource(existing)
	for _, bp := range bps {
		b = b.WithObjects(bp)
	}
	body, _ := json.Marshal(aiWorkloadCreateBody{Spec: spec})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/namespaces/default/aiworkloads/"+existing.Name, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("namespace", "default")
	req.SetPathValue("name", existing.Name)
	rec := httptest.NewRecorder()
	NewAIWorkloadHandler(b.Build()).updateAIWorkload(rec, req)
	return rec
}

func TestUpdate_OlderVersionHonoursRollbackPolicy(t *testing.T) {
	existing := blueprintWL("up-put")
	existing.Spec.Source.Blueprint.Version = "2.0.0"
	existing.Status.DeployedSource = &aiplatformv1alpha1.DeployedSourceSnapshot{Version: "2.0.0"}
	spec := *existing.Spec.DeepCopy()
	spec.Source.Blueprint.Version = "1.0.0"

	rec := putWorkload(t, existing, spec, rollbackBP("2.0.0", aiplatformv1alpha1.RollbackDisabled), rollbackBP("1.0.0", ""))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "rollback is disabled for this Blueprint") {
		t.Fatalf("want 400 rollback disabled, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = putWorkload(t, existing, spec, rollbackBP("2.0.0", ""), rollbackBP("1.0.0", ""))
	if strings.Contains(rec.Body.String(), "rollback is disabled") {
		t.Fatalf("allowed rollback rejected: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdate_AutoPinsBlockMultipleTargets(t *testing.T) {
	bp := rollbackBP("1.0.0", "")
	bp.Spec.Components = []aiplatformv1alpha1.BlueprintComponent{
		{ChartName: "app", Values: &apixv1.JSON{Raw: []byte(`{"global":{"domain":"aif:auto-domain:app"}}`)}},
		{ChartName: "extra"},
	}
	existing := blueprintWL("multi")
	existing.Status.ResolvedValues = []aiplatformv1alpha1.ResolvedValue{
		{ComponentName: "app", Path: "global.domain", Value: "app.10.0.0.12.sslip.io", Source: "auto", ClusterID: "local"},
	}
	spec := *existing.Spec.DeepCopy()
	spec.TargetClusters = []string{"local", "c-abc"}
	blocked := func(rec *httptest.ResponseRecorder) bool {
		return strings.Contains(rec.Body.String(), "can't target more than one cluster")
	}

	rec := putWorkload(t, existing, spec, bp)
	if rec.Code != http.StatusBadRequest || !blocked(rec) || !strings.Contains(rec.Body.String(), "global.domain") {
		t.Fatalf("want 400 naming the auto value, got %d: %s", rec.Code, rec.Body.String())
	}

	userOnly := existing.DeepCopy()
	userOnly.Status.ResolvedValues[0].Source = "user"
	if rec := putWorkload(t, userOnly, spec, bp); blocked(rec) {
		t.Fatalf("user-set values must not block multiple targets: %s", rec.Body.String())
	}

	unchanged := existing.DeepCopy()
	unchanged.Spec.TargetClusters = []string{"c-abc", "local"}
	if rec := putWorkload(t, unchanged, spec, bp); blocked(rec) {
		t.Fatalf("an unchanged multi-cluster target set must stay editable: %s", rec.Body.String())
	}

	excluded := *spec.DeepCopy()
	off := false
	excluded.ComponentValues = []aiplatformv1alpha1.ComponentValueOverride{{ComponentName: "app", Enabled: &off}}
	if rec := putWorkload(t, existing, excluded, bp); blocked(rec) {
		t.Fatalf("a pin of an excluded component must not block: %s", rec.Body.String())
	}

	gone := existing.DeepCopy()
	gone.Status.ResolvedValues[0].ComponentName = "removed"
	if rec := putWorkload(t, gone, spec, bp); blocked(rec) {
		t.Fatalf("a pin of a component the Blueprint no longer has must not block: %s", rec.Body.String())
	}
}
