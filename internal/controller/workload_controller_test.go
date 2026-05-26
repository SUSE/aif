package controller_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	stderrors "errors"
	"log/slog"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	fleetv1 "github.com/rancher/fleet/pkg/apis/fleet.cattle.io/v1alpha1"
	"github.com/rancher/wrangler/v3/pkg/genericcondition"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/blueprint"
	"github.com/SUSE/aif/pkg/conditions"
	"github.com/SUSE/aif/pkg/fleet"
	"github.com/SUSE/aif/pkg/helm"
	"github.com/SUSE/aif/pkg/nvidia"
	"github.com/SUSE/aif/pkg/workload"
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

	It("should reconcile a valid App Workload to Pending/Installed", func() {
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

		fakeDeployer.SetDeployResult(workload.DeployResult{
			Components: []workload.ComponentRelease{
				{Name: "n", ReleaseName: "n-1", Status: "deployed"},
			},
		})
		Expect(k8sClient.Create(ctx, w)).To(Succeed())

		Eventually(func(g Gomega) {
			var fetched aifv1.Workload
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(w), &fetched)).To(Succeed())
			g.Expect(fetched.Status.Phase).To(Equal(aifv1.WorkloadPhaseRunning))
			g.Expect(fetched.Status.ObservedGeneration).To(Equal(fetched.Generation))
			rc := findReady(fetched.Status.Conditions)
			g.Expect(rc).NotTo(BeNil())
			g.Expect(rc.Status).To(Equal(metav1.ConditionTrue))
			g.Expect(rc.Reason).To(Equal(conditions.ReasonWorkloadRunning))
		}, timeout, interval).Should(Succeed())

		// Verify finalizer
		var fetched aifv1.Workload
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(w), &fetched)).To(Succeed())
		Expect(fetched.Finalizers).To(ContainElement("ai.suse.com/cleanup"))
	})

	It("should reconcile a valid Blueprint Workload to Pending/Installed", func() {
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

		fakeDeployer.SetDeployResult(workload.DeployResult{
			Components: []workload.ComponentRelease{
				{Name: "n", ReleaseName: "n-1", Status: "deployed"},
			},
		})
		Expect(k8sClient.Create(ctx, w)).To(Succeed())

		Eventually(func(g Gomega) {
			var fetched aifv1.Workload
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(w), &fetched)).To(Succeed())
			g.Expect(fetched.Status.Phase).To(Equal(aifv1.WorkloadPhaseRunning))
			rc := findReady(fetched.Status.Conditions)
			g.Expect(rc).NotTo(BeNil())
			g.Expect(rc.Status).To(Equal(metav1.ConditionTrue))
			g.Expect(rc.Reason).To(Equal(conditions.ReasonWorkloadRunning))
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

var _ = Describe("Workload deployer error → condition mapping (P4-2)", func() {
	const timeout = 30 * time.Second
	const interval = 250 * time.Millisecond
	ctx := context.Background()

	var key client.ObjectKey

	BeforeEach(func() {
		// Baseline App-source workload that each spec mutates the FakeDeployer for.
		w := &aifv1.Workload{
			ObjectMeta: metav1.ObjectMeta{Name: "wid-err-" + randomSuffix(), Namespace: "default"},
			Spec: aifv1.WorkloadSpec{
				Name: "n",
				Source: aifv1.WorkloadSource{
					Kind: aifv1.WorkloadSourceKindApp,
					App:  &aifv1.AppRef{Repo: "r", Chart: "c", Version: "1"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, w)).To(Succeed())
		key = client.ObjectKeyFromObject(w)
	})

	AfterEach(func() {
		// Remove finalizer so deletion completes without Teardown wiring (Task 28).
		var fetched aifv1.Workload
		if err := k8sClient.Get(ctx, key, &fetched); err == nil {
			fetched.Finalizers = nil
			_ = k8sClient.Update(ctx, &fetched)
			_ = k8sClient.Delete(ctx, &fetched)
		}
	})

	eventuallyReason := func() string {
		var fetched aifv1.Workload
		if err := k8sClient.Get(ctx, key, &fetched); err != nil {
			return ""
		}
		for _, c := range fetched.Status.Conditions {
			if c.Type == conditions.TypeReady {
				return c.Reason
			}
		}
		return ""
	}

	It("maps ErrNestedBlueprintNotSupported to UnsupportedComposition (terminal)", func() {
		fakeDeployer.SetDeployErr(workload.ErrNestedBlueprintNotSupported)
		fakeDeployer.SetDeployResult(workload.DeployResult{})

		Eventually(eventuallyReason, timeout, interval).Should(Equal(conditions.ReasonUnsupportedComposition))
	})

	It("maps ErrSourceNotResolved to SourceNotResolved", func() {
		fakeDeployer.SetDeployErr(workload.ErrSourceNotResolved)
		fakeDeployer.SetDeployResult(workload.DeployResult{})

		Eventually(eventuallyReason, timeout, interval).Should(Equal(conditions.ReasonSourceNotResolved))
	})

	It("maps ErrComponentInstallFailed to ComponentInstallFailed", func() {
		fakeDeployer.SetDeployErr(workload.ErrComponentInstallFailed)
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Components: []workload.ComponentRelease{
				{Name: "n", ReleaseName: "n-1", Status: "failed"},
			},
		})

		Eventually(eventuallyReason, timeout, interval).Should(Equal(conditions.ReasonComponentInstallFailed))
	})

	It("maps ErrComponentUninstallFailed to OrphanCleanupPending", func() {
		fakeDeployer.SetDeployErr(workload.ErrComponentUninstallFailed)
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Components: []workload.ComponentRelease{
				{Name: "n", ReleaseName: "n-1", Status: workload.ComponentStatusOrphanUninstallFailed},
			},
		})

		Eventually(eventuallyReason, timeout, interval).Should(Equal(conditions.ReasonOrphanCleanupPending))
	})

	It("maps unclassified errors to ReconcileFailed", func() {
		fakeDeployer.SetDeployErr(stderrors.New("some unexpected error"))
		fakeDeployer.SetDeployResult(workload.DeployResult{})

		Eventually(eventuallyReason, timeout, interval).Should(Equal(conditions.ReasonReconcileFailed))
	})

	It("sets Ready=True Reason=WorkloadRunning when Deploy succeeds and components are all deployed", func() {
		fakeDeployer.SetDeployErr(nil)
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Components: []workload.ComponentRelease{{Name: "n", ReleaseName: "n-1", Status: "deployed"}},
		})

		Eventually(eventuallyReason, timeout, interval).Should(Equal(conditions.ReasonWorkloadRunning))
	})

	It("sets Reason=WorkloadDeploying when Deploy returns nil but components are in-flight", func() {
		fakeDeployer.SetDeployErr(nil)
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Components: []workload.ComponentRelease{{Name: "n", ReleaseName: "n-1", Status: "pending-install"}},
		})

		Eventually(eventuallyReason, timeout, interval).Should(Equal(conditions.ReasonWorkloadDeploying))
	})
})

var _ = Describe("Workload finalizer cleanup (P4-2)", func() {
	const timeout = 30 * time.Second
	const interval = 250 * time.Millisecond
	ctx := context.Background()

	It("calls Deployer.Teardown on delete and removes finalizer when Teardown succeeds", func() {
		fakeDeployer.SetTeardownErr(nil)
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Components: []workload.ComponentRelease{
				{Name: "n", ReleaseName: "wid-fin-n", Status: "deployed"},
			},
		})

		w := &aifv1.Workload{
			ObjectMeta: metav1.ObjectMeta{Name: "wid-fin-" + randomSuffix(), Namespace: "default"},
			Spec: aifv1.WorkloadSpec{
				Name: "n",
				Source: aifv1.WorkloadSource{Kind: aifv1.WorkloadSourceKindApp, App: &aifv1.AppRef{Repo: "r", Chart: "c", Version: "1"}},
			},
		}
		Expect(k8sClient.Create(ctx, w)).To(Succeed())
		key := client.ObjectKeyFromObject(w)

		// Wait for finalizer to be added AND ComponentReleases populated.
		Eventually(func(g Gomega) {
			var got aifv1.Workload
			g.Expect(k8sClient.Get(ctx, key, &got)).To(Succeed())
			g.Expect(got.Finalizers).To(ContainElement("ai.suse.com/cleanup"))
			g.Expect(got.Status.ComponentReleases).To(HaveLen(1))
		}, timeout, interval).Should(Succeed())

		teardownCallsBefore := len(fakeDeployer.GetTeardownCalls())
		Expect(k8sClient.Delete(ctx, w)).To(Succeed())

		// Teardown should be called eventually with the recorded release.
		Eventually(func() int {
			return len(fakeDeployer.GetTeardownCalls())
		}, timeout, interval).Should(BeNumerically(">", teardownCallsBefore))

		calls := fakeDeployer.GetTeardownCalls()
		last := calls[len(calls)-1]
		Expect(last.Namespace).To(Equal("default"))
		Expect(last.Releases).To(HaveLen(1))
		Expect(last.Releases[0].ReleaseName).To(Equal("wid-fin-n"))

		// Eventually the Workload should be gone (finalizer removed).
		Eventually(func() bool {
			var got aifv1.Workload
			err := k8sClient.Get(ctx, key, &got)
			return errors.IsNotFound(err)
		}, timeout, interval).Should(BeTrue())
	})

	It("keeps finalizer when Teardown fails (Workload not deleted until success)", func() {
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Components: []workload.ComponentRelease{
				{Name: "n", ReleaseName: "wid-fin2-n", Status: "deployed"},
			},
		})

		w := &aifv1.Workload{
			ObjectMeta: metav1.ObjectMeta{Name: "wid-fin2-" + randomSuffix(), Namespace: "default"},
			Spec: aifv1.WorkloadSpec{
				Name: "n",
				Source: aifv1.WorkloadSource{Kind: aifv1.WorkloadSourceKindApp, App: &aifv1.AppRef{Repo: "r", Chart: "c", Version: "1"}},
			},
		}
		Expect(k8sClient.Create(ctx, w)).To(Succeed())
		key := client.ObjectKeyFromObject(w)

		Eventually(func(g Gomega) {
			var got aifv1.Workload
			g.Expect(k8sClient.Get(ctx, key, &got)).To(Succeed())
			g.Expect(got.Finalizers).To(ContainElement("ai.suse.com/cleanup"))
			g.Expect(got.Status.ComponentReleases).To(HaveLen(1))
		}, timeout, interval).Should(Succeed())

		// Set TeardownErr right before deletion to avoid interference with Deploy phase
		fakeDeployer.SetTeardownErr(stderrors.New("teardown boom"))
		Expect(k8sClient.Delete(ctx, w)).To(Succeed())

		// First, wait for the reconciler to attempt deletion (DeletionTimestamp set)
		Eventually(func() bool {
			var got aifv1.Workload
			err := k8sClient.Get(ctx, key, &got)
			return err == nil && got.DeletionTimestamp != nil
		}, timeout, interval).Should(BeTrue())

		// Workload should NOT disappear; finalizer holds while Teardown errors.
		Consistently(func() bool {
			var got aifv1.Workload
			err := k8sClient.Get(ctx, key, &got)
			return err == nil && got.DeletionTimestamp != nil && len(got.Finalizers) > 0
		}, "3s", interval).Should(BeTrue())

		// Clear the err and let it succeed, so test cleanup completes.
		fakeDeployer.SetTeardownErr(nil)
		Eventually(func() bool {
			var got aifv1.Workload
			err := k8sClient.Get(ctx, key, &got)
			return errors.IsNotFound(err)
		}, timeout, interval).Should(BeTrue())
	})
})

var _ = Describe("Workload deployer events (P4-2)", func() {
	const timeout = 30 * time.Second
	const interval = 250 * time.Millisecond
	ctx := context.Background()

	eventReasons := func(w *aifv1.Workload) []string {
		var events corev1.EventList
		_ = k8sClient.List(ctx, &events, client.InNamespace(w.Namespace))
		var reasons []string
		for _, e := range events.Items {
			if e.InvolvedObject.UID == w.UID {
				reasons = append(reasons, e.Reason)
			}
		}
		return reasons
	}

	It("emits Running event on Deploying→Running transition", func() {
		// First reconcile: Deploying — driven by component Status="pending-install"
		// which RecomputePhase rule 2 maps to PhaseDeploying.
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Components: []workload.ComponentRelease{{Name: "n", Status: "pending-install"}},
		})

		name := "wid-evt-" + randomSuffix()
		w := &aifv1.Workload{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec:       aifv1.WorkloadSpec{Name: "n", Source: aifv1.WorkloadSource{Kind: aifv1.WorkloadSourceKindApp, App: &aifv1.AppRef{Repo: "r", Chart: "c", Version: "1"}}},
		}
		Expect(k8sClient.Create(ctx, w)).To(Succeed())
		key := client.ObjectKeyFromObject(w)

		Eventually(func(g Gomega) {
			var got aifv1.Workload
			g.Expect(k8sClient.Get(ctx, key, &got)).To(Succeed())
			g.Expect(string(got.Status.Phase)).To(Equal(string(aifv1.WorkloadPhaseDeploying)))
		}, timeout, interval).Should(Succeed())

		// Switch fake to Running — all components deployed, ReadyReplicas
		// synthesised to DesiredReplicas (pre-P5-2 default), so rule 4 maps to Running.
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Components: []workload.ComponentRelease{{Name: "n", Status: "deployed"}},
		})

		// Touch the Workload so the reconciler re-runs (status update doesn't auto-trigger a re-reconcile in envtest).
		Eventually(func(g Gomega) {
			var got aifv1.Workload
			g.Expect(k8sClient.Get(ctx, key, &got)).To(Succeed())
			if got.Annotations == nil {
				got.Annotations = map[string]string{}
			}
			got.Annotations["touch"] = "1"
			g.Expect(k8sClient.Update(ctx, &got)).To(Succeed())
		}, timeout, interval).Should(Succeed())

		Eventually(func(g Gomega) {
			var got aifv1.Workload
			g.Expect(k8sClient.Get(ctx, key, &got)).To(Succeed())
			g.Expect(string(got.Status.Phase)).To(Equal(string(aifv1.WorkloadPhaseRunning)))
		}, timeout, interval).Should(Succeed())

		// Verify event present.
		Eventually(func() []string {
			var got aifv1.Workload
			_ = k8sClient.Get(ctx, key, &got)
			return eventReasons(&got)
		}, timeout, interval).Should(ContainElement("Running"))

		// Cleanup
		Expect(k8sClient.Delete(ctx, w)).To(Succeed())
	})

})

var _ = Describe("Workload deployer envtest scenarios (P4-2)", func() {
	const timeout = 30 * time.Second
	const interval = 250 * time.Millisecond
	ctx := context.Background()

	It("App-source workload reaches Running", func() {
		name := "wid-e2e1-" + randomSuffix()
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Components: []workload.ComponentRelease{
				{Name: "n", ReleaseName: name + "-n", ChartRef: "oci://r/c:1", Status: "deployed", Revision: 1},
			},
		})

		w := &aifv1.Workload{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: aifv1.WorkloadSpec{
				Name: "n",
				Source: aifv1.WorkloadSource{Kind: aifv1.WorkloadSourceKindApp, App: &aifv1.AppRef{Repo: "r", Chart: "c", Version: "1"}},
			},
		}
		Expect(k8sClient.Create(ctx, w)).To(Succeed())
		key := client.ObjectKeyFromObject(w)

		Eventually(func() aifv1.WorkloadPhase {
			var got aifv1.Workload
			_ = k8sClient.Get(ctx, key, &got)
			return got.Status.Phase
		}, timeout, interval).Should(Equal(aifv1.WorkloadPhaseRunning))

		Eventually(func() metav1.ConditionStatus {
			var got aifv1.Workload
			_ = k8sClient.Get(ctx, key, &got)
			for _, c := range got.Status.Conditions {
				if c.Type == conditions.TypeReady {
					return c.Status
				}
			}
			return metav1.ConditionUnknown
		}, timeout, interval).Should(Equal(metav1.ConditionTrue))

		var got aifv1.Workload
		Expect(k8sClient.Get(ctx, key, &got)).To(Succeed())
		Expect(got.Status.ComponentReleases).To(HaveLen(1))
		Expect(got.Status.ComponentReleases[0].Status).To(Equal("deployed"))

		Expect(k8sClient.Delete(ctx, w)).To(Succeed())
	})

	It("Blueprint-source workload records 3 components in source order", func() {
		name := "wid-e2e2-" + randomSuffix()
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Components: []workload.ComponentRelease{
				{Name: "llm", ReleaseName: name + "-llm", Status: "deployed"},
				{Name: "vec", ReleaseName: name + "-vec", Status: "deployed"},
				{Name: "ret", ReleaseName: name + "-ret", Status: "deployed"},
			},
		})

		w := &aifv1.Workload{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: aifv1.WorkloadSpec{
				Name: "rag",
				Source: aifv1.WorkloadSource{
					Kind:      aifv1.WorkloadSourceKindBlueprint,
					Blueprint: &aifv1.BlueprintRef{Name: "rag", Version: "1.0"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, w)).To(Succeed())
		key := client.ObjectKeyFromObject(w)

		Eventually(func() int {
			var got aifv1.Workload
			_ = k8sClient.Get(ctx, key, &got)
			return len(got.Status.ComponentReleases)
		}, timeout, interval).Should(Equal(3))

		var got aifv1.Workload
		Expect(k8sClient.Get(ctx, key, &got)).To(Succeed())
		Expect([]string{
			got.Status.ComponentReleases[0].Name,
			got.Status.ComponentReleases[1].Name,
			got.Status.ComponentReleases[2].Name,
		}).To(Equal([]string{"llm", "vec", "ret"}))

		Expect(k8sClient.Delete(ctx, w)).To(Succeed())
	})

	It("Drift cleanup: orphan release removed when Workload patched", func() {
		name := "wid-e2e3-" + randomSuffix()
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Components: []workload.ComponentRelease{
				{Name: "n", ReleaseName: name + "-n", Status: "deployed"},
				{Name: "old", ReleaseName: name + "-old", Status: "deployed"},
			},
		})

		w := &aifv1.Workload{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: aifv1.WorkloadSpec{
				Name: "n",
				Source: aifv1.WorkloadSource{Kind: aifv1.WorkloadSourceKindApp, App: &aifv1.AppRef{Repo: "r", Chart: "c", Version: "1"}},
			},
		}
		Expect(k8sClient.Create(ctx, w)).To(Succeed())
		key := client.ObjectKeyFromObject(w)

		Eventually(func() int {
			var got aifv1.Workload
			_ = k8sClient.Get(ctx, key, &got)
			return len(got.Status.ComponentReleases)
		}, timeout, interval).Should(Equal(2))

		// Re-configure fake to return only "n" — simulates spec drift dropping "old".
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Components: []workload.ComponentRelease{
				{Name: "n", ReleaseName: name + "-n", Status: "deployed"},
			},
		})

		// Trigger reconcile by patching annotation (status changes alone don't requeue).
		Eventually(func(g Gomega) {
			var got aifv1.Workload
			g.Expect(k8sClient.Get(ctx, key, &got)).To(Succeed())
			if got.Annotations == nil {
				got.Annotations = map[string]string{}
			}
			got.Annotations["touch"] = randomSuffix()
			g.Expect(k8sClient.Update(ctx, &got)).To(Succeed())
		}, timeout, interval).Should(Succeed())

		Eventually(func() int {
			var got aifv1.Workload
			_ = k8sClient.Get(ctx, key, &got)
			return len(got.Status.ComponentReleases)
		}, timeout, interval).Should(Equal(1))

		Expect(k8sClient.Delete(ctx, w)).To(Succeed())
	})

	It("Nested Blueprint surfaces UnsupportedComposition; phase Failed; stable across reconciles", func() {
		name := "wid-e2e4-" + randomSuffix()
		fakeDeployer.SetDeployErr(workload.ErrNestedBlueprintNotSupported)
		// Deploy errors short-circuit before ApplyDeployResult, so DeployResult
		// is unused on the error path. Phase comes from the controller's
		// error-classification block setting Ready=UnsupportedComposition.
		fakeDeployer.SetDeployResult(workload.DeployResult{})

		w := &aifv1.Workload{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: aifv1.WorkloadSpec{
				Name: "n",
				Source: aifv1.WorkloadSource{
					Kind:      aifv1.WorkloadSourceKindBlueprint,
					Blueprint: &aifv1.BlueprintRef{Name: "outer", Version: "1.0"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, w)).To(Succeed())
		key := client.ObjectKeyFromObject(w)

		readyReason := func() string {
			var got aifv1.Workload
			_ = k8sClient.Get(ctx, key, &got)
			for _, c := range got.Status.Conditions {
				if c.Type == conditions.TypeReady {
					return c.Reason
				}
			}
			return ""
		}

		Eventually(readyReason, timeout, interval).Should(Equal(conditions.ReasonUnsupportedComposition))

		// No requeue → reason should stay stable.
		Consistently(readyReason, "3s", interval).Should(Equal(conditions.ReasonUnsupportedComposition))

		// Clear err so deletion's Teardown succeeds (FakeDeployer's TeardownErr is nil by default).
		fakeDeployer.SetDeployErr(nil)
		Expect(k8sClient.Delete(ctx, w)).To(Succeed())
	})

	It("Source not yet present → SourceNotResolved; later available → Running", func() {
		name := "wid-e2e5-" + randomSuffix()
		fakeDeployer.SetDeployErr(workload.ErrSourceNotResolved)
		fakeDeployer.SetDeployResult(workload.DeployResult{})

		w := &aifv1.Workload{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: aifv1.WorkloadSpec{
				Name: "rag",
				Source: aifv1.WorkloadSource{
					Kind:      aifv1.WorkloadSourceKindBlueprint,
					Blueprint: &aifv1.BlueprintRef{Name: "missing", Version: "1.0"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, w)).To(Succeed())
		key := client.ObjectKeyFromObject(w)

		Eventually(func() string {
			var got aifv1.Workload
			_ = k8sClient.Get(ctx, key, &got)
			for _, c := range got.Status.Conditions {
				if c.Type == conditions.TypeReady {
					return c.Reason
				}
			}
			return ""
		}, timeout, interval).Should(Equal(conditions.ReasonSourceNotResolved))

		// Source becomes available — fake transitions to Running via deployed component.
		fakeDeployer.SetDeployErr(nil)
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Components: []workload.ComponentRelease{
				{Name: "rag", ReleaseName: name + "-rag", Status: "deployed"},
			},
		})

		// Trigger reconcile via annotation touch.
		Eventually(func(g Gomega) {
			var got aifv1.Workload
			g.Expect(k8sClient.Get(ctx, key, &got)).To(Succeed())
			if got.Annotations == nil {
				got.Annotations = map[string]string{}
			}
			got.Annotations["touch"] = randomSuffix()
			g.Expect(k8sClient.Update(ctx, &got)).To(Succeed())
		}, timeout, interval).Should(Succeed())

		Eventually(func() aifv1.WorkloadPhase {
			var got aifv1.Workload
			_ = k8sClient.Get(ctx, key, &got)
			return got.Status.Phase
		}, timeout, interval).Should(Equal(aifv1.WorkloadPhaseRunning))

		Expect(k8sClient.Delete(ctx, w)).To(Succeed())
	})

	It("preserves prior phase on un-classified errors", func() {
		name := "wid-preserve-" + randomSuffix()
		// First reconcile: succeed → Running (all components deployed, ready synthesised).
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Components: []workload.ComponentRelease{
				{Name: "n", ReleaseName: name + "-n", Status: "deployed"},
			},
		})

		w := &aifv1.Workload{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: aifv1.WorkloadSpec{
				Name: "n",
				Source: aifv1.WorkloadSource{
					Kind: aifv1.WorkloadSourceKindApp,
					App:  &aifv1.AppRef{Repo: "r", Chart: "c", Version: "1"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, w)).To(Succeed())
		key := client.ObjectKeyFromObject(w)

		Eventually(func() aifv1.WorkloadPhase {
			var got aifv1.Workload
			_ = k8sClient.Get(ctx, key, &got)
			return got.Status.Phase
		}, timeout, interval).Should(Equal(aifv1.WorkloadPhaseRunning))

		// Now flip to un-classified error → phase must stay Running.
		fakeDeployer.SetDeployErr(stderrors.New("transient bug"))
		fakeDeployer.SetDeployResult(workload.DeployResult{})

		// Trigger a re-reconcile.
		Eventually(func(g Gomega) {
			var got aifv1.Workload
			g.Expect(k8sClient.Get(ctx, key, &got)).To(Succeed())
			if got.Annotations == nil {
				got.Annotations = map[string]string{}
			}
			got.Annotations["touch"] = randomSuffix()
			g.Expect(k8sClient.Update(ctx, &got)).To(Succeed())
		}, timeout, interval).Should(Succeed())

		// Wait for reconcile + assert phase preserved.
		Eventually(func() string {
			var got aifv1.Workload
			_ = k8sClient.Get(ctx, key, &got)
			for _, c := range got.Status.Conditions {
				if c.Type == conditions.TypeReady {
					return c.Reason
				}
			}
			return ""
		}, timeout, interval).Should(Equal(conditions.ReasonReconcileFailed))

		var got aifv1.Workload
		Expect(k8sClient.Get(ctx, key, &got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(aifv1.WorkloadPhaseRunning))

		fakeDeployer.SetDeployErr(nil)
		Expect(k8sClient.Delete(ctx, w)).To(Succeed())
	})

	It("orphan uninstall failure surfaces Phase=Deploying", func() {
		name := "wid-orphan-fail-" + randomSuffix()
		// One orphan-uninstall-failed component triggers rule 2 ("any in-progress"
		// status) → PhaseDeploying via RecomputePhase. ErrComponentUninstallFailed
		// is the classified sentinel that maps to ReasonOrphanCleanupPending.
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Components: []workload.ComponentRelease{
				{Name: "n", ReleaseName: name + "-n", Status: "deployed"},
				{Name: "old", ReleaseName: name + "-old", Status: workload.ComponentStatusOrphanUninstallFailed},
			},
		})
		fakeDeployer.SetDeployErr(workload.ErrComponentUninstallFailed)

		w := &aifv1.Workload{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: aifv1.WorkloadSpec{
				Name: "n",
				Source: aifv1.WorkloadSource{Kind: aifv1.WorkloadSourceKindApp, App: &aifv1.AppRef{Repo: "r", Chart: "c", Version: "1"}},
			},
		}
		Expect(k8sClient.Create(ctx, w)).To(Succeed())
		key := client.ObjectKeyFromObject(w)

		Eventually(func() aifv1.WorkloadPhase {
			var got aifv1.Workload
			_ = k8sClient.Get(ctx, key, &got)
			return got.Status.Phase
		}, timeout, interval).Should(Equal(aifv1.WorkloadPhaseDeploying))

		Eventually(func() string {
			var got aifv1.Workload
			_ = k8sClient.Get(ctx, key, &got)
			for _, c := range got.Status.Conditions {
				if c.Type == conditions.TypeReady {
					return c.Reason
				}
			}
			return ""
		}, timeout, interval).Should(Equal(conditions.ReasonOrphanCleanupPending))

		fakeDeployer.SetDeployErr(nil)
		Expect(k8sClient.Delete(ctx, w)).To(Succeed())
	})
})

// randomSuffix returns a short random string suitable for unique resource
// names within a single test suite. Avoids cross-spec name collision since
// each spec creates a Workload that persists until the AfterEach removes
// finalizers + deletes.
func randomSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

var _ = Describe("WorkloadReconciler Fleet Bundle integration (P4-3b)", Serial, func() {
	const timeout = 30 * time.Second
	const interval = 250 * time.Millisecond
	ctx := context.Background()

	// Regression — wire-up guard for the controller→Deployer→FleetBundleEngine
	// chain end-to-end in envtest.
	//
	// The suite-wide WorkloadReconciler is wired with workload.FakeDeployer
	// (suite_test.go), which never calls fleet.FleetBundleEngine.Apply, so the
	// reconciler alone cannot produce a Fleet Bundle. For this block we
	// hot-swap the suite reconciler's Deployer field to a production
	// workload.NewDeployer wired against fleet.NewBundleEngine + a direct
	// envtest client. Serial (above) prevents any other Describe block from
	// running concurrently while the swap is in effect; the AfterEach
	// restores the original FakeDeployer so downstream specs see the
	// suite-wide default.
	//
	// Asserts:
	//
	//   1. After creating a Workload CR, the suite-driven reconciler runs the
	//      production deployer and a fleet.cattle.io/v1alpha1 Bundle named
	//      {ns}-{workloadID} appears in the apiserver. This is the
	//      "reconciler creates Fleet Bundle" pillar.
	//
	//   2. After we patch Bundle.Status with a healthy "Ready=True"
	//      condition, the Workload.status.Phase does NOT flip to Failed.
	//      This is the "healthy status doesn't flip Failed" pillar. We cannot
	//      assert Eventually==Running here because pkg/fleet/status.go's
	//      mirrorStatus does not (yet) parse per-cluster FleetState from
	//      Bundle.Status — every per-cluster FleetState is "", which
	//      MapFleetStateToPhase translates to ClusterDeploying / PhaseDeploying.
	//      The plan's caveat (docs/superpowers/plans/2026-05-21-p4-3b-fleet-bundle-engine.md:3072)
	//      acknowledges this — the Modified→Running unit guard lives in
	//      pkg/workload/fleet_phase_test.go (Task 1).
	var savedDeployer workload.Deployer
	var fakeGitRepoEngine *fleet.FakeGitRepoEngine

	BeforeEach(func() {
		// Drain any Workloads left by prior Describe blocks before swapping
		// workloadReconciler.Deployer. Prior blocks delete their Workloads
		// fire-and-forget without waiting for completion (or skip Delete
		// entirely on negative-path specs), so the controller may still be
		// processing finalizer-removal Reconciles (which read r.Deployer
		// via handleDeletion) at the moment this BeforeEach runs. Mutating
		// r.Deployer concurrently with that read trips `go test -race` even
		// though the swap is logically harmless. Draining + waiting for zero
		// Workloads guarantees the controller's queue is idle for live
		// objects by the time the swap happens.
		var leftover aifv1.WorkloadList
		Expect(k8sClient.List(ctx, &leftover)).To(Succeed())
		for i := range leftover.Items {
			_ = k8sClient.Delete(ctx, &leftover.Items[i])
		}
		Eventually(func() int {
			var list aifv1.WorkloadList
			if err := k8sClient.List(ctx, &list); err != nil {
				return -1
			}
			return len(list.Items)
		}, 30*time.Second, 250*time.Millisecond).Should(Equal(0))

		savedDeployer = workloadReconciler.Deployer

		// Direct envtest client (not the manager's cached client). The
		// production fleet.bundleEngine does Patch-then-Get; the cache lags
		// the apiserver after Create on first reconcile, and a direct
		// client side-steps that.
		directClient, err := client.New(testEnv.Config, client.Options{Scheme: scheme.Scheme})
		Expect(err).NotTo(HaveOccurred())
		nvDisc, _ := nvidia.NewDiscovery(slog.Default())
		fakeGitRepoEngine = &fleet.FakeGitRepoEngine{}
		workloadReconciler.Deployer = workload.NewDeployer(
			slog.Default(),
			helm.NewFake(),
			fleet.NewBundleEngine(slog.Default(), directClient),
			fakeGitRepoEngine,
			blueprint.NewFakeRepository(),
			nvDisc,
			nvidia.NewDeployer(slog.Default()),
		)
	})

	AfterEach(func() {
		// Same drain as BeforeEach — Serial guarantees no other spec runs
		// concurrently, but reconciler goroutines from this spec's own
		// Workloads may still be in flight after the spec body returns.
		// Restoring the Deployer field while a Reconcile is reading it
		// trips `go test -race`. Delete anything left behind and wait for
		// zero Workloads cluster-wide before restoring the field.
		var leftover aifv1.WorkloadList
		Expect(k8sClient.List(ctx, &leftover)).To(Succeed())
		for i := range leftover.Items {
			_ = k8sClient.Delete(ctx, &leftover.Items[i])
		}
		Eventually(func() int {
			var list aifv1.WorkloadList
			if err := k8sClient.List(ctx, &list); err != nil {
				return -1
			}
			return len(list.Items)
		}, 30*time.Second, 250*time.Millisecond).Should(Equal(0))
		workloadReconciler.Deployer = savedDeployer
	})

	It("creates a Fleet Bundle and doesn't flip the Workload to Failed on healthy Bundle status", func() {
		// Unique namespace per spec; created here because no envtest helper
		// exists and the suite-wide "default" namespace is shared.
		nsName := "wl-fleet-" + randomSuffix()
		Expect(k8sClient.Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: nsName},
		})).To(Succeed())

		const wlName = "demo"
		w := &aifv1.Workload{
			ObjectMeta: metav1.ObjectMeta{Namespace: nsName, Name: wlName},
			Spec: aifv1.WorkloadSpec{
				Name:           "llama",
				DeployStrategy: aifv1.DeployStrategyTypeHelm,
				TargetClusters: []string{"prod-east"},
				Source: aifv1.WorkloadSource{
					Kind: aifv1.WorkloadSourceKindApp,
					App: &aifv1.AppRef{
						Repo:    "registry.example.test/charts",
						Chart:   "llama",
						Version: "1.0.0",
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, w)).To(Succeed())

		// Pillar 1: the suite reconciler (now driving the production
		// deployer chain) creates a Fleet Bundle for the Workload.
		// Bundle name is {ns}-{workloadID} (lowercased + DNS-1123
		// sanitized; see pkg/fleet/cr_builder.go.fleetBundleName).
		bundleKey := client.ObjectKey{Namespace: nsName, Name: nsName + "-" + wlName}
		Eventually(func() error {
			var b fleetv1.Bundle
			return k8sClient.Get(ctx, bundleKey, &b)
		}, timeout, interval).Should(Succeed(),
			"reconciler should drive production deployer to create Fleet Bundle")

		// Inject a healthy-looking Bundle status.
		// fleetv1.GenericCondition aliases wrangler's genericcondition.GenericCondition,
		// whose Status field is corev1.ConditionStatus (NOT metav1.ConditionStatus).
		// The Owns(&fleetv1.Bundle{}) watch on the reconciler should fire a
		// re-reconcile when this Update lands.
		Eventually(func() error {
			var bndl fleetv1.Bundle
			if err := k8sClient.Get(ctx, bundleKey, &bndl); err != nil {
				return err
			}
			bndl.Status = fleetv1.BundleStatus{
				Conditions: []genericcondition.GenericCondition{
					{Type: "Ready", Status: corev1.ConditionTrue},
				},
			}
			return k8sClient.Status().Update(ctx, &bndl)
		}, timeout, interval).Should(Succeed())

		// Pillar 2: the Owns(Bundle) re-reconcile triggered by the status
		// patch must NOT flip the Workload to Failed. mirrorStatus reports
		// per-cluster FleetState="" today (richer parsing is deferred per
		// the plan caveat), which MapFleetStateToPhase maps to
		// ClusterDeploying → PhaseDeploying — non-Failed and non-empty.
		Consistently(func() string {
			var got aifv1.Workload
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(w), &got); err != nil {
				return ""
			}
			return string(got.Status.Phase)
		}, "3s", interval).ShouldNot(Equal(string(aifv1.WorkloadPhaseFailed)))

		// Cleanup: remove the Workload. The production deployer's Teardown
		// calls FleetBundleEngine.Teardown, which deletes the Bundle.
		Expect(k8sClient.Delete(ctx, w)).To(Succeed())
		Eventually(func() bool {
			var got aifv1.Workload
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(w), &got)
			return errors.IsNotFound(err)
		}, timeout, interval).Should(BeTrue(), "Workload should be fully deleted")
	})

	It("dispatches to FleetGitRepoEngine when deployStrategy=gitops", func() {
		nsName := "wl-gitops-" + randomSuffix()
		Expect(k8sClient.Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: nsName},
		})).To(Succeed())

		const wlName = "demo-gitops"
		w := &aifv1.Workload{
			ObjectMeta: metav1.ObjectMeta{Namespace: nsName, Name: wlName},
			Spec: aifv1.WorkloadSpec{
				Name:           "llama",
				DeployStrategy: aifv1.DeployStrategyTypeGitOps,
				TargetClusters: []string{"prod-east"},
				Source: aifv1.WorkloadSource{
					Kind: aifv1.WorkloadSourceKindApp,
					App: &aifv1.AppRef{
						Repo:    "registry.example.test/charts",
						Chart:   "llama",
						Version: "1.0.0",
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, w)).To(Succeed())

		// The deployer must dispatch to FleetGitRepoEngine.Apply, not
		// FleetBundleEngine.Apply, when deployStrategy=gitops.
		// AppliedSnapshot takes the fake's mutex; raw .Applied access from
		// the test goroutine races the reconciler worker goroutine.
		Eventually(func() int {
			return len(fakeGitRepoEngine.AppliedSnapshot())
		}, timeout, interval).Should(BeNumerically(">=", 1),
			"reconciler should drive deployer to call FleetGitRepoEngine.Apply")

		applied := fakeGitRepoEngine.AppliedSnapshot()[0]
		Expect(applied.WorkloadID).To(Equal(wlName))
		Expect(applied.WorkloadNS).To(Equal(nsName))
		Expect(applied.TargetClusters).To(ConsistOf("prod-east"))

		// And no Fleet Bundle should have been created for this Workload.
		var b fleetv1.Bundle
		bundleKey := client.ObjectKey{Namespace: nsName, Name: nsName + "-" + wlName}
		Consistently(func() bool {
			err := k8sClient.Get(ctx, bundleKey, &b)
			return errors.IsNotFound(err)
		}, "2s", interval).Should(BeTrue(),
			"gitops dispatch must NOT create a Fleet Bundle")

		Expect(k8sClient.Delete(ctx, w)).To(Succeed())

		// The finalizer must drive FleetGitRepoEngine.Teardown before the
		// Workload disappears — guards against a regression where the
		// gitops dispatch wires Apply but skips Teardown (deployer.go
		// calls both engines unconditionally; this test catches drift).
		Eventually(func() int {
			return len(fakeGitRepoEngine.TearDownSnapshot())
		}, timeout, interval).Should(BeNumerically(">=", 1),
			"reconciler should drive deployer to call FleetGitRepoEngine.Teardown on delete")

		Eventually(func() bool {
			var got aifv1.Workload
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(w), &got)
			return errors.IsNotFound(err)
		}, timeout, interval).Should(BeTrue(), "Workload should be fully deleted")
	})
})
