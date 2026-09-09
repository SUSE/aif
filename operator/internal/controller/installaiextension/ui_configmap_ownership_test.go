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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// syncUIConfigMap deliberately does not touch Helm ownership metadata (see its
// doc comment) — adoption is now Helm's own job via TakeOwnership on the
// operator's install/upgrade actions (SUSEAI-1039), not something this
// self-heal hand-rolls. These tests pin that it only ever touches Data,
// regardless of what labels/annotations the object already carries.
var _ = Describe("UI ConfigMap sync", func() {
	const namespace = "ui-configmap-sync-test"

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

	It("creates the ConfigMap with operator coordinates when it does not exist", func() {
		Expect(r.syncUIConfigMap(ctx)).To(Succeed())

		var stored corev1.ConfigMap
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: uiConfigMapName, Namespace: namespace}, &stored)).To(Succeed())
		Expect(stored.Data).To(HaveKey("operatorNamespace"))
		Expect(stored.Data).To(HaveKey("operatorService"))
	})

	It("self-heals Data without touching pre-existing labels or annotations", func() {
		// Stands in for a Helm-owned object: what actually matters here is that
		// syncUIConfigMap leaves *whatever* labels/annotations are already there
		// alone, not that they specifically say "Helm" — ownership is no longer
		// this function's concern at all.
		owned := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:        uiConfigMapName,
				Namespace:   namespace,
				Labels:      map[string]string{"app.kubernetes.io/managed-by": "Helm"},
				Annotations: map[string]string{"meta.helm.sh/release-name": "aif-ui-server"},
			},
		}
		Expect(k8sClient.Create(ctx, owned)).To(Succeed())

		Expect(r.syncUIConfigMap(ctx)).To(Succeed())

		var stored corev1.ConfigMap
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: uiConfigMapName, Namespace: namespace}, &stored)).To(Succeed())
		Expect(stored.Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "Helm"))
		Expect(stored.Annotations).To(HaveKeyWithValue("meta.helm.sh/release-name", "aif-ui-server"))
		Expect(stored.Data).To(HaveKey("operatorNamespace"))
	})

	It("recreates an unowned ConfigMap without adding ownership metadata", func() {
		unowned := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: uiConfigMapName, Namespace: namespace}}
		Expect(k8sClient.Create(ctx, unowned)).To(Succeed())

		Expect(r.syncUIConfigMap(ctx)).To(Succeed())

		var stored corev1.ConfigMap
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: uiConfigMapName, Namespace: namespace}, &stored)).To(Succeed())
		Expect(stored.Labels).NotTo(HaveKey("app.kubernetes.io/managed-by"))
		Expect(stored.Annotations).NotTo(HaveKey("meta.helm.sh/release-name"))
	})
})
