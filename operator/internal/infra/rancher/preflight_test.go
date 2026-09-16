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

package rancher

import (
	"context"
	stderrors "errors"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// The case this whole file exists for: before CheckCRDs read through m.client,
// it dialled rest.InClusterConfig() directly, which only ever succeeds inside a
// real pod. Under go test that call fails unconditionally, so nothing here
// could previously exercise the real implementation — every caller had to stub
// the whole rancherManager interface instead. These tests are the first ones
// that call the genuine CheckCRDs.

func newPreflightScheme(t *testing.T) *kruntime.Scheme {
	t.Helper()
	scheme := kruntime.NewScheme()
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}
	return scheme
}

func crdNamed(name string) *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

func TestCheckCRDs_AllPresentSucceeds(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newPreflightScheme(t)).
		WithObjects(crdNamed("uiplugins.catalog.cattle.io"), crdNamed("clusterrepos.catalog.cattle.io")).
		Build()
	m := NewManager(c)

	err := m.CheckCRDs(context.Background(), []string{
		"uiplugins.catalog.cattle.io",
		"clusterrepos.catalog.cattle.io",
	})
	if err != nil {
		t.Fatalf("CheckCRDs = %v, want nil; both CRDs exist", err)
	}
}

func TestCheckCRDs_MissingCRDReportsWhichOne(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newPreflightScheme(t)).Build()
	m := NewManager(c)

	err := m.CheckCRDs(context.Background(), []string{"uiplugins.catalog.cattle.io"})

	var notReady *DependencyNotReadyError
	if !stderrors.As(err, &notReady) {
		t.Fatalf("CheckCRDs error = %v (%T), want a *DependencyNotReadyError", err, err)
	}
	if notReady.Dependency != "uiplugins.catalog.cattle.io" {
		t.Errorf("Dependency = %q, want %q", notReady.Dependency, "uiplugins.catalog.cattle.io")
	}
}

// The list is checked in order, and stops at the first gap rather than
// collecting every missing CRD — confirms a present CRD earlier in the list
// does not mask a missing one later, and vice versa.
func TestCheckCRDs_StopsAtTheFirstMissingEntry(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newPreflightScheme(t)).
		WithObjects(crdNamed("clusterrepos.catalog.cattle.io")).
		Build()
	m := NewManager(c)

	err := m.CheckCRDs(context.Background(), []string{
		"clusterrepos.catalog.cattle.io",
		"uiplugins.catalog.cattle.io",
	})

	var notReady *DependencyNotReadyError
	if !stderrors.As(err, &notReady) {
		t.Fatalf("CheckCRDs error = %v (%T), want a *DependencyNotReadyError", err, err)
	}
	if notReady.Dependency != "uiplugins.catalog.cattle.io" {
		t.Errorf("Dependency = %q, want the missing one (%q), not the present one",
			notReady.Dependency, "uiplugins.catalog.cattle.io")
	}
}
