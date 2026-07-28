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

var _ = Describe("Registry host allowlist gate", func() {
	// extWithBasic builds a Helm-source CR pointing at chartURL with basic auth that
	// references a Secret which does not exist. Whether the allowlist gate fires or
	// not is thus distinguishable by the failure reason: RegistryHostNotAllowed means
	// the gate rejected the host *before* the Secret was read; AuthResolutionFailed
	// means the gate passed and resolution proceeded to (and failed at) the Secret.
	extWithBasic := func(chartURL string) *aiplatformv1alpha1.InstallAIExtension {
		return &aiplatformv1alpha1.InstallAIExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "host-allowlist-test"},
			Spec: aiplatformv1alpha1.InstallAIExtensionSpec{
				Source: aiplatformv1alpha1.ExtensionSource{
					Kind: aiplatformv1alpha1.ExtensionSourceKindHelm,
					Helm: &aiplatformv1alpha1.HelmSource{
						ChartURL: chartURL,
						Version:  "1.0.0",
						Auth: &aiplatformv1alpha1.HelmAuth{
							Basic: &aiplatformv1alpha1.BasicAuth{
								UserSecretRef:  aiplatformv1alpha1.SecretKeyRef{Name: "does-not-exist", Key: "user"},
								TokenSecretRef: aiplatformv1alpha1.SecretKeyRef{Name: "does-not-exist", Key: "token"},
							},
						},
					},
				},
				Extension: aiplatformv1alpha1.ExtensionConfig{Name: "aif-ui", Version: "1.0.0"},
			},
		}
	}

	readyReason := func(ext *aiplatformv1alpha1.InstallAIExtension) string {
		cond := meta.FindStatusCondition(ext.Status.Conditions, conditionTypeReady)
		Expect(cond).NotTo(BeNil())
		return cond.Reason
	}

	It("refuses a chart host not on the allowlist, before reading the secret", func() {
		// The confused-deputy scenario: an attacker-chosen host with a privileged
		// secret name. The gate must reject before the operator reads/sends the secret.
		r := &InstallAIExtensionReconciler{
			Client: k8sClient, Scheme: k8sClient.Scheme(),
			AllowedRegistryHosts: []string{"harbor.example.com"},
		}
		ext := extWithBasic("oci://attacker.example.com/x")

		res, err := r.reconcileHelmSource(ctx, ext, "default")
		Expect(err).NotTo(HaveOccurred())
		Expect(res.IsZero()).To(BeTrue())
		Expect(ext.Status.Phase).To(Equal(aiplatformv1alpha1.InstallAIExtensionPhaseFailed))
		Expect(readyReason(ext)).To(Equal("RegistryHostNotAllowed"))
	})

	It("allows a chart host on the allowlist (gate passes, resolution proceeds)", func() {
		r := &InstallAIExtensionReconciler{
			Client: k8sClient, Scheme: k8sClient.Scheme(),
			AllowedRegistryHosts: []string{"registry.example.com"},
		}
		ext := extWithBasic("oci://registry.example.com/charts/aif-ui-server")

		_, err := r.reconcileHelmSource(ctx, ext, "default")
		Expect(err).NotTo(HaveOccurred())
		// Gate passed → failure is at auth resolution (missing secret), not the host gate.
		Expect(readyReason(ext)).To(Equal("AuthResolutionFailed"))
	})

	It("allows any host when the allowlist is empty", func() {
		r := &InstallAIExtensionReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		ext := extWithBasic("oci://anything.example.com/x")

		_, err := r.reconcileHelmSource(ctx, ext, "default")
		Expect(err).NotTo(HaveOccurred())
		Expect(readyReason(ext)).To(Equal("AuthResolutionFailed"))
	})
})
