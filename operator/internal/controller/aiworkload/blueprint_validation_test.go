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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func bp(name string, comps ...aiplatformv1alpha1.BlueprintComponent) *aiplatformv1alpha1.Blueprint {
	return &aiplatformv1alpha1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: aiplatformv1alpha1.BlueprintSpec{
			DisplayName: name, Version: "1.0.0", Components: comps,
		},
	}
}

func comp(chart string) aiplatformv1alpha1.BlueprintComponent {
	return aiplatformv1alpha1.BlueprintComponent{ChartRepo: "r", ChartName: chart, ChartVersion: "1.0.0"}
}

func appComp(name string) aiplatformv1alpha1.BlueprintComponent {
	return aiplatformv1alpha1.BlueprintComponent{
		ApplicationRef: &aiplatformv1alpha1.ApplicationReference{Name: name, Version: "1.0.0"},
	}
}

var _ = Describe("Blueprint component identity", func() {
	It("rejects duplicate chartName within a blueprint", func() {
		Expect(k8sClient.Create(ctx, bp("dup", comp("a"), comp("a")))).ToNot(Succeed())
	})
	It("rejects duplicate logical Application names within a blueprint", func() {
		Expect(k8sClient.Create(ctx, bp("dup-app", appComp("suse.a"), appComp("suse.a")))).ToNot(Succeed())
	})
	It("accepts distinct chartNames and allows reordering on update", func() {
		b := bp("distinct", comp("a"), comp("b"))
		Expect(k8sClient.Create(ctx, b)).To(Succeed())
		b.Spec.Components = []aiplatformv1alpha1.BlueprintComponent{comp("b"), comp("a")}
		Expect(k8sClient.Update(ctx, b)).To(Succeed())
	})
})
