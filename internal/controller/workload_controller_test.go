package controller

import (
	"context"
	"testing"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/conditions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// fakeRecorder implements record.EventRecorder for testing
type fakeRecorder struct {
	events []string
}

func (f *fakeRecorder) Event(object runtime.Object, eventtype, reason, message string) {
	f.events = append(f.events, eventtype+":"+reason+":"+message)
}

func (f *fakeRecorder) Eventf(object runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
	f.Event(object, eventtype, reason, messageFmt)
}

func (f *fakeRecorder) AnnotatedEventf(object runtime.Object, annotations map[string]string, eventtype, reason, messageFmt string, args ...interface{}) {
	f.Event(object, eventtype, reason, messageFmt)
}

var _ record.EventRecorder = (*fakeRecorder)(nil)

// findCondition finds a condition by type in a slice of conditions
func findCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}

// TestWorkloadReconciler_ValidApp verifies that a Workload with an App source
// is reconciled successfully: finalizer is added on first reconcile, phase is set
// to Pending with Ready=False/AwaitingDeployer on second reconcile, and a
// WorkloadCreated event is recorded.
func TestWorkloadReconciler_ValidApp(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	err := aifv1.AddToScheme(scheme)
	require.NoError(t, err)

	// Create test Workload with App source
	w := &aifv1.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-app-workload",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: aifv1.WorkloadSpec{
			Name: "Test App Workload",
			Source: aifv1.WorkloadSource{
				Kind: aifv1.WorkloadSourceKindApp,
				App: &aifv1.AppRef{
					Repo:    "https://example.com/charts",
					Chart:   "llama3",
					Version: "1.0.0",
				},
			},
		},
	}

	// Create fake client
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(w).
		WithStatusSubresource(w).
		Build()

	// Create fake recorder
	recorder := &fakeRecorder{}

	// Create reconciler
	reconciler := &WorkloadReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: recorder,
	}

	// First reconcile - should add finalizer
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-app-workload",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, result.Requeue, "should requeue after adding finalizer")

	// Fetch Workload to verify finalizer
	var fetchedW aifv1.Workload
	err = fakeClient.Get(context.Background(), req.NamespacedName, &fetchedW)
	require.NoError(t, err)
	assert.Contains(t, fetchedW.Finalizers, "ai.suse.com/cleanup", "finalizer should be added")

	// Second reconcile - should perform main logic
	result, err = reconciler.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, result.Requeue, "should not requeue on success")

	// Fetch Workload to verify status
	err = fakeClient.Get(context.Background(), req.NamespacedName, &fetchedW)
	require.NoError(t, err)

	// Verify status fields
	assert.Equal(t, aifv1.WorkloadPhasePending, fetchedW.Status.Phase, "phase should be Pending")
	assert.Equal(t, int64(1), fetchedW.Status.ObservedGeneration, "observedGeneration should match")

	// Verify Ready condition
	readyCondition := findCondition(fetchedW.Status.Conditions, conditions.TypeReady)
	require.NotNil(t, readyCondition, "Ready condition should exist")
	assert.Equal(t, metav1.ConditionFalse, readyCondition.Status, "Ready should be False")
	assert.Equal(t, conditions.ReasonAwaitingDeployer, readyCondition.Reason, "Reason should be AwaitingDeployer")

	// Verify event was recorded
	eventFound := false
	for _, evt := range recorder.events {
		if assert.Contains(t, evt, "WorkloadCreated") {
			eventFound = true
			break
		}
	}
	assert.True(t, eventFound, "should record WorkloadCreated event")
}

// TestWorkloadReconciler_ValidBlueprint verifies that a Workload with a Blueprint source
// is reconciled successfully: finalizer is added on first reconcile, phase is set
// to Pending with Ready=False/AwaitingDeployer on second reconcile, and a
// WorkloadCreated event is recorded.
func TestWorkloadReconciler_ValidBlueprint(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	err := aifv1.AddToScheme(scheme)
	require.NoError(t, err)

	// Create test Workload with Blueprint source
	w := &aifv1.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-blueprint-workload",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: aifv1.WorkloadSpec{
			Name: "Test Blueprint Workload",
			Source: aifv1.WorkloadSource{
				Kind: aifv1.WorkloadSourceKindBlueprint,
				Blueprint: &aifv1.BlueprintRef{
					Name:    "rag-stack",
					Version: "1.0.0",
				},
			},
		},
	}

	// Create fake client
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(w).
		WithStatusSubresource(w).
		Build()

	// Create fake recorder
	recorder := &fakeRecorder{}

	// Create reconciler
	reconciler := &WorkloadReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: recorder,
	}

	// First reconcile - should add finalizer
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-blueprint-workload",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, result.Requeue, "should requeue after adding finalizer")

	// Fetch Workload to verify finalizer
	var fetchedW aifv1.Workload
	err = fakeClient.Get(context.Background(), req.NamespacedName, &fetchedW)
	require.NoError(t, err)
	assert.Contains(t, fetchedW.Finalizers, "ai.suse.com/cleanup", "finalizer should be added")

	// Second reconcile - should perform main logic
	result, err = reconciler.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, result.Requeue, "should not requeue on success")

	// Fetch Workload to verify status
	err = fakeClient.Get(context.Background(), req.NamespacedName, &fetchedW)
	require.NoError(t, err)

	// Verify status fields
	assert.Equal(t, aifv1.WorkloadPhasePending, fetchedW.Status.Phase, "phase should be Pending")
	assert.Equal(t, int64(1), fetchedW.Status.ObservedGeneration, "observedGeneration should match")

	// Verify Ready condition
	readyCondition := findCondition(fetchedW.Status.Conditions, conditions.TypeReady)
	require.NotNil(t, readyCondition, "Ready condition should exist")
	assert.Equal(t, metav1.ConditionFalse, readyCondition.Status, "Ready should be False")
	assert.Equal(t, conditions.ReasonAwaitingDeployer, readyCondition.Reason, "Reason should be AwaitingDeployer")

	// Verify event was recorded
	eventFound := false
	for _, evt := range recorder.events {
		if assert.Contains(t, evt, "WorkloadCreated") {
			eventFound = true
			break
		}
	}
	assert.True(t, eventFound, "should record WorkloadCreated event")
}

// TestWorkloadReconciler_ValidBundleTest verifies that a Workload with a BundleTest source
// is reconciled successfully: finalizer is added on first reconcile, phase is set
// to Pending with Ready=False/AwaitingDeployer on second reconcile, and a
// WorkloadCreated event is recorded.
func TestWorkloadReconciler_ValidBundleTest(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	err := aifv1.AddToScheme(scheme)
	require.NoError(t, err)

	// Create test Workload with BundleTest source
	w := &aifv1.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-bundletest-workload",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: aifv1.WorkloadSpec{
			Name: "Test BundleTest Workload",
			Source: aifv1.WorkloadSource{
				Kind: aifv1.WorkloadSourceKindBundleTest,
				BundleTest: &aifv1.BundleTestRef{
					Namespace:  "default",
					Name:       "test-bundle",
					Generation: 1,
				},
			},
		},
	}

	// Create fake client
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(w).
		WithStatusSubresource(w).
		Build()

	// Create fake recorder
	recorder := &fakeRecorder{}

	// Create reconciler
	reconciler := &WorkloadReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: recorder,
	}

	// First reconcile - should add finalizer
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-bundletest-workload",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, result.Requeue, "should requeue after adding finalizer")

	// Fetch Workload to verify finalizer
	var fetchedW aifv1.Workload
	err = fakeClient.Get(context.Background(), req.NamespacedName, &fetchedW)
	require.NoError(t, err)
	assert.Contains(t, fetchedW.Finalizers, "ai.suse.com/cleanup", "finalizer should be added")

	// Second reconcile - should perform main logic
	result, err = reconciler.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, result.Requeue, "should not requeue on success")

	// Fetch Workload to verify status
	err = fakeClient.Get(context.Background(), req.NamespacedName, &fetchedW)
	require.NoError(t, err)

	// Verify status fields
	assert.Equal(t, aifv1.WorkloadPhasePending, fetchedW.Status.Phase, "phase should be Pending")
	assert.Equal(t, int64(1), fetchedW.Status.ObservedGeneration, "observedGeneration should match")

	// Verify Ready condition
	readyCondition := findCondition(fetchedW.Status.Conditions, conditions.TypeReady)
	require.NotNil(t, readyCondition, "Ready condition should exist")
	assert.Equal(t, metav1.ConditionFalse, readyCondition.Status, "Ready should be False")
	assert.Equal(t, conditions.ReasonAwaitingDeployer, readyCondition.Reason, "Reason should be AwaitingDeployer")

	// Verify event was recorded
	eventFound := false
	for _, evt := range recorder.events {
		if assert.Contains(t, evt, "WorkloadCreated") {
			eventFound = true
			break
		}
	}
	assert.True(t, eventFound, "should record WorkloadCreated event")
}

// TestWorkloadReconciler_InvalidSource_AppMissingField verifies that a Workload with
// Kind=App but missing App field is marked as invalid: phase is NOT set,
// Ready=False/InvalidSpec, message contains field name, and WorkloadInvalid event recorded.
func TestWorkloadReconciler_InvalidSource_AppMissingField(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	err := aifv1.AddToScheme(scheme)
	require.NoError(t, err)

	// Create test Workload with App Kind but missing App field
	w := &aifv1.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "invalid-app-workload",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: aifv1.WorkloadSpec{
			Name: "Invalid App Workload",
			Source: aifv1.WorkloadSource{
				Kind: aifv1.WorkloadSourceKindApp,
				App:  nil, // Missing App field
			},
		},
	}

	// Create fake client
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(w).
		WithStatusSubresource(w).
		Build()

	// Create fake recorder
	recorder := &fakeRecorder{}

	// Create reconciler
	reconciler := &WorkloadReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: recorder,
	}

	// First reconcile - should add finalizer
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "invalid-app-workload",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, result.Requeue, "should requeue after adding finalizer")

	// Fetch Workload to verify finalizer
	var fetchedW aifv1.Workload
	err = fakeClient.Get(context.Background(), req.NamespacedName, &fetchedW)
	require.NoError(t, err)
	assert.Contains(t, fetchedW.Finalizers, "ai.suse.com/cleanup", "finalizer should be added")

	// Second reconcile - should mark as invalid
	result, err = reconciler.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, result.Requeue, "should not requeue on validation failure")

	// Fetch Workload to verify status
	err = fakeClient.Get(context.Background(), req.NamespacedName, &fetchedW)
	require.NoError(t, err)

	// Verify phase is NOT set (empty)
	assert.Empty(t, fetchedW.Status.Phase, "phase should be empty for invalid Workload")
	assert.Equal(t, int64(1), fetchedW.Status.ObservedGeneration, "observedGeneration should match")

	// Verify Ready condition
	readyCondition := findCondition(fetchedW.Status.Conditions, conditions.TypeReady)
	require.NotNil(t, readyCondition, "Ready condition should exist")
	assert.Equal(t, metav1.ConditionFalse, readyCondition.Status, "Ready should be False")
	assert.Equal(t, conditions.ReasonInvalidSpec, readyCondition.Reason, "Reason should be InvalidSpec")
	assert.Contains(t, readyCondition.Message, "source.app", "message should contain missing field name")

	// Verify event was recorded
	eventFound := false
	for _, evt := range recorder.events {
		if assert.Contains(t, evt, "WorkloadInvalid") {
			eventFound = true
			break
		}
	}
	assert.True(t, eventFound, "should record WorkloadInvalid event")
}

// TestWorkloadReconciler_InvalidSource_BlueprintMissingField verifies that a Workload with
// Kind=Blueprint but missing Blueprint field is marked as invalid: phase is NOT set,
// Ready=False/InvalidSpec, message contains field name, and WorkloadInvalid event recorded.
func TestWorkloadReconciler_InvalidSource_BlueprintMissingField(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	err := aifv1.AddToScheme(scheme)
	require.NoError(t, err)

	// Create test Workload with Blueprint Kind but missing Blueprint field
	w := &aifv1.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "invalid-blueprint-workload",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: aifv1.WorkloadSpec{
			Name: "Invalid Blueprint Workload",
			Source: aifv1.WorkloadSource{
				Kind:      aifv1.WorkloadSourceKindBlueprint,
				Blueprint: nil, // Missing Blueprint field
			},
		},
	}

	// Create fake client
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(w).
		WithStatusSubresource(w).
		Build()

	// Create fake recorder
	recorder := &fakeRecorder{}

	// Create reconciler
	reconciler := &WorkloadReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: recorder,
	}

	// First reconcile - should add finalizer
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "invalid-blueprint-workload",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, result.Requeue, "should requeue after adding finalizer")

	// Fetch Workload to verify finalizer
	var fetchedW aifv1.Workload
	err = fakeClient.Get(context.Background(), req.NamespacedName, &fetchedW)
	require.NoError(t, err)
	assert.Contains(t, fetchedW.Finalizers, "ai.suse.com/cleanup", "finalizer should be added")

	// Second reconcile - should mark as invalid
	result, err = reconciler.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, result.Requeue, "should not requeue on validation failure")

	// Fetch Workload to verify status
	err = fakeClient.Get(context.Background(), req.NamespacedName, &fetchedW)
	require.NoError(t, err)

	// Verify phase is NOT set (empty)
	assert.Empty(t, fetchedW.Status.Phase, "phase should be empty for invalid Workload")
	assert.Equal(t, int64(1), fetchedW.Status.ObservedGeneration, "observedGeneration should match")

	// Verify Ready condition
	readyCondition := findCondition(fetchedW.Status.Conditions, conditions.TypeReady)
	require.NotNil(t, readyCondition, "Ready condition should exist")
	assert.Equal(t, metav1.ConditionFalse, readyCondition.Status, "Ready should be False")
	assert.Equal(t, conditions.ReasonInvalidSpec, readyCondition.Reason, "Reason should be InvalidSpec")
	assert.Contains(t, readyCondition.Message, "source.blueprint", "message should contain missing field name")

	// Verify event was recorded
	eventFound := false
	for _, evt := range recorder.events {
		if assert.Contains(t, evt, "WorkloadInvalid") {
			eventFound = true
			break
		}
	}
	assert.True(t, eventFound, "should record WorkloadInvalid event")
}

// TestWorkloadReconciler_InvalidSource_BundleTestMissingField verifies that a Workload with
// Kind=BundleTest but missing BundleTest field is marked as invalid: phase is NOT set,
// Ready=False/InvalidSpec, message contains field name, and WorkloadInvalid event recorded.
func TestWorkloadReconciler_InvalidSource_BundleTestMissingField(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	err := aifv1.AddToScheme(scheme)
	require.NoError(t, err)

	// Create test Workload with BundleTest Kind but missing BundleTest field
	w := &aifv1.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "invalid-bundletest-workload",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: aifv1.WorkloadSpec{
			Name: "Invalid BundleTest Workload",
			Source: aifv1.WorkloadSource{
				Kind:       aifv1.WorkloadSourceKindBundleTest,
				BundleTest: nil, // Missing BundleTest field
			},
		},
	}

	// Create fake client
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(w).
		WithStatusSubresource(w).
		Build()

	// Create fake recorder
	recorder := &fakeRecorder{}

	// Create reconciler
	reconciler := &WorkloadReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: recorder,
	}

	// First reconcile - should add finalizer
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "invalid-bundletest-workload",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, result.Requeue, "should requeue after adding finalizer")

	// Fetch Workload to verify finalizer
	var fetchedW aifv1.Workload
	err = fakeClient.Get(context.Background(), req.NamespacedName, &fetchedW)
	require.NoError(t, err)
	assert.Contains(t, fetchedW.Finalizers, "ai.suse.com/cleanup", "finalizer should be added")

	// Second reconcile - should mark as invalid
	result, err = reconciler.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, result.Requeue, "should not requeue on validation failure")

	// Fetch Workload to verify status
	err = fakeClient.Get(context.Background(), req.NamespacedName, &fetchedW)
	require.NoError(t, err)

	// Verify phase is NOT set (empty)
	assert.Empty(t, fetchedW.Status.Phase, "phase should be empty for invalid Workload")
	assert.Equal(t, int64(1), fetchedW.Status.ObservedGeneration, "observedGeneration should match")

	// Verify Ready condition
	readyCondition := findCondition(fetchedW.Status.Conditions, conditions.TypeReady)
	require.NotNil(t, readyCondition, "Ready condition should exist")
	assert.Equal(t, metav1.ConditionFalse, readyCondition.Status, "Ready should be False")
	assert.Equal(t, conditions.ReasonInvalidSpec, readyCondition.Reason, "Reason should be InvalidSpec")
	assert.Contains(t, readyCondition.Message, "source.bundleTest", "message should contain missing field name")

	// Verify event was recorded
	eventFound := false
	for _, evt := range recorder.events {
		if assert.Contains(t, evt, "WorkloadInvalid") {
			eventFound = true
			break
		}
	}
	assert.True(t, eventFound, "should record WorkloadInvalid event")
}

// TestWorkloadReconciler_Finalizer verifies that the Workload finalizer lifecycle
// is handled correctly: finalizer is added on first reconcile, and removed on deletion.
func TestWorkloadReconciler_Finalizer(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	err := aifv1.AddToScheme(scheme)
	require.NoError(t, err)

	// Create test Workload with App source
	w := &aifv1.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-finalizer-workload",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: aifv1.WorkloadSpec{
			Name: "Test Finalizer Workload",
			Source: aifv1.WorkloadSource{
				Kind: aifv1.WorkloadSourceKindApp,
				App: &aifv1.AppRef{
					Repo:    "https://example.com/charts",
					Chart:   "llama3",
					Version: "1.0.0",
				},
			},
		},
	}

	// Create fake client
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(w).
		WithStatusSubresource(w).
		Build()

	// Create fake recorder
	recorder := &fakeRecorder{}

	// Create reconciler
	reconciler := &WorkloadReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: recorder,
	}

	// First reconcile - should add finalizer
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-finalizer-workload",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, result.Requeue, "should requeue after adding finalizer")

	// Fetch Workload to verify finalizer was added
	var fetchedW aifv1.Workload
	err = fakeClient.Get(context.Background(), req.NamespacedName, &fetchedW)
	require.NoError(t, err)
	assert.Contains(t, fetchedW.Finalizers, "ai.suse.com/cleanup", "finalizer should be added on first reconcile")

	// Simulate deletion by setting DeletionTimestamp directly on the object
	// (We can't use Update because the fake client doesn't allow changing DeletionTimestamp,
	// so we create a new reconciler with the deleted object)
	now := metav1.Now()
	fetchedW.DeletionTimestamp = &now

	// Create a new fake client with the deleted Workload
	fakeClientWithDeletion := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(&fetchedW).
		WithStatusSubresource(&fetchedW).
		Build()

	// Create a new reconciler with the updated client
	reconcilerWithDeletion := &WorkloadReconciler{
		Client:   fakeClientWithDeletion,
		Scheme:   scheme,
		Recorder: recorder,
	}

	// Reconcile again - should remove finalizer
	result, err = reconcilerWithDeletion.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, result.Requeue, "should not requeue after removing finalizer")

	// Fetch Workload to verify finalizer was removed
	var deletedW aifv1.Workload
	err = fakeClientWithDeletion.Get(context.Background(), req.NamespacedName, &deletedW)
	// Object may be gone or finalizer may be removed - either is acceptable behavior
	if err == nil {
		// If object still exists, finalizer should be gone
		assert.NotContains(t, deletedW.Finalizers, "ai.suse.com/cleanup", "finalizer should be removed on deletion")
	} else {
		// If object is gone, that's also fine - finalizer removal allows GC
		assert.True(t, errors.IsNotFound(err), "object should be deleted or finalizer should be removed")
	}
}
