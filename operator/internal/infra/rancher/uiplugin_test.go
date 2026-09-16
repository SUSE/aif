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
	"errors"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func newUIPluginTestScheme(t *testing.T) *kruntime.Scheme {
	t.Helper()
	scheme := kruntime.NewScheme()
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "catalog.cattle.io", Version: "v1", Kind: "UIPlugin",
	}, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "catalog.cattle.io", Version: "v1", Kind: "UIPluginList",
	}, &unstructured.UnstructuredList{})
	return scheme
}

// Same failure mode as ClusterRepo, on the namespaced object: a Delete against
// a UIPlugin whose CRD is gone cannot resolve a REST mapping and comes back a
// meta.NoKindMatchError rather than NotFound. This is the branch that used to
// leave the finalizer permanently unable to clear once Rancher's CRDs (or the
// namespace they live in) were gone.
func TestDeleteUIPlugin_ToleratesMissingCRD(t *testing.T) {
	scheme := newUIPluginTestScheme(t)
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithInterceptorFuncs(interceptor.Funcs{
			Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
				return &meta.NoKindMatchError{
					GroupKind:        schema.GroupKind{Group: "catalog.cattle.io", Kind: "UIPlugin"},
					SearchedVersions: []string{"v1"},
				}
			},
		}).
		Build()
	m := NewManager(c)

	if err := m.DeleteUIPlugin(context.Background(), "my-plugin", "cattle-ui-plugin-system"); err != nil {
		t.Fatalf("DeleteUIPlugin returned %v, want nil; the CRD being gone means there is "+
			"nothing left to delete", err)
	}
}

func TestDeleteUIPlugin_SurfacesOtherErrors(t *testing.T) {
	scheme := newUIPluginTestScheme(t)
	wantErr := "admission webhook denied the request"
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithInterceptorFuncs(interceptor.Funcs{
			Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
				return errors.New(wantErr)
			},
		}).
		Build()
	m := NewManager(c)

	err := m.DeleteUIPlugin(context.Background(), "my-plugin", "cattle-ui-plugin-system")
	if err == nil || err.Error() != wantErr {
		t.Fatalf("DeleteUIPlugin error = %v, want %q", err, wantErr)
	}
}
