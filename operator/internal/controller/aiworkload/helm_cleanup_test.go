package aiworkload

import (
	"context"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type rejectLocalUninstaller struct{ t *testing.T }

func (u rejectLocalUninstaller) Uninstall(context.Context, string, string, string) error {
	u.t.Fatal("downstream cleanup reached the local Helm uninstaller")
	return nil
}

func TestDownstreamHelmCleanupNeverUsesLocalRelease(t *testing.T) {
	for _, mode := range []string{"unconfirmed", "confirmed", "stale target", "multiple targets"} {
		t.Run(mode, func(t *testing.T) {
			w := &aiplatformv1alpha1.AIWorkload{
				ObjectMeta: metav1.ObjectMeta{Name: "workload", Namespace: "apps", UID: "workload-uid"},
				Spec: aiplatformv1alpha1.AIWorkloadSpec{DeployStrategy: aiplatformv1alpha1.AIWorkloadDeployHelm,
					TargetClusters: []string{"c-downstream"}, TargetNamespace: "apps",
					Source: aiplatformv1alpha1.AIWorkloadSource{SourceType: aiplatformv1alpha1.AIWorkloadSourceApp, App: &aiplatformv1alpha1.AppSource{Release: "demo", ChartName: "demo-chart"}}},
			}
			if mode != "unconfirmed" {
				w.Annotations = map[string]string{aiplatformv1alpha1.ExternalHelmCleanupAnnotation: w.ExternalHelmCleanupID()}
			}
			if mode == "stale target" {
				w.Spec.Source.App.Release = "replacement"
			}
			if mode == "multiple targets" {
				w.Spec.TargetClusters = append(w.Spec.TargetClusters, "local")
			}
			c := fake.NewClientBuilder().WithScheme(gitRepoTestScheme()).WithStatusSubresource(w).WithObjects(w).Build()
			r := &AIWorkloadReconciler{Client: c, Uninstaller: rejectLocalUninstaller{t}}
			done, result, err := r.handleHelmRelease(context.Background(), w)
			if err != nil {
				t.Fatal(err)
			}
			if done != (mode == "confirmed") {
				t.Fatalf("cleanup done = %v", done)
			}
			if !done && result.RequeueAfter == 0 {
				t.Fatal("unconfirmed external cleanup must retain the finalizer and retry")
			}
		})
	}
}
