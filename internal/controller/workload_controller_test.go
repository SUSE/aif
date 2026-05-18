package controller_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	stderrors "errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

		fakeDeployer.DeployResult = workload.DeployResult{Phase: workload.PhaseRunning}
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

		fakeDeployer.DeployResult = workload.DeployResult{Phase: workload.PhaseRunning}
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

		fakeDeployer.DeployResult = workload.DeployResult{Phase: workload.PhaseRunning}
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
		fakeDeployer.DeployErr = workload.ErrNestedBlueprintNotSupported
		fakeDeployer.DeployResult = workload.DeployResult{Phase: workload.PhaseFailed}

		Eventually(eventuallyReason, timeout, interval).Should(Equal(conditions.ReasonUnsupportedComposition))
	})

	It("maps ErrSourceNotResolved to SourceNotResolved", func() {
		fakeDeployer.DeployErr = workload.ErrSourceNotResolved
		fakeDeployer.DeployResult = workload.DeployResult{Phase: workload.PhasePending}

		Eventually(eventuallyReason, timeout, interval).Should(Equal(conditions.ReasonSourceNotResolved))
	})

	It("maps ErrComponentInstallFailed to ComponentInstallFailed", func() {
		fakeDeployer.DeployErr = workload.ErrComponentInstallFailed
		fakeDeployer.DeployResult = workload.DeployResult{Phase: workload.PhaseFailed}

		Eventually(eventuallyReason, timeout, interval).Should(Equal(conditions.ReasonComponentInstallFailed))
	})

	It("maps ErrComponentUninstallFailed to OrphanCleanupPending", func() {
		fakeDeployer.DeployErr = workload.ErrComponentUninstallFailed
		fakeDeployer.DeployResult = workload.DeployResult{Phase: workload.PhaseDeploying}

		Eventually(eventuallyReason, timeout, interval).Should(Equal(conditions.ReasonOrphanCleanupPending))
	})

	It("maps unclassified errors to ReconcileFailed", func() {
		fakeDeployer.DeployErr = stderrors.New("some unexpected error")
		fakeDeployer.DeployResult = workload.DeployResult{}

		Eventually(eventuallyReason, timeout, interval).Should(Equal(conditions.ReasonReconcileFailed))
	})

	It("sets Ready=True Reason=Installed when Deploy succeeds and Phase=Running", func() {
		fakeDeployer.DeployErr = nil
		fakeDeployer.DeployResult = workload.DeployResult{
			Phase:      workload.PhaseRunning,
			Components: []workload.ComponentRelease{{Name: "n", Status: "deployed"}},
		}

		Eventually(eventuallyReason, timeout, interval).Should(Equal(conditions.ReasonInstalled))
	})

	It("sets Reason=Installing when Deploy returns nil but Phase=Deploying", func() {
		fakeDeployer.DeployErr = nil
		fakeDeployer.DeployResult = workload.DeployResult{
			Phase:      workload.PhaseDeploying,
			Components: []workload.ComponentRelease{{Name: "n", Status: "pending-install"}},
		}

		Eventually(eventuallyReason, timeout, interval).Should(Equal(conditions.ReasonInstalling))
	})
})

var _ = Describe("Workload finalizer cleanup (P4-2)", func() {
	const timeout = 30 * time.Second
	const interval = 250 * time.Millisecond
	ctx := context.Background()

	It("calls Deployer.Teardown on delete and removes finalizer when Teardown succeeds", func() {
		fakeDeployer.TeardownErr = nil
		fakeDeployer.DeployResult = workload.DeployResult{
			Phase: workload.PhaseRunning,
			Components: []workload.ComponentRelease{
				{Name: "n", ReleaseName: "wid-fin-n", Status: "deployed"},
			},
		}

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

		teardownCallsBefore := len(fakeDeployer.TeardownCalls)
		Expect(k8sClient.Delete(ctx, w)).To(Succeed())

		// Teardown should be called eventually with the recorded release.
		Eventually(func() int {
			return len(fakeDeployer.TeardownCalls)
		}, timeout, interval).Should(BeNumerically(">", teardownCallsBefore))

		last := fakeDeployer.TeardownCalls[len(fakeDeployer.TeardownCalls)-1]
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
		fakeDeployer.DeployResult = workload.DeployResult{
			Phase: workload.PhaseRunning,
			Components: []workload.ComponentRelease{
				{Name: "n", ReleaseName: "wid-fin2-n", Status: "deployed"},
			},
		}

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
		fakeDeployer.TeardownErr = stderrors.New("teardown boom")
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
		fakeDeployer.TeardownErr = nil
		Eventually(func() bool {
			var got aifv1.Workload
			err := k8sClient.Get(ctx, key, &got)
			return errors.IsNotFound(err)
		}, timeout, interval).Should(BeTrue())
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
