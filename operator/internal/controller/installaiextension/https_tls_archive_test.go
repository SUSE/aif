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

// The https+tls download path bypasses repo-index resolution and does not honor
// Version, so CEL requires an https:// chartURL used with tls to point directly at
// a .tgz archive. oci:// is unaffected.
var _ = Describe("HelmSource https+tls archive CEL validation", func() {
	ca := &aiplatformv1alpha1.SecretKeyRef{Name: "ca", Key: "ca.crt"}

	build := func(chartURL string, tls *aiplatformv1alpha1.HelmTLS) *aiplatformv1alpha1.InstallAIExtension {
		return &aiplatformv1alpha1.InstallAIExtension{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "https-tls-cel-"},
			Spec: aiplatformv1alpha1.InstallAIExtensionSpec{
				Source: aiplatformv1alpha1.ExtensionSource{
					Kind: aiplatformv1alpha1.ExtensionSourceKindHelm,
					Helm: &aiplatformv1alpha1.HelmSource{
						ChartURL: chartURL,
						Version:  "1.0.0",
						TLS:      tls,
					},
				},
				Extension: aiplatformv1alpha1.ExtensionConfig{Name: "aif-ui", Version: "1.0.0"},
			},
		}
	}

	It("accepts https + tls when the chartURL is a direct .tgz archive", func() {
		o := build("https://charts.example.com/aif-ui-1.0.0.tgz", &aiplatformv1alpha1.HelmTLS{CASecretRef: ca})
		Expect(k8sClient.Create(ctx, o)).To(Succeed())
		Expect(k8sClient.Delete(ctx, o)).To(Succeed())
	})

	It("rejects https + tls when the chartURL is a repository URL (no .tgz)", func() {
		err := k8sClient.Create(ctx, build("https://charts.example.com/repo", &aiplatformv1alpha1.HelmTLS{CASecretRef: ca}))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("must point directly at a chart archive ending in .tgz"))
	})

	It("accepts https without tls at a repository URL (rule does not apply)", func() {
		o := build("https://charts.example.com/repo", nil)
		Expect(k8sClient.Create(ctx, o)).To(Succeed())
		Expect(k8sClient.Delete(ctx, o)).To(Succeed())
	})

	It("accepts oci + tls at a non-.tgz URL (rule is https-only)", func() {
		o := build("oci://registry.example.com/charts/aif-ui-server", &aiplatformv1alpha1.HelmTLS{CASecretRef: ca})
		Expect(k8sClient.Create(ctx, o)).To(Succeed())
		Expect(k8sClient.Delete(ctx, o)).To(Succeed())
	})
})
