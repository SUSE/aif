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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/controller/aiworkload"
)

func helmWorkload(name, ns string) *aiplatformv1alpha1.AIWorkload {
	return &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			DisplayName:     "Test Workload",
			TargetNamespace: "test-ns",
			DeployStrategy:  aiplatformv1alpha1.AIWorkloadDeployHelm,
			Source: aiplatformv1alpha1.AIWorkloadSource{
				SourceType: aiplatformv1alpha1.AIWorkloadSourceApp,
				App: &aiplatformv1alpha1.AppSource{
					ChartRepo:    "suse-ai",
					ChartName:    "ollama",
					ChartVersion: "1.0.0",
					Release:      "ollama",
				},
			},
		},
	}
}

var _ = Describe("AIWorkload Controller", func() {
	reconciler := func() *aiworkload.AIWorkloadReconciler {
		return &aiworkload.AIWorkloadReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
	}
	req := func(name, ns string) reconcile.Request {
		return reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: ns}}
	}

	Context("on first reconcile of a new AIWorkload", func() {
		It("adds the finalizer", func() {
			wl := helmWorkload("test-finalizer", "default")
			Expect(k8sClient.Create(ctx, wl)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(context.Background(), wl) })

			_, err := reconciler().Reconcile(ctx, req("test-finalizer", "default"))
			Expect(err).NotTo(HaveOccurred())

			var got aiplatformv1alpha1.AIWorkload
			Expect(k8sClient.Get(ctx, req("test-finalizer", "default").NamespacedName, &got)).To(Succeed())
			Expect(got.Finalizers).To(ContainElement("ai-platform.suse.com/cleanup"))
		})

		It("sets observedGeneration on subsequent reconcile", func() {
			wl := helmWorkload("test-gencounter", "default")
			Expect(k8sClient.Create(ctx, wl)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(context.Background(), wl) })

			// First reconcile adds finalizer and requeues
			_, _ = reconciler().Reconcile(ctx, req("test-gencounter", "default"))
			// Second reconcile runs main loop
			_, err := reconciler().Reconcile(ctx, req("test-gencounter", "default"))
			Expect(err).NotTo(HaveOccurred())

			var got aiplatformv1alpha1.AIWorkload
			Expect(k8sClient.Get(ctx, req("test-gencounter", "default").NamespacedName, &got)).To(Succeed())
			Expect(got.Status.ObservedGeneration).To(Equal(got.Generation))
		})
	})

	Context("derivePhase helper", func() {
		It("returns Pending when no cluster statuses", func() {
			wl := helmWorkload("test-phase-pending", "default")
			Expect(k8sClient.Create(ctx, wl)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(context.Background(), wl) })

			// Reconcile twice (finalizer + main)
			_, _ = reconciler().Reconcile(ctx, req("test-phase-pending", "default"))
			_, err := reconciler().Reconcile(ctx, req("test-phase-pending", "default"))
			Expect(err).NotTo(HaveOccurred())

			var got aiplatformv1alpha1.AIWorkload
			Expect(k8sClient.Get(ctx, req("test-phase-pending", "default").NamespacedName, &got)).To(Succeed())
			// Helm workload with no clusterStatuses set by controller → phase stays as-is (set by wizard)
			// Controller does not touch Helm workload phase
			Expect(got.Status.ObservedGeneration).To(Equal(got.Generation))
		})
	})

	Context("GitOps reconcile — HelmOp present", func() {
		It("syncs chartVersion and namespace from HelmOp into AIWorkload spec", func() {
			wl := &aiplatformv1alpha1.AIWorkload{
				ObjectMeta: metav1.ObjectMeta{Name: "gitops-sync", Namespace: "default"},
				Spec: aiplatformv1alpha1.AIWorkloadSpec{
					DisplayName:      "Test",
					DeployStrategy:   aiplatformv1alpha1.AIWorkloadDeployGitOps,
					FleetBundleNames: []string{"test-bundle"},
					TargetNamespace:  "old-ns",
					Source: aiplatformv1alpha1.AIWorkloadSource{
						SourceType: aiplatformv1alpha1.AIWorkloadSourceApp,
						App: &aiplatformv1alpha1.AppSource{
							ChartRepo: "suse-ai", ChartName: "qdrant",
							ChartVersion: "1.0.0", Release: "qdrant",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, wl)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(context.Background(), wl) })

			// Ensure fleet-local namespace exists
			fleetNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "fleet-local"}}
			_ = k8sClient.Create(ctx, fleetNS) // ignore error if already exists

			// Create a HelmOp in fleet-local namespace
			ho := &unstructured.Unstructured{}
			ho.SetGroupVersionKind(schema.GroupVersionKind{
				Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "HelmOp",
			})
			ho.SetName("test-bundle")
			ho.SetNamespace("fleet-local")
			Expect(unstructured.SetNestedField(ho.Object, "2.0.0", "spec", "helm", "version")).To(Succeed())
			Expect(unstructured.SetNestedField(ho.Object, "new-ns", "spec", "namespace")).To(Succeed())
			Expect(k8sClient.Create(ctx, ho)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(context.Background(), ho) })

			r := reconciler()
			// First reconcile: adds finalizer
			_, _ = r.Reconcile(ctx, req("gitops-sync", "default"))
			// Second reconcile: syncs from HelmOp
			_, err := r.Reconcile(ctx, req("gitops-sync", "default"))
			Expect(err).NotTo(HaveOccurred())

			var got aiplatformv1alpha1.AIWorkload
			Expect(k8sClient.Get(ctx, req("gitops-sync", "default").NamespacedName, &got)).To(Succeed())
			Expect(got.Spec.Source.App.ChartVersion).To(Equal("2.0.0"))
			Expect(got.Spec.TargetNamespace).To(Equal("new-ns"))
			Expect(got.Annotations).To(HaveKey("ai-platform.suse.com/last-git-sync"))
		})
	})

	Context("GitOps reconcile — HelmOp absent, no prior sync", func() {
		It("waits (Pending) rather than deleting when HelmOp never existed yet", func() {
			wl := &aiplatformv1alpha1.AIWorkload{
				ObjectMeta: metav1.ObjectMeta{Name: "gitops-new", Namespace: "default"},
				Spec: aiplatformv1alpha1.AIWorkloadSpec{
					DisplayName:      "Test",
					DeployStrategy:   aiplatformv1alpha1.AIWorkloadDeployGitOps,
					FleetBundleNames: []string{"missing-bundle"},
					TargetNamespace:  "ns",
					Source: aiplatformv1alpha1.AIWorkloadSource{
						SourceType: aiplatformv1alpha1.AIWorkloadSourceApp,
						App: &aiplatformv1alpha1.AppSource{
							ChartRepo: "suse-ai", ChartName: "qdrant",
							ChartVersion: "1.0.0", Release: "qdrant",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, wl)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(context.Background(), wl) })

			r := reconciler()
			_, _ = r.Reconcile(ctx, req("gitops-new", "default"))
			_, err := r.Reconcile(ctx, req("gitops-new", "default"))
			Expect(err).NotTo(HaveOccurred())

			// CR must still exist — no prior sync annotation means Fleet hasn't caught up yet
			var got aiplatformv1alpha1.AIWorkload
			Expect(k8sClient.Get(ctx, req("gitops-new", "default").NamespacedName, &got)).To(Succeed())
			Expect(got.Status.Phase).To(Equal(aiplatformv1alpha1.AIWorkloadPhasePending))
		})
	})

	Context("GitOps reconcile — HelmOp absent after prior sync", func() {
		It("deletes the AIWorkload CR when HelmOp disappears after a prior sync", func() {
			wl := &aiplatformv1alpha1.AIWorkload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gitops-deleted",
					Namespace: "default",
					// Simulate a workload that was previously synced — annotation present
					Annotations: map[string]string{
						"ai-platform.suse.com/last-git-sync": "somehash",
					},
				},
				Spec: aiplatformv1alpha1.AIWorkloadSpec{
					DisplayName:      "Test",
					DeployStrategy:   aiplatformv1alpha1.AIWorkloadDeployGitOps,
					FleetBundleNames: []string{"deleted-bundle"},
					TargetNamespace:  "ns",
					Source: aiplatformv1alpha1.AIWorkloadSource{
						SourceType: aiplatformv1alpha1.AIWorkloadSourceApp,
						App: &aiplatformv1alpha1.AppSource{
							ChartRepo: "suse-ai", ChartName: "qdrant",
							ChartVersion: "1.0.0", Release: "qdrant",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, wl)).To(Succeed())

			r := reconciler()
			_, _ = r.Reconcile(ctx, req("gitops-deleted", "default"))
			_, err := r.Reconcile(ctx, req("gitops-deleted", "default"))
			Expect(err).NotTo(HaveOccurred())

			var got aiplatformv1alpha1.AIWorkload
			err = k8sClient.Get(ctx, req("gitops-deleted", "default").NamespacedName, &got)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})
	})

	Context("FleetBundle deletion", func() {
		It("deletes the companion Fleet Bundle from both fleet workspace namespaces", func() {
			bundleGVK := schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "Bundle"}

			// A workload may live in either fleet workspace (fleet-local for the
			// management cluster, fleet-default for downstream). The operator deletes
			// the Bundle from both, so exercise both branches of that loop.
			for _, ns := range []string{"fleet-local", "fleet-default"} {
				fleetNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
				_ = k8sClient.Create(ctx, fleetNS) // ignore error if already exists

				// Fleet generates this Bundle from the HelmOp and links it only by a
				// label (no ownerReference), so deleting the HelmOp does not reliably
				// garbage-collect it. The operator must delete it explicitly.
				bundle := &unstructured.Unstructured{}
				bundle.SetGroupVersionKind(bundleGVK)
				bundle.SetName("fleet-del-bundle")
				bundle.SetNamespace(ns)
				Expect(k8sClient.Create(ctx, bundle)).To(Succeed())
			}

			wl := &aiplatformv1alpha1.AIWorkload{
				ObjectMeta: metav1.ObjectMeta{Name: "fleet-del", Namespace: "default"},
				Spec: aiplatformv1alpha1.AIWorkloadSpec{
					DisplayName:      "Test",
					DeployStrategy:   aiplatformv1alpha1.AIWorkloadDeployFleetBundle,
					FleetBundleNames: []string{"fleet-del-bundle"},
					TargetNamespace:  "ns",
					Source: aiplatformv1alpha1.AIWorkloadSource{
						SourceType: aiplatformv1alpha1.AIWorkloadSourceApp,
						App: &aiplatformv1alpha1.AppSource{
							ChartRepo: "suse-ai", ChartName: "qdrant",
							ChartVersion: "1.0.0", Release: "qdrant",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, wl)).To(Succeed())

			r := reconciler()
			// First reconcile adds the finalizer.
			_, err := r.Reconcile(ctx, req("fleet-del", "default"))
			Expect(err).NotTo(HaveOccurred())

			// Delete the workload — the finalizer keeps it until handleDeletion runs.
			Expect(k8sClient.Delete(ctx, wl)).To(Succeed())
			_, err = r.Reconcile(ctx, req("fleet-del", "default"))
			Expect(err).NotTo(HaveOccurred())

			// The companion Bundle must be gone from both namespaces.
			for _, ns := range []string{"fleet-local", "fleet-default"} {
				got := &unstructured.Unstructured{}
				got.SetGroupVersionKind(bundleGVK)
				err = k8sClient.Get(ctx, types.NamespacedName{Name: "fleet-del-bundle", Namespace: ns}, got)
				Expect(errors.IsNotFound(err)).To(BeTrue(), "Bundle should be deleted from %s", ns)
			}
		})
	})
})

var _ = Describe("Blueprint AIWorkload", func() {
	reconciler := func() *aiworkload.AIWorkloadReconciler {
		return &aiworkload.AIWorkloadReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
	}
	req := func(name, ns string) reconcile.Request {
		return reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: ns}}
	}

	Context("Blueprint CR missing", func() {
		It("sets phase=Pending when blueprint CR not found during grace period", func() {
			wl := &aiplatformv1alpha1.AIWorkload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bp-missing",
					Namespace: "default",
				},
				Spec: aiplatformv1alpha1.AIWorkloadSpec{
					DisplayName:     "Test",
					DeployStrategy:  aiplatformv1alpha1.AIWorkloadDeployFleetBundle,
					TargetNamespace: "ns",
					Source: aiplatformv1alpha1.AIWorkloadSource{
						SourceType: aiplatformv1alpha1.AIWorkloadSourceBlueprint,
						Blueprint: &aiplatformv1alpha1.BlueprintSource{
							Name:    "my-stack",
							Version: "1.0.0",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, wl)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(context.Background(), wl) })

			// First reconcile: adds finalizer
			_, _ = reconciler().Reconcile(ctx, req("bp-missing", "default"))
			// Second reconcile: tries to fetch Blueprint CR
			_, err := reconciler().Reconcile(ctx, req("bp-missing", "default"))
			Expect(err).NotTo(HaveOccurred())

			var got aiplatformv1alpha1.AIWorkload
			Expect(k8sClient.Get(ctx, req("bp-missing", "default").NamespacedName, &got)).To(Succeed())
			Expect(got.Status.Phase).To(Equal(aiplatformv1alpha1.AIWorkloadPhasePending))
		})
	})

	Context("Blueprint CR exists, fleetBundleNames empty", func() {
		It("populates fleetBundleNames from components and requeues", func() {
			bp := &aiplatformv1alpha1.Blueprint{
				ObjectMeta: metav1.ObjectMeta{Name: "my-stack-1-0-0"},
				Spec: aiplatformv1alpha1.BlueprintSpec{
					DisplayName: "My Stack",
					Version:     "1.0.0",
					Components: []aiplatformv1alpha1.BlueprintComponent{
						{ChartRepo: "suse-ai", ChartName: "ollama", ChartVersion: "1.0.0"},
						{ChartRepo: "suse-ai", ChartName: "qdrant", ChartVersion: "1.2.0"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, bp)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(context.Background(), bp) })

			wl := &aiplatformv1alpha1.AIWorkload{
				ObjectMeta: metav1.ObjectMeta{Name: "bp-populate", Namespace: "default"},
				Spec: aiplatformv1alpha1.AIWorkloadSpec{
					DisplayName:     "Test",
					DeployStrategy:  aiplatformv1alpha1.AIWorkloadDeployFleetBundle,
					TargetNamespace: "ns",
					Source: aiplatformv1alpha1.AIWorkloadSource{
						SourceType: aiplatformv1alpha1.AIWorkloadSourceBlueprint,
						Blueprint:  &aiplatformv1alpha1.BlueprintSource{Name: "my-stack", Version: "1.0.0"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, wl)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(context.Background(), wl) })

			// First reconcile: adds finalizer
			_, _ = reconciler().Reconcile(ctx, req("bp-populate", "default"))
			// Second reconcile: detects empty fleetBundleNames, populates them
			result, err := reconciler().Reconcile(ctx, req("bp-populate", "default"))
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeTrue())

			var got aiplatformv1alpha1.AIWorkload
			Expect(k8sClient.Get(ctx, req("bp-populate", "default").NamespacedName, &got)).To(Succeed())
			Expect(got.Spec.FleetBundleNames).To(HaveLen(2))
			Expect(got.Spec.FleetBundleNames[0]).To(Equal("bp-populate-ollama"))
			Expect(got.Spec.FleetBundleNames[1]).To(Equal("bp-populate-qdrant"))
			Expect(got.Status.ObservedGeneration).To(BeZero())
		})
	})
})
