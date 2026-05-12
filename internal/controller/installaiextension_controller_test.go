package controller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/conditions"
	"github.com/SUSE/aif/pkg/helm"
	openapi_v2 "github.com/google/gnostic-models/openapiv2"
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

// createInstallAIExtension creates a test InstallAIExtension resource
func createInstallAIExtension(name, namespace string) *aifv1.InstallAIExtension {
	return &aifv1.InstallAIExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Generation: 1,
		},
		Spec: aifv1.InstallAIExtensionSpec{
			Helm: aifv1.HelmConfig{
				Name:    "aif-ui",
				URL:     "oci://registry.suse.com/ai/charts/aif-ui:1.0.0",
				Version: "1.0.0",
			},
			Extension: aifv1.ExtensionConfig{
				Name:    "AI Factory",
				Version: "1.0.0",
			},
		},
	}
}

// TestInstallAIExtensionReconciler_HappyPath tests successful installation
func TestInstallAIExtensionReconciler_HappyPath(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := aifv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add aifv1 to scheme: %v", err)
	}

	ext := createInstallAIExtension("test-ext", "aif")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ext).
		WithStatusSubresource(&aifv1.InstallAIExtension{}).
		Build()

	fakeHelm := helm.NewFake()
	fakeDisc := &fakeDiscovery{shouldFail: false}
	recorder := &fakeRecorder{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		Scheme:     scheme,
		Logger:     logger,
		HelmEngine: fakeHelm,
		Discovery:  fakeDisc,
		Recorder:   recorder,
	}

	ctx := context.Background()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-ext",
			Namespace: "aif",
		},
	}

	// First reconcile: adds finalizer
	result, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("first reconcile failed: %v", err)
	}
	if !result.Requeue {
		t.Error("expected requeue after adding finalizer")
	}

	// Create UIPlugin to satisfy verification
	uiPlugin := &unstructured.Unstructured{}
	uiPlugin.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "catalog.cattle.io",
		Version: "v1",
		Kind:    "UIPlugin",
	})
	uiPlugin.SetNamespace("cattle-ui-plugin-system")
	uiPlugin.SetName("aif-ui")
	if err := fakeClient.Create(ctx, uiPlugin); err != nil {
		t.Fatalf("failed to create UIPlugin: %v", err)
	}

	// Second reconcile: installs extension
	result, err = reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("second reconcile failed: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue for successful installation")
	}

	// Verify status
	var updated aifv1.InstallAIExtension
	if err := fakeClient.Get(ctx, req.NamespacedName, &updated); err != nil {
		t.Fatalf("failed to get updated resource: %v", err)
	}

	if updated.Status.Phase != aifv1.InstallAIExtensionPhaseInstalled {
		t.Errorf("expected phase Installed, got %s", updated.Status.Phase)
	}

	readyCond := findCondition(updated.Status.Conditions, conditions.TypeReady)
	if readyCond == nil {
		t.Fatal("Ready condition not set")
	}
	if readyCond.Status != metav1.ConditionTrue {
		t.Errorf("expected Ready=True, got %s: %s", readyCond.Status, readyCond.Message)
	}
	if readyCond.Reason != conditions.ReasonInstalled {
		t.Errorf("expected reason %s, got %s", conditions.ReasonInstalled, readyCond.Reason)
	}

	// Verify Helm was called correctly
	installs := filterCalls(fakeHelm.Calls, "InstallChartFromRepo")
	if len(installs) != 1 {
		t.Errorf("expected 1 Helm install call, got %d", len(installs))
	}
	if len(installs) > 0 {
		call := installs[0].Request
		if call.Namespace != "cattle-ui-plugin-system" {
			t.Errorf("expected namespace cattle-ui-plugin-system, got %s", call.Namespace)
		}
		if call.ReleaseName != "aif-ui" {
			t.Errorf("expected release name aif-ui, got %s", call.ReleaseName)
		}
		if call.ChartRef != "oci://registry.suse.com/ai/charts/aif-ui:1.0.0" {
			t.Errorf("expected chart ref from spec, got %s", call.ChartRef)
		}
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
}

// TestInstallAIExtensionReconciler_UIPluginCRDMissing tests permanent failure when UIPlugin CRD missing
func TestInstallAIExtensionReconciler_UIPluginCRDMissing(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := aifv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add aifv1 to scheme: %v", err)
	}

	ext := createInstallAIExtension("test-ext", "aif")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ext).
		WithStatusSubresource(&aifv1.InstallAIExtension{}).
		Build()

	fakeHelm := helm.NewFake()
	fakeDisc := &fakeDiscovery{shouldFail: true} // UIPlugin CRD missing
	recorder := &fakeRecorder{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		Scheme:     scheme,
		Logger:     logger,
		HelmEngine: fakeHelm,
		Discovery:  fakeDisc,
		Recorder:   recorder,
	}

	ctx := context.Background()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-ext",
			Namespace: "aif",
		},
	}

	// First reconcile: adds finalizer
	_, _ = reconciler.Reconcile(ctx, req)

	// Second reconcile: fails CRD check, no requeue (permanent error)
	result, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("expected nil error for permanent failure, got %v", err)
	}
	if result.Requeue || result.RequeueAfter > 0 {
		t.Error("expected no requeue for permanent CRD missing error")
	}

	// Verify status
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

// TestInstallAIExtensionReconciler_HelmInstallFailed tests transient failure during Helm install
func TestInstallAIExtensionReconciler_HelmInstallFailed(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := aifv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add aifv1 to scheme: %v", err)
	}

	ext := createInstallAIExtension("test-ext", "aif")

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
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		Scheme:     scheme,
		Logger:     logger,
		HelmEngine: fakeHelm,
		Discovery:  fakeDisc,
		Recorder:   recorder,
	}

	ctx := context.Background()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-ext",
			Namespace: "aif",
		},
	}

	// First reconcile: adds finalizer
	_, _ = reconciler.Reconcile(ctx, req)

	// Second reconcile: Helm install fails, should return error
	_, err := reconciler.Reconcile(ctx, req)
	if err == nil {
		t.Error("expected error from Helm install failure")
	}
	// Controller-runtime will handle retry with exponential backoff

	// Note: Status is NOT updated when reconcile returns an error
	// This is current controller behavior - status update only happens
	// when reconcile succeeds (line 84 in controller)
	// The status changes made in reconcile() are lost when error is returned

	// Verify event was still recorded (happens before error return)
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

// TestInstallAIExtensionReconciler_UIPluginVerificationTimeout tests verification timeout
func TestInstallAIExtensionReconciler_UIPluginVerificationTimeout(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := aifv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add aifv1 to scheme: %v", err)
	}

	ext := createInstallAIExtension("test-ext", "aif")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ext).
		WithStatusSubresource(&aifv1.InstallAIExtension{}).
		Build()

	fakeHelm := helm.NewFake()
	fakeDisc := &fakeDiscovery{shouldFail: false}
	recorder := &fakeRecorder{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		Scheme:     scheme,
		Logger:     logger,
		HelmEngine: fakeHelm,
		Discovery:  fakeDisc,
		Recorder:   recorder,
	}

	ctx := context.Background()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-ext",
			Namespace: "aif",
		},
	}

	// First reconcile: adds finalizer
	_, _ = reconciler.Reconcile(ctx, req)

	// Second reconcile: Helm succeeds but UIPlugin not created
	// Note: UIPlugin is NOT created in the fake client, so verification will return requeue
	result, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("unexpected error from UIPlugin verification: %v", err)
	}
	// Should return RequeueAfter instead of error (non-blocking pattern)
	if result.RequeueAfter != 5*time.Second {
		t.Errorf("expected RequeueAfter=5s, got %v", result.RequeueAfter)
	}

	// Verify status was updated (reconcile returns success with requeue)
	var updatedExt aifv1.InstallAIExtension
	if err := fakeClient.Get(ctx, req.NamespacedName, &updatedExt); err != nil {
		t.Fatalf("failed to get updated resource: %v", err)
	}
	// Phase should be Installing (not Failed) when waiting for UIPlugin
	if updatedExt.Status.Phase != aifv1.InstallAIExtensionPhaseInstalling {
		t.Errorf("expected phase Installing, got %s", updatedExt.Status.Phase)
	}

	// Verify event was recorded
	found := false
	for _, evt := range recorder.events {
		if containsEventReason(evt, conditions.ReasonUIPluginNotCreated) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected UIPluginNotCreated event, got: %v", recorder.events)
	}
}

// TestInstallAIExtensionReconciler_Deletion tests finalizer cleanup
func TestInstallAIExtensionReconciler_Deletion(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := aifv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add aifv1 to scheme: %v", err)
	}

	ext := createInstallAIExtension("test-ext", "aif")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ext).
		WithStatusSubresource(&aifv1.InstallAIExtension{}).
		Build()

	fakeHelm := helm.NewFake()
	fakeDisc := &fakeDiscovery{shouldFail: false}
	recorder := &fakeRecorder{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		Scheme:     scheme,
		Logger:     logger,
		HelmEngine: fakeHelm,
		Discovery:  fakeDisc,
		Recorder:   recorder,
	}

	ctx := context.Background()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-ext",
			Namespace: "aif",
		},
	}

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
	if len(updated.Finalizers) != 1 || updated.Finalizers[0] != "ai.suse.com/cleanup" {
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

	// Verify resource deleted (or finalizer removed)
	err = fakeClient.Get(ctx, req.NamespacedName, &updated)
	if err == nil && len(updated.Finalizers) != 0 {
		t.Errorf("finalizer not removed: %v", updated.Finalizers)
	}
}

// TestInstallAIExtensionReconciler_ResourceNotFound tests graceful handling of deleted resource
func TestInstallAIExtensionReconciler_ResourceNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := aifv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add aifv1 to scheme: %v", err)
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&aifv1.InstallAIExtension{}).
		Build()

	fakeHelm := helm.NewFake()
	fakeDisc := &fakeDiscovery{shouldFail: false}
	recorder := &fakeRecorder{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		Scheme:     scheme,
		Logger:     logger,
		HelmEngine: fakeHelm,
		Discovery:  fakeDisc,
		Recorder:   recorder,
	}

	ctx := context.Background()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "nonexistent",
			Namespace: "aif",
		},
	}

	// Reconcile non-existent resource
	result, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("expected nil error for not found resource, got %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue for not found resource")
	}
}

// TestCheckUIPluginCRD tests the checkUIPluginCRD helper method
func TestCheckUIPluginCRD(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	ext := createInstallAIExtension("test-ext", "aif")

	tests := []struct {
		name        string
		shouldFail  bool
		expectError bool
	}{
		{
			name:        "CRD exists",
			shouldFail:  false,
			expectError: false,
		},
		{
			name:        "CRD missing",
			shouldFail:  true,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeDisc := &fakeDiscovery{shouldFail: tt.shouldFail}
			reconciler := &InstallAIExtensionReconciler{
				Logger:    logger,
				Discovery: fakeDisc,
			}

			err := reconciler.checkUIPluginCRD(context.Background(), ext)
			if tt.expectError && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

// TestInstallChart tests the installChart helper method
func TestInstallChart(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	ext := createInstallAIExtension("test-ext", "aif")

	tests := []struct {
		name        string
		helmErr     error
		expectError bool
	}{
		{
			name:        "install succeeds",
			helmErr:     nil,
			expectError: false,
		},
		{
			name:        "install fails",
			helmErr:     errors.New("chart pull failed"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeHelm := helm.NewFake()
			if tt.helmErr != nil {
				fakeHelm.InstallResult = func(helm.InstallRequest) (helm.ReleaseStatus, error) {
					return helm.ReleaseStatus{}, tt.helmErr
				}
			}
			reconciler := &InstallAIExtensionReconciler{
				Logger:     logger,
				HelmEngine: fakeHelm,
			}

			err := reconciler.installChart(context.Background(), ext)
			if tt.expectError && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error, got %v", err)
			}

			// Verify Helm was called with correct parameters
			if !tt.expectError {
				installs := filterCalls(fakeHelm.Calls, "InstallChartFromRepo")
				if len(installs) != 1 {
					t.Errorf("expected 1 install call, got %d", len(installs))
				}
				if len(installs) == 1 {
					call := installs[0].Request
					if call.Namespace != "cattle-ui-plugin-system" {
						t.Errorf("expected namespace cattle-ui-plugin-system, got %s", call.Namespace)
					}
					if call.ReleaseName != "aif-ui" {
						t.Errorf("expected release name aif-ui, got %s", call.ReleaseName)
					}
					if call.ChartRef != ext.Spec.Helm.URL {
						t.Errorf("expected chart ref %s, got %s", ext.Spec.Helm.URL, call.ChartRef)
					}
					if !call.Wait {
						t.Error("expected Wait=true")
					}
					if call.Timeout != 5*time.Minute {
						t.Errorf("expected timeout 5m, got %v", call.Timeout)
					}
				}
			}
		})
	}
}

// TestVerifyUIPlugin tests the verifyUIPlugin helper method
func TestVerifyUIPlugin(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := aifv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add aifv1 to scheme: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	ext := createInstallAIExtension("test-ext", "aif")

	tests := []struct {
		name         string
		createPlugin bool
		expectError  bool
	}{
		{
			name:         "UIPlugin exists",
			createPlugin: true,
			expectError:  false,
		},
		{
			name:         "UIPlugin missing",
			createPlugin: false,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := fake.NewClientBuilder().WithScheme(scheme)

			if tt.createPlugin {
				uiPlugin := &unstructured.Unstructured{}
				uiPlugin.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "catalog.cattle.io",
					Version: "v1",
					Kind:    "UIPlugin",
				})
				uiPlugin.SetNamespace("cattle-ui-plugin-system")
				uiPlugin.SetName("aif-ui")
				builder = builder.WithObjects(uiPlugin)
			}

			fakeClient := builder.Build()
			reconciler := &InstallAIExtensionReconciler{
				Client: fakeClient,
				Logger: logger,
			}

			// Note: verifyUIPlugin has sleep loops, so this test will be slow if UIPlugin is missing
			// We accept this for test coverage
			err := reconciler.verifyUIPlugin(context.Background(), ext)
			if tt.expectError && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

// TestSetCondition tests the setCondition helper method
func TestSetCondition(t *testing.T) {
	ext := createInstallAIExtension("test-ext", "aif")
	reconciler := &InstallAIExtensionReconciler{}

	// Set initial condition
	reconciler.setCondition(ext, metav1.Condition{
		Type:               conditions.TypeReady,
		Status:             metav1.ConditionFalse,
		Reason:             conditions.ReasonInstallFailed,
		Message:            "Installation failed",
		ObservedGeneration: ext.Generation,
	})

	if len(ext.Status.Conditions) != 1 {
		t.Errorf("expected 1 condition, got %d", len(ext.Status.Conditions))
	}

	cond := ext.Status.Conditions[0]
	if cond.Type != conditions.TypeReady {
		t.Errorf("expected type %s, got %s", conditions.TypeReady, cond.Type)
	}
	if cond.Reason != conditions.ReasonInstallFailed {
		t.Errorf("expected reason %s, got %s", conditions.ReasonInstallFailed, cond.Reason)
	}

	// Update condition
	reconciler.setCondition(ext, metav1.Condition{
		Type:               conditions.TypeReady,
		Status:             metav1.ConditionTrue,
		Reason:             conditions.ReasonInstalled,
		Message:            "Installed successfully",
		ObservedGeneration: ext.Generation,
	})

	if len(ext.Status.Conditions) != 1 {
		t.Errorf("expected 1 condition after update, got %d", len(ext.Status.Conditions))
	}

	updatedCond := ext.Status.Conditions[0]
	if updatedCond.Status != metav1.ConditionTrue {
		t.Errorf("expected status True, got %s", updatedCond.Status)
	}
	if updatedCond.Reason != conditions.ReasonInstalled {
		t.Errorf("expected reason %s, got %s", conditions.ReasonInstalled, updatedCond.Reason)
	}
}

// TestCleanup tests the cleanup helper method
func TestCleanup(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	ext := createInstallAIExtension("test-ext", "aif")

	tests := []struct {
		name         string
		uninstallErr error
		expectError  bool
	}{
		{
			name:         "uninstall succeeds",
			uninstallErr: nil,
			expectError:  false,
		},
		{
			name:         "uninstall fails",
			uninstallErr: fmt.Errorf("release not found"),
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeHelm := helm.NewFake()
			if tt.uninstallErr != nil {
				fakeHelm.UninstallResult = func(string, string) error {
					return tt.uninstallErr
				}
			}
			reconciler := &InstallAIExtensionReconciler{
				Logger:     logger,
				HelmEngine: fakeHelm,
			}

			err := reconciler.cleanup(context.Background(), ext)
			if tt.expectError && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

// TestReconcile_ObservedGeneration tests that ObservedGeneration is set correctly
func TestReconcile_ObservedGeneration(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := aifv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add aifv1 to scheme: %v", err)
	}

	ext := createInstallAIExtension("test-ext", "aif")
	ext.Generation = 5 // Simulate multiple updates

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ext).
		WithStatusSubresource(&aifv1.InstallAIExtension{}).
		Build()

	fakeHelm := helm.NewFake()
	fakeDisc := &fakeDiscovery{shouldFail: false}
	recorder := &fakeRecorder{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	reconciler := &InstallAIExtensionReconciler{
		Client:     fakeClient,
		Scheme:     scheme,
		Logger:     logger,
		HelmEngine: fakeHelm,
		Discovery:  fakeDisc,
		Recorder:   recorder,
	}

	ctx := context.Background()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-ext",
			Namespace: "aif",
		},
	}

	// Reconcile
	_, _ = reconciler.Reconcile(ctx, req)

	// Create UIPlugin to allow successful reconciliation
	uiPlugin := &unstructured.Unstructured{}
	uiPlugin.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "catalog.cattle.io",
		Version: "v1",
		Kind:    "UIPlugin",
	})
	uiPlugin.SetNamespace("cattle-ui-plugin-system")
	uiPlugin.SetName("aif-ui")
	if err := fakeClient.Create(ctx, uiPlugin); err != nil {
		t.Fatalf("failed to create UIPlugin: %v", err)
	}

	_, _ = reconciler.Reconcile(ctx, req)

	// Verify ObservedGeneration
	var updated aifv1.InstallAIExtension
	if err := fakeClient.Get(ctx, req.NamespacedName, &updated); err != nil {
		t.Fatalf("failed to get updated resource: %v", err)
	}

	if updated.Status.ObservedGeneration != ext.Generation {
		t.Errorf("expected observedGeneration=%d, got %d", ext.Generation, updated.Status.ObservedGeneration)
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
