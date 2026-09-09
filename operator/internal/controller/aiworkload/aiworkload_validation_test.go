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

func blueprintWorkload(name string, strategy aiplatformv1alpha1.AIWorkloadDeployStrategy, clusters []string) *aiplatformv1alpha1.AIWorkload {
	return &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			DisplayName:     name,
			TargetNamespace: "ai",
			DeployStrategy:  strategy,
			TargetClusters:  clusters,
			Source: aiplatformv1alpha1.AIWorkloadSource{
				SourceType: aiplatformv1alpha1.AIWorkloadSourceBlueprint,
				Blueprint:  &aiplatformv1alpha1.BlueprintSource{Name: "rag", Version: "1.0.0"},
			},
		},
	}
}

var _ = Describe("AIWorkload admission validation", func() {
	It("rejects a Blueprint workload with no target clusters", func() {
		w := blueprintWorkload("no-targets", aiplatformv1alpha1.AIWorkloadDeployFleetBundle, nil)
		Expect(k8sClient.Create(ctx, w)).ToNot(Succeed())
	})

	It("accepts a Blueprint workload with one target cluster", func() {
		w := blueprintWorkload("one-target", aiplatformv1alpha1.AIWorkloadDeployFleetBundle, []string{"local"})
		Expect(k8sClient.Create(ctx, w)).To(Succeed())
	})

	It("accepts mixed local+downstream GitOps", func() {
		w := blueprintWorkload("mixed-gitops", aiplatformv1alpha1.AIWorkloadDeployGitOps, []string{"local", "c-downstream"})
		Expect(k8sClient.Create(ctx, w)).To(Succeed())
	})

	It("accepts local-only GitOps and downstream-only GitOps", func() {
		Expect(k8sClient.Create(ctx, blueprintWorkload("local-gitops", aiplatformv1alpha1.AIWorkloadDeployGitOps, []string{"local"}))).To(Succeed())
		Expect(k8sClient.Create(ctx, blueprintWorkload("ds-gitops", aiplatformv1alpha1.AIWorkloadDeployGitOps, []string{"c-a", "c-b"}))).To(Succeed())
	})

	It("accepts mixed local+downstream for FleetBundle (two HelmOps)", func() {
		Expect(k8sClient.Create(ctx, blueprintWorkload("mixed-fleet", aiplatformv1alpha1.AIWorkloadDeployFleetBundle, []string{"local", "c-x"}))).To(Succeed())
	})

	It("rejects a deployStrategy change after creation", func() {
		w := blueprintWorkload("immutable-strategy", aiplatformv1alpha1.AIWorkloadDeployFleetBundle, []string{"local"})
		Expect(k8sClient.Create(ctx, w)).To(Succeed())
		w.Spec.DeployStrategy = aiplatformv1alpha1.AIWorkloadDeployGitOps
		Expect(k8sClient.Update(ctx, w)).ToNot(Succeed())
	})
})
