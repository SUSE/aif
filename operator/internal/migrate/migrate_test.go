/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package migrate

import (
	"context"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const workloadNS = "aif-workloads"

var bundleGVK = schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "Bundle"}

func newScheme(t *testing.T) *kruntime.Scheme {
	t.Helper()
	s := kruntime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	// Register the Fleet Bundle GVK as unstructured so the fake client can list it.
	s.AddKnownTypeWithName(bundleGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(bundleListGVK, &unstructured.UnstructuredList{})
	return s
}

func workload(ns, name string, withFinalizer bool) *aiplatformv1alpha1.AIWorkload {
	w := &aiplatformv1alpha1.AIWorkload{}
	w.Namespace = ns
	w.Name = name
	w.Spec.DisplayName = name
	w.Spec.TargetNamespace = ns
	w.Spec.DeployStrategy = "Helm"
	if withFinalizer {
		w.Finalizers = []string{aiWorkloadFinalizer}
	}
	return w
}

func newClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	return fake.NewClientBuilder().
		WithScheme(newScheme(t)).
		WithStatusSubresource(&aiplatformv1alpha1.AIWorkload{}).
		WithObjects(objs...).
		Build()
}

func TestRun_RelocatesAndStripsFinalizer(t *testing.T) {
	c := newClient(t, workload("team-a", "llama", true))

	report, err := Run(context.Background(), c, workloadNS, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Migrated) != 1 {
		t.Fatalf("expected 1 migrated, got %d (%v)", len(report.Migrated), report.Migrated)
	}
	if len(report.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", report.Warnings)
	}

	// Source is gone (finalizer stripped, so delete fully removed it).
	src := &aiplatformv1alpha1.AIWorkload{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "team-a", Name: "llama"}, src); !errors.IsNotFound(err) {
		t.Fatalf("expected source removed, got err=%v (finalizers=%v, deletionTimestamp=%v)",
			err, src.Finalizers, src.DeletionTimestamp)
	}

	// Destination exists with preserved spec and the migrated-from marker.
	dst := &aiplatformv1alpha1.AIWorkload{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: workloadNS, Name: "llama"}, dst); err != nil {
		t.Fatalf("expected destination CR: %v", err)
	}
	if dst.Spec.TargetNamespace != "team-a" {
		t.Fatalf("Spec.TargetNamespace = %q, want %q (deployment target must be preserved)", dst.Spec.TargetNamespace, "team-a")
	}
	if dst.Annotations[migratedFromAnnotation] != "team-a/llama" {
		t.Fatalf("migrated-from = %q, want %q", dst.Annotations[migratedFromAnnotation], "team-a/llama")
	}
}

func TestRun_AlreadyInPlace(t *testing.T) {
	c := newClient(t, workload(workloadNS, "llama", true))

	report, err := Run(context.Background(), c, workloadNS, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Migrated) != 0 {
		t.Fatalf("expected 0 migrated, got %v", report.Migrated)
	}
	if len(report.AlreadyInPlace) != 1 {
		t.Fatalf("expected 1 already-in-place, got %v", report.AlreadyInPlace)
	}
	// The in-place CR must keep its finalizer (untouched).
	got := &aiplatformv1alpha1.AIWorkload{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: workloadNS, Name: "llama"}, got); err != nil {
		t.Fatal(err)
	}
	if len(got.Finalizers) != 1 {
		t.Fatalf("in-place CR finalizer should be untouched, got %v", got.Finalizers)
	}
}

func TestRun_CollisionSkipped(t *testing.T) {
	// A different workload already occupies the destination name (no migrated-from).
	occupant := workload(workloadNS, "llama", true)
	c := newClient(t, occupant, workload("team-a", "llama", true))

	report, err := Run(context.Background(), c, workloadNS, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Migrated) != 0 {
		t.Fatalf("expected 0 migrated, got %v", report.Migrated)
	}
	if len(report.Skipped) != 1 {
		t.Fatalf("expected 1 skipped collision, got %v", report.Skipped)
	}
	// Source must be left untouched for manual resolution.
	src := &aiplatformv1alpha1.AIWorkload{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "team-a", Name: "llama"}, src); err != nil {
		t.Fatalf("source should be preserved on collision: %v", err)
	}
}

func TestRun_ResumesPartialMigration(t *testing.T) {
	// Destination copy already exists (our own, marked migrated-from) but the
	// source was not yet deleted — simulate a crashed prior run.
	dst := workload(workloadNS, "llama", false)
	dst.Annotations = map[string]string{migratedFromAnnotation: "team-a/llama"}
	c := newClient(t, dst, workload("team-a", "llama", true))

	report, err := Run(context.Background(), c, workloadNS, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Skipped) != 0 {
		t.Fatalf("resume must not be a collision, got skipped=%v", report.Skipped)
	}
	if len(report.Migrated) != 1 {
		t.Fatalf("expected 1 migrated (resumed), got %v", report.Migrated)
	}
	// Source should now be gone.
	src := &aiplatformv1alpha1.AIWorkload{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "team-a", Name: "llama"}, src); !errors.IsNotFound(err) {
		t.Fatalf("expected source removed after resume, got err=%v", err)
	}
}

func TestRun_DryRunMutatesNothing(t *testing.T) {
	c := newClient(t, workload("team-a", "llama", true))

	report, err := Run(context.Background(), c, workloadNS, Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Migrated) != 1 {
		t.Fatalf("dry-run should report 1 planned migration, got %v", report.Migrated)
	}
	// Source still present, destination not created.
	src := &aiplatformv1alpha1.AIWorkload{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "team-a", Name: "llama"}, src); err != nil {
		t.Fatalf("dry-run must not delete source: %v", err)
	}
	dst := &aiplatformv1alpha1.AIWorkload{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: workloadNS, Name: "llama"}, dst); !errors.IsNotFound(err) {
		t.Fatalf("dry-run must not create destination, got err=%v", err)
	}
}

func TestRun_SweepsOrphanPullSecretBundles(t *testing.T) {
	orphan := &unstructured.Unstructured{}
	orphan.SetGroupVersionKind(bundleGVK)
	orphan.SetNamespace("fleet-default")
	orphan.SetName("ai-pullsecrets-llama-c-abc")
	orphan.SetLabels(map[string]string{
		ownerNameLabel:      "llama",
		ownerNamespaceLabel: "team-a",
	})

	c := newClient(t, workload("team-a", "llama", true), orphan)

	report, err := Run(context.Background(), c, workloadNS, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.OrphanBundlesDeleted != 1 {
		t.Fatalf("expected 1 orphan bundle deleted, got %d (warnings=%v)", report.OrphanBundlesDeleted, report.Warnings)
	}
	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(bundleGVK)
	err = c.Get(context.Background(), types.NamespacedName{Namespace: "fleet-default", Name: "ai-pullsecrets-llama-c-abc"}, got)
	if !errors.IsNotFound(err) {
		t.Fatalf("expected orphan bundle removed, got err=%v", err)
	}
}
