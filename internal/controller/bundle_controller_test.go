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

var _ = Describe("BundleReconciler", func() {
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

	It("should reconcile a valid Bundle to Ready=True", func() {
		bundle := &aifv1.Bundle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid-bundle",
				Namespace: "default",
			},
			Spec: aifv1.BundleSpec{
				Title:           "Test Bundle",
				TargetBlueprint: "test-blueprint",
				UseCase:         "rag",
				Components: []aifv1.ComponentRef{
					{
						Name: "app1",
						Kind: aifv1.ComponentKindApp,
						App: &aifv1.AppRef{
							Repo:    "https://example.com/charts",
							Chart:   "test-chart",
							Version: "1.0.0",
						},
					},
				},
			},
		}

		Expect(k8sClient.Create(ctx, bundle)).To(Succeed())

		Eventually(func(g Gomega) {
			var fetched aifv1.Bundle
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(bundle), &fetched)).To(Succeed())
			rc := findReady(fetched.Status.Conditions)
			g.Expect(rc).NotTo(BeNil())
			g.Expect(rc.Status).To(Equal(metav1.ConditionTrue))
			g.Expect(rc.Reason).To(Equal(conditions.ReasonReconciled))
			g.Expect(fetched.Status.ObservedGeneration).To(Equal(fetched.Generation))
		}, timeout, interval).Should(Succeed())

		// Verify finalizer
		var fetched aifv1.Bundle
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(bundle), &fetched)).To(Succeed())
		Expect(fetched.Finalizers).To(ContainElement("ai.suse.com/cleanup"))
	})

	It("should reject invalid useCase at the CRD level", func() {
		bundle := &aifv1.Bundle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-bundle",
				Namespace: "default",
			},
			Spec: aifv1.BundleSpec{
				Title:           "Invalid Bundle",
				TargetBlueprint: "test-blueprint",
				UseCase:         "invalid-use-case",
				Components: []aifv1.ComponentRef{
					{
						Name: "app1",
						Kind: aifv1.ComponentKindApp,
						App: &aifv1.AppRef{
							Repo:    "https://example.com/charts",
							Chart:   "test-chart",
							Version: "1.0.0",
						},
					},
				},
			},
		}

		err := k8sClient.Create(ctx, bundle)
		Expect(err).To(HaveOccurred())
		Expect(errors.IsInvalid(err)).To(BeTrue())
	})

	It("should add finalizer and remove it on deletion", func() {
		bundle := &aifv1.Bundle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "finalizer-bundle",
				Namespace: "default",
			},
			Spec: aifv1.BundleSpec{
				Title:           "Finalizer Test",
				TargetBlueprint: "test-blueprint",
				UseCase:         "rag",
				Components: []aifv1.ComponentRef{
					{
						Name: "app1",
						Kind: aifv1.ComponentKindApp,
						App: &aifv1.AppRef{
							Repo:    "https://example.com/charts",
							Chart:   "test-chart",
							Version: "1.0.0",
						},
					},
				},
			},
		}

		Expect(k8sClient.Create(ctx, bundle)).To(Succeed())

		// Wait for finalizer to be added
		Eventually(func(g Gomega) {
			var fetched aifv1.Bundle
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(bundle), &fetched)).To(Succeed())
			g.Expect(fetched.Finalizers).To(ContainElement("ai.suse.com/cleanup"))
		}, timeout, interval).Should(Succeed())

		// Delete
		Expect(k8sClient.Delete(ctx, bundle)).To(Succeed())

		// Wait for object to be fully deleted (finalizer removed)
		Eventually(func() bool {
			var fetched aifv1.Bundle
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(bundle), &fetched)
			return errors.IsNotFound(err)
		}, timeout, interval).Should(BeTrue())
	})

	It("should self-heal a Submitted Bundle when matching Blueprint exists", func() {
		// Create the matching Blueprint first (cluster-scoped)
		bp := &aifv1.Blueprint{
			ObjectMeta: metav1.ObjectMeta{
				Name: "heal-bp.1.0.0",
			},
			Spec: aifv1.BlueprintSpec{
				BlueprintName: "heal-bp",
				Version:       "1.0.0",
				UseCase:       "rag",
				Source: aifv1.BlueprintSource{
					Type: aifv1.BlueprintSourcePublished,
					PublishedFrom: &aifv1.PublishedFromRef{
						BundleNamespace:  "default",
						BundleName:       "heal-bundle",
						BundleGeneration: 1,
					},
				},
				Components: []aifv1.ComponentRef{
					{
						Name: "app1",
						Kind: aifv1.ComponentKindApp,
						App: &aifv1.AppRef{
							Repo:    "https://example.com/charts",
							Chart:   "test-chart",
							Version: "1.0.0",
						},
					},
				},
				PublishedBy: "approver",
				PublishedAt: metav1.Now(),
			},
		}
		Expect(k8sClient.Create(ctx, bp)).To(Succeed())

		// Create the Submitted Bundle
		bundle := &aifv1.Bundle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "heal-bundle",
				Namespace: "default",
			},
			Spec: aifv1.BundleSpec{
				Title:           "Heal Bundle",
				TargetBlueprint: "heal-bp",
				UseCase:         "rag",
				Components: []aifv1.ComponentRef{
					{
						Name: "app1",
						Kind: aifv1.ComponentKindApp,
						App: &aifv1.AppRef{
							Repo:    "https://example.com/charts",
							Chart:   "test-chart",
							Version: "1.0.0",
						},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, bundle)).To(Succeed())

		// Wait for initial reconciliation to complete
		Eventually(func(g Gomega) {
			var fetched aifv1.Bundle
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(bundle), &fetched)).To(Succeed())
			g.Expect(fetched.Finalizers).To(ContainElement("ai.suse.com/cleanup"))
			rc := findReady(fetched.Status.Conditions)
			g.Expect(rc).NotTo(BeNil())
		}, timeout, interval).Should(Succeed())

		// Set Bundle to Submitted phase with matching submission data via status update
		var fetched aifv1.Bundle
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(bundle), &fetched)).To(Succeed())
		fetched.Status.Phase = aifv1.BundlePhaseSubmitted
		fetched.Status.Submission = &aifv1.SubmissionStatus{
			ProposedVersion:    "1.0.0",
			ChangeDescription:  "Initial release",
			SubmittedBy:        "alice",
			SubmittedAt:        metav1.Now(),
			GenerationAtSubmit: fetched.Generation,
		}
		Expect(k8sClient.Status().Update(ctx, &fetched)).To(Succeed())

		// Trigger re-reconciliation by touching the spec (retry on conflict)
		Eventually(func() error {
			var fresh aifv1.Bundle
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(bundle), &fresh); err != nil {
				return err
			}
			if fresh.Spec.Description == "" {
				fresh.Spec.Description = "trigger"
			}
			return k8sClient.Update(ctx, &fresh)
		}, timeout, interval).Should(Succeed())

		// Wait for self-healing: phase should reset to Draft, publishedVersions appended
		Eventually(func(g Gomega) {
			var healed aifv1.Bundle
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(bundle), &healed)).To(Succeed())
			g.Expect(healed.Status.Phase).To(Equal(aifv1.BundlePhaseDraft))
			g.Expect(healed.Status.Submission).To(BeNil())
			g.Expect(healed.Status.PublishedVersions).To(HaveLen(1))
			g.Expect(healed.Status.PublishedVersions[0].BlueprintName).To(Equal("heal-bp"))
			g.Expect(healed.Status.PublishedVersions[0].Version).To(Equal("1.0.0"))
		}, timeout, interval).Should(Succeed())
	})

	It("should NOT self-heal when Blueprint is missing", func() {
		bundle := &aifv1.Bundle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "no-heal-bundle",
				Namespace: "default",
			},
			Spec: aifv1.BundleSpec{
				Title:           "No Heal Bundle",
				TargetBlueprint: "missing-bp",
				UseCase:         "rag",
				Components: []aifv1.ComponentRef{
					{
						Name: "app1",
						Kind: aifv1.ComponentKindApp,
						App: &aifv1.AppRef{
							Repo:    "https://example.com/charts",
							Chart:   "test-chart",
							Version: "1.0.0",
						},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, bundle)).To(Succeed())

		// Wait for initial reconciliation
		Eventually(func(g Gomega) {
			var fetched aifv1.Bundle
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(bundle), &fetched)).To(Succeed())
			g.Expect(fetched.Finalizers).To(ContainElement("ai.suse.com/cleanup"))
		}, timeout, interval).Should(Succeed())

		// Set to Submitted phase
		var fetched aifv1.Bundle
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(bundle), &fetched)).To(Succeed())
		fetched.Status.Phase = aifv1.BundlePhaseSubmitted
		fetched.Status.Submission = &aifv1.SubmissionStatus{
			ProposedVersion:    "1.0.0",
			ChangeDescription:  "Initial release",
			SubmittedBy:        "alice",
			SubmittedAt:        metav1.Now(),
			GenerationAtSubmit: fetched.Generation,
		}
		Expect(k8sClient.Status().Update(ctx, &fetched)).To(Succeed())

		// Trigger re-reconciliation by touching the spec (retry on conflict)
		Eventually(func() error {
			var fresh aifv1.Bundle
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(bundle), &fresh); err != nil {
				return err
			}
			if fresh.Spec.Description == "" {
				fresh.Spec.Description = "trigger"
			}
			return k8sClient.Update(ctx, &fresh)
		}, timeout, interval).Should(Succeed())

		// Wait for reconciliation to process, then verify phase is still Submitted
		Eventually(func(g Gomega) {
			var result aifv1.Bundle
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(bundle), &result)).To(Succeed())
			rc := findReady(result.Status.Conditions)
			g.Expect(rc).NotTo(BeNil())
		}, timeout, interval).Should(Succeed())

		// Consistently verify no healing occurred
		Consistently(func(g Gomega) {
			var result aifv1.Bundle
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(bundle), &result)).To(Succeed())
			g.Expect(result.Status.Phase).To(Equal(aifv1.BundlePhaseSubmitted))
			g.Expect(result.Status.Submission).NotTo(BeNil())
			g.Expect(result.Status.PublishedVersions).To(BeEmpty())
		}, 2*time.Second, interval).Should(Succeed())
	})

	It("should NOT self-heal when Blueprint is from a different Bundle", func() {
		// Create Blueprint from a different namespace
		bp := &aifv1.Blueprint{
			ObjectMeta: metav1.ObjectMeta{
				Name: "nomatch-bp.1.0.0",
			},
			Spec: aifv1.BlueprintSpec{
				BlueprintName: "nomatch-bp",
				Version:       "1.0.0",
				UseCase:       "rag",
				Source: aifv1.BlueprintSource{
					Type: aifv1.BlueprintSourcePublished,
					PublishedFrom: &aifv1.PublishedFromRef{
						BundleNamespace:  "different-ns",
						BundleName:       "nomatch-heal-bundle",
						BundleGeneration: 1,
					},
				},
				Components: []aifv1.ComponentRef{
					{
						Name: "app1",
						Kind: aifv1.ComponentKindApp,
						App: &aifv1.AppRef{
							Repo:    "https://example.com/charts",
							Chart:   "test-chart",
							Version: "1.0.0",
						},
					},
				},
				PublishedBy: "approver",
				PublishedAt: metav1.Now(),
			},
		}
		Expect(k8sClient.Create(ctx, bp)).To(Succeed())

		bundle := &aifv1.Bundle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "nomatch-heal-bundle",
				Namespace: "default",
			},
			Spec: aifv1.BundleSpec{
				Title:           "No Match Bundle",
				TargetBlueprint: "nomatch-bp",
				UseCase:         "rag",
				Components: []aifv1.ComponentRef{
					{
						Name: "app1",
						Kind: aifv1.ComponentKindApp,
						App: &aifv1.AppRef{
							Repo:    "https://example.com/charts",
							Chart:   "test-chart",
							Version: "1.0.0",
						},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, bundle)).To(Succeed())

		// Wait for initial reconciliation
		Eventually(func(g Gomega) {
			var fetched aifv1.Bundle
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(bundle), &fetched)).To(Succeed())
			g.Expect(fetched.Finalizers).To(ContainElement("ai.suse.com/cleanup"))
		}, timeout, interval).Should(Succeed())

		// Set to Submitted phase
		var fetched aifv1.Bundle
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(bundle), &fetched)).To(Succeed())
		fetched.Status.Phase = aifv1.BundlePhaseSubmitted
		fetched.Status.Submission = &aifv1.SubmissionStatus{
			ProposedVersion:    "1.0.0",
			ChangeDescription:  "Initial release",
			SubmittedBy:        "alice",
			SubmittedAt:        metav1.Now(),
			GenerationAtSubmit: fetched.Generation,
		}
		Expect(k8sClient.Status().Update(ctx, &fetched)).To(Succeed())

		// Trigger re-reconciliation by touching the spec (retry on conflict)
		Eventually(func() error {
			var fresh aifv1.Bundle
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(bundle), &fresh); err != nil {
				return err
			}
			if fresh.Spec.Description == "" {
				fresh.Spec.Description = "trigger"
			}
			return k8sClient.Update(ctx, &fresh)
		}, timeout, interval).Should(Succeed())

		// Wait for reconciliation, then verify no healing
		Eventually(func(g Gomega) {
			var result aifv1.Bundle
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(bundle), &result)).To(Succeed())
			rc := findReady(result.Status.Conditions)
			g.Expect(rc).NotTo(BeNil())
		}, timeout, interval).Should(Succeed())

		Consistently(func(g Gomega) {
			var result aifv1.Bundle
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(bundle), &result)).To(Succeed())
			g.Expect(result.Status.Phase).To(Equal(aifv1.BundlePhaseSubmitted))
			g.Expect(result.Status.Submission).NotTo(BeNil())
			g.Expect(result.Status.PublishedVersions).To(BeEmpty())
		}, 2*time.Second, interval).Should(Succeed())
	})
})
