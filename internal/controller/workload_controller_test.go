package controller_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	stderrors "errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/conditions"
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

		fakeDeployer.SetDeployResult(workload.DeployResult{Phase: workload.PhaseRunning})
		Expect(k8sClient.Create(ctx, w)).To(Succeed())

		Eventually(func(g Gomega) {
			var fetched aifv1.Workload
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(w), &fetched)).To(Succeed())
			g.Expect(fetched.Status.Phase).To(Equal(aifv1.WorkloadPhaseRunning))
			g.Expect(fetched.Status.ObservedGeneration).To(Equal(fetched.Generation))
			rc := findReady(fetched.Status.Conditions)
			g.Expect(rc).NotTo(BeNil())
			g.Expect(rc.Status).To(Equal(metav1.ConditionTrue))
			g.Expect(rc.Reason).To(Equal(conditions.ReasonInstalled))
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

		fakeDeployer.SetDeployResult(workload.DeployResult{Phase: workload.PhaseRunning})
		Expect(k8sClient.Create(ctx, w)).To(Succeed())

		Eventually(func(g Gomega) {
			var fetched aifv1.Workload
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(w), &fetched)).To(Succeed())
			g.Expect(fetched.Status.Phase).To(Equal(aifv1.WorkloadPhaseRunning))
			rc := findReady(fetched.Status.Conditions)
			g.Expect(rc).NotTo(BeNil())
			g.Expect(rc.Status).To(Equal(metav1.ConditionTrue))
			g.Expect(rc.Reason).To(Equal(conditions.ReasonInstalled))
		}, timeout, interval).Should(Succeed())
	})

	It("should reconcile a valid BundleTest Workload to Pending/Installed", func() {
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

		fakeDeployer.SetDeployResult(workload.DeployResult{Phase: workload.PhaseRunning})
		Expect(k8sClient.Create(ctx, w)).To(Succeed())

		Eventually(func(g Gomega) {
			var fetched aifv1.Workload
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(w), &fetched)).To(Succeed())
			g.Expect(fetched.Status.Phase).To(Equal(aifv1.WorkloadPhaseRunning))
			rc := findReady(fetched.Status.Conditions)
			g.Expect(rc).NotTo(BeNil())
			g.Expect(rc.Status).To(Equal(metav1.ConditionTrue))
			g.Expect(rc.Reason).To(Equal(conditions.ReasonInstalled))
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
		fakeDeployer.SetDeployResult(workload.DeployResult{Phase: workload.PhaseFailed})

		Eventually(eventuallyReason, timeout, interval).Should(Equal(conditions.ReasonUnsupportedComposition))
	})

	It("maps ErrSourceNotResolved to SourceNotResolved", func() {
		fakeDeployer.SetDeployErr(workload.ErrSourceNotResolved)
		fakeDeployer.SetDeployResult(workload.DeployResult{Phase: workload.PhasePending})

		Eventually(eventuallyReason, timeout, interval).Should(Equal(conditions.ReasonSourceNotResolved))
	})

	It("maps ErrComponentInstallFailed to ComponentInstallFailed", func() {
		fakeDeployer.SetDeployErr(workload.ErrComponentInstallFailed)
		fakeDeployer.SetDeployResult(workload.DeployResult{Phase: workload.PhaseFailed})

		Eventually(eventuallyReason, timeout, interval).Should(Equal(conditions.ReasonComponentInstallFailed))
	})

	It("maps ErrComponentUninstallFailed to OrphanCleanupPending", func() {
		fakeDeployer.SetDeployErr(workload.ErrComponentUninstallFailed)
		fakeDeployer.SetDeployResult(workload.DeployResult{Phase: workload.PhaseDeploying})

		Eventually(eventuallyReason, timeout, interval).Should(Equal(conditions.ReasonOrphanCleanupPending))
	})

	It("maps unclassified errors to ReconcileFailed", func() {
		fakeDeployer.SetDeployErr(stderrors.New("some unexpected error"))
		fakeDeployer.SetDeployResult(workload.DeployResult{})

		Eventually(eventuallyReason, timeout, interval).Should(Equal(conditions.ReasonReconcileFailed))
	})

	It("sets Ready=True Reason=Installed when Deploy succeeds and Phase=Running", func() {
		fakeDeployer.SetDeployErr(nil)
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Phase:      workload.PhaseRunning,
			Components: []workload.ComponentRelease{{Name: "n", Status: "deployed"}},
		})

		Eventually(eventuallyReason, timeout, interval).Should(Equal(conditions.ReasonInstalled))
	})

	It("sets Reason=Installing when Deploy returns nil but Phase=Deploying", func() {
		fakeDeployer.SetDeployErr(nil)
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Phase:      workload.PhaseDeploying,
			Components: []workload.ComponentRelease{{Name: "n", Status: "pending-install"}},
		})

		Eventually(eventuallyReason, timeout, interval).Should(Equal(conditions.ReasonInstalling))
	})
})

var _ = Describe("Workload finalizer cleanup (P4-2)", func() {
	const timeout = 30 * time.Second
	const interval = 250 * time.Millisecond
	ctx := context.Background()

	It("calls Deployer.Teardown on delete and removes finalizer when Teardown succeeds", func() {
		fakeDeployer.SetTeardownErr(nil)
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Phase: workload.PhaseRunning,
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
			Phase: workload.PhaseRunning,
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
		// First reconcile: Deploying.
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Phase:      workload.PhaseDeploying,
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

		// Switch fake to Running.
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Phase:      workload.PhaseRunning,
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

	It("emits BundleTestGenerationDrift when observed != recorded", func() {
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Phase:                    workload.PhaseRunning,
			ObservedBundleGeneration: 9, // recorded was 5
			Components:               []workload.ComponentRelease{{Name: "c", Status: "deployed"}},
		})

		name := "wid-drift-" + randomSuffix()
		w := &aifv1.Workload{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: aifv1.WorkloadSpec{
				Name: "c",
				Source: aifv1.WorkloadSource{
					Kind:       aifv1.WorkloadSourceKindBundleTest,
					BundleTest: &aifv1.BundleTestRef{Namespace: "default", Name: "b1", Generation: 5},
				},
			},
		}
		Expect(k8sClient.Create(ctx, w)).To(Succeed())
		key := client.ObjectKeyFromObject(w)

		Eventually(func() []string {
			var got aifv1.Workload
			_ = k8sClient.Get(ctx, key, &got)
			return eventReasons(&got)
		}, timeout, interval).Should(ContainElement("BundleTestGenerationDrift"))

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
			Phase: workload.PhaseRunning,
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
			Phase: workload.PhaseRunning,
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
			Phase: workload.PhaseRunning,
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
			Phase: workload.PhaseRunning,
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
		fakeDeployer.SetDeployResult(workload.DeployResult{Phase: workload.PhaseFailed})

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
		fakeDeployer.SetDeployResult(workload.DeployResult{Phase: workload.PhasePending})

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

		// Source becomes available — fake transitions to Running.
		fakeDeployer.SetDeployErr(nil)
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Phase: workload.PhaseRunning,
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
