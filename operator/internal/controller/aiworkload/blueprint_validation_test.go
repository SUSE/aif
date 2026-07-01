package aiworkload_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

var _ = Describe("BlueprintComponent content-type validation", func() {
	newBP := func(name string, comp aiplatformv1alpha1.BlueprintComponent) *aiplatformv1alpha1.Blueprint {
		return &aiplatformv1alpha1.Blueprint{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: aiplatformv1alpha1.BlueprintSpec{
				DisplayName: "d", Version: "1.0.0",
				Components: []aiplatformv1alpha1.BlueprintComponent{comp},
			},
		}
	}

	It("accepts a valid Helm component", func() {
		bp := newBP("val-helm", aiplatformv1alpha1.BlueprintComponent{
			Name: "c1", Type: aiplatformv1alpha1.ComponentContentTypeHelm,
			Helm: &aiplatformv1alpha1.BlueprintHelmSource{ChartRepo: "r", ChartName: "n", ChartVersion: "1.0.0"},
		})
		Expect(k8sClient.Create(ctx, bp)).To(Succeed())
	})

	It("accepts a valid Kustomize component", func() {
		bp := newBP("val-kust", aiplatformv1alpha1.BlueprintComponent{
			Name: "c1", Type: aiplatformv1alpha1.ComponentContentTypeKustomize,
			Kustomize: &aiplatformv1alpha1.KustomizeSource{Repo: "https://git/x", Path: "overlays/dev"},
		})
		Expect(k8sClient.Create(ctx, bp)).To(Succeed())
	})

	It("rejects type=Helm without helm", func() {
		bp := newBP("bad-helm", aiplatformv1alpha1.BlueprintComponent{
			Name: "c1", Type: aiplatformv1alpha1.ComponentContentTypeHelm,
			Kustomize: &aiplatformv1alpha1.KustomizeSource{Repo: "https://git/x", Path: "p"},
		})
		Expect(k8sClient.Create(ctx, bp)).ToNot(Succeed())
	})

	It("rejects type=Kustomize without kustomize", func() {
		bp := newBP("bad-kust", aiplatformv1alpha1.BlueprintComponent{
			Name: "c1", Type: aiplatformv1alpha1.ComponentContentTypeKustomize,
			Helm: &aiplatformv1alpha1.BlueprintHelmSource{ChartRepo: "r", ChartName: "n", ChartVersion: "1.0.0"},
		})
		Expect(k8sClient.Create(ctx, bp)).ToNot(Succeed())
	})
})
