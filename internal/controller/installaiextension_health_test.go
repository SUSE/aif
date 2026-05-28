package controller

import (
	"context"
	"strings"
	"testing"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/internal/infra/rancher"
	"github.com/SUSE/aif/pkg/conditions"
	"github.com/SUSE/aif/pkg/helm"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestInstallAIExtensionReconciler_DeploymentNotReady tests requeue when Deployment not yet ready.
func TestInstallAIExtensionReconciler_DeploymentNotReady(t *testing.T) {
	scheme := newTestScheme(t)
	ext := createInstallAIExtension("test-ext")

	deploy := helmDeployment("aif-ui")
	deploy.Status.ReadyReplicas = 0

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ext, deploy).
		WithStatusSubresource(&aifv1.InstallAIExtension{}).
		Build()

	fakeHelm := helm.NewFake()
	fakeCatalog := rancher.NewFake()
	recorder := &fakeRecorder{}

	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		Scheme:     scheme,
		HelmEngine: fakeHelm,
		Catalog:    fakeCatalog,
		Recorder:   recorder,
	}

	ctx := context.Background()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-ext"}}

	// First reconcile: adds finalizer
	_, _ = reconciler.Reconcile(ctx, req)

	// Second reconcile: Helm OK but Deployment not ready → requeue
	result, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result.RequeueAfter != readinessRequeue {
		t.Errorf("expected RequeueAfter=%v, got %v", readinessRequeue, result.RequeueAfter)
	}

	var updated aifv1.InstallAIExtension
	if err := fakeClient.Get(ctx, req.NamespacedName, &updated); err != nil {
		t.Fatalf("failed to get updated resource: %v", err)
	}

	deployCond := findCondition(updated.Status.Conditions, conditions.TypeDeploymentReady)
	if deployCond == nil {
		t.Fatal("DeploymentReady condition not set")
	}
	if deployCond.Status != metav1.ConditionFalse {
		t.Errorf("expected DeploymentReady=False, got %s", deployCond.Status)
	}
}

// TestInstallAIExtensionReconciler_DeploymentPodFailure tests that pod-level errors
// (ImagePullBackOff, CrashLoopBackOff) are surfaced in the DeploymentReady condition message.
func TestInstallAIExtensionReconciler_DeploymentPodFailure(t *testing.T) {
	scheme := newTestScheme(t)
	ext := createInstallAIExtension("test-ext")

	deploy := helmDeployment("aif-ui")
	deploy.Status.ReadyReplicas = 0

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "aif-ui-abc123",
			Namespace: rancher.UIPluginNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/instance": "aif-ui",
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "aif-ui",
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason:  "ImagePullBackOff",
							Message: "Back-off pulling image \"ghcr.io/suse/aif-ui:0.1.0\"",
						},
					},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ext, deploy, pod).
		WithStatusSubresource(&aifv1.InstallAIExtension{}).
		Build()

	fakeHelm := helm.NewFake()
	fakeCatalog := rancher.NewFake()
	recorder := &fakeRecorder{}

	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		Scheme:     scheme,
		HelmEngine: fakeHelm,
		Catalog:    fakeCatalog,
		Recorder:   recorder,
	}

	ctx := context.Background()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-ext"}}

	// First reconcile: adds finalizer
	_, _ = reconciler.Reconcile(ctx, req)

	// Second reconcile: Deployment not ready, pod has ImagePullBackOff
	result, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result.RequeueAfter != readinessRequeue {
		t.Errorf("expected RequeueAfter=%v, got %v", readinessRequeue, result.RequeueAfter)
	}

	var updated aifv1.InstallAIExtension
	if err := fakeClient.Get(ctx, req.NamespacedName, &updated); err != nil {
		t.Fatalf("failed to get updated resource: %v", err)
	}

	deployCond := findCondition(updated.Status.Conditions, conditions.TypeDeploymentReady)
	if deployCond == nil {
		t.Fatal("DeploymentReady condition not set")
	}
	if deployCond.Status != metav1.ConditionFalse {
		t.Errorf("expected DeploymentReady=False, got %s", deployCond.Status)
	}
	if deployCond.Message == "Deployment not yet ready" {
		t.Error("expected enriched message with pod failure detail, got generic message")
	}
	if !strings.Contains(deployCond.Message, "ImagePullBackOff") {
		t.Errorf("expected message to contain ImagePullBackOff, got: %s", deployCond.Message)
	}
}
