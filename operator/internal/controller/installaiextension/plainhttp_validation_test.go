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

var _ = Describe("HelmSource.plainHTTP CEL validation", func() {
	mk := func(chartURL string, plainHTTP bool, tls *aiplatformv1alpha1.HelmTLS) *aiplatformv1alpha1.InstallAIExtension {
		return &aiplatformv1alpha1.InstallAIExtension{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "ph-cel-"},
			Spec: aiplatformv1alpha1.InstallAIExtensionSpec{
				Source: aiplatformv1alpha1.ExtensionSource{
					Kind: aiplatformv1alpha1.ExtensionSourceKindHelm,
					Helm: &aiplatformv1alpha1.HelmSource{
						ChartURL:  chartURL,
						Version:   "1.0.0",
						PlainHTTP: plainHTTP,
						TLS:       tls,
					},
				},
				Extension: aiplatformv1alpha1.ExtensionConfig{Name: "aif-ui", Version: "1.0.0"},
			},
		}
	}

	It("accepts plainHTTP with an oci URL", func() {
		o := mk("oci://reg.local:8088/aif/chart/aif-ui", true, nil)
		Expect(k8sClient.Create(ctx, o)).To(Succeed())
		Expect(k8sClient.Delete(ctx, o)).To(Succeed())
	})
	It("accepts plainHTTP omitted with an https URL", func() {
		o := mk("https://charts.example.com/aif-ui.tgz", false, nil)
		Expect(k8sClient.Create(ctx, o)).To(Succeed())
		Expect(k8sClient.Delete(ctx, o)).To(Succeed())
	})
	It("rejects plainHTTP with an https URL", func() {
		err := k8sClient.Create(ctx, mk("https://charts.example.com/aif-ui.tgz", true, nil))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("plainHTTP is only supported for oci:// chart URLs"))
	})
	It("rejects plainHTTP together with tls", func() {
		tls := &aiplatformv1alpha1.HelmTLS{InsecureSkipVerify: true}
		err := k8sClient.Create(ctx, mk("oci://reg.local:8088/aif/chart/aif-ui", true, tls))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("tls must not be set when plainHTTP is true"))
	})
})
