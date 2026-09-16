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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// Everything else in this package tests the watch mechanism's pieces in
// isolation: the predicate correctly identifies relevant events, the mapper
// correctly enqueues every InstallAIExtension, checkPreconditions correctly
// passes once the namespace/CRDs exist. None of that proves the pieces are
// actually wired together through a live manager and cache — which is exactly
// the class of gap that let the CustomResourceDefinition scheme-registration
// bug through undetected: SetupWithManager returning Succeed() proves nothing
// about what Start does. These specs run a real, started manager against
// envtest and assert on wall-clock behavior: recovery inside a few seconds,
// not "eventually" — a tight Eventually window is itself the assertion that
// the watch fired rather than the CR waiting out its own backoff (floor
// healthCheckInterval, 60s).
//
// A chart URL pointed at a closed local port is used throughout so a pass
// proves the precondition gate cleared without ever making these specs
// depend on a real Helm registry: the scheme still has to be oci:// or
// https:// (the CRD's own OpenAPI validation rejects anything else before it
// reaches the reconciler at all), but 127.0.0.1 on a port nothing listens on
// fails instantly with connection-refused — no DNS lookup, no timeout to
// wait out. The resulting InstallFailed is a different Ready reason than
// either precondition being tested, so seeing the reason move to it is
// exactly the signal that reconcile got past checkPreconditions.

func extensionWithBogusChart(name string) *v1alpha1.InstallAIExtension {
	return &v1alpha1.InstallAIExtension{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha1.InstallAIExtensionSpec{
			Extension: v1alpha1.ExtensionConfig{Name: name, Version: "1.0.0"},
			Source: v1alpha1.ExtensionSource{
				Kind: v1alpha1.ExtensionSourceKindHelm,
				Helm: &v1alpha1.HelmSource{ChartURL: "oci://127.0.0.1:1/does-not-exist", Version: "1.0.0"},
			},
		},
	}
}

// minimalCRD is the smallest schema envtest's real API server accepts and
// establishes on its own — no controller needed, unlike the
// namespace-terminating case elsewhere in this package. CheckCRDs only cares
// that the object exists, so no fields beyond that are needed.
func minimalCRD(name, kind, plural string, namespaced bool) *apiextensionsv1.CustomResourceDefinition {
	scope := apiextensionsv1.ClusterScoped
	if namespaced {
		scope = apiextensionsv1.NamespaceScoped
	}
	preserveUnknown := true
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "catalog.cattle.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural: plural,
				Kind:   kind,
			},
			Scope: scope,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type:                   "object",
							XPreserveUnknownFields: &preserveUnknown,
						},
					},
				},
			},
		},
	}
}

func createCatalogCRDs(ctx context.Context) {
	GinkgoHelper()
	Expect(k8sClient.Create(ctx, minimalCRD("uiplugins.catalog.cattle.io", "UIPlugin", "uiplugins", true))).To(Succeed())
	Expect(k8sClient.Create(ctx, minimalCRD("clusterrepos.catalog.cattle.io", "ClusterRepo", "clusterrepos", false))).To(Succeed())
}

func deleteCatalogCRDs(ctx context.Context) {
	GinkgoHelper()
	_ = client.IgnoreNotFound(k8sClient.Delete(ctx, minimalCRD("uiplugins.catalog.cattle.io", "UIPlugin", "uiplugins", true)))
	_ = client.IgnoreNotFound(k8sClient.Delete(ctx, minimalCRD("clusterrepos.catalog.cattle.io", "ClusterRepo", "clusterrepos", false)))
}

func readyReason(ctx context.Context, name string) func() string {
	return func() string {
		var ext v1alpha1.InstallAIExtension
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: name}, &ext); err != nil {
			return ""
		}
		ready := meta.FindStatusCondition(ext.Status.Conditions, conditionTypeReady)
		if ready == nil {
			return ""
		}
		return ready.Reason
	}
}

var _ = Describe("watch-driven recovery, end to end", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
	)

	BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.Background())
		DeferCleanup(cancel)
	})

	// The scenario item 2 of the SUSEAI-803 investigation exists for: this
	// namespace is Rancher's to create, not the operator's, and the operator
	// must notice the moment it appears rather than waiting out its own
	// backoff. Uses a manager that is actually started, unlike every other
	// SetupWithManager spec in this package (see newWiringManager's doc
	// comment) — Succeed() there only proves the watch was declared, not that
	// a live cache resolves and fires it.
	It("wakes up within seconds of the extension namespace being created", func() {
		const name = "watch-recovery-namespace-probe"
		const namespace = "watch-recovery-namespace"

		createCatalogCRDs(ctx)
		DeferCleanup(deleteCatalogCRDs, ctx)

		mgr := newWiringManager()
		r := &InstallAIExtensionReconciler{
			Client:             mgr.GetClient(),
			Scheme:             mgr.GetScheme(),
			ExtensionNamespace: namespace,
		}
		Expect(r.SetupWithManager(mgr)).To(Succeed())

		go func() {
			defer GinkgoRecover()
			Expect(mgr.Start(ctx)).To(Succeed())
		}()

		ext := extensionWithBogusChart(name)
		Expect(k8sClient.Create(ctx, ext)).To(Succeed())
		DeferCleanup(func() {
			_ = client.IgnoreNotFound(k8sClient.Delete(context.Background(), ext))
		})

		By("blocking on the namespace, which does not exist yet")
		Eventually(readyReason(ctx, name)).Should(Equal("ExtensionNamespaceMissing"))

		By("creating the namespace Rancher would have created")
		Expect(k8sClient.Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: namespace},
		})).To(Succeed())
		DeferCleanup(func() {
			_ = client.IgnoreNotFound(k8sClient.Delete(context.Background(),
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}))
		})

		By("recovering within a few seconds, not the 60s+ backoff floor")
		Eventually(readyReason(ctx, name), "5s", "100ms").Should(Equal("InstallFailed"),
			"want the reason to have moved off ExtensionNamespaceMissing within a tight "+
				"window; a pass here that only succeeded because of a long Eventually timeout "+
				"would prove the backoff eventually caught up, not that the watch fired")
	})

	// The Rancher-CRD-appearing counterpart to the namespace spec above —
	// same reasoning, the other half of checkPreconditions.
	It("wakes up within seconds of the catalog CRDs being registered", func() {
		const name = "watch-recovery-crd-probe"
		const namespace = "watch-recovery-crd-namespace"

		Expect(k8sClient.Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: namespace},
		})).To(Succeed())
		DeferCleanup(func() {
			_ = client.IgnoreNotFound(k8sClient.Delete(context.Background(),
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}))
		})

		mgr := newWiringManager()
		r := &InstallAIExtensionReconciler{
			Client:             mgr.GetClient(),
			Scheme:             mgr.GetScheme(),
			ExtensionNamespace: namespace,
		}
		Expect(r.SetupWithManager(mgr)).To(Succeed())

		go func() {
			defer GinkgoRecover()
			Expect(mgr.Start(ctx)).To(Succeed())
		}()

		ext := extensionWithBogusChart(name)
		Expect(k8sClient.Create(ctx, ext)).To(Succeed())
		DeferCleanup(func() {
			_ = client.IgnoreNotFound(k8sClient.Delete(context.Background(), ext))
		})

		By("blocking on Rancher's catalog CRDs, which are not registered yet")
		Eventually(readyReason(ctx, name)).Should(Equal("RancherUnavailable"))

		By("registering the CRDs Rancher would have installed")
		createCatalogCRDs(ctx)
		DeferCleanup(deleteCatalogCRDs, ctx)

		By("recovering within a few seconds, not the 60s+ backoff floor")
		Eventually(readyReason(ctx, name), "5s", "100ms").Should(Equal("InstallFailed"),
			"want the reason to have moved off RancherUnavailable within a tight window; a "+
				"pass here that only succeeded because of a long Eventually timeout would "+
				"prove the backoff eventually caught up, not that the watch fired")
	})
})
