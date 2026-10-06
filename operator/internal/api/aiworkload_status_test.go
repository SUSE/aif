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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func newAIWorkloadHandlerWithClient(t *testing.T, objs ...client.Object) (http.Handler, client.Client) {
	t.Helper()
	s := kruntime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().
		WithScheme(s).
		WithStatusSubresource(&aiplatformv1alpha1.AIWorkload{}).
		WithObjects(objs...).
		Build()
	mux := http.NewServeMux()
	NewAIWorkloadHandler(c).Register(mux)
	return mux, c
}

const statusTestSpec = `"spec":{"displayName":"W","targetNamespace":"my-ns","deployStrategy":"FleetBundle",` +
	`"source":{"sourceType":"App","app":{"chartRepo":"private-charts","chartName":"demo","chartVersion":"0.1.0","release":"demo"}}}`

// clientStatus carries the fields the UI legitimately sends plus
// operator-owned ones a client must not be able to set.
const clientStatus = `"status":{"phase":"Pending","clusterStatuses":[{"clusterId":"local","phase":"Pending"}],` +
	`"pullSecretDeliveries":[{"namespace":"mine","names":["aif-custom-pull-other-repo"]}],` +
	`"conditions":[{"type":"Ready","status":"True","reason":"Forged","message":"x","lastTransitionTime":"2026-01-01T00:00:00Z"}]}`

func storedWorkload(t *testing.T, c client.Client, name string) aiplatformv1alpha1.AIWorkload {
	t.Helper()
	var wl aiplatformv1alpha1.AIWorkload
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: name}, &wl); err != nil {
		t.Fatalf("get %s: %v", name, err)
	}
	return wl
}

// Pull-secret deliveries and conditions are written by the operator only;
// the API must not take them from a client when creating a workload.
func TestCreateAIWorkload_IgnoresOperatorOwnedStatus(t *testing.T) {
	h, c := newAIWorkloadHandlerWithClient(t)
	body := `{"metadata":{"name":"wl"},` + statusTestSpec + `,` + clientStatus + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/namespaces/default/aiworkloads", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}

	wl := storedWorkload(t, c, "wl")
	if wl.Status.Phase != aiplatformv1alpha1.AIWorkloadPhasePending || len(wl.Status.ClusterStatuses) != 1 {
		t.Errorf("phase/clusterStatuses not kept: %+v", wl.Status)
	}
	if len(wl.Status.PullSecretDeliveries) != 0 {
		t.Errorf("pullSecretDeliveries taken from the client: %+v", wl.Status.PullSecretDeliveries)
	}
	if len(wl.Status.Conditions) != 0 {
		t.Errorf("conditions taken from the client: %+v", wl.Status.Conditions)
	}
}

// Updating a workload keeps the operator's deliveries and conditions and
// takes only the client-owned fields from the request.
func TestUpdateAIWorkload_KeepsOperatorOwnedStatus(t *testing.T) {
	existing := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			DisplayName: "W", TargetNamespace: "my-ns", DeployStrategy: aiplatformv1alpha1.AIWorkloadDeployFleetBundle,
			Source: aiplatformv1alpha1.AIWorkloadSource{
				SourceType: aiplatformv1alpha1.AIWorkloadSourceApp,
				App:        &aiplatformv1alpha1.AppSource{ChartRepo: "private-charts", ChartName: "demo", ChartVersion: "0.1.0", Release: "demo"},
			},
		},
		Status: aiplatformv1alpha1.AIWorkloadStatus{
			Phase:                aiplatformv1alpha1.AIWorkloadPhaseRunning,
			PullSecretDeliveries: []aiplatformv1alpha1.PullSecretDelivery{{Namespace: "my-ns", Names: []string{"aif-custom-pull-private-charts"}}},
			Conditions:           []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Reconciled", LastTransitionTime: metav1.Now()}},
		},
	}
	h, c := newAIWorkloadHandlerWithClient(t, existing)
	body := `{"metadata":{"name":"wl"},` + statusTestSpec + `,` + clientStatus + `}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/namespaces/default/aiworkloads/wl", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body)
	}

	wl := storedWorkload(t, c, "wl")
	if wl.Status.Phase != aiplatformv1alpha1.AIWorkloadPhasePending {
		t.Errorf("phase = %q, want the client's Pending", wl.Status.Phase)
	}
	if len(wl.Status.PullSecretDeliveries) != 1 || wl.Status.PullSecretDeliveries[0].Names[0] != "aif-custom-pull-private-charts" {
		t.Errorf("pullSecretDeliveries = %+v, want the operator's value kept", wl.Status.PullSecretDeliveries)
	}
	if len(wl.Status.Conditions) != 1 || wl.Status.Conditions[0].Reason != "Reconciled" {
		t.Errorf("conditions = %+v, want the operator's value kept", wl.Status.Conditions)
	}
}
