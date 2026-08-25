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
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/flowcontrol"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// simulateUISave reproduces saveOperatorConfig's (ui/pkg/aif-ui/utils/operator-config.ts)
// GET-then-create-or-update pattern against the real API server, so a concurrent
// test can race it against the operator's own writer without a browser.
func simulateUISave(ctx context.Context, c client.Client, namespace string) error {
	var cm corev1.ConfigMap
	err := c.Get(ctx, client.ObjectKey{Name: uiConfigMapName, Namespace: namespace}, &cm)
	if apierrors.IsNotFound(err) {
		return c.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      uiConfigMapName,
				Namespace: namespace,
				Labels:    map[string]string{"app.kubernetes.io/managed-by": "Helm"},
				Annotations: map[string]string{
					"meta.helm.sh/release-name":      "aif-ui-server",
					"meta.helm.sh/release-namespace": namespace,
				},
			},
			Data: map[string]string{"operatorNamespace": "aif-operator", "operatorService": "aif-operator"},
		})
	}
	if err != nil {
		return err
	}
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}
	cm.Data["operatorNamespace"] = "aif-operator"
	cm.Data["operatorService"] = "aif-operator"
	return c.Update(ctx, &cm)
}

// A ConfigMap syncUIConfigMap creates or self-heals without Helm's ownership
// stamps permanently blocks a later `helm install`/`upgrade` of the
// aif-ui-server release with "invalid ownership metadata" (SUSEAI-1039): Helm
// refuses to adopt a pre-existing object it does not already own. These tests
// pin the fix — a Helm-sourced sync adopts the object, a git-sourced one
// leaves it alone, since there is no release to adopt into.
var _ = Describe("UI ConfigMap Helm ownership", func() {
	const namespace = "ui-configmap-ownership-test"

	var (
		ctx context.Context
		r   *InstallAIExtensionReconciler
	)

	BeforeEach(func() {
		ctx = context.Background()
		r = &InstallAIExtensionReconciler{Client: k8sClient, ExtensionNamespace: namespace}

		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
		Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, ns))).To(Succeed())
	})

	AfterEach(func() {
		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: uiConfigMapName, Namespace: namespace}}
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, cm))).To(Succeed())
	})

	It("adopts a pre-existing unowned ConfigMap for a Helm-sourced extension", func() {
		unowned := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: uiConfigMapName, Namespace: namespace},
		}
		Expect(k8sClient.Create(ctx, unowned)).To(Succeed())

		ext := &v1alpha1.InstallAIExtension{
			Status: v1alpha1.InstallAIExtensionStatus{
				ActiveSourceKind: v1alpha1.ExtensionSourceKindHelm,
				HelmReleaseName:  "aif-ui-server",
			},
		}
		Expect(r.syncUIConfigMap(ctx, ext)).To(Succeed())

		var stored corev1.ConfigMap
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: uiConfigMapName, Namespace: namespace}, &stored)).To(Succeed())
		Expect(stored.Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "Helm"))
		Expect(stored.Annotations).To(HaveKeyWithValue("meta.helm.sh/release-name", "aif-ui-server"))
		Expect(stored.Annotations).To(HaveKeyWithValue("meta.helm.sh/release-namespace", namespace))
	})

	It("leaves a git-sourced extension's ConfigMap unowned", func() {
		ext := &v1alpha1.InstallAIExtension{
			Status: v1alpha1.InstallAIExtensionStatus{
				ActiveSourceKind: v1alpha1.ExtensionSourceKindGit,
			},
		}
		Expect(r.syncUIConfigMap(ctx, ext)).To(Succeed())

		var stored corev1.ConfigMap
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: uiConfigMapName, Namespace: namespace}, &stored)).To(Succeed())
		Expect(stored.Labels).NotTo(HaveKey("app.kubernetes.io/managed-by"))
		Expect(stored.Annotations).NotTo(HaveKey("meta.helm.sh/release-name"))
	})

	// This is the case syncUIConfigMap alone cannot fix: an unowned ConfigMap left
	// behind before any Helm release has ever succeeded (a leftover from a prior
	// git-sourced period, or a self-heal after an uninstall) blocks the very next
	// `helm install` too, not just recreations after one has already succeeded.
	It("pre-adopts a leftover unowned ConfigMap before the first Helm install ever runs", func() {
		unowned := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: uiConfigMapName, Namespace: namespace},
		}
		Expect(k8sClient.Create(ctx, unowned)).To(Succeed())

		r.adoptUIConfigMap(ctx, "aif-ui-server")

		var stored corev1.ConfigMap
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: uiConfigMapName, Namespace: namespace}, &stored)).To(Succeed())
		Expect(stored.Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "Helm"))
		Expect(stored.Annotations).To(HaveKeyWithValue("meta.helm.sh/release-name", "aif-ui-server"))
		Expect(stored.Annotations).To(HaveKeyWithValue("meta.helm.sh/release-namespace", namespace))
	})

	It("pre-adopts even when the ConfigMap does not exist yet, without touching Data", func() {
		r.adoptUIConfigMap(ctx, "aif-ui-server")

		var stored corev1.ConfigMap
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: uiConfigMapName, Namespace: namespace}, &stored)).To(Succeed())
		Expect(stored.Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "Helm"))
		Expect(stored.Data).To(BeEmpty())
	})

	// The UI's Settings-save (ui/pkg/aif-ui/utils/operator-config.ts) and the
	// operator's pre-install adoption both do their own GET-then-create-or-update
	// against the same object with no coordination between them. Kubernetes'
	// optimistic concurrency (resourceVersion) should turn every genuine collision
	// into a Conflict/AlreadyExists that the loser can retry — never silent data
	// loss (e.g. one writer's Helm ownership stamps or the other's Data clobbered).
	It("keeps the ConfigMap consistent under concurrent UI-save and operator-adopt writes", func() {
		raceCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		// Two goroutines hammering with no pacing exhaust the default client-side
		// QPS/burst well before raceCtx's deadline, and the rate limiter then
		// rejects proactively whenever it can predict a wait would outlast the
		// remaining context — a client-side artifact of this test's own tight loop,
		// unrelated to the actual writers' correctness. A dedicated client with
		// client-side throttling disabled keeps that from dominating the run
		// instead of the real race (a raised QPS/burst still throttled under this
		// loop's real request rate against envtest's local API server).
		raceCfg := rest.CopyConfig(cfg)
		raceCfg.RateLimiter = flowcontrol.NewFakeAlwaysRateLimiter()
		raceClient, err := client.New(raceCfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())
		raceReconciler := &InstallAIExtensionReconciler{Client: raceClient, ExtensionNamespace: namespace}

		var (
			wg         sync.WaitGroup
			mu         sync.Mutex
			unexpected []error
		)
		recordUnexpected := func(err error) {
			// Conflict/AlreadyExists are the genuine race outcomes this test checks
			// for. Once raceCtx has expired, an in-flight request can still return a
			// context-cancellation error a moment later — wind-down noise from the
			// test's own fixed window, not a correctness signal about the writers.
			if err == nil || apierrors.IsConflict(err) || apierrors.IsAlreadyExists(err) || raceCtx.Err() != nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			unexpected = append(unexpected, err)
		}

		wg.Add(2)
		go func() {
			defer wg.Done()
			for raceCtx.Err() == nil {
				raceReconciler.adoptUIConfigMap(raceCtx, "aif-ui-server")
			}
		}()
		go func() {
			defer wg.Done()
			for raceCtx.Err() == nil {
				recordUnexpected(simulateUISave(raceCtx, raceClient, namespace))
			}
		}()
		wg.Wait()

		Expect(unexpected).To(BeEmpty(), "race produced a non-conflict error: %v", unexpected)

		var stored corev1.ConfigMap
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: uiConfigMapName, Namespace: namespace}, &stored)).To(Succeed())
		Expect(stored.Data).To(HaveKeyWithValue("operatorNamespace", "aif-operator"))
		Expect(stored.Data).To(HaveKeyWithValue("operatorService", "aif-operator"))
		Expect(stored.Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "Helm"))
		Expect(stored.Annotations).To(HaveKeyWithValue("meta.helm.sh/release-name", "aif-ui-server"))
	})
})
