package controller

import (
	"context"
	"testing"
	"time"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/conditions"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// Test helpers: fakeRecorder, findCondition, and containsEventReason are in controller_test_helpers.go

// createSecret creates a test Secret
func createSecret(name, namespace string, data map[string]string) *corev1.Secret {
	secretData := make(map[string][]byte)
	for k, v := range data {
		secretData[k] = []byte(v)
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: secretData,
	}
}

// createSettings creates a test Settings resource
func createSettings(name, namespace string, spec aifv1.SettingsSpec) *aifv1.Settings {
	return &aifv1.Settings{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Generation: 1,
		},
		Spec: spec,
	}
}

func TestSettingsReconciler_ValidCredentials(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	if err := aifv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add aifv1 to scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 to scheme: %v", err)
	}

	// Create test Secrets in "aif" namespace (Settings is singleton in aif namespace)
	suseRegSecret := createSecret("suse-reg-creds", "aif", map[string]string{
		"username": "test-user",
		"password": "test-pass",
	})
	appCollSecret := createSecret("app-coll-creds", "aif", map[string]string{
		"user":  "coll-user",
		"token": "coll-token",
	})

	// Create Settings with valid SecretKeyRefs (Settings is singleton in aif namespace)
	settings := createSettings("aif-settings", "aif", aifv1.SettingsSpec{
		SUSERegistry: &aifv1.SUSERegistryConfig{
			UserSecretRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "suse-reg-creds"},
				Key:                  "username",
			},
			TokenSecretRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "suse-reg-creds"},
				Key:                  "password",
			},
		},
		ApplicationCollection: &aifv1.ApplicationCollectionConfig{
			UserSecretRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "app-coll-creds"},
				Key:                  "user",
			},
			TokenSecretRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "app-coll-creds"},
				Key:                  "token",
			},
		},
	})

	// Create fake client
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(settings, suseRegSecret, appCollSecret).
		WithStatusSubresource(&aifv1.Settings{}).
		Build()

	// Create reconciler
	recorder := &fakeRecorder{}
	reconciler := &SettingsReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: recorder,
	}

	// Reconcile
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "aif-settings",
			Namespace: "aif",
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

	// Second reconcile resolves secrets and applies settings
	result, err = reconciler.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("second reconcile failed: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue for valid settings")
	}

	// Verify status updated
	var updatedSettings aifv1.Settings
	if err := fakeClient.Get(context.Background(), req.NamespacedName, &updatedSettings); err != nil {
		t.Fatalf("failed to get updated settings: %v", err)
	}

	// Check Ready condition
	readyCond := findCondition(updatedSettings.Status.Conditions, conditions.TypeReady)
	if readyCond == nil {
		t.Fatal("Ready condition not set")
	}
	if readyCond.Status != metav1.ConditionTrue {
		t.Errorf("expected Ready=True, got %s: %s", readyCond.Status, readyCond.Message)
	}
	if readyCond.Reason != conditions.ReasonReconciled {
		t.Errorf("expected reason %s, got %s", conditions.ReasonReconciled, readyCond.Reason)
	}

	// Check ObservedGeneration
	if updatedSettings.Status.ObservedGeneration != settings.Generation {
		t.Errorf("expected observedGeneration=%d, got %d", settings.Generation, updatedSettings.Status.ObservedGeneration)
	}

	// Check LastApplied timestamp set
	if updatedSettings.Status.LastApplied.IsZero() {
		t.Error("expected LastApplied timestamp to be set")
	}

	// Verify event emitted (format: "eventtype:reason:message")
	found := false
	for _, evt := range recorder.events {
		if containsEventReason(evt, "SettingsApplied") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SettingsApplied event to be emitted, got events: %v", recorder.events)
	}

	// Note: assertions on cached credential fields removed — those fields were
	// dead state (write-only) and were dropped per OOP code-review report
	// finding #7. Real EngineSettings propagation lands with P5-4.
}

func TestSettingsReconciler_MissingSecret_ApplicationCollection(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := aifv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add aifv1 to scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 to scheme: %v", err)
	}

	// Create Settings with reference to non-existent Secret
	settings := createSettings("aif-settings", "aif", aifv1.SettingsSpec{
		ApplicationCollection: &aifv1.ApplicationCollectionConfig{
			UserSecretRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "missing-secret"},
				Key:                  "user",
			},
		},
		SUSERegistry: &aifv1.SUSERegistryConfig{},
		Fleet:        &aifv1.FleetConfig{},
	})

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(settings).
		WithStatusSubresource(&aifv1.Settings{}).
		Build()

	recorder := &fakeRecorder{}
	reconciler := &SettingsReconciler{
		Client:   client,
		Scheme:   scheme,
		Recorder: recorder,
	}

	ctx := context.Background()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "aif-settings",
			Namespace: "aif",
		},
	}

	// First reconcile (adds finalizer)
	_, _ = reconciler.Reconcile(ctx, req)

	// Second reconcile (processes Secret refs)
	result, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	// Should requeue after 30s (fail-closed behavior)
	if result.RequeueAfter != 30*time.Second {
		t.Errorf("expected RequeueAfter=30s, got %v", result.RequeueAfter)
	}

	// Fetch updated Settings
	var updated aifv1.Settings
	if err := client.Get(ctx, req.NamespacedName, &updated); err != nil {
		t.Fatalf("failed to get updated Settings: %v", err)
	}

	// Assert Ready=False Reason=SecretNotFound
	readyCond := findCondition(updated.Status.Conditions, conditions.TypeReady)
	if readyCond == nil {
		t.Fatal("Ready condition not set")
	}
	if readyCond.Status != metav1.ConditionFalse {
		t.Errorf("Ready status = %s, want False", readyCond.Status)
	}
	if readyCond.Reason != conditions.ReasonSecretNotFound {
		t.Errorf("Ready reason = %s, want %s", readyCond.Reason, conditions.ReasonSecretNotFound)
	}

	// Assert lastApplied NOT updated (remains zero)
	if !updated.Status.LastApplied.IsZero() {
		t.Error("lastApplied should not be set on error")
	}

	// Assert event recorded
	found := false
	for _, e := range recorder.events {
		if containsEventReason(e, "SecretNotFound") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("SecretNotFound event not recorded, got: %v", recorder.events)
	}
}

func TestSettingsReconciler_InvalidSecretKey_ApplicationCollection(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := aifv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add aifv1 to scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 to scheme: %v", err)
	}

	// Create Secret but without the expected key
	appCollSecret := createSecret("suse-app-collection-creds", "aif", map[string]string{
		"wrongkey": "value",
	})

	// Create Settings referencing a key that doesn't exist
	settings := createSettings("aif-settings", "aif", aifv1.SettingsSpec{
		ApplicationCollection: &aifv1.ApplicationCollectionConfig{
			UserSecretRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "suse-app-collection-creds"},
				Key:                  "user", // This key doesn't exist in Secret
			},
		},
		SUSERegistry: &aifv1.SUSERegistryConfig{},
		Fleet:        &aifv1.FleetConfig{},
	})

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(appCollSecret, settings).
		WithStatusSubresource(&aifv1.Settings{}).
		Build()

	recorder := &fakeRecorder{}
	reconciler := &SettingsReconciler{
		Client:   client,
		Scheme:   scheme,
		Recorder: recorder,
	}

	ctx := context.Background()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "aif-settings",
			Namespace: "aif",
		},
	}

	// First reconcile (adds finalizer)
	_, _ = reconciler.Reconcile(ctx, req)

	// Second reconcile (processes Secret refs)
	result, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	// Should requeue after 30s (fail-closed behavior)
	if result.RequeueAfter != 30*time.Second {
		t.Errorf("expected RequeueAfter=30s, got %v", result.RequeueAfter)
	}

	// Fetch updated Settings
	var updated aifv1.Settings
	if err := client.Get(ctx, req.NamespacedName, &updated); err != nil {
		t.Fatalf("failed to get updated Settings: %v", err)
	}

	// Assert Ready=False Reason=InvalidSecretKey
	readyCond := findCondition(updated.Status.Conditions, conditions.TypeReady)
	if readyCond == nil {
		t.Fatal("Ready condition not set")
	}
	if readyCond.Status != metav1.ConditionFalse {
		t.Errorf("Ready status = %s, want False", readyCond.Status)
	}
	if readyCond.Reason != conditions.ReasonInvalidSecretKey {
		t.Errorf("Ready reason = %s, want %s", readyCond.Reason, conditions.ReasonInvalidSecretKey)
	}

	// Assert lastApplied NOT updated
	if !updated.Status.LastApplied.IsZero() {
		t.Error("lastApplied should not be set on error")
	}

	// Assert event recorded
	found := false
	for _, e := range recorder.events {
		if containsEventReason(e, "InvalidSecretKey") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("InvalidSecretKey event not recorded, got: %v", recorder.events)
	}
}

func TestSettingsReconciler_OptionalFields(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := aifv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add aifv1 to scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 to scheme: %v", err)
	}

	// Create Settings with all SecretKeyRefs nil (optional)
	settings := createSettings("aif-settings", "aif", aifv1.SettingsSpec{
		ApplicationCollection: &aifv1.ApplicationCollectionConfig{
			UserSecretRef:  nil, // Optional
			TokenSecretRef: nil, // Optional
		},
		SUSERegistry: &aifv1.SUSERegistryConfig{
			UserSecretRef:  nil,
			TokenSecretRef: nil,
		},
		Fleet: &aifv1.FleetConfig{
			CredSecretRef: nil,
		},
	})

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(settings).
		WithStatusSubresource(&aifv1.Settings{}).
		Build()

	recorder := &fakeRecorder{}
	reconciler := &SettingsReconciler{
		Client:   client,
		Scheme:   scheme,
		Recorder: recorder,
	}

	ctx := context.Background()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "aif-settings",
			Namespace: "aif",
		},
	}

	// First reconcile (adds finalizer)
	_, _ = reconciler.Reconcile(ctx, req)

	// Second reconcile (processes nil refs)
	result, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	// Should not requeue
	if result.Requeue || result.RequeueAfter > 0 {
		t.Errorf("unexpected requeue: %v", result)
	}

	// Fetch updated Settings
	var updated aifv1.Settings
	if err := client.Get(ctx, req.NamespacedName, &updated); err != nil {
		t.Fatalf("failed to get updated Settings: %v", err)
	}

	// Assert Ready=True (nil refs are acceptable)
	readyCond := findCondition(updated.Status.Conditions, conditions.TypeReady)
	if readyCond == nil {
		t.Fatal("Ready condition not set")
	}
	if readyCond.Status != metav1.ConditionTrue {
		t.Errorf("Ready status = %s, want True", readyCond.Status)
	}

	// Assert lastApplied set
	if updated.Status.LastApplied.IsZero() {
		t.Error("lastApplied should be set even with nil refs")
	}

	// Note: assertions on cached credential fields removed (dead state per
	// OOP review finding #7). With nil refs, no Secret is read; status alone
	// proves the reconciler ran cleanly.
}

func TestSettingsReconciler_Finalizer(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := aifv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add aifv1 to scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 to scheme: %v", err)
	}

	settings := createSettings("aif-settings", "aif", aifv1.SettingsSpec{
		ApplicationCollection: &aifv1.ApplicationCollectionConfig{},
		SUSERegistry:          &aifv1.SUSERegistryConfig{},
		Fleet:                 &aifv1.FleetConfig{},
	})

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(settings).
		WithStatusSubresource(&aifv1.Settings{}).
		Build()

	recorder := &fakeRecorder{}
	reconciler := &SettingsReconciler{
		Client:   client,
		Scheme:   scheme,
		Recorder: recorder,
	}

	ctx := context.Background()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "aif-settings",
			Namespace: "aif",
		},
	}

	// First reconcile - should add finalizer
	result, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if !result.Requeue {
		t.Error("expected requeue after adding finalizer")
	}

	// Fetch and verify finalizer added
	var updated aifv1.Settings
	if err := client.Get(ctx, req.NamespacedName, &updated); err != nil {
		t.Fatalf("failed to get Settings: %v", err)
	}
	if len(updated.Finalizers) != 1 || updated.Finalizers[0] != "ai.suse.com/cleanup" {
		t.Errorf("finalizer not added: %v", updated.Finalizers)
	}

	// Delete the resource
	if err := client.Delete(ctx, &updated); err != nil {
		t.Fatalf("failed to delete Settings: %v", err)
	}

	// Reconcile deletion
	result, err = reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("deletion reconcile failed: %v", err)
	}

	// Verify finalizer was removed by checking if resource can be fetched
	// (with fake client, once finalizer is removed, the resource is deleted)
	err = client.Get(ctx, req.NamespacedName, &updated)
	if err == nil {
		// Resource still exists - check if finalizer was removed
		if len(updated.Finalizers) != 0 {
			t.Errorf("finalizer not removed: %v", updated.Finalizers)
		}
	}
	// If resource not found, finalizer was successfully removed and resource deleted
}

func TestSettingsReconciler_UpdateCredentials(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := aifv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add aifv1 to scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 to scheme: %v", err)
	}

	// Create initial Secret
	appCollSecret := createSecret("suse-app-collection-creds", "aif", map[string]string{
		"user":  "olduser",
		"token": "oldtoken",
	})

	settings := createSettings("aif-settings", "aif", aifv1.SettingsSpec{
		ApplicationCollection: &aifv1.ApplicationCollectionConfig{
			UserSecretRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "suse-app-collection-creds"},
				Key:                  "user",
			},
			TokenSecretRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "suse-app-collection-creds"},
				Key:                  "token",
			},
		},
		SUSERegistry: &aifv1.SUSERegistryConfig{},
		Fleet:        &aifv1.FleetConfig{},
	})

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(appCollSecret, settings).
		WithStatusSubresource(&aifv1.Settings{}).
		Build()

	recorder := &fakeRecorder{}
	reconciler := &SettingsReconciler{
		Client:   client,
		Scheme:   scheme,
		Recorder: recorder,
	}

	ctx := context.Background()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "aif-settings",
			Namespace: "aif",
		},
	}

	// First reconcile (finalizer)
	_, _ = reconciler.Reconcile(ctx, req)
	// Second reconcile (process credentials)
	_, _ = reconciler.Reconcile(ctx, req)

	// Get first lastApplied timestamp
	var settings1 aifv1.Settings
	if err := client.Get(ctx, req.NamespacedName, &settings1); err != nil {
		t.Fatalf("failed to get Settings: %v", err)
	}
	time1 := settings1.Status.LastApplied

	// Note: assertions on cached credential fields removed (dead state per
	// OOP review finding #7). The lastApplied/timestamp comparison below is
	// the durable signal that the second reconcile actually saw the new value.

	// Wait to ensure timestamp changes (metav1.Now() has second granularity)
	time.Sleep(1100 * time.Millisecond)

	// Update Secret
	var secret corev1.Secret
	if err := client.Get(ctx, types.NamespacedName{Name: "suse-app-collection-creds", Namespace: "aif"}, &secret); err != nil {
		t.Fatalf("failed to get Secret: %v", err)
	}
	secret.Data["user"] = []byte("newuser")
	secret.Data["token"] = []byte("newtoken")
	if err := client.Update(ctx, &secret); err != nil {
		t.Fatalf("failed to update Secret: %v", err)
	}

	// Reconcile again
	_, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("second reconcile failed: %v", err)
	}

	// Get updated lastApplied timestamp
	var settings2 aifv1.Settings
	if err := client.Get(ctx, req.NamespacedName, &settings2); err != nil {
		t.Fatalf("failed to get Settings: %v", err)
	}
	time2 := settings2.Status.LastApplied

	// Assert lastApplied updated
	if !time2.After(time1.Time) {
		t.Errorf("lastApplied not updated: %v -> %v", time1, time2)
	}

	// Note: assertions on cached credential fields removed (dead state per
	// OOP review finding #7). lastApplied advancing is the proof the second
	// reconcile re-read the (now-updated) Secret successfully.
}

func TestSettingsReconciler_MissingSecret_SUSERegistry(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := aifv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add aifv1 to scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 to scheme: %v", err)
	}

	settings := createSettings("aif-settings", "aif", aifv1.SettingsSpec{
		ApplicationCollection: &aifv1.ApplicationCollectionConfig{},
		SUSERegistry: &aifv1.SUSERegistryConfig{
			TokenSecretRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "missing-registry-secret"},
				Key:                  "token",
			},
		},
		Fleet: &aifv1.FleetConfig{},
	})

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(settings).
		WithStatusSubresource(&aifv1.Settings{}).
		Build()

	recorder := &fakeRecorder{}
	reconciler := &SettingsReconciler{
		Client:   client,
		Scheme:   scheme,
		Recorder: recorder,
	}

	ctx := context.Background()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "aif-settings",
			Namespace: "aif",
		},
	}

	_, _ = reconciler.Reconcile(ctx, req)
	_, _ = reconciler.Reconcile(ctx, req)

	var updated aifv1.Settings
	if err := client.Get(ctx, req.NamespacedName, &updated); err != nil {
		t.Fatalf("failed to get Settings: %v", err)
	}

	readyCond := findCondition(updated.Status.Conditions, conditions.TypeReady)
	if readyCond == nil || readyCond.Reason != conditions.ReasonSecretNotFound {
		t.Errorf("expected SecretNotFound condition, got: %v", readyCond)
	}
}

func TestSettingsReconciler_MissingSecret_Fleet(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := aifv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add aifv1 to scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 to scheme: %v", err)
	}

	settings := createSettings("aif-settings", "aif", aifv1.SettingsSpec{
		ApplicationCollection: &aifv1.ApplicationCollectionConfig{},
		SUSERegistry:          &aifv1.SUSERegistryConfig{},
		Fleet: &aifv1.FleetConfig{
			CredSecretRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "missing-fleet-secret"},
				Key:                  "cred",
			},
		},
	})

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(settings).
		WithStatusSubresource(&aifv1.Settings{}).
		Build()

	recorder := &fakeRecorder{}
	reconciler := &SettingsReconciler{
		Client:   client,
		Scheme:   scheme,
		Recorder: recorder,
	}

	ctx := context.Background()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "aif-settings",
			Namespace: "aif",
		},
	}

	_, _ = reconciler.Reconcile(ctx, req)
	_, _ = reconciler.Reconcile(ctx, req)

	var updated aifv1.Settings
	if err := client.Get(ctx, req.NamespacedName, &updated); err != nil {
		t.Fatalf("failed to get Settings: %v", err)
	}

	readyCond := findCondition(updated.Status.Conditions, conditions.TypeReady)
	if readyCond == nil || readyCond.Reason != conditions.ReasonSecretNotFound {
		t.Errorf("expected SecretNotFound condition, got: %v", readyCond)
	}
}

func TestSettingsReconciler_InvalidSecretKey_SUSERegistry(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := aifv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add aifv1 to scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 to scheme: %v", err)
	}

	secret := createSecret("suse-registry-creds-source", "aif", map[string]string{
		"wrongkey": "value",
	})

	settings := createSettings("aif-settings", "aif", aifv1.SettingsSpec{
		ApplicationCollection: &aifv1.ApplicationCollectionConfig{},
		SUSERegistry: &aifv1.SUSERegistryConfig{
			UserSecretRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "suse-registry-creds-source"},
				Key:                  "user", // Key doesn't exist
			},
		},
		Fleet: &aifv1.FleetConfig{},
	})

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(secret, settings).
		WithStatusSubresource(&aifv1.Settings{}).
		Build()

	recorder := &fakeRecorder{}
	reconciler := &SettingsReconciler{
		Client:   client,
		Scheme:   scheme,
		Recorder: recorder,
	}

	ctx := context.Background()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "aif-settings",
			Namespace: "aif",
		},
	}

	_, _ = reconciler.Reconcile(ctx, req)
	_, _ = reconciler.Reconcile(ctx, req)

	var updated aifv1.Settings
	if err := client.Get(ctx, req.NamespacedName, &updated); err != nil {
		t.Fatalf("failed to get Settings: %v", err)
	}

	readyCond := findCondition(updated.Status.Conditions, conditions.TypeReady)
	if readyCond == nil || readyCond.Reason != conditions.ReasonInvalidSecretKey {
		t.Errorf("expected InvalidSecretKey condition, got: %v", readyCond)
	}
}

func TestSettingsReconciler_InvalidSecretKey_Fleet(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := aifv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add aifv1 to scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 to scheme: %v", err)
	}

	secret := createSecret("fleet-git-creds", "aif", map[string]string{
		"wrongkey": "value",
	})

	settings := createSettings("aif-settings", "aif", aifv1.SettingsSpec{
		ApplicationCollection: &aifv1.ApplicationCollectionConfig{},
		SUSERegistry:          &aifv1.SUSERegistryConfig{},
		Fleet: &aifv1.FleetConfig{
			CredSecretRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "fleet-git-creds"},
				Key:                  "cred", // Key doesn't exist
			},
		},
	})

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(secret, settings).
		WithStatusSubresource(&aifv1.Settings{}).
		Build()

	recorder := &fakeRecorder{}
	reconciler := &SettingsReconciler{
		Client:   client,
		Scheme:   scheme,
		Recorder: recorder,
	}

	ctx := context.Background()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "aif-settings",
			Namespace: "aif",
		},
	}

	_, _ = reconciler.Reconcile(ctx, req)
	_, _ = reconciler.Reconcile(ctx, req)

	var updated aifv1.Settings
	if err := client.Get(ctx, req.NamespacedName, &updated); err != nil {
		t.Fatalf("failed to get Settings: %v", err)
	}

	readyCond := findCondition(updated.Status.Conditions, conditions.TypeReady)
	if readyCond == nil || readyCond.Reason != conditions.ReasonInvalidSecretKey {
		t.Errorf("expected InvalidSecretKey condition, got: %v", readyCond)
	}
}
