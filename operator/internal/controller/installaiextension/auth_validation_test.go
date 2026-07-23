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

var _ = Describe("HelmSource.auth CEL validation", func() {
	base := func(auth *aiplatformv1alpha1.HelmAuth) *aiplatformv1alpha1.InstallAIExtension {
		return &aiplatformv1alpha1.InstallAIExtension{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "auth-cel-"},
			Spec: aiplatformv1alpha1.InstallAIExtensionSpec{
				Source: aiplatformv1alpha1.ExtensionSource{
					Kind: aiplatformv1alpha1.ExtensionSourceKindHelm,
					Helm: &aiplatformv1alpha1.HelmSource{
						ChartURL: "oci://registry.example.com/charts/aif-ui-server",
						Version:  "1.0.0",
						Auth:     auth,
					},
				},
				Extension: aiplatformv1alpha1.ExtensionConfig{Name: "aif-ui", Version: "1.0.0"},
			},
		}
	}
	ref := aiplatformv1alpha1.SecretKeyRef{Name: "s", Key: "k"}

	It("accepts a single basic block", func() {
		obj := base(&aiplatformv1alpha1.HelmAuth{
			Basic: &aiplatformv1alpha1.BasicAuth{UserSecretRef: ref, TokenSecretRef: ref},
		})
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())
		Expect(k8sClient.Delete(ctx, obj)).To(Succeed())
	})

	It("accepts a single token block", func() {
		obj := base(&aiplatformv1alpha1.HelmAuth{
			Token: &aiplatformv1alpha1.TokenAuth{TokenSecretRef: ref},
		})
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())
		Expect(k8sClient.Delete(ctx, obj)).To(Succeed())
	})

	It("accepts a single dockerConfig block", func() {
		obj := base(&aiplatformv1alpha1.HelmAuth{
			DockerConfig: &aiplatformv1alpha1.DockerConfigAuth{SecretRef: aiplatformv1alpha1.LocalSecretRef{Name: "s"}},
		})
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())
		Expect(k8sClient.Delete(ctx, obj)).To(Succeed())
	})

	It("accepts omitted auth", func() {
		obj := base(nil)
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())
		Expect(k8sClient.Delete(ctx, obj)).To(Succeed())
	})

	It("rejects two blocks set at once", func() {
		obj := base(&aiplatformv1alpha1.HelmAuth{
			Basic: &aiplatformv1alpha1.BasicAuth{UserSecretRef: ref, TokenSecretRef: ref},
			Token: &aiplatformv1alpha1.TokenAuth{TokenSecretRef: ref},
		})
		err := k8sClient.Create(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exactly one of basic, token, or dockerConfig must be set"))
	})

	It("rejects three blocks set at once", func() {
		obj := base(&aiplatformv1alpha1.HelmAuth{
			Basic:        &aiplatformv1alpha1.BasicAuth{UserSecretRef: ref, TokenSecretRef: ref},
			Token:        &aiplatformv1alpha1.TokenAuth{TokenSecretRef: ref},
			DockerConfig: &aiplatformv1alpha1.DockerConfigAuth{SecretRef: aiplatformv1alpha1.LocalSecretRef{Name: "s"}},
		})
		err := k8sClient.Create(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exactly one of basic, token, or dockerConfig must be set"))
	})

	It("rejects an empty auth block (zero set)", func() {
		obj := base(&aiplatformv1alpha1.HelmAuth{})
		err := k8sClient.Create(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exactly one of basic, token, or dockerConfig must be set"))
	})
})
