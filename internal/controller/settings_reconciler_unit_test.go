package controller

import (
	"context"
	"testing"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// noopRecorder stubs events.EventRecorder for unit tests that don't care
// about event emission.
type noopRecorder struct{}

func (noopRecorder) Eventf(_ kruntime.Object, _ kruntime.Object, _, _, _, _ string, _ ...interface{}) {
}
func (noopRecorder) AnnotatedEventf(_ kruntime.Object, _ kruntime.Object, _ map[string]string, _, _, _, _ string, _ ...interface{}) {
}

// TestSettingsReconciler_AppliesSnapshotOnReconcile: reconcile a Settings CR
// → applier captured a snapshot with the expected fields.
func TestSettingsReconciler_AppliesSnapshotOnReconcile(t *testing.T) {
	scheme := kruntime.NewScheme()
	if err := aifv1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}

	settings := &aifv1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: "aif"},
		Spec: aifv1.SettingsSpec{
			RegistryEndpoints: &aifv1.RegistryEndpointsSpec{
				SUSERegistry: "harbor.example.com",
			},
		},
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&aifv1.Settings{}).WithObjects(settings).Build()
	applier := &FakeSettingsApplier{}

	r := &SettingsReconciler{
		Client:   cli,
		Scheme:   scheme,
		Recorder: noopRecorder{},
		Applier:  applier,
	}
	// First reconcile adds the finalizer (existing P1-4 behavior); second runs the body.
	for i := 0; i < 2; i++ {
		if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "settings", Namespace: "aif"}}); err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
	}

	if len(applier.Calls) == 0 {
		t.Fatal("Applier.Apply was never called")
	}
	last := applier.Calls[len(applier.Calls)-1]
	if last.SUSERegistry != "harbor.example.com" {
		t.Errorf("SUSERegistry: got %q", last.SUSERegistry)
	}
}

// TestSettingsReconciler_SkipsApplyOnSecretError: Secret resolution fails →
// applier was never called (no partial state pushed to engines).
func TestSettingsReconciler_SkipsApplyOnSecretError(t *testing.T) {
	scheme := kruntime.NewScheme()
	if err := aifv1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}

	settings := &aifv1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: "aif"},
		Spec: aifv1.SettingsSpec{
			SUSERegistry: &aifv1.SUSERegistryConfig{
				UserSecretRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "missing-secret"},
					Key:                  "user",
				},
			},
		},
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&aifv1.Settings{}).WithObjects(settings).Build()
	applier := &FakeSettingsApplier{}

	r := &SettingsReconciler{
		Client:   cli,
		Scheme:   scheme,
		Recorder: noopRecorder{},
		Applier:  applier,
	}
	for i := 0; i < 2; i++ {
		_, _ = r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "settings", Namespace: "aif"}})
	}
	if len(applier.Calls) != 0 {
		t.Errorf("Apply must NOT be called when Secret resolution fails; got %d calls", len(applier.Calls))
	}
}

// TestSettingsReconciler_AppliesEvenWhenNoSpec: reconcile a Settings CR with
// empty Spec → applier called with the all-defaults snapshot.
func TestSettingsReconciler_AppliesEvenWhenNoSpec(t *testing.T) {
	scheme := kruntime.NewScheme()
	if err := aifv1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}

	settings := &aifv1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: "aif"},
		Spec:       aifv1.SettingsSpec{},
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&aifv1.Settings{}).WithObjects(settings).Build()
	applier := &FakeSettingsApplier{}

	r := &SettingsReconciler{
		Client:   cli,
		Scheme:   scheme,
		Recorder: noopRecorder{},
		Applier:  applier,
	}
	for i := 0; i < 2; i++ {
		if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "settings", Namespace: "aif"}}); err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
	}
	if len(applier.Calls) == 0 {
		t.Fatal("Applier.Apply was never called")
	}
	if applier.Calls[len(applier.Calls)-1].SUSERegistry != "registry.suse.com" {
		t.Errorf("default SUSERegistry not in snapshot: %q", applier.Calls[len(applier.Calls)-1].SUSERegistry)
	}
}
