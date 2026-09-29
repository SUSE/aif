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

package aiworkload_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	crconfig "sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/SUSE/aif-operator/internal/controller/aiworkload"
)

// newWiringManager builds a manager for the specs below to interrogate. It is
// never started; it exists only to be asked what it would hand a reconciler.
//
// SkipNameValidation because controller-runtime rejects a second controller
// registered under a name already taken, and the specs run SetupWithManager more
// than once.
func newWiringManager() ctrl.Manager {
	GinkgoHelper()

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 scheme.Scheme,
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
		Controller:             crconfig.Controller{SkipNameValidation: ptr.To(true)},
	})
	Expect(err).NotTo(HaveOccurred())
	return mgr
}

// The App/Helm readiness scan must read release-owned controllers through the
// uncached APIReader, never the manager's cache — caching those types would spin
// up cluster-wide informers (and need `watch` RBAC) for every StatefulSet and
// DaemonSet in the cluster just to poll a handful. controllerReader() falls back
// to the cached Client, so the only thing keeping production off the cache is
// SetupWithManager wiring APIReader. Every unit test sets APIReader by hand, so
// without this spec that wiring could regress and the whole suite stay green.
var _ = Describe("AIWorkload SetupWithManager", func() {
	It("takes the controller reader from the API server, not the cache", func() {
		mgr := newWiringManager()

		r := &aiworkload.AIWorkloadReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		}
		Expect(r.SetupWithManager(mgr)).To(Succeed())

		Expect(r.APIReader).To(BeIdenticalTo(mgr.GetAPIReader()),
			"the readiness scan would read through the manager's cache, starting the "+
				"cluster-wide informers reading via APIReader exists to avoid")
		Expect(r.APIReader).NotTo(BeIdenticalTo(mgr.GetClient()),
			"the API reader and the cached client are the same object, so reading through "+
				"the former buys nothing")
	})

	// The field is a seam for tests, so SetupWithManager must not stamp over a
	// value the caller already chose. Without this, hardwiring the assignment
	// would satisfy the spec above.
	It("leaves an explicitly supplied reader alone", func() {
		mgr := newWiringManager()

		r := &aiworkload.AIWorkloadReconciler{
			Client:    mgr.GetClient(),
			Scheme:    mgr.GetScheme(),
			APIReader: k8sClient,
		}
		Expect(r.SetupWithManager(mgr)).To(Succeed())

		Expect(r.APIReader).To(BeIdenticalTo(k8sClient))
	})
})
