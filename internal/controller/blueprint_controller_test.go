package controller_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/conditions"
)

var _ = Describe("BlueprintReconciler", func() {
	const timeout = 30 * time.Second
	const interval = 250 * time.Millisecond

	ctx := context.Background()

	findReady := func(conds []metav1.Condition) *metav1.Condition {
		for i := range conds {
			if conds[i].Type == conditions.TypeReady {
				return &conds[i]
			}
		}
		return nil
	}

	It("should reconcile a valid Blueprint to Ready=True", func() {
		bp := &aifv1.Blueprint{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid-bp.1.0.0",
			},
			Spec: aifv1.BlueprintSpec{
				BlueprintName: "valid-bp",
				Version:       "1.0.0",
				UseCase:       "rag",
				Description:   "Test Blueprint",
				Source: aifv1.BlueprintSource{
					Type: aifv1.BlueprintSourcePublished,
					PublishedFrom: &aifv1.PublishedFromRef{
						BundleNamespace:  "test-ns",
						BundleName:       "test-bundle",
						BundleGeneration: 1,
					},
				},
				Components: []aifv1.ComponentRef{
					{
						Name: "nim",
						Kind: aifv1.ComponentKindApp,
						App: &aifv1.AppRef{
							Repo:    "oci://registry.suse.com/ai/charts/nvidia",
							Chart:   "nim-llm",
							Version: "1.0.0",
						},
					},
				},
				PublishedBy: "test-user",
				PublishedAt: metav1.Now(),
			},
		}

		Expect(k8sClient.Create(ctx, bp)).To(Succeed())

		Eventually(func(g Gomega) {
			var fetched aifv1.Blueprint
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(bp), &fetched)).To(Succeed())
			g.Expect(fetched.Status.Phase).To(Equal(aifv1.BlueprintPhaseActive))
			g.Expect(fetched.Status.DeploymentCount).To(Equal(int32(0)))
			g.Expect(fetched.Status.ObservedGeneration).To(Equal(fetched.Generation))
			rc := findReady(fetched.Status.Conditions)
			g.Expect(rc).NotTo(BeNil())
			g.Expect(rc.Status).To(Equal(metav1.ConditionTrue))
			g.Expect(rc.Reason).To(Equal(conditions.ReasonBlueprintValidated))
		}, timeout, interval).Should(Succeed())

		// Verify finalizer
		var fetched aifv1.Blueprint
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(bp), &fetched)).To(Succeed())
		Expect(fetched.Finalizers).To(ContainElement("ai.suse.com/cleanup"))
	})

	It("should reject invalid semver at the CRD level", func() {
		bp := &aifv1.Blueprint{
			ObjectMeta: metav1.ObjectMeta{
				Name: "invalid-semver-bp",
			},
			Spec: aifv1.BlueprintSpec{
				BlueprintName: "invalid-semver-bp",
				Version:       "1.0", // invalid - missing patch
				UseCase:       "rag",
				Source: aifv1.BlueprintSource{
					Type: aifv1.BlueprintSourcePublished,
					PublishedFrom: &aifv1.PublishedFromRef{
						BundleNamespace:  "test-ns",
						BundleName:       "test-bundle",
						BundleGeneration: 1,
					},
				},
				Components: []aifv1.ComponentRef{
					{
						Name: "nim",
						Kind: aifv1.ComponentKindApp,
						App: &aifv1.AppRef{
							Repo:    "oci://registry.suse.com/ai/charts/nvidia",
							Chart:   "nim-llm",
							Version: "1.0.0",
						},
					},
				},
				PublishedBy: "test-user",
				PublishedAt: metav1.Now(),
			},
		}

		err := k8sClient.Create(ctx, bp)
		Expect(err).To(HaveOccurred())
		Expect(errors.IsInvalid(err)).To(BeTrue())
	})

	It("should reject invalid source type at the CRD level", func() {
		bp := &aifv1.Blueprint{
			ObjectMeta: metav1.ObjectMeta{
				Name: "invalid-source-bp",
			},
			Spec: aifv1.BlueprintSpec{
				BlueprintName: "invalid-source-bp",
				Version:       "1.0.0",
				UseCase:       "rag",
				Source: aifv1.BlueprintSource{
					Type: "InvalidType",
				},
				Components: []aifv1.ComponentRef{
					{
						Name: "nim",
						Kind: aifv1.ComponentKindApp,
						App: &aifv1.AppRef{
							Repo:    "oci://registry.suse.com/ai/charts/nvidia",
							Chart:   "nim-llm",
							Version: "1.0.0",
						},
					},
				},
				PublishedBy: "test-user",
				PublishedAt: metav1.Now(),
			},
		}

		err := k8sClient.Create(ctx, bp)
		Expect(err).To(HaveOccurred())
		Expect(errors.IsInvalid(err)).To(BeTrue())
	})

	It("should compute deploymentCount from Workloads", func() {
		bp := &aifv1.Blueprint{
			ObjectMeta: metav1.ObjectMeta{
				Name: "deploy-count-bp.1.0.0",
			},
			Spec: aifv1.BlueprintSpec{
				BlueprintName: "deploy-count-bp",
				Version:       "1.0.0",
				UseCase:       "rag",
				Description:   "Test deploymentCount",
				Source: aifv1.BlueprintSource{
					Type: aifv1.BlueprintSourcePublished,
					PublishedFrom: &aifv1.PublishedFromRef{
						BundleNamespace:  "test-ns",
						BundleName:       "test-bundle",
						BundleGeneration: 1,
					},
				},
				Components: []aifv1.ComponentRef{
					{
						Name: "nim",
						Kind: aifv1.ComponentKindApp,
						App: &aifv1.AppRef{
							Repo:    "oci://registry.suse.com/ai/charts/nvidia",
							Chart:   "nim-llm",
							Version: "1.0.0",
						},
					},
				},
				PublishedBy: "test-user",
				PublishedAt: metav1.Now(),
			},
		}

		Expect(k8sClient.Create(ctx, bp)).To(Succeed())

		// Wait for initial reconciliation with count = 0
		Eventually(func(g Gomega) {
			var fetched aifv1.Blueprint
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(bp), &fetched)).To(Succeed())
			g.Expect(fetched.Status.DeploymentCount).To(Equal(int32(0)))
			rc := findReady(fetched.Status.Conditions)
			g.Expect(rc).NotTo(BeNil())
			g.Expect(rc.Status).To(Equal(metav1.ConditionTrue))
		}, timeout, interval).Should(Succeed())

		// Create a Workload referencing this Blueprint
		workload := &aifv1.Workload{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bp-test-workload",
				Namespace: "default",
			},
			Spec: aifv1.WorkloadSpec{
				Name: "Test Workload",
				Source: aifv1.WorkloadSource{
					Kind: aifv1.WorkloadSourceKindBlueprint,
					Blueprint: &aifv1.BlueprintRef{
						Name:    "deploy-count-bp",
						Version: "1.0.0",
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, workload)).To(Succeed())

		// Wait for count to become 1 (Workload create triggers Blueprint reconcile via watch)
		Eventually(func(g Gomega) {
			var fetched aifv1.Blueprint
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(bp), &fetched)).To(Succeed())
			g.Expect(fetched.Status.DeploymentCount).To(Equal(int32(1)))
		}, timeout, interval).Should(Succeed())

		// Delete the Workload
		Expect(k8sClient.Delete(ctx, workload)).To(Succeed())

		// Wait for count to return to 0
		Eventually(func(g Gomega) {
			var fetched aifv1.Blueprint
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(bp), &fetched)).To(Succeed())
			g.Expect(fetched.Status.DeploymentCount).To(Equal(int32(0)))
		}, timeout, interval).Should(Succeed())
	})

	It("should block deletion when active Workloads exist", func() {
		bp := &aifv1.Blueprint{
			ObjectMeta: metav1.ObjectMeta{
				Name: "block-del-bp.1.0.0",
			},
			Spec: aifv1.BlueprintSpec{
				BlueprintName: "block-del-bp",
				Version:       "1.0.0",
				UseCase:       "rag",
				Source: aifv1.BlueprintSource{
					Type: aifv1.BlueprintSourcePublished,
					PublishedFrom: &aifv1.PublishedFromRef{
						BundleNamespace:  "test-ns",
						BundleName:       "test-bundle",
						BundleGeneration: 1,
					},
				},
				Components: []aifv1.ComponentRef{
					{
						Name: "nim",
						Kind: aifv1.ComponentKindApp,
						App: &aifv1.AppRef{
							Repo:    "oci://registry.suse.com/ai/charts/nvidia",
							Chart:   "nim-llm",
							Version: "1.0.0",
						},
					},
				},
				PublishedBy: "test-user",
				PublishedAt: metav1.Now(),
			},
		}
		Expect(k8sClient.Create(ctx, bp)).To(Succeed())

		// Create a Workload referencing this Blueprint
		workload := &aifv1.Workload{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "block-del-workload",
				Namespace: "default",
			},
			Spec: aifv1.WorkloadSpec{
				Name: "Blocking Workload",
				Source: aifv1.WorkloadSource{
					Kind: aifv1.WorkloadSourceKindBlueprint,
					Blueprint: &aifv1.BlueprintRef{
						Name:    "block-del-bp",
						Version: "1.0.0",
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, workload)).To(Succeed())

		// Wait for deployment count = 1
		Eventually(func(g Gomega) {
			var fetched aifv1.Blueprint
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(bp), &fetched)).To(Succeed())
			g.Expect(fetched.Status.DeploymentCount).To(Equal(int32(1)))
		}, timeout, interval).Should(Succeed())

		// Delete Blueprint (should be blocked by finalizer while Workloads exist)
		Expect(k8sClient.Delete(ctx, bp)).To(Succeed())

		// Wait for DeletionTimestamp to be visible in the cache
		Eventually(func(g Gomega) {
			var fetched aifv1.Blueprint
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(bp), &fetched)).To(Succeed())
			g.Expect(fetched.DeletionTimestamp).NotTo(BeNil())
		}, timeout, interval).Should(Succeed())

		// Blueprint should still exist (finalizer blocks deletion)
		Consistently(func(g Gomega) {
			var fetched aifv1.Blueprint
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(bp), &fetched)).To(Succeed())
			g.Expect(fetched.Finalizers).To(ContainElement("ai.suse.com/cleanup"))
			g.Expect(fetched.DeletionTimestamp).NotTo(BeNil())
		}, 2*time.Second, interval).Should(Succeed())

		// Delete the Workload to unblock
		Expect(k8sClient.Delete(ctx, workload)).To(Succeed())

		// Now Blueprint should be fully deleted
		Eventually(func() bool {
			var fetched aifv1.Blueprint
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(bp), &fetched)
			return errors.IsNotFound(err)
		}, timeout, interval).Should(BeTrue())
	})
})
