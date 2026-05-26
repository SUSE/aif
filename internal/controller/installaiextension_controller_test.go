package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/conditions"
	"github.com/SUSE/aif/pkg/helm"
	openapi_v2 "github.com/google/gnostic-models/openapiv2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/openapi"
	restclient "k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// fakeDiscovery implements discovery.DiscoveryInterface for testing
type fakeDiscovery struct {
	shouldFail bool
}

func (f *fakeDiscovery) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	if f.shouldFail {
		return nil, errors.New("UIPlugin CRD not found")
	}
	return &metav1.APIResourceList{
		GroupVersion: groupVersion,
	}, nil
}

func (f *fakeDiscovery) ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
	return nil, nil, errors.New("not implemented")
}

func (f *fakeDiscovery) ServerGroups() (*metav1.APIGroupList, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeDiscovery) ServerResourcesForGroupVersionKind(gvk schema.GroupVersionKind) (*metav1.APIResourceList, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeDiscovery) ServerPreferredResources() ([]*metav1.APIResourceList, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeDiscovery) ServerPreferredNamespacedResources() ([]*metav1.APIResourceList, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeDiscovery) ServerVersion() (*version.Info, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeDiscovery) OpenAPISchema() (*openapi_v2.Document, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeDiscovery) OpenAPIV3() openapi.Client {
	return nil
}

func (f *fakeDiscovery) RESTClient() restclient.Interface {
	return nil
}

func (f *fakeDiscovery) WithLegacy() discovery.DiscoveryInterface {
	return f
}

var _ discovery.DiscoveryInterface = &fakeDiscovery{}

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

// createInstallAIExtension creates a test InstallAIExtension resource (cluster-scoped).
func createInstallAIExtension(name, _ string) *aifv1.InstallAIExtension {
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

// createHelmDeployedResources creates the Deployment, Service, and UIPlugin that
// the Helm chart would create in a real install. Tests pre-create these so
// reconcileHelmSource can discover them.
func createHelmDeployedResources(t *testing.T, ctx context.Context, fakeClient *fake.ClientBuilder, releaseName, extensionName string) {
	t.Helper()
	// Not used — resources are added via WithObjects on builder instead.
}

func helmDeployment(releaseName string) *appsv1.Deployment {
	replicas := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      releaseName,
			Namespace: uiPluginNamespace,
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
			Namespace: uiPluginNamespace,
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

func uiPluginObj(extensionName string) *unstructured.Unstructured {
	plugin := &unstructured.Unstructured{}
	plugin.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "catalog.cattle.io",
		Version: "v1",
		Kind:    "UIPlugin",
	})
	plugin.SetNamespace(uiPluginNamespace)
	plugin.SetName(extensionName)
	return plugin
}

// TestInstallAIExtensionReconciler_HappyPath tests successful Helm source installation.
func TestInstallAIExtensionReconciler_HappyPath(t *testing.T) {
	scheme := newTestScheme(t)
	ext := createInstallAIExtension("test-ext", "")

	deploy := helmDeployment("aif-ui")
	svc := helmService("aif-ui")

	// Serve a mock index.yaml for fetchIndexMetadata
	indexServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		fmt.Fprint(w, `apiVersion: v1
entries:
  ai-factory:
    - name: ai-factory
      version: "1.0.0"
      annotations:
        catalog.cattle.io/display-name: AI Factory
        catalog.cattle.io/rancher-version: ">= 2.10.0"
        catalog.cattle.io/ui-extensions-version: ">= 3.0.0 < 4.0.0"
`)
	}))
	defer indexServer.Close()

	// Override the Service URL so the controller fetches from our test server
	svc.Name = "test-svc"
	svc.Spec.Ports[0].Port = 8080

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ext, deploy, svc).
		WithStatusSubresource(&aifv1.InstallAIExtension{}).
		Build()

	fakeHelm := helm.NewFake()
	fakeDisc := &fakeDiscovery{shouldFail: false}
	recorder := &fakeRecorder{}

	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		Scheme:     scheme,
		HelmEngine: fakeHelm,
		Discovery:  fakeDisc,
		Recorder:   recorder,
		HTTPClient: indexServer.Client(),
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

	// Second reconcile: full Helm flow (UIPlugin created by controller)
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
		if call.Namespace != uiPluginNamespace {
			t.Errorf("expected namespace %s, got %s", uiPluginNamespace, call.Namespace)
		}
		if call.ReleaseName != "aif-ui" {
			t.Errorf("expected release name aif-ui, got %s", call.ReleaseName)
		}
		if call.ChartRef != "oci://registry.suse.com/ai/charts/aif-ui:1.0.0" {
			t.Errorf("expected chart ref with version, got %s", call.ChartRef)
		}
	}

	// Verify ClusterRepo was created with back-reference label
	var repo unstructured.Unstructured
	repo.SetGroupVersionKind(clusterRepoGVK())
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "ai-factory-charts"}, &repo); err != nil {
		t.Fatalf("expected ClusterRepo ai-factory-charts to exist: %v", err)
	}
	repoLabels := repo.GetLabels()
	if repoLabels["ai.suse.com/installaiextension"] != "test-ext" {
		t.Errorf("expected back-reference label ai.suse.com/installaiextension=test-ext, got %q", repoLabels["ai.suse.com/installaiextension"])
	}
	if repoLabels["catalog.cattle.io/ui-extensions-catalog-image"] != "ai-factory" {
		t.Errorf("expected catalog label, got %q", repoLabels["catalog.cattle.io/ui-extensions-catalog-image"])
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

	// Verify tracking fields are set
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
	fakeDisc := &fakeDiscovery{shouldFail: false}
	recorder := &fakeRecorder{}

	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		Scheme:     scheme,
		HelmEngine: fakeHelm,
		Discovery:  fakeDisc,
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

	// Git mode: ClusterRepoReady + UIPluginReady + Ready should be set
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

	// Verify ClusterRepo was created with git fields
	var repo unstructured.Unstructured
	repo.SetGroupVersionKind(clusterRepoGVK())
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "ai-factory-charts"}, &repo); err != nil {
		t.Fatalf("expected ClusterRepo ai-factory-charts: %v", err)
	}
	gitRepo, _, _ := unstructured.NestedString(repo.Object, "spec", "gitRepo")
	if gitRepo != "https://github.com/suse/aif-ui-extension" {
		t.Errorf("expected gitRepo URL, got %q", gitRepo)
	}
	gitBranch, _, _ := unstructured.NestedString(repo.Object, "spec", "gitBranch")
	if gitBranch != "gh-pages" {
		t.Errorf("expected gitBranch gh-pages, got %q", gitBranch)
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
	if repoInstalls[0].Namespace != uiPluginNamespace {
		t.Errorf("expected namespace %s, got %s", uiPluginNamespace, repoInstalls[0].Namespace)
	}
	if repoInstalls[0].Name != "ai-factory" {
		t.Errorf("expected release name ai-factory, got %s", repoInstalls[0].Name)
	}

	// Verify tracking fields are set
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
	ext := createInstallAIExtension("test-ext", "")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ext).
		WithStatusSubresource(&aifv1.InstallAIExtension{}).
		Build()

	fakeHelm := helm.NewFake()
	fakeDisc := &fakeDiscovery{shouldFail: true}
	recorder := &fakeRecorder{}

	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		Scheme:     scheme,
		HelmEngine: fakeHelm,
		Discovery:  fakeDisc,
		Recorder:   recorder,
	}

	ctx := context.Background()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-ext"}}

	// First reconcile: adds finalizer
	_, _ = reconciler.Reconcile(ctx, req)

	// Second reconcile: CRD check fails — no requeue (permanent error)
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

	// Verify Helm was NOT called
	installs := filterCalls(fakeHelm.Calls, "InstallChartFromRepo")
	if len(installs) != 0 {
		t.Errorf("expected 0 Helm install calls when CRD missing, got %d", len(installs))
	}

	// Verify warning event
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

// TestInstallAIExtensionReconciler_HelmInstallFailed tests transient failure during Helm install.
func TestInstallAIExtensionReconciler_HelmInstallFailed(t *testing.T) {
	scheme := newTestScheme(t)
	ext := createInstallAIExtension("test-ext", "")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ext).
		WithStatusSubresource(&aifv1.InstallAIExtension{}).
		Build()

	fakeHelm := helm.NewFake()
	fakeHelm.InstallResult = func(helm.InstallRequest) (helm.ReleaseStatus, error) {
		return helm.ReleaseStatus{}, errors.New("failed to pull chart")
	}
	fakeDisc := &fakeDiscovery{shouldFail: false}
	recorder := &fakeRecorder{}

	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		Scheme:     scheme,
		HelmEngine: fakeHelm,
		Discovery:  fakeDisc,
		Recorder:   recorder,
	}

	ctx := context.Background()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-ext"}}

	// First reconcile: adds finalizer
	_, _ = reconciler.Reconcile(ctx, req)

	// Second reconcile: Helm install fails → no error returned (prevents blocking retry loop)
	_, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("expected nil error (no retry on install failure), got: %v", err)
	}

	// Verify event was recorded (happens before error return)
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

// TestInstallAIExtensionReconciler_UIPluginCreatedWithoutMetadata tests that UIPlugin is created
// even when index.yaml fetch fails (metadata is empty but plugin is still created).
func TestInstallAIExtensionReconciler_UIPluginCreatedWithoutMetadata(t *testing.T) {
	scheme := newTestScheme(t)
	ext := createInstallAIExtension("test-ext", "")

	deploy := helmDeployment("aif-ui")
	svc := helmService("aif-ui")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ext, deploy, svc).
		WithStatusSubresource(&aifv1.InstallAIExtension{}).
		Build()

	fakeHelm := helm.NewFake()
	fakeDisc := &fakeDiscovery{shouldFail: false}
	recorder := &fakeRecorder{}

	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		Scheme:     scheme,
		HelmEngine: fakeHelm,
		Discovery:  fakeDisc,
		Recorder:   recorder,
	}

	ctx := context.Background()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-ext"}}

	// First reconcile: adds finalizer
	_, _ = reconciler.Reconcile(ctx, req)

	// Second reconcile: Helm succeeds, index.yaml fetch fails (no HTTPClient/server),
	// but UIPlugin is still created with empty metadata
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

	// Verify UIPlugin was created
	var plugin unstructured.Unstructured
	plugin.SetGroupVersionKind(uiPluginGVK())
	if err := fakeClient.Get(ctx, types.NamespacedName{
		Name:      "ai-factory",
		Namespace: uiPluginNamespace,
	}, &plugin); err != nil {
		t.Fatalf("UIPlugin not created: %v", err)
	}
}

// TestInstallAIExtensionReconciler_DeploymentNotReady tests requeue when Deployment not yet ready.
func TestInstallAIExtensionReconciler_DeploymentNotReady(t *testing.T) {
	scheme := newTestScheme(t)
	ext := createInstallAIExtension("test-ext", "")

	deploy := helmDeployment("aif-ui")
	deploy.Status.ReadyReplicas = 0 // not ready yet

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ext, deploy).
		WithStatusSubresource(&aifv1.InstallAIExtension{}).
		Build()

	fakeHelm := helm.NewFake()
	fakeDisc := &fakeDiscovery{shouldFail: false}
	recorder := &fakeRecorder{}

	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		Scheme:     scheme,
		HelmEngine: fakeHelm,
		Discovery:  fakeDisc,
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
	ext := createInstallAIExtension("test-ext", "")

	deploy := helmDeployment("aif-ui")
	deploy.Status.ReadyReplicas = 0

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "aif-ui-abc123",
			Namespace: uiPluginNamespace,
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
	fakeDisc := &fakeDiscovery{shouldFail: false}
	recorder := &fakeRecorder{}

	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		Scheme:     scheme,
		HelmEngine: fakeHelm,
		Discovery:  fakeDisc,
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
	if !containsSubstring(deployCond.Message, "ImagePullBackOff") {
		t.Errorf("expected message to contain ImagePullBackOff, got: %s", deployCond.Message)
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestInstallAIExtensionReconciler_Deletion tests finalizer cleanup.
func TestInstallAIExtensionReconciler_Deletion(t *testing.T) {
	scheme := newTestScheme(t)
	ext := createInstallAIExtension("test-ext", "")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ext).
		WithStatusSubresource(&aifv1.InstallAIExtension{}).
		Build()

	fakeHelm := helm.NewFake()
	fakeDisc := &fakeDiscovery{shouldFail: false}
	recorder := &fakeRecorder{}

	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		Scheme:     scheme,
		HelmEngine: fakeHelm,
		Discovery:  fakeDisc,
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

	// Verify finalizer added
	var updated aifv1.InstallAIExtension
	if err := fakeClient.Get(ctx, req.NamespacedName, &updated); err != nil {
		t.Fatalf("failed to get updated resource: %v", err)
	}
	if len(updated.Finalizers) != 1 || updated.Finalizers[0] != extensionFinalizerName {
		t.Errorf("finalizer not added: %v", updated.Finalizers)
	}

	// Delete the resource
	if err := fakeClient.Delete(ctx, &updated); err != nil {
		t.Fatalf("failed to delete resource: %v", err)
	}

	// Reconcile deletion
	result, err = reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("deletion reconcile failed: %v", err)
	}

	// Verify finalizer removed
	err = fakeClient.Get(ctx, req.NamespacedName, &updated)
	if err == nil && len(updated.Finalizers) != 0 {
		t.Errorf("finalizer not removed: %v", updated.Finalizers)
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
	fakeDisc := &fakeDiscovery{shouldFail: false}
	recorder := &fakeRecorder{}

	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		Scheme:     scheme,
		HelmEngine: fakeHelm,
		Discovery:  fakeDisc,
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

// TestCheckRancherCRDs tests the checkRancherCRDs helper method.
func TestCheckRancherCRDs(t *testing.T) {
	tests := []struct {
		name        string
		shouldFail  bool
		expectError bool
	}{
		{
			name:        "CRDs exist",
			shouldFail:  false,
			expectError: false,
		},
		{
			name:        "CRDs missing",
			shouldFail:  true,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeDisc := &fakeDiscovery{shouldFail: tt.shouldFail}
			reconciler := &InstallAIExtensionReconciler{
				Discovery: fakeDisc,
			}

			err := reconciler.checkRancherCRDs(context.Background())
			if tt.expectError && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

// TestEnsureUIPluginHelm tests that ensureUIPluginHelm creates a UIPlugin with correct spec.
func TestEnsureUIPluginHelm(t *testing.T) {
	scheme := newTestScheme(t)
	ext := createInstallAIExtension("test-ext", "")

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := &InstallAIExtensionReconciler{Client: fakeClient}

	meta := uiPluginMeta{
		DisplayName:       "Test Extension",
		RancherVersion:    ">= 2.10.0",
		ExtensionsVersion: ">= 3.0.0 < 4.0.0",
	}

	err := reconciler.ensureUIPluginHelm(context.Background(), ext, "http://test-svc:8080/plugin/ai-factory-1.0.0", meta)
	if err != nil {
		t.Fatalf("ensureUIPluginHelm failed: %v", err)
	}

	var plugin unstructured.Unstructured
	plugin.SetGroupVersionKind(uiPluginGVK())
	if err := fakeClient.Get(context.Background(), types.NamespacedName{
		Name:      "ai-factory",
		Namespace: uiPluginNamespace,
	}, &plugin); err != nil {
		t.Fatalf("UIPlugin not found: %v", err)
	}

	pluginLabels := plugin.GetLabels()
	if pluginLabels["ai.suse.com/installaiextension"] != "test-ext" {
		t.Errorf("expected back-reference label, got %q", pluginLabels["ai.suse.com/installaiextension"])
	}

	name, _, _ := unstructured.NestedString(plugin.Object, "spec", "plugin", "name")
	if name != "ai-factory" {
		t.Errorf("expected plugin name ai-factory, got %q", name)
	}
	ver, _, _ := unstructured.NestedString(plugin.Object, "spec", "plugin", "version")
	if ver != "1.0.0" {
		t.Errorf("expected plugin version 1.0.0, got %q", ver)
	}
	endpoint, _, _ := unstructured.NestedString(plugin.Object, "spec", "plugin", "endpoint")
	if endpoint != "http://test-svc:8080/plugin/ai-factory-1.0.0" {
		t.Errorf("expected plugin endpoint, got %q", endpoint)
	}
	displayName, _, _ := unstructured.NestedString(plugin.Object, "spec", "plugin", "metadata", "catalog.cattle.io/display-name")
	if displayName != "Test Extension" {
		t.Errorf("expected display-name, got %q", displayName)
	}
}

func TestEnsureUIPluginHelm_Update(t *testing.T) {
	scheme := newTestScheme(t)
	ext := createInstallAIExtension("test-ext", "")

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := &InstallAIExtensionReconciler{Client: fakeClient}

	// Create initial UIPlugin
	initialMeta := uiPluginMeta{
		DisplayName:       "Old Name",
		RancherVersion:    ">= 2.9.0",
		ExtensionsVersion: ">= 2.0.0 < 3.0.0",
	}
	err := reconciler.ensureUIPluginHelm(context.Background(), ext, "http://old-svc:8080/plugin/ai-factory-0.9.0", initialMeta)
	if err != nil {
		t.Fatalf("initial ensureUIPluginHelm failed: %v", err)
	}

	// Update with new values
	updatedMeta := uiPluginMeta{
		DisplayName:       "Updated Name",
		RancherVersion:    ">= 2.10.0",
		ExtensionsVersion: ">= 3.0.0 < 4.0.0",
	}
	err = reconciler.ensureUIPluginHelm(context.Background(), ext, "http://new-svc:8080/plugin/ai-factory-1.0.0", updatedMeta)
	if err != nil {
		t.Fatalf("update ensureUIPluginHelm failed: %v", err)
	}

	var plugin unstructured.Unstructured
	plugin.SetGroupVersionKind(uiPluginGVK())
	if err := fakeClient.Get(context.Background(), types.NamespacedName{
		Name:      "ai-factory",
		Namespace: uiPluginNamespace,
	}, &plugin); err != nil {
		t.Fatalf("UIPlugin not found: %v", err)
	}

	endpoint, _, _ := unstructured.NestedString(plugin.Object, "spec", "plugin", "endpoint")
	if endpoint != "http://new-svc:8080/plugin/ai-factory-1.0.0" {
		t.Errorf("expected updated endpoint, got %q", endpoint)
	}
	displayName, _, _ := unstructured.NestedString(plugin.Object, "spec", "plugin", "metadata", "catalog.cattle.io/display-name")
	if displayName != "Updated Name" {
		t.Errorf("expected updated display-name, got %q", displayName)
	}
	rancherVer, _, _ := unstructured.NestedString(plugin.Object, "spec", "plugin", "metadata", "catalog.cattle.io/rancher-version")
	if rancherVer != ">= 2.10.0" {
		t.Errorf("expected updated rancher-version, got %q", rancherVer)
	}
}

// TestCleanup tests the cleanup helper method.
func TestCleanup(t *testing.T) {
	scheme := newTestScheme(t)
	ext := createInstallAIExtension("test-ext", "")
	ext.Status.HelmReleaseName = "aif-ui"

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	fakeHelm := helm.NewFake()
	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		HelmEngine: fakeHelm,
	}

	err := reconciler.cleanup(context.Background(), ext)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify Helm uninstalls: one for HelmReleaseName, one for extension name
	uninstalls := filterCalls(fakeHelm.Calls, "Uninstall")
	if len(uninstalls) != 2 {
		t.Fatalf("expected 2 Uninstall calls, got %d", len(uninstalls))
	}

	// First call: HelmReleaseName
	if uninstalls[0].Name != "aif-ui" {
		t.Errorf("expected first uninstall of aif-ui, got %s", uninstalls[0].Name)
	}
	if uninstalls[0].Namespace != uiPluginNamespace {
		t.Errorf("expected namespace %s, got %s", uiPluginNamespace, uninstalls[0].Namespace)
	}

	// Second call: extension name (Git source UIPlugin release)
	if uninstalls[1].Name != "ai-factory" {
		t.Errorf("expected second uninstall of ai-factory, got %s", uninstalls[1].Name)
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
	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		HelmEngine: fakeHelm,
	}

	err := reconciler.cleanup(context.Background(), ext)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify Helm uninstall was called for the UIPlugin release (extension name)
	uninstalls := filterCalls(fakeHelm.Calls, "Uninstall")
	if len(uninstalls) != 1 {
		t.Fatalf("expected 1 Uninstall call for git mode UIPlugin release, got %d", len(uninstalls))
	}
	if uninstalls[0].Namespace != uiPluginNamespace {
		t.Errorf("expected namespace %s, got %s", uiPluginNamespace, uninstalls[0].Namespace)
	}
	if uninstalls[0].Name != "ai-factory" {
		t.Errorf("expected release name ai-factory, got %s", uninstalls[0].Name)
	}
}

// TestReconcile_ObservedGeneration tests that ObservedGeneration is set correctly.
func TestReconcile_ObservedGeneration(t *testing.T) {
	scheme := newTestScheme(t)
	ext := createInstallAIExtension("test-ext", "")
	ext.Generation = 5

	deploy := helmDeployment("aif-ui")
	svc := helmService("aif-ui")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ext, deploy, svc).
		WithStatusSubresource(&aifv1.InstallAIExtension{}).
		Build()

	fakeHelm := helm.NewFake()
	fakeDisc := &fakeDiscovery{shouldFail: false}
	recorder := &fakeRecorder{}

	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		Scheme:     scheme,
		HelmEngine: fakeHelm,
		Discovery:  fakeDisc,
		Recorder:   recorder,
	}

	ctx := context.Background()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-ext"}}

	// First reconcile: adds finalizer
	_, _ = reconciler.Reconcile(ctx, req)

	// Second reconcile: full install (UIPlugin created by controller)
	_, _ = reconciler.Reconcile(ctx, req)

	var updated aifv1.InstallAIExtension
	if err := fakeClient.Get(ctx, req.NamespacedName, &updated); err != nil {
		t.Fatalf("failed to get updated resource: %v", err)
	}

	if updated.Status.ObservedGeneration != ext.Generation {
		t.Errorf("expected observedGeneration=%d, got %d", ext.Generation, updated.Status.ObservedGeneration)
	}
}

// TestDeriveReleaseName tests release name derivation from chart URL.
func TestDeriveReleaseName(t *testing.T) {
	tests := []struct {
		chartURL string
		expected string
	}{
		{"oci://registry.suse.com/ai/charts/aif-ui", "aif-ui"},
		{"oci://ghcr.io/suse/chart/suse-ai-lifecycle-manager", "suse-ai-lifecycle-manager"},
		{"https://example.com/charts/my-extension", "my-extension"},
	}
	for _, tt := range tests {
		t.Run(tt.chartURL, func(t *testing.T) {
			got := deriveReleaseName(tt.chartURL)
			if got != tt.expected {
				t.Errorf("deriveReleaseName(%q) = %q, want %q", tt.chartURL, got, tt.expected)
			}
		})
	}
}

// TestClusterRepoName tests cluster repo name derivation.
func TestClusterRepoName(t *testing.T) {
	if got := clusterRepoName("ai-factory"); got != "ai-factory-charts" {
		t.Errorf("clusterRepoName(ai-factory) = %q, want ai-factory-charts", got)
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

	ext := createInstallAIExtension("test-ext", "")
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
	fakeDisc := &fakeDiscovery{shouldFail: false}
	recorder := &fakeRecorder{}

	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		Scheme:     scheme,
		HelmEngine: fakeHelm,
		Discovery:  fakeDisc,
		Recorder:   recorder,
	}

	ctx := context.Background()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-ext"}}

	// First reconcile: adds finalizer
	_, _ = reconciler.Reconcile(ctx, req)

	// Second reconcile: full install with values
	result, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if result.RequeueAfter != healthCheckInterval {
		t.Errorf("expected requeue after %v, got %v", healthCheckInterval, result.RequeueAfter)
	}

	// Verify values were passed as Overrides.Workload
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

// TestFetchIndexMetadata tests parsing of Helm repo index.yaml.
func TestFetchIndexMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		fmt.Fprint(w, `apiVersion: v1
entries:
  my-extension:
    - name: my-extension
      version: "1.2.0"
      annotations:
        catalog.cattle.io/display-name: My Extension
        catalog.cattle.io/rancher-version: ">= 2.10.0"
        catalog.cattle.io/ui-extensions-version: ">= 3.0.0 < 4.0.0"
    - name: my-extension
      version: "1.1.0"
      annotations:
        catalog.cattle.io/display-name: My Extension Old
`)
	}))
	defer server.Close()

	reconciler := &InstallAIExtensionReconciler{HTTPClient: server.Client()}

	meta, err := reconciler.fetchIndexMetadata(context.Background(), server.URL+"/index.yaml", "my-extension", "1.2.0")
	if err != nil {
		t.Fatalf("fetchIndexMetadata failed: %v", err)
	}
	if meta.DisplayName != "My Extension" {
		t.Errorf("expected display name 'My Extension', got %q", meta.DisplayName)
	}
	if meta.RancherVersion != ">= 2.10.0" {
		t.Errorf("expected rancher version, got %q", meta.RancherVersion)
	}
	if meta.ExtensionsVersion != ">= 3.0.0 < 4.0.0" {
		t.Errorf("expected extensions version, got %q", meta.ExtensionsVersion)
	}
}

// TestFetchIndexMetadata_VersionNotFound tests error when version is missing.
func TestFetchIndexMetadata_VersionNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `apiVersion: v1
entries:
  my-extension:
    - name: my-extension
      version: "1.0.0"
`)
	}))
	defer server.Close()

	reconciler := &InstallAIExtensionReconciler{HTTPClient: server.Client()}

	_, err := reconciler.fetchIndexMetadata(context.Background(), server.URL+"/index.yaml", "my-extension", "9.9.9")
	if err == nil {
		t.Error("expected error for missing version, got nil")
	}
}

// TestGitRepoToRawURL tests GitHub URL conversion.
func TestGitRepoToRawURL(t *testing.T) {
	tests := []struct {
		name      string
		repoURL   string
		branch    string
		expected  string
		expectErr bool
	}{
		{
			name:     "standard github URL",
			repoURL:  "https://github.com/suse/aif-ui-extension",
			branch:   "gh-pages",
			expected: "https://raw.githubusercontent.com/suse/aif-ui-extension/refs/heads/gh-pages",
		},
		{
			name:     "github URL with .git suffix",
			repoURL:  "https://github.com/suse/aif-ui-extension.git",
			branch:   "main",
			expected: "https://raw.githubusercontent.com/suse/aif-ui-extension/refs/heads/main",
		},
		{
			name:      "non-github host",
			repoURL:   "https://gitlab.com/suse/aif-ui-extension",
			branch:    "gh-pages",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := gitRepoToRawURL(tt.repoURL, tt.branch)
			if tt.expectErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

// TestCleanupStaleResources_NameChange verifies that changing extension.name
// cleans up old ClusterRepo and UIPlugin.
func TestCleanupStaleResources_NameChange(t *testing.T) {
	scheme := newTestScheme(t)
	ext := createInstallAIExtension("test-ext", "")

	// Simulate previous reconcile with old name
	ext.Status.ActiveExtensionName = "old-extension"
	ext.Status.ActiveSourceKind = aifv1.ExtensionSourceKindHelm
	ext.Status.HelmReleaseName = "old-chart"

	// Create old ClusterRepo and UIPlugin that should be cleaned up
	oldRepo := &unstructured.Unstructured{}
	oldRepo.SetGroupVersionKind(clusterRepoGVK())
	oldRepo.SetName(clusterRepoName("old-extension"))

	oldPlugin := &unstructured.Unstructured{}
	oldPlugin.SetGroupVersionKind(uiPluginGVK())
	oldPlugin.SetName("old-extension")
	oldPlugin.SetNamespace(uiPluginNamespace)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ext, oldRepo, oldPlugin).
		WithStatusSubresource(&aifv1.InstallAIExtension{}).
		Build()

	fakeHelm := helm.NewFake()
	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		HelmEngine: fakeHelm,
	}

	ctx := context.Background()
	reconciler.cleanupStaleResources(ctx, ext)

	// Old ClusterRepo should be deleted
	var repo unstructured.Unstructured
	repo.SetGroupVersionKind(clusterRepoGVK())
	err := fakeClient.Get(ctx, types.NamespacedName{Name: clusterRepoName("old-extension")}, &repo)
	if !apierrors.IsNotFound(err) {
		t.Errorf("expected old ClusterRepo to be deleted, got err: %v", err)
	}

	// Old UIPlugin should be deleted
	var plugin unstructured.Unstructured
	plugin.SetGroupVersionKind(uiPluginGVK())
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "old-extension", Namespace: uiPluginNamespace}, &plugin)
	if !apierrors.IsNotFound(err) {
		t.Errorf("expected old UIPlugin to be deleted, got err: %v", err)
	}

	// Old Helm release should be uninstalled
	uninstalls := filterCalls(fakeHelm.Calls, "Uninstall")
	if len(uninstalls) != 1 {
		t.Fatalf("expected 1 Uninstall call, got %d", len(uninstalls))
	}
	if uninstalls[0].Name != "old-chart" {
		t.Errorf("expected uninstall of old-chart, got %s", uninstalls[0].Name)
	}

	// HelmReleaseName should be cleared
	if ext.Status.HelmReleaseName != "" {
		t.Errorf("expected HelmReleaseName cleared, got %q", ext.Status.HelmReleaseName)
	}
}

// TestCleanupStaleResources_HelmToGit verifies that switching from Helm to Git
// cleans up the Helm release and removes stale conditions.
func TestCleanupStaleResources_HelmToGit(t *testing.T) {
	scheme := newTestScheme(t)
	ext := createGitInstallAIExtension("test-ext")

	// Simulate previous Helm reconcile
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
	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		HelmEngine: fakeHelm,
	}

	ctx := context.Background()
	reconciler.cleanupStaleResources(ctx, ext)

	// Helm release should be uninstalled
	uninstalls := filterCalls(fakeHelm.Calls, "Uninstall")
	if len(uninstalls) != 1 {
		t.Fatalf("expected 1 Uninstall call, got %d", len(uninstalls))
	}
	if uninstalls[0].Name != "aif-ui" {
		t.Errorf("expected uninstall of aif-ui, got %s", uninstalls[0].Name)
	}

	// HelmReleaseName and revision should be cleared
	if ext.Status.HelmReleaseName != "" {
		t.Errorf("expected HelmReleaseName cleared, got %q", ext.Status.HelmReleaseName)
	}
	if ext.Status.HelmReleaseRevision != 0 {
		t.Errorf("expected HelmReleaseRevision cleared, got %d", ext.Status.HelmReleaseRevision)
	}

	// Stale Helm-specific conditions should be removed
	for _, condType := range []string{
		conditions.TypeHelmInstalled,
		conditions.TypeDeploymentReady,
		conditions.TypeServiceReady,
	} {
		if cond := findCondition(ext.Status.Conditions, condType); cond != nil {
			t.Errorf("expected %s condition to be removed after Helm→Git switch, but it exists", condType)
		}
	}

	// ClusterRepoReady should survive (Git flow also sets it)
	if cond := findCondition(ext.Status.Conditions, conditions.TypeClusterRepoReady); cond == nil {
		t.Error("expected ClusterRepoReady condition to survive source switch")
	}
}

// TestCleanupStaleResources_GitToHelm verifies that switching from Git to Helm
// cleans up the UIPlugin Helm release from the Git flow.
func TestCleanupStaleResources_GitToHelm(t *testing.T) {
	scheme := newTestScheme(t)
	ext := createInstallAIExtension("test-ext", "")

	// Simulate previous Git reconcile
	ext.Status.ActiveExtensionName = "ai-factory"
	ext.Status.ActiveSourceKind = aifv1.ExtensionSourceKindGit

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ext).
		WithStatusSubresource(&aifv1.InstallAIExtension{}).
		Build()

	fakeHelm := helm.NewFake()
	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		HelmEngine: fakeHelm,
	}

	ctx := context.Background()
	reconciler.cleanupStaleResources(ctx, ext)

	// UIPlugin Helm release should be uninstalled
	uninstalls := filterCalls(fakeHelm.Calls, "Uninstall")
	if len(uninstalls) != 1 {
		t.Fatalf("expected 1 Uninstall call for Git UIPlugin release, got %d", len(uninstalls))
	}
	if uninstalls[0].Name != "ai-factory" {
		t.Errorf("expected uninstall of ai-factory, got %s", uninstalls[0].Name)
	}
}

// TestCleanup_BothNames verifies that deletion cleans up resources under
// both the current and previously-active extension names.
func TestCleanup_BothNames(t *testing.T) {
	scheme := newTestScheme(t)
	ext := createInstallAIExtension("test-ext", "")
	ext.Status.ActiveExtensionName = "old-extension"
	ext.Status.HelmReleaseName = "aif-ui"

	// Create resources for both old and new names
	oldRepo := &unstructured.Unstructured{}
	oldRepo.SetGroupVersionKind(clusterRepoGVK())
	oldRepo.SetName(clusterRepoName("old-extension"))

	newRepo := &unstructured.Unstructured{}
	newRepo.SetGroupVersionKind(clusterRepoGVK())
	newRepo.SetName(clusterRepoName("ai-factory"))

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ext, oldRepo, newRepo).
		WithStatusSubresource(&aifv1.InstallAIExtension{}).
		Build()

	fakeHelm := helm.NewFake()
	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		HelmEngine: fakeHelm,
	}

	ctx := context.Background()
	err := reconciler.cleanup(ctx, ext)
	if err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}

	// Both ClusterRepos should be deleted
	for _, name := range []string{"old-extension-charts", "ai-factory-charts"} {
		var repo unstructured.Unstructured
		repo.SetGroupVersionKind(clusterRepoGVK())
		if getErr := fakeClient.Get(ctx, types.NamespacedName{Name: name}, &repo); !apierrors.IsNotFound(getErr) {
			t.Errorf("expected ClusterRepo %s to be deleted", name)
		}
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
