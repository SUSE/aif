package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDeleteWorkloadExternalHelmConfirmation(t *testing.T) {
	for _, mode := range []string{"confirmed", "without confirmation", "wrong uid", "changed release", "local"} {
		t.Run(mode, func(t *testing.T) {
			w := &aiplatformv1alpha1.AIWorkload{
				ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "apps", UID: "uid", Finalizers: []string{"ai-factory.suse.com/cleanup"}},
				Spec: aiplatformv1alpha1.AIWorkloadSpec{DeployStrategy: aiplatformv1alpha1.AIWorkloadDeployHelm,
					TargetClusters: []string{"c-downstream"}, TargetNamespace: "apps",
					Source: aiplatformv1alpha1.AIWorkloadSource{SourceType: aiplatformv1alpha1.AIWorkloadSourceApp, App: &aiplatformv1alpha1.AppSource{Release: "demo"}}},
			}
			confirmation := w.ExternalHelmCleanupID()
			switch mode {
			case "without confirmation":
				confirmation = ""
			case "wrong uid":
				w.UID = "replacement"
			case "changed release":
				w.Spec.Source.App.Release = "replacement"
			case "local":
				w.Spec.TargetClusters = []string{"local"}
			}
			s := runtime.NewScheme()
			_ = aiplatformv1alpha1.AddToScheme(s)
			c := fake.NewClientBuilder().WithScheme(s).WithObjects(w).Build()
			h := newTestMux(NewAIWorkloadHandler(c))
			req := httptest.NewRequest(http.MethodDelete, "/api/v1/namespaces/apps/aiworkloads/demo", nil)
			req.Header.Set("X-AIF-Completed-Helm-Uninstall", confirmation)
			response := httptest.NewRecorder()
			h.ServeHTTP(response, req)
			var got aiplatformv1alpha1.AIWorkload
			if err := c.Get(context.Background(), client.ObjectKeyFromObject(w), &got); err != nil {
				t.Fatal(err)
			}
			if mode == "confirmed" || mode == "without confirmation" {
				if response.Code != http.StatusNoContent || got.DeletionTimestamp == nil {
					t.Fatalf("delete failed: %d %s", response.Code, response.Body.String())
				}
				if got.Annotations[aiplatformv1alpha1.ExternalHelmCleanupAnnotation] != confirmation {
					t.Fatal("confirmation was not preserved")
				}
			} else if response.Code != http.StatusConflict || got.DeletionTimestamp != nil {
				t.Fatalf("stale/invalid confirmation caused deletion: %d %s", response.Code, response.Body.String())
			}
		})
	}
}
