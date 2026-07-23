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

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

var _ = Describe("Operator-level insecure TLS gate", func() {
	// extWith builds a Helm-source CR with insecureSkipVerify (+ acknowledgeInsecure,
	// so it would pass admission) pointing at chartURL.
	extWith := func(chartURL string) *aiplatformv1alpha1.InstallAIExtension {
		return &aiplatformv1alpha1.InstallAIExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "gate-test"},
			Spec: aiplatformv1alpha1.InstallAIExtensionSpec{
				Source: aiplatformv1alpha1.ExtensionSource{
					Kind: aiplatformv1alpha1.ExtensionSourceKindHelm,
					Helm: &aiplatformv1alpha1.HelmSource{
						ChartURL: chartURL,
						Version:  "1.0.0",
						TLS: &aiplatformv1alpha1.HelmTLS{
							InsecureSkipVerify:  true,
							AcknowledgeInsecure: true,
						},
					},
				},
				Extension: aiplatformv1alpha1.ExtensionConfig{Name: "aif-ui", Version: "1.0.0"},
			},
		}
	}

	It("rejects insecureSkipVerify when the operator gate is off", func() {
		r := &InstallAIExtensionReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), AllowInsecureRegistryTLS: false}
		ext := extWith("oci://registry.example.com/charts/aif-ui-server")

		res, err := r.reconcileHelmSource(ctx, ext, "default")
		Expect(err).NotTo(HaveOccurred())
		Expect(res.IsZero()).To(BeTrue())
		Expect(ext.Status.Phase).To(Equal(aiplatformv1alpha1.InstallAIExtensionPhaseFailed))
		cond := meta.FindStatusCondition(ext.Status.Conditions, conditionTypeReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Reason).To(Equal("InsecureTLSNotAllowed"))
	})

	It("passes the gate when the operator flag is on", func() {
		// Flag on: the gate must not fire. We steer to an unsupported chart URL scheme
		// so reconcile fails at the URL check (which runs *after* the gate), proving the
		// gate was bypassed — without attempting a real chart pull.
		r := &InstallAIExtensionReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), AllowInsecureRegistryTLS: true}
		ext := extWith("http://registry.example.com/charts/aif-ui-server")

		res, err := r.reconcileHelmSource(ctx, ext, "default")
		Expect(err).NotTo(HaveOccurred())
		Expect(res.IsZero()).To(BeTrue())
		Expect(ext.Status.Phase).To(Equal(aiplatformv1alpha1.InstallAIExtensionPhaseFailed))
		cond := meta.FindStatusCondition(ext.Status.Conditions, conditionTypeReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Reason).To(Equal("InvalidSpec"))
	})
})
