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

var _ = Describe("WorkloadReconciler", func() {
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

	It("should reconcile a valid App Workload to Pending/AwaitingDeployer", func() {
		w := &aifv1.Workload{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid-app-workload",
				Namespace: "default",
			},
			Spec: aifv1.WorkloadSpec{
				Name: "Test App Workload",
				Source: aifv1.WorkloadSource{
					Kind: aifv1.WorkloadSourceKindApp,
					App: &aifv1.AppRef{
						Repo:    "https://example.com/charts",
						Chart:   "llama3",
						Version: "1.0.0",
					},
				},
			},
		}

		Expect(k8sClient.Create(ctx, w)).To(Succeed())

		Eventually(func(g Gomega) {
			var fetched aifv1.Workload
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(w), &fetched)).To(Succeed())
			g.Expect(fetched.Status.Phase).To(Equal(aifv1.WorkloadPhasePending))
			g.Expect(fetched.Status.ObservedGeneration).To(Equal(fetched.Generation))
			rc := findReady(fetched.Status.Conditions)
			g.Expect(rc).NotTo(BeNil())
			g.Expect(rc.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(rc.Reason).To(Equal(conditions.ReasonAwaitingDeployer))
		}, timeout, interval).Should(Succeed())

		// Verify finalizer
		var fetched aifv1.Workload
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(w), &fetched)).To(Succeed())
		Expect(fetched.Finalizers).To(ContainElement("ai.suse.com/cleanup"))
	})

	It("should reconcile a valid Blueprint Workload to Pending/AwaitingDeployer", func() {
		w := &aifv1.Workload{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid-bp-workload",
				Namespace: "default",
			},
			Spec: aifv1.WorkloadSpec{
				Name: "Test Blueprint Workload",
				Source: aifv1.WorkloadSource{
					Kind: aifv1.WorkloadSourceKindBlueprint,
					Blueprint: &aifv1.BlueprintRef{
						Name:    "rag-stack",
						Version: "1.0.0",
					},
				},
			},
		}

		Expect(k8sClient.Create(ctx, w)).To(Succeed())

		Eventually(func(g Gomega) {
			var fetched aifv1.Workload
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(w), &fetched)).To(Succeed())
			g.Expect(fetched.Status.Phase).To(Equal(aifv1.WorkloadPhasePending))
			rc := findReady(fetched.Status.Conditions)
			g.Expect(rc).NotTo(BeNil())
			g.Expect(rc.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(rc.Reason).To(Equal(conditions.ReasonAwaitingDeployer))
		}, timeout, interval).Should(Succeed())
	})

	It("should reconcile a valid BundleTest Workload to Pending/AwaitingDeployer", func() {
		w := &aifv1.Workload{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid-bundletest-workload",
				Namespace: "default",
			},
			Spec: aifv1.WorkloadSpec{
				Name: "Test BundleTest Workload",
				Source: aifv1.WorkloadSource{
					Kind: aifv1.WorkloadSourceKindBundleTest,
					BundleTest: &aifv1.BundleTestRef{
						Namespace:  "default",
						Name:       "test-bundle",
						Generation: 1,
					},
				},
			},
		}

		Expect(k8sClient.Create(ctx, w)).To(Succeed())

		Eventually(func(g Gomega) {
			var fetched aifv1.Workload
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(w), &fetched)).To(Succeed())
			g.Expect(fetched.Status.Phase).To(Equal(aifv1.WorkloadPhasePending))
			rc := findReady(fetched.Status.Conditions)
			g.Expect(rc).NotTo(BeNil())
			g.Expect(rc.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(rc.Reason).To(Equal(conditions.ReasonAwaitingDeployer))
		}, timeout, interval).Should(Succeed())
	})

	It("should set InvalidSpec when Kind=App but App field is nil", func() {
		w := &aifv1.Workload{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-app-workload",
				Namespace: "default",
			},
			Spec: aifv1.WorkloadSpec{
				Name: "Invalid App Workload",
				Source: aifv1.WorkloadSource{
					Kind: aifv1.WorkloadSourceKindApp,
					App:  nil,
				},
			},
		}

		Expect(k8sClient.Create(ctx, w)).To(Succeed())

		Eventually(func(g Gomega) {
			var fetched aifv1.Workload
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(w), &fetched)).To(Succeed())
			rc := findReady(fetched.Status.Conditions)
			g.Expect(rc).NotTo(BeNil())
			g.Expect(rc.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(rc.Reason).To(Equal(conditions.ReasonInvalidSpec))
			g.Expect(rc.Message).To(ContainSubstring("source.app"))
			g.Expect(fetched.Status.Phase).To(BeEmpty())
		}, timeout, interval).Should(Succeed())
	})

	It("should set InvalidSpec when Kind=Blueprint but Blueprint field is nil", func() {
		w := &aifv1.Workload{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-bp-workload",
				Namespace: "default",
			},
			Spec: aifv1.WorkloadSpec{
				Name: "Invalid Blueprint Workload",
				Source: aifv1.WorkloadSource{
					Kind:      aifv1.WorkloadSourceKindBlueprint,
					Blueprint: nil,
				},
			},
		}

		Expect(k8sClient.Create(ctx, w)).To(Succeed())

		Eventually(func(g Gomega) {
			var fetched aifv1.Workload
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(w), &fetched)).To(Succeed())
			rc := findReady(fetched.Status.Conditions)
			g.Expect(rc).NotTo(BeNil())
			g.Expect(rc.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(rc.Reason).To(Equal(conditions.ReasonInvalidSpec))
			g.Expect(rc.Message).To(ContainSubstring("source.blueprint"))
		}, timeout, interval).Should(Succeed())
	})

	It("should set InvalidSpec when Kind=BundleTest but BundleTest field is nil", func() {
		w := &aifv1.Workload{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-bt-workload",
				Namespace: "default",
			},
			Spec: aifv1.WorkloadSpec{
				Name: "Invalid BundleTest Workload",
				Source: aifv1.WorkloadSource{
					Kind:       aifv1.WorkloadSourceKindBundleTest,
					BundleTest: nil,
				},
			},
		}

		Expect(k8sClient.Create(ctx, w)).To(Succeed())

		Eventually(func(g Gomega) {
			var fetched aifv1.Workload
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(w), &fetched)).To(Succeed())
			rc := findReady(fetched.Status.Conditions)
			g.Expect(rc).NotTo(BeNil())
			g.Expect(rc.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(rc.Reason).To(Equal(conditions.ReasonInvalidSpec))
			g.Expect(rc.Message).To(ContainSubstring("source.bundleTest"))
		}, timeout, interval).Should(Succeed())
	})

	It("should remove finalizer on deletion", func() {
		w := &aifv1.Workload{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "finalizer-workload",
				Namespace: "default",
			},
			Spec: aifv1.WorkloadSpec{
				Name: "Finalizer Workload",
				Source: aifv1.WorkloadSource{
					Kind: aifv1.WorkloadSourceKindApp,
					App: &aifv1.AppRef{
						Repo:    "https://example.com/charts",
						Chart:   "llama3",
						Version: "1.0.0",
					},
				},
			},
		}

		Expect(k8sClient.Create(ctx, w)).To(Succeed())

		// Wait for finalizer
		Eventually(func(g Gomega) {
			var fetched aifv1.Workload
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(w), &fetched)).To(Succeed())
			g.Expect(fetched.Finalizers).To(ContainElement("ai.suse.com/cleanup"))
		}, timeout, interval).Should(Succeed())

		// Delete
		Expect(k8sClient.Delete(ctx, w)).To(Succeed())

		// Wait for full deletion
		Eventually(func() bool {
			var fetched aifv1.Workload
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(w), &fetched)
			return errors.IsNotFound(err)
		}, timeout, interval).Should(BeTrue())
	})
})
