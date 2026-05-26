package controller

import (
	"context"
	"errors"
	"strings"
	"testing"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/internal/infra/rancher"
	"github.com/SUSE/aif/pkg/conditions"
	"github.com/SUSE/aif/pkg/helm"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		aifv1.AddToScheme,
		appsv1.AddToScheme,
		corev1.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			t.Fatalf("failed to add to scheme: %v", err)
		}
	}
	return scheme
}

func createInstallAIExtension(name string) *aifv1.InstallAIExtension {
	return &aifv1.InstallAIExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Generation: 1,
		},
		Spec: aifv1.InstallAIExtensionSpec{
			Source: aifv1.ExtensionSource{
				Kind: aifv1.ExtensionSourceKindHelm,
				Helm: &aifv1.HelmSource{
					ChartURL: "oci://registry.suse.com/ai/charts/aif-ui",
					Version:  "1.0.0",
				},
			},
			Extension: aifv1.ExtensionConfig{
				Name:    "ai-factory",
				Version: "1.0.0",
			},
		},
	}
}

func createGitInstallAIExtension(name string) *aifv1.InstallAIExtension {
	return &aifv1.InstallAIExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Generation: 1,
		},
		Spec: aifv1.InstallAIExtensionSpec{
			Source: aifv1.ExtensionSource{
				Kind: aifv1.ExtensionSourceKindGit,
				Git: &aifv1.GitSource{
					Repo:   "https://github.com/suse/aif-ui-extension",
					Branch: "gh-pages",
				},
			},
			Extension: aifv1.ExtensionConfig{
				Name:    "ai-factory",
				Version: "1.0.0",
			},
		},
	}
}

func helmDeployment(releaseName string) *appsv1.Deployment {
	replicas := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      releaseName,
			Namespace: rancher.UIPluginNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/instance": releaseName,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/instance": releaseName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/instance": releaseName,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "ext", Image: "ext:latest"},
					},
				},
			},
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: 1,
			Replicas:      1,
		},
	}
}

func helmService(releaseName string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      releaseName + "-svc",
			Namespace: rancher.UIPluginNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/instance": releaseName,
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Port: 8080},
			},
		},
	}
}

// TestInstallAIExtensionReconciler_HappyPath tests successful Helm source installation.
func TestInstallAIExtensionReconciler_HappyPath(t *testing.T) {
	scheme := newTestScheme(t)
	ext := createInstallAIExtension("test-ext")

	deploy := helmDeployment("aif-ui")
	svc := helmService("aif-ui")

	svc.Name = "test-svc"
	svc.Spec.Ports[0].Port = 8080

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ext, deploy, svc).
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
	result, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("first reconcile failed: %v", err)
	}
	if !result.Requeue {
		t.Error("expected requeue after adding finalizer")
	}

	// Second reconcile: full Helm flow
	result, err = reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("second reconcile failed: %v", err)
	}
	if result.RequeueAfter != healthCheckInterval {
		t.Errorf("expected RequeueAfter=%v for health monitoring, got %v", healthCheckInterval, result.RequeueAfter)
	}

	var updated aifv1.InstallAIExtension
	if err := fakeClient.Get(ctx, req.NamespacedName, &updated); err != nil {
		t.Fatalf("failed to get updated resource: %v", err)
	}

	if updated.Status.Phase != aifv1.InstallAIExtensionPhaseInstalled {
		t.Errorf("expected phase Installed, got %s", updated.Status.Phase)
	}

	// Verify multi-condition status
	for _, tc := range []struct {
		condType string
		status   metav1.ConditionStatus
		reason   string
	}{
		{conditions.TypeReady, metav1.ConditionTrue, conditions.ReasonInstalled},
		{conditions.TypeHelmInstalled, metav1.ConditionTrue, conditions.ReasonInstalled},
		{conditions.TypeDeploymentReady, metav1.ConditionTrue, conditions.ReasonDeploymentAvailable},
		{conditions.TypeServiceReady, metav1.ConditionTrue, conditions.ReasonServiceCreated},
		{conditions.TypeClusterRepoReady, metav1.ConditionTrue, conditions.ReasonClusterRepoCreated},
		{conditions.TypeUIPluginReady, metav1.ConditionTrue, conditions.ReasonUIPluginVerified},
	} {
		cond := findCondition(updated.Status.Conditions, tc.condType)
		if cond == nil {
			t.Errorf("%s condition not set", tc.condType)
			continue
		}
		if cond.Status != tc.status {
			t.Errorf("%s: expected status %s, got %s (reason=%s, msg=%s)", tc.condType, tc.status, cond.Status, cond.Reason, cond.Message)
		}
		if cond.Reason != tc.reason {
			t.Errorf("%s: expected reason %s, got %s", tc.condType, tc.reason, cond.Reason)
		}
	}

	// Verify Helm was called correctly
	installs := filterCalls(fakeHelm.Calls, "InstallChartFromRepo")
	if len(installs) != 1 {
		t.Errorf("expected 1 Helm install call, got %d", len(installs))
	}
	if len(installs) > 0 {
		call := installs[0].Request
		if call.Namespace != rancher.UIPluginNamespace {
			t.Errorf("expected namespace %s, got %s", rancher.UIPluginNamespace, call.Namespace)
		}
		if call.ReleaseName != "aif-ui" {
			t.Errorf("expected release name aif-ui, got %s", call.ReleaseName)
		}
		if call.ChartRef != "oci://registry.suse.com/ai/charts/aif-ui:1.0.0" {
			t.Errorf("expected chart ref with version, got %s", call.ChartRef)
		}
	}

	// Verify CatalogManager was called for ClusterRepo and UIPlugin
	clusterRepoCalls := fakeCatalog.FilterCalls("EnsureClusterRepo")
	if len(clusterRepoCalls) != 1 {
		t.Errorf("expected 1 EnsureClusterRepo call, got %d", len(clusterRepoCalls))
	}
	if len(clusterRepoCalls) > 0 && clusterRepoCalls[0].ExtensionName != "ai-factory" {
		t.Errorf("expected extension name ai-factory, got %s", clusterRepoCalls[0].ExtensionName)
	}

	uiPluginCalls := fakeCatalog.FilterCalls("EnsureUIPlugin")
	if len(uiPluginCalls) != 1 {
		t.Errorf("expected 1 EnsureUIPlugin call, got %d", len(uiPluginCalls))
	}

	// Verify event emitted
	found := false
	for _, evt := range recorder.events {
		if containsEventReason(evt, conditions.ReasonInstalled) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected Installed event, got: %v", recorder.events)
	}

	// Verify tracking fields
	if updated.Status.ActiveExtensionName != "ai-factory" {
		t.Errorf("expected ActiveExtensionName=ai-factory, got %q", updated.Status.ActiveExtensionName)
	}
	if updated.Status.ActiveSourceKind != aifv1.ExtensionSourceKindHelm {
		t.Errorf("expected ActiveSourceKind=Helm, got %q", updated.Status.ActiveSourceKind)
	}
}

// TestInstallAIExtensionReconciler_GitSource tests successful Git source installation.
func TestInstallAIExtensionReconciler_GitSource(t *testing.T) {
	scheme := newTestScheme(t)
	ext := createGitInstallAIExtension("test-git")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ext).
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
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-git"}}

	// First reconcile: adds finalizer
	result, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("first reconcile failed: %v", err)
	}
	if !result.Requeue {
		t.Error("expected requeue after adding finalizer")
	}

	// Second reconcile: git source flow
	result, err = reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("second reconcile failed: %v", err)
	}
	if result.RequeueAfter != healthCheckInterval {
		t.Errorf("expected RequeueAfter=%v for health monitoring, got %v", healthCheckInterval, result.RequeueAfter)
	}

	var updated aifv1.InstallAIExtension
	if err := fakeClient.Get(ctx, req.NamespacedName, &updated); err != nil {
		t.Fatalf("failed to get updated resource: %v", err)
	}

	if updated.Status.Phase != aifv1.InstallAIExtensionPhaseInstalled {
		t.Errorf("expected phase Installed, got %s", updated.Status.Phase)
	}

	// Git mode: no HelmInstalled, DeploymentReady, or ServiceReady conditions
	for _, absent := range []string{
		conditions.TypeHelmInstalled,
		conditions.TypeDeploymentReady,
		conditions.TypeServiceReady,
	} {
		if cond := findCondition(updated.Status.Conditions, absent); cond != nil {
			t.Errorf("did not expect %s condition in git mode, got %v", absent, cond)
		}
	}

	for _, tc := range []struct {
		condType string
		reason   string
	}{
		{conditions.TypeClusterRepoReady, conditions.ReasonClusterRepoCreated},
		{conditions.TypeUIPluginReady, conditions.ReasonUIPluginVerified},
		{conditions.TypeReady, conditions.ReasonInstalled},
	} {
		cond := findCondition(updated.Status.Conditions, tc.condType)
		if cond == nil {
			t.Errorf("%s condition not set", tc.condType)
			continue
		}
		if cond.Status != metav1.ConditionTrue {
			t.Errorf("%s: expected True, got %s", tc.condType, cond.Status)
		}
		if cond.Reason != tc.reason {
			t.Errorf("%s: expected reason %s, got %s", tc.condType, tc.reason, cond.Reason)
		}
	}

	// Verify CatalogManager was called for ClusterRepoGit
	gitRepoCalls := fakeCatalog.FilterCalls("EnsureClusterRepoGit")
	if len(gitRepoCalls) != 1 {
		t.Fatalf("expected 1 EnsureClusterRepoGit call, got %d", len(gitRepoCalls))
	}
	if gitRepoCalls[0].RepoURL != "https://github.com/suse/aif-ui-extension" {
		t.Errorf("expected git repo URL, got %q", gitRepoCalls[0].RepoURL)
	}
	if gitRepoCalls[0].Branch != "gh-pages" {
		t.Errorf("expected branch gh-pages, got %q", gitRepoCalls[0].Branch)
	}

	// Verify OCI Helm install was NOT called for git mode
	ociInstalls := filterCalls(fakeHelm.Calls, "InstallChartFromRepo")
	if len(ociInstalls) != 0 {
		t.Errorf("expected 0 OCI Helm install calls for git mode, got %d", len(ociInstalls))
	}

	// Verify UIPlugin was installed via Helm from repo URL
	repoInstalls := filterCalls(fakeHelm.Calls, "InstallFromRepoURL")
	if len(repoInstalls) != 1 {
		t.Fatalf("expected 1 InstallFromRepoURL call, got %d", len(repoInstalls))
	}
	if repoInstalls[0].Namespace != rancher.UIPluginNamespace {
		t.Errorf("expected namespace %s, got %s", rancher.UIPluginNamespace, repoInstalls[0].Namespace)
	}
	if repoInstalls[0].Name != "ai-factory" {
		t.Errorf("expected release name ai-factory, got %s", repoInstalls[0].Name)
	}

	if updated.Status.ActiveExtensionName != "ai-factory" {
		t.Errorf("expected ActiveExtensionName=ai-factory, got %q", updated.Status.ActiveExtensionName)
	}
	if updated.Status.ActiveSourceKind != aifv1.ExtensionSourceKindGit {
		t.Errorf("expected ActiveSourceKind=Git, got %q", updated.Status.ActiveSourceKind)
	}
}

// TestInstallAIExtensionReconciler_UIPluginCRDMissing tests permanent failure when Rancher CRDs missing.
func TestInstallAIExtensionReconciler_UIPluginCRDMissing(t *testing.T) {
	scheme := newTestScheme(t)
	ext := createInstallAIExtension("test-ext")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ext).
		WithStatusSubresource(&aifv1.InstallAIExtension{}).
		Build()

	fakeHelm := helm.NewFake()
	fakeCatalog := rancher.NewFake()
	fakeCatalog.CheckCRDsErr = errors.New("UIPlugin CRD not found")
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

	// Second reconcile: CRD check fails
	result, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("expected nil error for permanent failure, got %v", err)
	}
	if result.Requeue || result.RequeueAfter > 0 {
		t.Error("expected no requeue for permanent CRD missing error")
	}

	var updated aifv1.InstallAIExtension
	if err := fakeClient.Get(ctx, req.NamespacedName, &updated); err != nil {
		t.Fatalf("failed to get updated resource: %v", err)
	}

	if updated.Status.Phase != aifv1.InstallAIExtensionPhaseFailed {
		t.Errorf("expected phase Failed, got %s", updated.Status.Phase)
	}

	readyCond := findCondition(updated.Status.Conditions, conditions.TypeReady)
	if readyCond == nil {
		t.Fatal("Ready condition not set")
	}
	if readyCond.Status != metav1.ConditionFalse {
		t.Errorf("expected Ready=False, got %s", readyCond.Status)
	}
	if readyCond.Reason != conditions.ReasonUIPluginCRDMissing {
		t.Errorf("expected reason %s, got %s", conditions.ReasonUIPluginCRDMissing, readyCond.Reason)
	}

	installs := filterCalls(fakeHelm.Calls, "InstallChartFromRepo")
	if len(installs) != 0 {
		t.Errorf("expected 0 Helm install calls when CRD missing, got %d", len(installs))
	}

	found := false
	for _, evt := range recorder.events {
		if containsEventReason(evt, conditions.ReasonUIPluginCRDMissing) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected UIPluginCRDMissing event, got: %v", recorder.events)
	}
}

// TestInstallAIExtensionReconciler_HelmInstallFailed tests failure during Helm install.
func TestInstallAIExtensionReconciler_HelmInstallFailed(t *testing.T) {
	scheme := newTestScheme(t)
	ext := createInstallAIExtension("test-ext")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ext).
		WithStatusSubresource(&aifv1.InstallAIExtension{}).
		Build()

	fakeHelm := helm.NewFake()
	fakeHelm.InstallResult = func(helm.InstallRequest) (helm.ReleaseStatus, error) {
		return helm.ReleaseStatus{}, errors.New("failed to pull chart")
	}
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

	// Second reconcile: Helm install fails → no error returned
	_, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("expected nil error (no retry on install failure), got: %v", err)
	}

	found := false
	for _, evt := range recorder.events {
		if containsEventReason(evt, conditions.ReasonInstallFailed) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected InstallFailed event, got: %v", recorder.events)
	}
}

// TestInstallAIExtensionReconciler_HelmInstallFailedWithPodDiag tests that pod-level errors
// (ImagePullBackOff) are surfaced in the HelmInstalled condition when Helm install fails.
func TestInstallAIExtensionReconciler_HelmInstallFailedWithPodDiag(t *testing.T) {
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
							Message: "Back-off pulling image",
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
	fakeHelm.InstallResult = func(helm.InstallRequest) (helm.ReleaseStatus, error) {
		return helm.ReleaseStatus{}, errors.New("timed out waiting for the condition")
	}
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

	// Second reconcile: Helm install fails, pod has ImagePullBackOff
	_, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}

	var updated aifv1.InstallAIExtension
	if err := fakeClient.Get(ctx, req.NamespacedName, &updated); err != nil {
		t.Fatalf("failed to get updated resource: %v", err)
	}

	helmCond := findCondition(updated.Status.Conditions, conditions.TypeHelmInstalled)
	if helmCond == nil {
		t.Fatal("HelmInstalled condition not set")
	}
	if helmCond.Status != metav1.ConditionFalse {
		t.Errorf("expected HelmInstalled=False, got %s", helmCond.Status)
	}
	if !strings.Contains(helmCond.Message, "ImagePullBackOff") {
		t.Errorf("expected enriched message with ImagePullBackOff, got: %s", helmCond.Message)
	}
}

// TestInstallAIExtensionReconciler_UIPluginCreatedWithoutMetadata tests that UIPlugin is created
// even when index metadata fetch fails.
func TestInstallAIExtensionReconciler_UIPluginCreatedWithoutMetadata(t *testing.T) {
	scheme := newTestScheme(t)
	ext := createInstallAIExtension("test-ext")

	deploy := helmDeployment("aif-ui")
	svc := helmService("aif-ui")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ext, deploy, svc).
		WithStatusSubresource(&aifv1.InstallAIExtension{}).
		Build()

	fakeHelm := helm.NewFake()
	fakeCatalog := rancher.NewFake()
	fakeCatalog.FetchIndexMetadataErr = errors.New("connection refused")
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

	// Second reconcile: metadata fetch fails but UIPlugin still created
	result, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != healthCheckInterval {
		t.Errorf("expected RequeueAfter=%v for health monitoring, got %v", healthCheckInterval, result.RequeueAfter)
	}

	var updated aifv1.InstallAIExtension
	if err := fakeClient.Get(ctx, req.NamespacedName, &updated); err != nil {
		t.Fatalf("failed to get updated resource: %v", err)
	}

	if updated.Status.Phase != aifv1.InstallAIExtensionPhaseInstalled {
		t.Errorf("expected phase Installed, got %s", updated.Status.Phase)
	}

	pluginCond := findCondition(updated.Status.Conditions, conditions.TypeUIPluginReady)
	if pluginCond == nil {
		t.Fatal("UIPluginReady condition not set")
	}
	if pluginCond.Status != metav1.ConditionTrue {
		t.Errorf("expected UIPluginReady=True, got %s (reason=%s, msg=%s)", pluginCond.Status, pluginCond.Reason, pluginCond.Message)
	}

	// Verify EnsureUIPlugin was still called (with empty metadata)
	uiPluginCalls := fakeCatalog.FilterCalls("EnsureUIPlugin")
	if len(uiPluginCalls) != 1 {
		t.Fatalf("expected 1 EnsureUIPlugin call, got %d", len(uiPluginCalls))
	}
	if uiPluginCalls[0].Metadata.DisplayName != "" {
		t.Errorf("expected empty metadata, got display name %q", uiPluginCalls[0].Metadata.DisplayName)
	}
}

// TestInstallAIExtensionReconciler_Deletion tests finalizer cleanup.
func TestInstallAIExtensionReconciler_Deletion(t *testing.T) {
	scheme := newTestScheme(t)
	ext := createInstallAIExtension("test-ext")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ext).
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
	result, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("first reconcile failed: %v", err)
	}
	if !result.Requeue {
		t.Error("expected requeue after adding finalizer")
	}

	var updated aifv1.InstallAIExtension
	if err := fakeClient.Get(ctx, req.NamespacedName, &updated); err != nil {
		t.Fatalf("failed to get updated resource: %v", err)
	}
	if len(updated.Finalizers) != 1 || updated.Finalizers[0] != extensionFinalizerName {
		t.Errorf("finalizer not added: %v", updated.Finalizers)
	}

	if err := fakeClient.Delete(ctx, &updated); err != nil {
		t.Fatalf("failed to delete resource: %v", err)
	}

	// Reconcile deletion
	result, err = reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("deletion reconcile failed: %v", err)
	}

	err = fakeClient.Get(ctx, req.NamespacedName, &updated)
	if err == nil && len(updated.Finalizers) != 0 {
		t.Errorf("finalizer not removed: %v", updated.Finalizers)
	}

	// Verify catalog cleanup was called
	deleteCRCalls := fakeCatalog.FilterCalls("DeleteClusterRepo")
	if len(deleteCRCalls) != 1 {
		t.Errorf("expected 1 DeleteClusterRepo call, got %d", len(deleteCRCalls))
	}
	deleteUICalls := fakeCatalog.FilterCalls("DeleteUIPlugin")
	if len(deleteUICalls) != 1 {
		t.Errorf("expected 1 DeleteUIPlugin call, got %d", len(deleteUICalls))
	}
}

// TestInstallAIExtensionReconciler_ResourceNotFound tests graceful handling of deleted resource.
func TestInstallAIExtensionReconciler_ResourceNotFound(t *testing.T) {
	scheme := newTestScheme(t)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
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
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "nonexistent"}}

	result, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("expected nil error for not found resource, got %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue for not found resource")
	}
}

// TestReconcile_ObservedGeneration tests that ObservedGeneration is set correctly.
func TestReconcile_ObservedGeneration(t *testing.T) {
	scheme := newTestScheme(t)
	ext := createInstallAIExtension("test-ext")
	ext.Generation = 5

	deploy := helmDeployment("aif-ui")
	svc := helmService("aif-ui")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ext, deploy, svc).
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

	_, _ = reconciler.Reconcile(ctx, req)
	_, _ = reconciler.Reconcile(ctx, req)

	var updated aifv1.InstallAIExtension
	if err := fakeClient.Get(ctx, req.NamespacedName, &updated); err != nil {
		t.Fatalf("failed to get updated resource: %v", err)
	}

	if updated.Status.ObservedGeneration != ext.Generation {
		t.Errorf("expected observedGeneration=%d, got %d", ext.Generation, updated.Status.ObservedGeneration)
	}
}

// TestConvertValues tests the JSON conversion helper.
func TestConvertValues(t *testing.T) {
	tests := []struct {
		name        string
		values      map[string]apiextensionsv1.JSON
		expectError bool
		expectKeys  []string
	}{
		{
			name:       "empty map",
			values:     map[string]apiextensionsv1.JSON{},
			expectKeys: []string{},
		},
		{
			name: "valid values",
			values: map[string]apiextensionsv1.JSON{
				"replicaCount": {Raw: []byte(`3`)},
				"image":        {Raw: []byte(`{"tag":"v2.0","repository":"ghcr.io/test"}`)},
			},
			expectKeys: []string{"replicaCount", "image"},
		},
		{
			name: "invalid JSON",
			values: map[string]apiextensionsv1.JSON{
				"bad": {Raw: []byte(`{invalid}`)},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := convertValues(tt.values)
			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(result) != len(tt.expectKeys) {
				t.Errorf("expected %d keys, got %d", len(tt.expectKeys), len(result))
			}
			for _, k := range tt.expectKeys {
				if _, ok := result[k]; !ok {
					t.Errorf("expected key %q in result", k)
				}
			}
		})
	}
}

// TestReconcileHelmSource_WithValues verifies values are passed to the Helm install.
func TestReconcileHelmSource_WithValues(t *testing.T) {
	scheme := newTestScheme(t)

	ext := createInstallAIExtension("test-ext")
	ext.Spec.Source.Helm.Values = map[string]apiextensionsv1.JSON{
		"replicaCount": {Raw: []byte(`2`)},
		"image":        {Raw: []byte(`{"tag":"v3.0"}`)},
	}

	deploy := helmDeployment("aif-ui")
	svc := helmService("aif-ui")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ext, deploy, svc).
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

	_, _ = reconciler.Reconcile(ctx, req)

	result, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if result.RequeueAfter != healthCheckInterval {
		t.Errorf("expected requeue after %v, got %v", healthCheckInterval, result.RequeueAfter)
	}

	installs := filterCalls(fakeHelm.Calls, "InstallChartFromRepo")
	if len(installs) != 1 {
		t.Fatalf("expected 1 Helm install call, got %d", len(installs))
	}
	workload := installs[0].Request.Overrides.Workload
	if workload == nil {
		t.Fatal("expected Overrides.Workload to be populated")
	}
	if v, ok := workload["replicaCount"]; !ok {
		t.Error("expected replicaCount in overrides")
	} else if v != float64(2) {
		t.Errorf("expected replicaCount=2, got %v", v)
	}
	if v, ok := workload["image"]; !ok {
		t.Error("expected image in overrides")
	} else {
		imgMap, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("expected image to be map, got %T", v)
		}
		if imgMap["tag"] != "v3.0" {
			t.Errorf("expected image.tag=v3.0, got %v", imgMap["tag"])
		}
	}
}

// TestCleanup tests the cleanup helper method.
func TestCleanup(t *testing.T) {
	scheme := newTestScheme(t)
	ext := createInstallAIExtension("test-ext")
	ext.Status.HelmReleaseName = "aif-ui"

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	fakeHelm := helm.NewFake()
	fakeCatalog := rancher.NewFake()
	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		HelmEngine: fakeHelm,
		Catalog:    fakeCatalog,
	}

	err := reconciler.cleanup(context.Background(), ext)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify Helm uninstalls — Helm mode only uninstalls HelmReleaseName;
	// per-name UIPlugin release uninstall is Git-mode only.
	uninstalls := filterCalls(fakeHelm.Calls, "Uninstall")
	if len(uninstalls) != 1 {
		t.Fatalf("expected 1 Uninstall call (Helm mode), got %d", len(uninstalls))
	}
	if uninstalls[0].Name != "aif-ui" {
		t.Errorf("expected uninstall of aif-ui, got %s", uninstalls[0].Name)
	}

	// Verify catalog cleanup
	deleteCRCalls := fakeCatalog.FilterCalls("DeleteClusterRepo")
	if len(deleteCRCalls) != 1 {
		t.Errorf("expected 1 DeleteClusterRepo call, got %d", len(deleteCRCalls))
	}
	deleteUICalls := fakeCatalog.FilterCalls("DeleteUIPlugin")
	if len(deleteUICalls) != 1 {
		t.Errorf("expected 1 DeleteUIPlugin call, got %d", len(deleteUICalls))
	}
}

// TestCleanup_GitMode tests that cleanup uninstalls the UIPlugin Helm release for Git source mode.
func TestCleanup_GitMode(t *testing.T) {
	scheme := newTestScheme(t)
	ext := createGitInstallAIExtension("test-git")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	fakeHelm := helm.NewFake()
	fakeCatalog := rancher.NewFake()
	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		HelmEngine: fakeHelm,
		Catalog:    fakeCatalog,
	}

	err := reconciler.cleanup(context.Background(), ext)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	uninstalls := filterCalls(fakeHelm.Calls, "Uninstall")
	if len(uninstalls) != 1 {
		t.Fatalf("expected 1 Uninstall call for git mode UIPlugin release, got %d", len(uninstalls))
	}
	if uninstalls[0].Namespace != rancher.UIPluginNamespace {
		t.Errorf("expected namespace %s, got %s", rancher.UIPluginNamespace, uninstalls[0].Namespace)
	}
	if uninstalls[0].Name != "ai-factory" {
		t.Errorf("expected release name ai-factory, got %s", uninstalls[0].Name)
	}
}

// TestCleanupStaleResources_NameChange verifies that changing extension.name cleans up old resources.
func TestCleanupStaleResources_NameChange(t *testing.T) {
	scheme := newTestScheme(t)
	ext := createInstallAIExtension("test-ext")
	ext.Status.ActiveExtensionName = "old-extension"
	ext.Status.ActiveSourceKind = aifv1.ExtensionSourceKindHelm
	ext.Status.HelmReleaseName = "old-chart"

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ext).
		WithStatusSubresource(&aifv1.InstallAIExtension{}).
		Build()

	fakeHelm := helm.NewFake()
	fakeCatalog := rancher.NewFake()
	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		HelmEngine: fakeHelm,
		Catalog:    fakeCatalog,
	}

	if err := reconciler.cleanupStaleResources(context.Background(), ext); err != nil {
		t.Fatalf("cleanupStaleResources failed: %v", err)
	}

	// Verify catalog cleanup for old name
	deleteCRCalls := fakeCatalog.FilterCalls("DeleteClusterRepo")
	if len(deleteCRCalls) != 1 {
		t.Fatalf("expected 1 DeleteClusterRepo call, got %d", len(deleteCRCalls))
	}
	if deleteCRCalls[0].ExtensionName != "old-extension" {
		t.Errorf("expected DeleteClusterRepo for old-extension, got %s", deleteCRCalls[0].ExtensionName)
	}

	deleteUICalls := fakeCatalog.FilterCalls("DeleteUIPlugin")
	if len(deleteUICalls) != 1 {
		t.Fatalf("expected 1 DeleteUIPlugin call, got %d", len(deleteUICalls))
	}
	if deleteUICalls[0].ExtensionName != "old-extension" {
		t.Errorf("expected DeleteUIPlugin for old-extension, got %s", deleteUICalls[0].ExtensionName)
	}

	// Old Helm release should be uninstalled
	uninstalls := filterCalls(fakeHelm.Calls, "Uninstall")
	if len(uninstalls) != 1 {
		t.Fatalf("expected 1 Uninstall call, got %d", len(uninstalls))
	}
	if uninstalls[0].Name != "old-chart" {
		t.Errorf("expected uninstall of old-chart, got %s", uninstalls[0].Name)
	}

	if ext.Status.HelmReleaseName != "" {
		t.Errorf("expected HelmReleaseName cleared, got %q", ext.Status.HelmReleaseName)
	}
}

// TestCleanupStaleResources_HelmToGit verifies source switch cleanup.
func TestCleanupStaleResources_HelmToGit(t *testing.T) {
	scheme := newTestScheme(t)
	ext := createGitInstallAIExtension("test-ext")
	ext.Status.ActiveExtensionName = "ai-factory"
	ext.Status.ActiveSourceKind = aifv1.ExtensionSourceKindHelm
	ext.Status.HelmReleaseName = "aif-ui"
	ext.Status.HelmReleaseRevision = 3
	ext.Status.Conditions = []metav1.Condition{
		{Type: conditions.TypeHelmInstalled, Status: metav1.ConditionTrue, Reason: conditions.ReasonInstalled},
		{Type: conditions.TypeDeploymentReady, Status: metav1.ConditionTrue, Reason: conditions.ReasonDeploymentAvailable},
		{Type: conditions.TypeServiceReady, Status: metav1.ConditionTrue, Reason: conditions.ReasonServiceCreated},
		{Type: conditions.TypeClusterRepoReady, Status: metav1.ConditionTrue, Reason: conditions.ReasonClusterRepoCreated},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ext).
		WithStatusSubresource(&aifv1.InstallAIExtension{}).
		Build()

	fakeHelm := helm.NewFake()
	fakeCatalog := rancher.NewFake()
	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		HelmEngine: fakeHelm,
		Catalog:    fakeCatalog,
	}

	if err := reconciler.cleanupStaleResources(context.Background(), ext); err != nil {
		t.Fatalf("cleanupStaleResources failed: %v", err)
	}

	uninstalls := filterCalls(fakeHelm.Calls, "Uninstall")
	if len(uninstalls) != 1 {
		t.Fatalf("expected 1 Uninstall call, got %d", len(uninstalls))
	}
	if uninstalls[0].Name != "aif-ui" {
		t.Errorf("expected uninstall of aif-ui, got %s", uninstalls[0].Name)
	}

	if ext.Status.HelmReleaseName != "" {
		t.Errorf("expected HelmReleaseName cleared, got %q", ext.Status.HelmReleaseName)
	}
	if ext.Status.HelmReleaseRevision != 0 {
		t.Errorf("expected HelmReleaseRevision cleared, got %d", ext.Status.HelmReleaseRevision)
	}

	for _, condType := range []string{
		conditions.TypeHelmInstalled,
		conditions.TypeDeploymentReady,
		conditions.TypeServiceReady,
	} {
		if cond := findCondition(ext.Status.Conditions, condType); cond != nil {
			t.Errorf("expected %s condition removed after Helm→Git switch", condType)
		}
	}

	if cond := findCondition(ext.Status.Conditions, conditions.TypeClusterRepoReady); cond == nil {
		t.Error("expected ClusterRepoReady condition to survive source switch")
	}
}

// TestCleanupStaleResources_GitToHelm verifies Git-to-Helm switch cleanup.
func TestCleanupStaleResources_GitToHelm(t *testing.T) {
	scheme := newTestScheme(t)
	ext := createInstallAIExtension("test-ext")
	ext.Status.ActiveExtensionName = "ai-factory"
	ext.Status.ActiveSourceKind = aifv1.ExtensionSourceKindGit

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ext).
		WithStatusSubresource(&aifv1.InstallAIExtension{}).
		Build()

	fakeHelm := helm.NewFake()
	fakeCatalog := rancher.NewFake()
	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		HelmEngine: fakeHelm,
		Catalog:    fakeCatalog,
	}

	if err := reconciler.cleanupStaleResources(context.Background(), ext); err != nil {
		t.Fatalf("cleanupStaleResources failed: %v", err)
	}

	uninstalls := filterCalls(fakeHelm.Calls, "Uninstall")
	if len(uninstalls) != 1 {
		t.Fatalf("expected 1 Uninstall call for Git UIPlugin release, got %d", len(uninstalls))
	}
	if uninstalls[0].Name != "ai-factory" {
		t.Errorf("expected uninstall of ai-factory, got %s", uninstalls[0].Name)
	}
}

// TestCleanup_BothNames verifies that deletion cleans up both current and previously-active names.
func TestCleanup_BothNames(t *testing.T) {
	scheme := newTestScheme(t)
	ext := createInstallAIExtension("test-ext")
	ext.Status.ActiveExtensionName = "old-extension"
	ext.Status.HelmReleaseName = "aif-ui"

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ext).
		WithStatusSubresource(&aifv1.InstallAIExtension{}).
		Build()

	fakeHelm := helm.NewFake()
	fakeCatalog := rancher.NewFake()
	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		HelmEngine: fakeHelm,
		Catalog:    fakeCatalog,
	}

	err := reconciler.cleanup(context.Background(), ext)
	if err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}

	// Both names should trigger DeleteClusterRepo/DeleteUIPlugin calls
	deleteCRCalls := fakeCatalog.FilterCalls("DeleteClusterRepo")
	if len(deleteCRCalls) != 2 {
		t.Errorf("expected 2 DeleteClusterRepo calls (current + old name), got %d", len(deleteCRCalls))
	}
	deleteUICalls := fakeCatalog.FilterCalls("DeleteUIPlugin")
	if len(deleteUICalls) != 2 {
		t.Errorf("expected 2 DeleteUIPlugin calls (current + old name), got %d", len(deleteUICalls))
	}
}

// filterCalls filters helm.FakeCall by method name
func filterCalls(calls []helm.FakeCall, method string) []helm.FakeCall {
	out := []helm.FakeCall{}
	for _, c := range calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}
