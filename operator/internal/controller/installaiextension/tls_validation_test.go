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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

var _ = Describe("HelmSource.tls CEL validation", func() {
	base := func(t *aiplatformv1alpha1.HelmTLS) *aiplatformv1alpha1.InstallAIExtension {
		return &aiplatformv1alpha1.InstallAIExtension{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "tls-cel-"},
			Spec: aiplatformv1alpha1.InstallAIExtensionSpec{
				Source: aiplatformv1alpha1.ExtensionSource{
					Kind: aiplatformv1alpha1.ExtensionSourceKindHelm,
					Helm: &aiplatformv1alpha1.HelmSource{
						ChartURL: "oci://registry.example.com/charts/aif-ui-server",
						Version:  "1.0.0",
						TLS:      t,
					},
				},
				Extension: aiplatformv1alpha1.ExtensionConfig{Name: "aif-ui", Version: "1.0.0"},
			},
		}
	}
	ca := &aiplatformv1alpha1.SecretKeyRef{Name: "ca", Key: "ca.crt"}

	It("accepts omitted tls", func() {
		o := base(nil)
		Expect(k8sClient.Create(ctx, o)).To(Succeed())
		Expect(k8sClient.Delete(ctx, o)).To(Succeed())
	})
	It("accepts caSecretRef only", func() {
		o := base(&aiplatformv1alpha1.HelmTLS{CASecretRef: ca})
		Expect(k8sClient.Create(ctx, o)).To(Succeed())
		Expect(k8sClient.Delete(ctx, o)).To(Succeed())
	})
	It("accepts insecureSkipVerify with acknowledgeInsecure", func() {
		o := base(&aiplatformv1alpha1.HelmTLS{InsecureSkipVerify: true, AcknowledgeInsecure: true})
		Expect(k8sClient.Create(ctx, o)).To(Succeed())
		Expect(k8sClient.Delete(ctx, o)).To(Succeed())
	})
	It("rejects insecureSkipVerify without acknowledgeInsecure", func() {
		err := k8sClient.Create(ctx, base(&aiplatformv1alpha1.HelmTLS{InsecureSkipVerify: true}))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("insecureSkipVerify requires acknowledgeInsecure"))
	})
	It("ignores acknowledgeInsecure on its own (no insecureSkipVerify)", func() {
		// acknowledgeInsecure alone does not satisfy the "at least one" rule.
		err := k8sClient.Create(ctx, base(&aiplatformv1alpha1.HelmTLS{AcknowledgeInsecure: true}))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("at least one of caSecretRef"))
	})
	It("accepts clientTLSSecretRef only", func() {
		o := base(&aiplatformv1alpha1.HelmTLS{
			ClientTLSSecretRef: &aiplatformv1alpha1.LocalSecretRef{Name: "mtls"},
		})
		Expect(k8sClient.Create(ctx, o)).To(Succeed())
		Expect(k8sClient.Delete(ctx, o)).To(Succeed())
	})
	It("rejects an empty tls block", func() {
		err := k8sClient.Create(ctx, base(&aiplatformv1alpha1.HelmTLS{}))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("at least one of caSecretRef"))
	})
	It("rejects insecureSkipVerify + caSecretRef together", func() {
		err := k8sClient.Create(ctx, base(&aiplatformv1alpha1.HelmTLS{InsecureSkipVerify: true, AcknowledgeInsecure: true, CASecretRef: ca}))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("caSecretRef must not be set when insecureSkipVerify is true"))
	})
})
