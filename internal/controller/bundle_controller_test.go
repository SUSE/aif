package controller

import (
	"context"
	"fmt"
	"testing"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/bundle"
	"github.com/SUSE/aif/pkg/conditions"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestBundleReconciler_ValidBundle(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	if err := aifv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add aifv1 to scheme: %v", err)
	}

	// Create valid Bundle
	validBundle := &aifv1.Bundle{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-bundle",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: aifv1.BundleSpec{
			Title:           "Test Bundle",
			TargetBlueprint: "test-blueprint",
			UseCase:         "rag",
			Components: []aifv1.ComponentRef{
				{
					Name: "app1",
					Kind: aifv1.ComponentKindApp,
					App: &aifv1.AppRef{
						Repo:    "https://example.com/charts",
						Chart:   "test-chart",
						Version: "1.0.0",
					},
				},
			},
		},
		Status: aifv1.BundleStatus{
			Phase: aifv1.BundlePhaseDraft,
		},
	}

	// Create fake client
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(validBundle).
		WithStatusSubresource(&aifv1.Bundle{}).
		Build()

	// Create fake manager
	fakeManager := &fakeBundleManager{
		upsertFunc: func(ctx context.Context, b bundle.Bundle) error {
			return nil // Valid bundle passes validation
		},
	}

	// Create reconciler
	reconciler := &BundleReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: &fakeRecorder{},
		Manager:  fakeManager,
	}

	// Reconcile
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-bundle",
			Namespace: "default",
		},
	}

	// First reconcile adds finalizer and requeues
	result, err := reconciler.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("first reconcile failed: %v", err)
	}
	if !result.Requeue {
		t.Error("expected requeue after adding finalizer")
	}

	// Second reconcile does the actual work
	result, err = reconciler.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("second reconcile failed: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue for valid bundle")
	}

	// Verify Bundle was updated with Ready condition and finalizer
	var updated aifv1.Bundle
	if err := fakeClient.Get(context.Background(), req.NamespacedName, &updated); err != nil {
		t.Fatalf("failed to get updated bundle: %v", err)
	}

	// Check finalizer
	if len(updated.Finalizers) != 1 || updated.Finalizers[0] != "ai.suse.com/cleanup" {
		t.Errorf("expected finalizer ai.suse.com/cleanup, got %v", updated.Finalizers)
	}

	// Check Ready condition
	readyCond := findCondition(updated.Status.Conditions, conditions.TypeReady)
	if readyCond == nil {
		t.Fatal("Ready condition not set")
	}
	if readyCond.Status != metav1.ConditionTrue {
		t.Errorf("expected Ready=True, got %s", readyCond.Status)
	}
	if readyCond.Reason != conditions.ReasonReconciled {
		t.Errorf("expected reason Reconciled, got %s", readyCond.Reason)
	}

	// Check ObservedGeneration
	if updated.Status.ObservedGeneration != 1 {
		t.Errorf("expected observedGeneration=1, got %d", updated.Status.ObservedGeneration)
	}
}

// fakeBundleManager implements bundle.Manager for testing
type fakeBundleManager struct {
	upsertFunc func(ctx context.Context, b bundle.Bundle) error
	getFunc    func(ctx context.Context, namespace, name string) (bundle.Bundle, bool)
}

func (f *fakeBundleManager) Upsert(ctx context.Context, b bundle.Bundle) error {
	if f.upsertFunc != nil {
		return f.upsertFunc(ctx, b)
	}
	return nil
}

func (f *fakeBundleManager) Get(ctx context.Context, namespace, name string) (bundle.Bundle, bool) {
	if f.getFunc != nil {
		return f.getFunc(ctx, namespace, name)
	}
	return bundle.Bundle{}, false
}

var _ bundle.Manager = (*fakeBundleManager)(nil)

func TestBundleReconciler_InvalidSpec(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	if err := aifv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add aifv1 to scheme: %v", err)
	}

	// Create invalid Bundle (invalid useCase)
	invalidBundle := &aifv1.Bundle{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "invalid-bundle",
			Namespace:  "default",
			Generation: 1,
			Finalizers: []string{"ai.suse.com/cleanup"}, // Pre-add finalizer to skip first reconcile
		},
		Spec: aifv1.BundleSpec{
			Title:           "Invalid Bundle",
			TargetBlueprint: "test-blueprint",
			UseCase:         "invalid-use-case", // This should fail validation
			Components: []aifv1.ComponentRef{
				{
					Name: "app1",
					Kind: aifv1.ComponentKindApp,
					App: &aifv1.AppRef{
						Repo:    "https://example.com/charts",
						Chart:   "test-chart",
						Version: "1.0.0",
					},
				},
			},
		},
		Status: aifv1.BundleStatus{
			Phase: aifv1.BundlePhaseDraft,
		},
	}

	// Create fake client
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(invalidBundle).
		WithStatusSubresource(&aifv1.Bundle{}).
		Build()

	// Create fake manager that rejects invalid bundles
	fakeManager := &fakeBundleManager{
		upsertFunc: func(ctx context.Context, b bundle.Bundle) error {
			if b.UseCase == "invalid-use-case" {
				return fmt.Errorf("invalid useCase: must be one of [rag, vision, fine-tuning, inference, other]")
			}
			return nil
		},
	}

	// Create reconciler
	reconciler := &BundleReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: &fakeRecorder{},
		Manager:  fakeManager,
	}

	// Reconcile
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "invalid-bundle",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue for invalid bundle")
	}

	// Verify Bundle has Ready=False condition
	var updated aifv1.Bundle
	if err := fakeClient.Get(context.Background(), req.NamespacedName, &updated); err != nil {
		t.Fatalf("failed to get updated bundle: %v", err)
	}

	// Check Ready condition
	readyCond := findCondition(updated.Status.Conditions, conditions.TypeReady)
	if readyCond == nil {
		t.Fatal("Ready condition not set")
	}
	if readyCond.Status != metav1.ConditionFalse {
		t.Errorf("expected Ready=False, got %s", readyCond.Status)
	}
	if readyCond.Reason != conditions.ReasonInvalidSpec {
		t.Errorf("expected reason InvalidSpec, got %s", readyCond.Reason)
	}
	if readyCond.Message == "" {
		t.Error("expected non-empty message")
	}

	// Check ObservedGeneration
	if updated.Status.ObservedGeneration != 1 {
		t.Errorf("expected observedGeneration=1, got %d", updated.Status.ObservedGeneration)
	}
}

func TestBundleReconciler_Finalizer(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	if err := aifv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add aifv1 to scheme: %v", err)
	}

	// Create Bundle without finalizer
	testBundle := &aifv1.Bundle{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "finalizer-test",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: aifv1.BundleSpec{
			Title:           "Finalizer Test",
			TargetBlueprint: "test-blueprint",
			UseCase:         "rag",
			Components: []aifv1.ComponentRef{
				{
					Name: "app1",
					Kind: aifv1.ComponentKindApp,
					App: &aifv1.AppRef{
						Repo:    "https://example.com/charts",
						Chart:   "test-chart",
						Version: "1.0.0",
					},
				},
			},
		},
		Status: aifv1.BundleStatus{
			Phase: aifv1.BundlePhaseDraft,
		},
	}

	// Create fake client
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(testBundle).
		WithStatusSubresource(&aifv1.Bundle{}).
		Build()

	// Create fake manager
	fakeManager := &fakeBundleManager{
		upsertFunc: func(ctx context.Context, b bundle.Bundle) error {
			return nil
		},
	}

	// Create reconciler
	reconciler := &BundleReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: &fakeRecorder{},
		Manager:  fakeManager,
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "finalizer-test",
			Namespace: "default",
		},
	}

	// Test 1: Finalizer added on first reconcile
	result, err := reconciler.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("first reconcile failed: %v", err)
	}
	if !result.Requeue {
		t.Error("expected requeue after adding finalizer")
	}

	var updated aifv1.Bundle
	if err := fakeClient.Get(context.Background(), req.NamespacedName, &updated); err != nil {
		t.Fatalf("failed to get updated bundle: %v", err)
	}

	if len(updated.Finalizers) != 1 || updated.Finalizers[0] != "ai.suse.com/cleanup" {
		t.Errorf("expected finalizer ai.suse.com/cleanup, got %v", updated.Finalizers)
	}

	// Test 2: Finalizer removed on deletion
	// Delete bundle (fake client will set DeletionTimestamp but won't actually delete due to finalizer)
	if err := fakeClient.Delete(context.Background(), &updated); err != nil {
		t.Fatalf("failed to delete bundle: %v", err)
	}

	// Reconcile deletion
	result, err = reconciler.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("deletion reconcile failed: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue after deletion")
	}

	// After finalizer is removed, object should be gone
	err = fakeClient.Get(context.Background(), req.NamespacedName, &updated)
	if err == nil {
		// If object still exists, verify finalizer was removed
		if len(updated.Finalizers) != 0 {
			t.Errorf("expected no finalizers, got %v", updated.Finalizers)
		}
	} else if !errors.IsNotFound(err) {
		t.Fatalf("unexpected error getting bundle: %v", err)
	}
	// If IsNotFound, that's also OK - the object was deleted after finalizer removal
}

func TestBundleReconciler_SelfHealing(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	if err := aifv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add aifv1 to scheme: %v", err)
	}

	// Create Submitted Bundle with submission data
	submittedBundle := &aifv1.Bundle{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "submitted-bundle",
			Namespace:  "test-ns",
			Generation: 5,
			Finalizers: []string{"ai.suse.com/cleanup"},
		},
		Spec: aifv1.BundleSpec{
			Title:           "Submitted Bundle",
			TargetBlueprint: "test-blueprint",
			UseCase:         "rag",
			Components: []aifv1.ComponentRef{
				{
					Name: "app1",
					Kind: aifv1.ComponentKindApp,
					App: &aifv1.AppRef{
						Repo:    "https://example.com/charts",
						Chart:   "test-chart",
						Version: "1.0.0",
					},
				},
			},
		},
		Status: aifv1.BundleStatus{
			Phase: aifv1.BundlePhaseSubmitted,
			Submission: &aifv1.SubmissionStatus{
				ProposedVersion:    "1.0.0",
				ChangeDescription:  "Initial release",
				SubmittedBy:        "alice",
				SubmittedAt:        metav1.Now(),
				GenerationAtSubmit: 5,
			},
		},
	}

	// Create matching Blueprint published from this Bundle
	matchingBlueprint := &aifv1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-blueprint.1.0.0",
		},
		Spec: aifv1.BlueprintSpec{
			BlueprintName: "test-blueprint",
			Version:       "1.0.0",
			UseCase:       "rag",
			Source: aifv1.BlueprintSource{
				Type: aifv1.BlueprintSourcePublished,
				PublishedFrom: &aifv1.PublishedFromRef{
					BundleNamespace:  "test-ns",
					BundleName:       "submitted-bundle",
					BundleGeneration: 5,
				},
			},
			Components: []aifv1.ComponentRef{
				{
					Name: "app1",
					Kind: aifv1.ComponentKindApp,
					App: &aifv1.AppRef{
						Repo:    "https://example.com/charts",
						Chart:   "test-chart",
						Version: "1.0.0",
					},
				},
			},
			PublishedBy: "approver",
			PublishedAt: metav1.Now(),
		},
	}

	// Create fake client
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(submittedBundle, matchingBlueprint).
		WithStatusSubresource(&aifv1.Bundle{}).
		Build()

	// Create fake manager
	fakeManager := &fakeBundleManager{
		upsertFunc: func(ctx context.Context, b bundle.Bundle) error {
			return nil
		},
	}

	// Create reconciler
	reconciler := &BundleReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: &fakeRecorder{},
		Manager:  fakeManager,
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "submitted-bundle",
			Namespace: "test-ns",
		},
	}

	// Reconcile - should detect Blueprint and heal Bundle status
	result, err := reconciler.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue after self-healing")
	}

	// Verify Bundle status was healed
	var updated aifv1.Bundle
	if err := fakeClient.Get(context.Background(), req.NamespacedName, &updated); err != nil {
		t.Fatalf("failed to get updated bundle: %v", err)
	}

	// Check phase reset to Draft
	if updated.Status.Phase != aifv1.BundlePhaseDraft {
		t.Errorf("expected phase Draft after healing, got %s", updated.Status.Phase)
	}

	// Check submission cleared
	if updated.Status.Submission != nil {
		t.Error("expected submission to be cleared after healing")
	}

	// Check review cleared (should be nil already, but verify)
	if updated.Status.Review != nil {
		t.Error("expected review to be nil after healing")
	}

	// Check publishedVersions appended
	if len(updated.Status.PublishedVersions) != 1 {
		t.Fatalf("expected 1 published version, got %d", len(updated.Status.PublishedVersions))
	}
	pubVer := updated.Status.PublishedVersions[0]
	if pubVer.BlueprintName != "test-blueprint" {
		t.Errorf("expected blueprint name test-blueprint, got %s", pubVer.BlueprintName)
	}
	if pubVer.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", pubVer.Version)
	}
	if pubVer.PublishedBy != "approver" {
		t.Errorf("expected published by approver, got %s", pubVer.PublishedBy)
	}
}

func TestBundleReconciler_SelfHealing_MissingBP(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	if err := aifv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add aifv1 to scheme: %v", err)
	}

	// Create Submitted Bundle with submission data
	submittedBundle := &aifv1.Bundle{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "submitted-bundle",
			Namespace:  "test-ns",
			Generation: 5,
			Finalizers: []string{"ai.suse.com/cleanup"},
		},
		Spec: aifv1.BundleSpec{
			Title:           "Submitted Bundle",
			TargetBlueprint: "test-blueprint",
			UseCase:         "rag",
			Components: []aifv1.ComponentRef{
				{
					Name: "app1",
					Kind: aifv1.ComponentKindApp,
					App: &aifv1.AppRef{
						Repo:    "https://example.com/charts",
						Chart:   "test-chart",
						Version: "1.0.0",
					},
				},
			},
		},
		Status: aifv1.BundleStatus{
			Phase: aifv1.BundlePhaseSubmitted,
			Submission: &aifv1.SubmissionStatus{
				ProposedVersion:    "1.0.0",
				ChangeDescription:  "Initial release",
				SubmittedBy:        "alice",
				SubmittedAt:        metav1.Now(),
				GenerationAtSubmit: 5,
			},
		},
	}

	// NO Blueprint created - should not heal

	// Create fake client
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(submittedBundle). // Only bundle, no blueprint
		WithStatusSubresource(&aifv1.Bundle{}).
		Build()

	// Create fake manager
	fakeManager := &fakeBundleManager{
		upsertFunc: func(ctx context.Context, b bundle.Bundle) error {
			return nil
		},
	}

	// Create reconciler
	reconciler := &BundleReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: &fakeRecorder{},
		Manager:  fakeManager,
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "submitted-bundle",
			Namespace: "test-ns",
		},
	}

	// Reconcile - should NOT heal because Blueprint is missing
	result, err := reconciler.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue")
	}

	// Verify Bundle status was NOT healed
	var updated aifv1.Bundle
	if err := fakeClient.Get(context.Background(), req.NamespacedName, &updated); err != nil {
		t.Fatalf("failed to get updated bundle: %v", err)
	}

	// Check phase still Submitted (not healed)
	if updated.Status.Phase != aifv1.BundlePhaseSubmitted {
		t.Errorf("expected phase Submitted (no healing), got %s", updated.Status.Phase)
	}

	// Check submission still present (not cleared)
	if updated.Status.Submission == nil {
		t.Error("expected submission to still be present (no healing)")
	}

	// Check publishedVersions still empty (not appended)
	if len(updated.Status.PublishedVersions) != 0 {
		t.Errorf("expected 0 published versions (no healing), got %d", len(updated.Status.PublishedVersions))
	}
}

func TestBundleReconciler_SelfHealing_NoMatch(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	if err := aifv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add aifv1 to scheme: %v", err)
	}

	// Create Submitted Bundle with submission data
	submittedBundle := &aifv1.Bundle{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "submitted-bundle",
			Namespace:  "test-ns",
			Generation: 5,
			Finalizers: []string{"ai.suse.com/cleanup"},
		},
		Spec: aifv1.BundleSpec{
			Title:           "Submitted Bundle",
			TargetBlueprint: "test-blueprint",
			UseCase:         "rag",
			Components: []aifv1.ComponentRef{
				{
					Name: "app1",
					Kind: aifv1.ComponentKindApp,
					App: &aifv1.AppRef{
						Repo:    "https://example.com/charts",
						Chart:   "test-chart",
						Version: "1.0.0",
					},
				},
			},
		},
		Status: aifv1.BundleStatus{
			Phase: aifv1.BundlePhaseSubmitted,
			Submission: &aifv1.SubmissionStatus{
				ProposedVersion:    "1.0.0",
				ChangeDescription:  "Initial release",
				SubmittedBy:        "alice",
				SubmittedAt:        metav1.Now(),
				GenerationAtSubmit: 5,
			},
		},
	}

	// Create Blueprint published from DIFFERENT Bundle (different namespace)
	nonMatchingBlueprint := &aifv1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-blueprint.1.0.0",
		},
		Spec: aifv1.BlueprintSpec{
			BlueprintName: "test-blueprint",
			Version:       "1.0.0",
			UseCase:       "rag",
			Source: aifv1.BlueprintSource{
				Type: aifv1.BlueprintSourcePublished,
				PublishedFrom: &aifv1.PublishedFromRef{
					BundleNamespace:  "different-ns", // Different namespace
					BundleName:       "submitted-bundle",
					BundleGeneration: 5,
				},
			},
			Components: []aifv1.ComponentRef{
				{
					Name: "app1",
					Kind: aifv1.ComponentKindApp,
					App: &aifv1.AppRef{
						Repo:    "https://example.com/charts",
						Chart:   "test-chart",
						Version: "1.0.0",
					},
				},
			},
			PublishedBy: "approver",
			PublishedAt: metav1.Now(),
		},
	}

	// Create fake client
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(submittedBundle, nonMatchingBlueprint).
		WithStatusSubresource(&aifv1.Bundle{}).
		Build()

	// Create fake manager
	fakeManager := &fakeBundleManager{
		upsertFunc: func(ctx context.Context, b bundle.Bundle) error {
			return nil
		},
	}

	// Create reconciler
	reconciler := &BundleReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: &fakeRecorder{},
		Manager:  fakeManager,
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "submitted-bundle",
			Namespace: "test-ns",
		},
	}

	// Reconcile - should NOT heal because Blueprint from different Bundle
	result, err := reconciler.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue")
	}

	// Verify Bundle status was NOT healed
	var updated aifv1.Bundle
	if err := fakeClient.Get(context.Background(), req.NamespacedName, &updated); err != nil {
		t.Fatalf("failed to get updated bundle: %v", err)
	}

	// Check phase still Submitted (not healed)
	if updated.Status.Phase != aifv1.BundlePhaseSubmitted {
		t.Errorf("expected phase Submitted (no healing), got %s", updated.Status.Phase)
	}

	// Check submission still present (not cleared)
	if updated.Status.Submission == nil {
		t.Error("expected submission to still be present (no healing)")
	}

	// Check publishedVersions still empty (not appended)
	if len(updated.Status.PublishedVersions) != 0 {
		t.Errorf("expected 0 published versions (no healing), got %d", len(updated.Status.PublishedVersions))
	}
}


