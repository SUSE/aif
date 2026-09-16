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

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// The scenario this exists for: SUSEAI-803's operator started before Rancher,
// found catalog.cattle.io unregistered, and — before catalogCRDBecameReady
// existed — sat blocked until failureRetryInterval's own backoff happened to
// land after Rancher caught up, up to maxFailureRetryInterval (15 minutes)
// later. Watching CustomResourceDefinition directly turns that into "the next
// reconcile after the CRD shows up", regardless of where the backoff clock
// happened to be.

func unestablishedCRD(name string) *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

func establishedCRD(name string) *apiextensionsv1.CustomResourceDefinition {
	crd := unestablishedCRD(name)
	crd.Status.Conditions = []apiextensionsv1.CustomResourceDefinitionCondition{
		{Type: apiextensionsv1.Established, Status: apiextensionsv1.ConditionTrue},
	}
	return crd
}

// A brand-new CRD is worth reacting to immediately even if this particular
// watch event does not yet show it Established: checkPreconditions will
// simply re-fail and re-backoff if it genuinely is not ready, at the cost of
// one extra reconcile — far cheaper than missing the real transition because
// the watch's own cache lagged behind the Create.
func TestCatalogCRDBecameReady_FiresOnCreateOfARelevantCRD(t *testing.T) {
	if !catalogCRDBecameReady.Create(event.CreateEvent{Object: unestablishedCRD("uiplugins.catalog.cattle.io")}) {
		t.Error("want Create to fire for a relevant CRD even before it reports Established")
	}
}

func TestCatalogCRDBecameReady_IgnoresCreateOfAnUnrelatedCRD(t *testing.T) {
	if catalogCRDBecameReady.Create(event.CreateEvent{Object: unestablishedCRD("widgets.example.com")}) {
		t.Error("want Create to ignore a CRD this operator has no stake in")
	}
}

func TestCatalogCRDBecameReady_FiresWhenEstablishedTransitionsToTrue(t *testing.T) {
	e := event.UpdateEvent{
		ObjectOld: unestablishedCRD("clusterrepos.catalog.cattle.io"),
		ObjectNew: establishedCRD("clusterrepos.catalog.cattle.io"),
	}
	if !catalogCRDBecameReady.Update(e) {
		t.Error("want Update to fire on the Established false-to-true transition")
	}
}

// Every other reconcile that already succeeded, or every status churn once
// established, re-triggering this watch would only add load without changing
// the verdict — CheckCRDs already passed and will keep passing.
func TestCatalogCRDBecameReady_IgnoresUpdatesThatAreNotTheTransition(t *testing.T) {
	tests := []struct {
		name string
		old  *apiextensionsv1.CustomResourceDefinition
		new  *apiextensionsv1.CustomResourceDefinition
	}{
		{"already established, no change", establishedCRD("uiplugins.catalog.cattle.io"), establishedCRD("uiplugins.catalog.cattle.io")},
		{"still not established", unestablishedCRD("uiplugins.catalog.cattle.io"), unestablishedCRD("uiplugins.catalog.cattle.io")},
		{"unrelated CRD becoming established", unestablishedCRD("widgets.example.com"), establishedCRD("widgets.example.com")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := event.UpdateEvent{ObjectOld: tt.old, ObjectNew: tt.new}
			if catalogCRDBecameReady.Update(e) {
				t.Errorf("want Update to ignore %s", tt.name)
			}
		})
	}
}

// Deletes and generic events carry no information this watch acts on — a CRD
// disappearing does not un-block anything, and a generic/poll event has no
// object transition to react to.
func TestCatalogCRDBecameReady_IgnoresDeleteAndGeneric(t *testing.T) {
	if catalogCRDBecameReady.Delete(event.DeleteEvent{Object: establishedCRD("uiplugins.catalog.cattle.io")}) {
		t.Error("want Delete to never fire")
	}
	if catalogCRDBecameReady.Generic(event.GenericEvent{Object: establishedCRD("uiplugins.catalog.cattle.io")}) {
		t.Error("want Generic to never fire")
	}
}

func installAIExtensionScheme(t *testing.T) *kruntime.Scheme {
	t.Helper()
	scheme := kruntime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}
	return scheme
}

// InstallAIExtension is cluster-scoped (confirmed by SUSEAI-803's own logs,
// which show reconciles with an empty namespace), so there is exactly one
// name to enqueue per object, no namespace to resolve. Shared by both the
// CRD watch and the extension-namespace watch — the object passed in is
// never inspected, only used to trigger the enqueue.
func TestEnqueueAllInstallAIExtensions_EnqueuesEveryExtension(t *testing.T) {
	scheme := installAIExtensionScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(helmExtension(), &v1alpha1.InstallAIExtension{ObjectMeta: metav1.ObjectMeta{Name: "second-extension"}}).
		Build()
	r := &InstallAIExtensionReconciler{Client: c}

	reqs := r.enqueueAllInstallAIExtensions(context.Background(), establishedCRD("uiplugins.catalog.cattle.io"))

	if len(reqs) != 2 {
		t.Fatalf("got %d requests, want 2: %+v", len(reqs), reqs)
	}
	names := map[string]bool{}
	for _, req := range reqs {
		names[req.Name] = true
	}
	if !names["aif-ui"] || !names["second-extension"] {
		t.Errorf("requests = %+v, want both aif-ui and second-extension", reqs)
	}
}

func TestEnqueueAllInstallAIExtensions_NoExtensionsEnqueuesNothing(t *testing.T) {
	scheme := installAIExtensionScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &InstallAIExtensionReconciler{Client: c}

	reqs := r.enqueueAllInstallAIExtensions(context.Background(), establishedCRD("uiplugins.catalog.cattle.io"))

	if len(reqs) != 0 {
		t.Errorf("got %d requests, want 0: %+v", len(reqs), reqs)
	}
}
