package controller_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/workload"
)

// Lifecycle envtest for the controller-owned phase machine (P5-1).
//
// Walks the full state space (AutomaticRecovery.Enabled=true): Pending →
// Deploying → Running → Degraded (counter increments on entry) → Running
// (counter resets on entry) → Degraded (counter increments again) →
// RecoveryInProgress (after counter reaches threshold; ARCHITECTURE.md
// §4.4 rule 2 branch B). P5-6 will own the RecoveryInProgress → Failed
// transition; the spec-change-from-Failed counter reset is covered at the
// unit level by
// TestComputePhaseWithTransitions_SpecChangeFromFailedResets in
// workload_phase_helpers_test.go (cannot be exercised here because the
// controller will keep overwriting any seed back to RecoveryInProgress
// while the failed component + at-threshold counter persist).
//
// A second spec covers the AutomaticRecovery.Enabled=false branch (rule 2
// branch C): a failed component routes to PhaseFailed immediately,
// bypassing the Degraded → RecoveryInProgress ladder.
//
// The threshold-cross step uses a Status().Update to inject
// RecoveryFailureCount = threshold-1. In production, P5-2's
// ProgressDeadlineExceeded Event watch will bump the counter outside
// the deploy path; P5-1's controller alone resets the counter on every
// Running entry, so the only way to reach the threshold from the
// controller path is to pre-seed the count (the unit-test pattern in
// TestComputePhaseWithTransitions_ThresholdPromotesRecoveryInProgress
// uses the same "counter at threshold-1, then enter Degraded" setup).
var _ = Describe("Workload phase lifecycle (P5-1)", func() {
	const (
		ns       = "default"
		timeout  = 10 * time.Second
		interval = 100 * time.Millisecond
	)

	ctx := context.Background()

	// getPhase / getCounter / touchAnnotation / setCounter are spec-local
	// helpers that close over ctx.
	getPhase := func(key client.ObjectKey) func() aifv1.WorkloadPhase {
		return func() aifv1.WorkloadPhase {
			var got aifv1.Workload
			if err := k8sClient.Get(ctx, key, &got); err != nil {
				return ""
			}
			return got.Status.Phase
		}
	}

	getCounter := func(key client.ObjectKey) func() int32 {
		return func() int32 {
			var got aifv1.Workload
			if err := k8sClient.Get(ctx, key, &got); err != nil {
				return -1
			}
			return got.Status.RecoveryFailureCount
		}
	}

	// touchAnnotation bumps a benign annotation so the controller sees
	// a generation-independent Update event and reconciles again — this
	// is how we synchronously drive the reconciler from the test thread.
	touchAnnotation := func(key client.ObjectKey) {
		Eventually(func(g Gomega) {
			var got aifv1.Workload
			g.Expect(k8sClient.Get(ctx, key, &got)).To(Succeed())
			if got.Annotations == nil {
				got.Annotations = map[string]string{}
			}
			got.Annotations["touch"] = randomSuffix()
			g.Expect(k8sClient.Update(ctx, &got)).To(Succeed())
		}, timeout, interval).Should(Succeed())
	}

	// setCounter writes Status.RecoveryFailureCount directly via the
	// status subresource — used to simulate the P5-2 PDE-Event bump that
	// the controller does not perform on its own.
	setCounter := func(key client.ObjectKey, value int32) {
		Eventually(func(g Gomega) {
			var got aifv1.Workload
			g.Expect(k8sClient.Get(ctx, key, &got)).To(Succeed())
			got.Status.RecoveryFailureCount = value
			g.Expect(k8sClient.Status().Update(ctx, &got)).To(Succeed())
		}, timeout, interval).Should(Succeed())
	}

	It("walks Pending → Deploying → Running → Degraded → Running → Degraded → RecoveryInProgress", func() {
		name := "wid-lifecycle-" + randomSuffix()
		threshold := int32(3)

		// Start in Pending: empty deploy result, no components.
		fakeDeployer.SetDeployResult(workload.DeployResult{})

		w := &aifv1.Workload{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec: aifv1.WorkloadSpec{
				Name: "n",
				Source: aifv1.WorkloadSource{
					Kind: aifv1.WorkloadSourceKindApp,
					App:  &aifv1.AppRef{Repo: "r", Chart: "c", Version: "1"},
				},
				Strategy: &aifv1.DeploymentStrategy{
					AutomaticRecovery: &aifv1.AutomaticRecoveryStrategy{
						Enabled:          true,
						FailureThreshold: &threshold,
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, w)).To(Succeed())
		key := client.ObjectKeyFromObject(w)

		// 1. Pending: no components → rule 1.
		Eventually(getPhase(key), timeout, interval).Should(Equal(aifv1.WorkloadPhasePending))
		Expect(getCounter(key)()).To(Equal(int32(0)))

		// 2. Deploying: switch fake to return pending-install → rule 2.
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Components: []workload.ComponentRelease{
				{Name: "n", ReleaseName: name + "-n", Status: "pending-install"},
			},
		})
		touchAnnotation(key)
		Eventually(getPhase(key), timeout, interval).Should(Equal(aifv1.WorkloadPhaseDeploying))
		// Phase + counter are stable across repeated reconciles.
		Consistently(getPhase(key), 1*time.Second, interval).Should(Equal(aifv1.WorkloadPhaseDeploying))
		Expect(getCounter(key)()).To(Equal(int32(0)))

		// 3. Running: switch fake to deployed → rule 4.
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Components: []workload.ComponentRelease{
				{Name: "n", ReleaseName: name + "-n", Status: "deployed"},
			},
		})
		touchAnnotation(key)
		Eventually(getPhase(key), timeout, interval).Should(Equal(aifv1.WorkloadPhaseRunning))
		Expect(getCounter(key)()).To(Equal(int32(0)))

		// 4. Degraded #1: one failed component, Running→Degraded entry → counter 0→1.
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Components: []workload.ComponentRelease{
				{Name: "n", ReleaseName: name + "-n", Status: "failed"},
			},
		})
		touchAnnotation(key)
		Eventually(getPhase(key), timeout, interval).Should(Equal(aifv1.WorkloadPhaseDegraded))
		Eventually(getCounter(key), timeout, interval).Should(Equal(int32(1)))

		// 5. Running #2: recover, Degraded→Running entry → counter resets 1→0.
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Components: []workload.ComponentRelease{
				{Name: "n", ReleaseName: name + "-n", Status: "deployed"},
			},
		})
		touchAnnotation(key)
		Eventually(getPhase(key), timeout, interval).Should(Equal(aifv1.WorkloadPhaseRunning))
		Eventually(getCounter(key), timeout, interval).Should(Equal(int32(0)))

		// 6. Degraded #2: counter 0→1 on Running→Degraded entry.
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Components: []workload.ComponentRelease{
				{Name: "n", ReleaseName: name + "-n", Status: "failed"},
			},
		})
		touchAnnotation(key)
		Eventually(getPhase(key), timeout, interval).Should(Equal(aifv1.WorkloadPhaseDegraded))
		Eventually(getCounter(key), timeout, interval).Should(Equal(int32(1)))
		// Subsequent reconciles while staying in Degraded MUST NOT increment.
		touchAnnotation(key)
		Consistently(getCounter(key), 1*time.Second, interval).Should(Equal(int32(1)))

		// 7. Pre-seed counter to threshold-1 to simulate P5-2's PDE-event bumps.
		//    The controller alone resets on Running entry, so without external
		//    bumps the counter can never climb past 1 via the deploy path.
		//    Move out of Degraded first so the next failed touch is a real
		//    Running→Degraded entry (drives the increment + threshold check).
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Components: []workload.ComponentRelease{
				{Name: "n", ReleaseName: name + "-n", Status: "deployed"},
			},
		})
		touchAnnotation(key)
		Eventually(getPhase(key), timeout, interval).Should(Equal(aifv1.WorkloadPhaseRunning))
		Eventually(getCounter(key), timeout, interval).Should(Equal(int32(0)))

		setCounter(key, threshold-1)
		Eventually(getCounter(key), timeout, interval).Should(Equal(threshold - 1))

		// 8. RecoveryInProgress: failed components + counter==threshold-1
		//    + AutomaticRecovery.Enabled=true → on Running→Degraded entry the
		//    increment lifts the counter to threshold, the double-recompute
		//    in computePhaseWithTransitions promotes Degraded →
		//    RecoveryInProgress in the same pass (rule 2 branch B).
		//    RecoveryInProgress is terminal in P5-1 — P5-6 owns the rollback
		//    exit; we just verify entry here.
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Components: []workload.ComponentRelease{
				{Name: "n", ReleaseName: name + "-n", Status: "failed"},
			},
		})
		touchAnnotation(key)
		Eventually(getPhase(key), timeout, interval).Should(Equal(aifv1.WorkloadPhaseRecoveryInProgress))
		Eventually(getCounter(key), timeout, interval).Should(Equal(threshold))
		// RecoveryInProgress is stable until P5-6 lands or the spec changes.
		Consistently(getPhase(key), 1*time.Second, interval).Should(Equal(aifv1.WorkloadPhaseRecoveryInProgress))
		Consistently(getCounter(key), 1*time.Second, interval).Should(Equal(threshold))

		// Cleanup.
		Expect(k8sClient.Delete(ctx, w)).To(Succeed())
	})

	It("recovery=off: failed component routes to Failed immediately (no Degraded intermediate)", func() {
		// ARCHITECTURE.md §4.4 rule 2 branch C: when
		// AutomaticRecovery.Enabled=false (or the Strategy is absent), the
		// first failed component MUST surface as PhaseFailed without
		// passing through Degraded, and the counter MUST stay at 0 since
		// the candidate is never Degraded.
		name := "wid-fail-immediate-" + randomSuffix()

		// Start with a healthy deploy so we land in Running first; this
		// makes the Running → Failed transition observable (without going
		// through Degraded) and asserts the counter stayed at 0.
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Components: []workload.ComponentRelease{
				{Name: "n", ReleaseName: name + "-n", Status: "deployed"},
			},
		})

		w := &aifv1.Workload{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec: aifv1.WorkloadSpec{
				Name: "n",
				Source: aifv1.WorkloadSource{
					Kind: aifv1.WorkloadSourceKindApp,
					App:  &aifv1.AppRef{Repo: "r", Chart: "c", Version: "1"},
				},
				// No Strategy → AutomaticRecovery.Enabled defaults to false.
			},
		}
		Expect(k8sClient.Create(ctx, w)).To(Succeed())
		key := client.ObjectKeyFromObject(w)

		Eventually(getPhase(key), timeout, interval).Should(Equal(aifv1.WorkloadPhaseRunning))
		Expect(getCounter(key)()).To(Equal(int32(0)))

		// Flip to a failed component; with recovery off this MUST go
		// straight to Failed (skipping Degraded entirely) and the counter
		// MUST remain at 0.
		fakeDeployer.SetDeployResult(workload.DeployResult{
			Components: []workload.ComponentRelease{
				{Name: "n", ReleaseName: name + "-n", Status: "failed"},
			},
		})
		touchAnnotation(key)
		Eventually(getPhase(key), timeout, interval).Should(Equal(aifv1.WorkloadPhaseFailed))
		// Counter never bumped: branch C bypasses the Degraded-entry side
		// effect entirely.
		Consistently(getCounter(key), 1*time.Second, interval).Should(Equal(int32(0)))
		// Failed is terminal until spec change.
		Consistently(getPhase(key), 1*time.Second, interval).Should(Equal(aifv1.WorkloadPhaseFailed))

		// Cleanup.
		Expect(k8sClient.Delete(ctx, w)).To(Succeed())
	})
})
