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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	crconfig "sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// newWiringManager builds a manager for the specs below to interrogate. It is
// never started; it exists only to be asked what it would hand a reconciler.
//
// SkipNameValidation because controller-runtime rejects a second controller
// registered under a name already taken, and the whole point here is to run
// SetupWithManager more than once.
func newWiringManager() ctrl.Manager {
	GinkgoHelper()

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		// Both servers disabled so building a manager per spec binds no ports.
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
		Controller:             crconfig.Controller{SkipNameValidation: ptr.To(true)},
	})
	Expect(err).NotTo(HaveOccurred())
	return mgr
}

// The readiness check must not read the manager's cache — the reasoning is in
// TestDeploymentReadinessDoesNotReadTheCache, and the check itself is covered
// there by handing the reconciler two readers that disagree.
//
// What that cannot cover is the wiring. Every unit test in this package builds
// its reconciler by hand and sets APIReader itself, so SetupWithManager is never
// executed: swap GetAPIReader for GetClient here and the whole suite stays green
// while production silently goes back to reading the cache, where the stale
// entry is a self-consistent picture of the *previous* rollout completing.
//
// A Ginkgo spec rather than a plain test, because it needs the envtest config
// the suite's BeforeSuite brings up, and a real manager is the only thing that
// can be asked which reader it would hand over.
var _ = Describe("SetupWithManager", func() {
	It("takes the readiness reader from the API server, not the cache", func() {
		mgr := newWiringManager()

		r := &InstallAIExtensionReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		}
		Expect(r.SetupWithManager(mgr)).To(Succeed())

		Expect(r.APIReader).To(BeIdenticalTo(mgr.GetAPIReader()),
			"the readiness check reads through the manager's cache, which can serve an "+
				"entry from before the apply this pass just made")
		Expect(r.APIReader).NotTo(BeIdenticalTo(mgr.GetClient()),
			"the API reader and the cached client are the same object, so reading through "+
				"the former buys nothing")
	})

	// The field is a seam for tests, so SetupWithManager must not stamp over a
	// value the caller already chose. Without this, hardwiring the assignment
	// would satisfy the spec above.
	It("leaves an explicitly supplied reader alone", func() {
		mgr := newWiringManager()

		r := &InstallAIExtensionReconciler{
			Client:    mgr.GetClient(),
			Scheme:    mgr.GetScheme(),
			APIReader: k8sClient,
		}
		Expect(r.SetupWithManager(mgr)).To(Succeed())

		Expect(r.APIReader).To(BeIdenticalTo(k8sClient))
	})

	// DefaultReadinessTimeout is the other thing SetupWithManager fills in, and a
	// zero one turns every readiness wait into an instant timeout.
	It("defaults the readiness timeout rather than leaving it zero", func() {
		mgr := newWiringManager()

		r := &InstallAIExtensionReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}
		Expect(r.SetupWithManager(mgr)).To(Succeed())

		Expect(r.ReadinessTimeout).To(Equal(DefaultReadinessTimeout))
	})
})
