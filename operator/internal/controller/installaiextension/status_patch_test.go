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

package controller

import (
	"context"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// persistStatus must tolerate a concurrent resourceVersion bump: it patches the
// status subresource with no optimistic-concurrency precondition, so a status
// write cannot fail with an "object has been modified" (409) conflict even when
// the object was modified on the server after we read it.
func TestPersistStatus_ToleratesConcurrentResourceVersionBump(t *testing.T) {
	scheme := kruntime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}

	ext := &v1alpha1.InstallAIExtension{
		ObjectMeta: metav1.ObjectMeta{Name: "aif-ui"},
		Spec: v1alpha1.InstallAIExtensionSpec{
			Extension: v1alpha1.ExtensionConfig{Name: "aif-ui", Version: "1.0.0"},
			Source: v1alpha1.ExtensionSource{
				Kind: v1alpha1.ExtensionSourceKindHelm,
				Helm: &v1alpha1.HelmSource{ChartURL: "oci://example.com/aif-ui", Version: "1.0.0"},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha1.InstallAIExtension{}).
		WithObjects(ext).
		Build()

	r := &InstallAIExtensionReconciler{Client: c}
	ctx := context.Background()
	key := types.NamespacedName{Name: "aif-ui"}

	// Read the object we intend to update, and snapshot it as the patch base.
	var got v1alpha1.InstallAIExtension
	if err := c.Get(ctx, key, &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	base := got.DeepCopy()

	// Simulate a concurrent writer bumping the object's resourceVersion after
	// our read (e.g. a stale informer cache re-reconcile scenario).
	var concurrent v1alpha1.InstallAIExtension
	if err := c.Get(ctx, key, &concurrent); err != nil {
		t.Fatalf("get concurrent: %v", err)
	}
	if concurrent.Labels == nil {
		concurrent.Labels = map[string]string{}
	}
	concurrent.Labels["touched"] = "true"
	if err := c.Update(ctx, &concurrent); err != nil {
		t.Fatalf("concurrent update: %v", err)
	}

	// Our in-memory object still carries the pre-bump resourceVersion.
	got.Status.Phase = v1alpha1.InstallAIExtensionPhaseInstalled
	if err := r.persistStatus(ctx, &got, base); err != nil {
		t.Fatalf("persistStatus should tolerate a stale resourceVersion, got: %v", err)
	}

	// The status write landed despite the concurrent modification.
	var after v1alpha1.InstallAIExtension
	if err := c.Get(ctx, key, &after); err != nil {
		t.Fatalf("get after: %v", err)
	}
	if after.Status.Phase != v1alpha1.InstallAIExtensionPhaseInstalled {
		t.Fatalf("expected Phase %q, got %q", v1alpha1.InstallAIExtensionPhaseInstalled, after.Status.Phase)
	}
}

// Documents why we patch instead of Update: the same stale write via
// Status().Update is rejected with a conflict.
func TestStatusUpdate_ConflictsOnStaleResourceVersion(t *testing.T) {
	scheme := kruntime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}

	ext := &v1alpha1.InstallAIExtension{
		ObjectMeta: metav1.ObjectMeta{Name: "aif-ui"},
	}
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha1.InstallAIExtension{}).
		WithObjects(ext).
		Build()

	ctx := context.Background()
	key := types.NamespacedName{Name: "aif-ui"}

	var stale v1alpha1.InstallAIExtension
	if err := c.Get(ctx, key, &stale); err != nil {
		t.Fatalf("get: %v", err)
	}

	// Concurrent bump.
	var concurrent v1alpha1.InstallAIExtension
	if err := c.Get(ctx, key, &concurrent); err != nil {
		t.Fatalf("get concurrent: %v", err)
	}
	concurrent.Labels = map[string]string{"touched": "true"}
	if err := c.Update(ctx, &concurrent); err != nil {
		t.Fatalf("concurrent update: %v", err)
	}

	stale.Status.Phase = v1alpha1.InstallAIExtensionPhaseInstalled
	err := c.Status().Update(ctx, &stale)
	if !apierrors.IsConflict(err) {
		t.Fatalf("expected a conflict error from stale Status().Update, got: %v", err)
	}
}
