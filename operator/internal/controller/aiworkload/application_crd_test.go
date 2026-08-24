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
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

var _ = Describe("Application-backed Blueprint admission", func() {
	It("accepts a logical requirement and defaults the Application profile", func() {
		application := &aiplatformv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{Name: "suse.crd-validation"},
			Spec: aiplatformv1alpha1.ApplicationSpec{
				Chart: aiplatformv1alpha1.ApplicationChart{Name: "validated", SourceRef: "private-source"},
			},
		}
		Expect(k8sClient.Create(ctx, application)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(context.Background(), application) })

		storedApplication := &aiplatformv1alpha1.Application{}
		Expect(k8sClient.Get(ctx, clientKey(application.Name), storedApplication)).To(Succeed())
		Expect(storedApplication.Spec.CredentialProfile).To(Equal(aiplatformv1alpha1.ComponentVendorSUSE))

		blueprint := admissionBlueprint("logical-only")
		blueprint.Spec.Components = []aiplatformv1alpha1.BlueprintComponent{{
			ApplicationRef: &aiplatformv1alpha1.ApplicationReference{Name: application.Name, Version: "1.0.0"},
		}}
		Expect(k8sClient.Create(ctx, blueprint)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(context.Background(), blueprint) })
	})

	It("rejects a component that mixes a logical reference with chart coordinates", func() {
		blueprint := admissionBlueprint("mixed-forms")
		blueprint.Spec.Components = []aiplatformv1alpha1.BlueprintComponent{{
			ApplicationRef: &aiplatformv1alpha1.ApplicationReference{Name: "suse.ollama", Version: "1.55.0"},
			ChartRepo:      "application-collection",
			ChartName:      "ollama",
			ChartVersion:   "1.55.0",
		}}

		Expect(k8sClient.Create(ctx, blueprint)).To(MatchError(ContainSubstring("set exactly one of applicationRef or the direct chart fields")))
	})

	It("rejects an incomplete legacy chart coordinate set", func() {
		blueprint := admissionBlueprint("incomplete-direct")
		blueprint.Spec.Components = []aiplatformv1alpha1.BlueprintComponent{{
			ChartRepo: "application-collection",
			ChartName: "ollama",
		}}

		Expect(k8sClient.Create(ctx, blueprint)).To(MatchError(ContainSubstring("chartRepo, chartName, and chartVersion must be set together")))
	})

	It("rejects an Application without a package mapping", func() {
		application := &aiplatformv1alpha1.Application{ObjectMeta: metav1.ObjectMeta{Name: "suse.empty"}}

		Expect(k8sClient.Create(ctx, application)).To(MatchError(ContainSubstring("spec.chart")))
	})

	It("rejects a sourceRef that cannot name a ClusterRepo", func() {
		application := &aiplatformv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{Name: "suse.invalid-source"},
			Spec: aiplatformv1alpha1.ApplicationSpec{
				Chart: aiplatformv1alpha1.ApplicationChart{Name: "chart", SourceRef: "oci://harbor/charts"},
			},
		}

		Expect(k8sClient.Create(ctx, application)).To(MatchError(ContainSubstring("spec.chart.sourceRef")))
	})

	It("rejects a Blueprint that uses a source URL as its logical identity", func() {
		blueprint := admissionBlueprint("url-as-identity")
		blueprint.Spec.Components = []aiplatformv1alpha1.BlueprintComponent{{
			ApplicationRef: &aiplatformv1alpha1.ApplicationReference{
				Name:    "oci://registry.internal/charts/ollama",
				Version: "1.55.0",
			},
		}}

		Expect(k8sClient.Create(ctx, blueprint)).To(MatchError(ContainSubstring("spec.components[0].applicationRef.name")))
	})
})

func admissionBlueprint(name string) *aiplatformv1alpha1.Blueprint {
	return &aiplatformv1alpha1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: aiplatformv1alpha1.BlueprintSpec{
			DisplayName: name,
			Version:     "1.0.0",
		},
	}
}

func clientKey(name string) client.ObjectKey {
	return client.ObjectKey{Name: name}
}
